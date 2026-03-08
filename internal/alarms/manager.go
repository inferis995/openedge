package alarms

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// MQTT publisher interface expected by AlarmManager
type MQTTPublisher interface {
	PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error
}

// ActiveAlarmTracks state of an alarm rule that is currently active or in delay
type activeAlarmTrack struct {
	Definition   models.AlarmDefinition
	ActiveSince  time.Time
	Triggered    bool // True if it has passed the delay phase and fired via MQTT
	InitialValue float64
}

type Manager struct {
	db           *sql.DB
	mqttClient   MQTTPublisher
	gatewayID    int
	orgID        int
	siteName     string
	areaName     string
	gatewayName  string
	definitions  map[int][]models.AlarmDefinition  // tag_id -> definitions
	activeTracks map[int]map[int]*activeAlarmTrack // tag_id -> definition_id -> track
	mu           sync.RWMutex

	// OnAlarmEvent is called whenever an alarm state changes (e.g. triggered or cleared)
	// This allows the parent driver to publish the alarm to the cloud (e.g. via Sparkplug B)
	OnAlarmEvent func(tagID int, alias string, def models.AlarmDefinition, val float64, status string)
}

func NewManager(db *sql.DB, mqttClient MQTTPublisher, gatewayID int) *Manager {
	m := &Manager{
		db:           db,
		mqttClient:   mqttClient,
		gatewayID:    gatewayID,
		definitions:  make(map[int][]models.AlarmDefinition),
		activeTracks: make(map[int]map[int]*activeAlarmTrack),
	}
	m.loadGatewayContext()
	m.LoadDefinitions()

	return m
}

// loadGatewayContext fetches routing info for MQTT topics
func (m *Manager) loadGatewayContext() {
	if m.db == nil {
		return
	}
	query := `
		SELECT o.id, s.name, a.name, g.name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`
	err := m.db.QueryRow(query, m.gatewayID).Scan(&m.orgID, &m.siteName, &m.areaName, &m.gatewayName)
	if err != nil {
		log.Printf("[ALARM-MANAGER] Error loading gateway context for %d: %v", m.gatewayID, err)
	}
}

// LoadDefinitions reads all enabled alarm definitions for tags in this gateway
func (m *Manager) LoadDefinitions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.db == nil {
		return
	}

	rows, err := m.db.Query(`
		SELECT a.id, a.tag_id, a.alarm_type, a.threshold, a.deadband, a.delay_seconds, a.severity, a.message, a.enabled
		FROM alarm_definitions a
		JOIN tags t ON a.tag_id = t.id
		WHERE t.gateway_id = $1 AND a.enabled = true AND t.enabled = true
	`, m.gatewayID)

	if err != nil {
		log.Printf("[ALARM-MANAGER] Error loading definitions: %v", err)
		return
	}
	defer rows.Close()

	newDefs := make(map[int][]models.AlarmDefinition)
	count := 0
	for rows.Next() {
		var d models.AlarmDefinition
		err := rows.Scan(
			&d.ID, &d.TagID, &d.AlarmType, &d.Threshold, &d.Deadband, &d.DelaySeconds,
			&d.Severity, &d.Message, &d.Enabled,
		)
		if err == nil {
			newDefs[d.TagID] = append(newDefs[d.TagID], d)
			count++
		}
	}

	m.definitions = newDefs

	// Clean up active tracks that no longer have definitions
	for tagID, tracks := range m.activeTracks {
		validDefs := newDefs[tagID]
		for defID := range tracks {
			found := false
			for _, vd := range validDefs {
				if vd.ID == defID {
					found = true
					break
				}
			}
			if !found {
				// We must fire CLEAR if the track was triggered but definition is gone
				if tracks[defID].Triggered {
					var alias string
					m.db.QueryRow("SELECT alias FROM tags WHERE id = $1", tagID).Scan(&alias)
					m.fireAlarmEvent(tagID, alias, tracks[defID].Definition, tracks[defID].InitialValue, "CLEARED")
				}
				delete(tracks, defID)
			}
		}
	}

	log.Printf("[ALARM-MANAGER] Loaded %d active alarm rules for %d tags", count, len(newDefs))
}

// StartTicker runs a background loop to constantly evaluate DelaySeconds for tracked alarms
// This ensures that delays trigger on time even if the Modbus value doesn't change/poll frequently
func (m *Manager) StartTicker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[ALARM-MANAGER] Ticker stopped")
			return
		case <-ticker.C:
			m.tickDelays()
		}
	}
}

// tickDelays safely evaluates all activeTracks to see if they reached their fire duration
func (m *Manager) tickDelays() {
	m.mu.Lock()

	now := time.Now()

	// Collect alarms to trigger so we don't hold the lock while querying the DB
	type pendingTrigger struct {
		tagID        int
		definition   models.AlarmDefinition
		initialValue float64
	}
	var toTrigger []pendingTrigger

	for tagID, tracks := range m.activeTracks {
		for _, track := range tracks {
			if !track.Triggered {
				durationSecs := int(now.Sub(track.ActiveSince).Seconds())
				if durationSecs >= track.Definition.DelaySeconds {
					toTrigger = append(toTrigger, pendingTrigger{
						tagID:        tagID,
						definition:   track.Definition,
						initialValue: track.InitialValue,
					})
					track.Triggered = true
				}
			}
		}
	}
	m.mu.Unlock() // Unlock early before DB call!

	// Now trigger the alarms
	for _, pt := range toTrigger {
		var alias string
		m.db.QueryRow("SELECT alias FROM tags WHERE id = $1", pt.tagID).Scan(&alias)
		m.fireAlarmEvent(pt.tagID, alias, pt.definition, pt.initialValue, "ACTIVE")
	}
}

// EvaluateTag checks a new tag value against all its alarm rules
func (m *Manager) EvaluateTag(tagID int, alias string, value interface{}, quality int) {
	if quality != 192 {
		return // Do not evaluate bad quality data
	}

	floatVal, ok := toFloat(value)
	if !ok {
		return // Unsupported data type for alarm logic
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	defs, exists := m.definitions[tagID]
	if !exists || len(defs) == 0 {
		return
	}

	if m.activeTracks[tagID] == nil {
		m.activeTracks[tagID] = make(map[int]*activeAlarmTrack)
	}
	tracks := m.activeTracks[tagID]

	now := time.Now()

	for _, def := range defs {
		isViolating := isConditionViolated(def, floatVal)
		track, isTracking := tracks[def.ID]

		if isViolating {
			if !isTracking {
				// Condition just met. Start tracking.
				tracks[def.ID] = &activeAlarmTrack{
					Definition:   def,
					ActiveSince:  now,
					Triggered:    false,
					InitialValue: floatVal,
				}
				track = tracks[def.ID]

				// Evaluate immediately if delay is 0
				if def.DelaySeconds == 0 {
					m.fireAlarmEvent(tagID, alias, def, floatVal, "ACTIVE")
					track.Triggered = true
				}
			}
			// Delay ticking is handled by StartTicker() background loop now
		} else {
			// Not violating. Check if we need to clear an active alarm.
			// Important: Use Deadband for clearing to prevent chattering!
			if isTracking {
				if isCleared(def, floatVal) {
					if track.Triggered {
						// It was fired, so we must publish a CLEAR event
						m.fireAlarmEvent(tagID, alias, def, floatVal, "CLEARED")
					}
					// Remove from tracking
					delete(tracks, def.ID)
				}
			}
		}
	}
}

func (m *Manager) fireAlarmEvent(tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
	topic := fmt.Sprintf("sys/alarms/%d/%s/%s/%s/%s", m.orgID, m.siteName, m.areaName, m.gatewayName, alias)

	payload := map[string]interface{}{
		"tag_id":           tagID,
		"definition_id":    def.ID,
		"status":           status,
		"alarm_type":       def.AlarmType,
		"severity":         def.Severity,
		"message":          def.Message,
		"value_at_trigger": val,
		"timestamp":        time.Now().UnixMilli(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ALARM-MANAGER] Failed to marshal alarm payload: %v", err)
		return
	}

	if m.mqttClient != nil {
		err := m.mqttClient.PublishWithQoS(topic, string(payloadBytes), 1, false)
		if err != nil {
			log.Printf("[ALARM-MANAGER] Failed to publish %s event for tag %d: %v", status, tagID, err)
		} else {
			log.Printf("[ALARM] %s -> Tag %d (%s) - Rule %d", status, tagID, def.AlarmType, def.ID)
		}
	}

	// Notify parent driver if callback is set
	if m.OnAlarmEvent != nil {
		m.OnAlarmEvent(tagID, alias, def, val, status)
	}
}

// Helpers

func isConditionViolated(def models.AlarmDefinition, val float64) bool {
	switch def.AlarmType {
	case "bool_true":
		return val != 0
	case "bool_false":
		return val == 0
	case "high", "high_high":
		return def.Threshold != nil && val > *def.Threshold
	case "low", "low_low":
		return def.Threshold != nil && val < *def.Threshold
	}
	return false
}

func isCleared(def models.AlarmDefinition, val float64) bool {
	switch def.AlarmType {
	case "bool_true":
		return val == 0
	case "bool_false":
		return val != 0
	case "high", "high_high":
		// Clear only when it drops below threshold minus deadband
		return def.Threshold != nil && val <= (*def.Threshold-def.Deadband)
	case "low", "low_low":
		// Clear only when it rises above threshold plus deadband
		return def.Threshold != nil && val >= (*def.Threshold+def.Deadband)
	}
	return true
}

func toFloat(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
