package alarms

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
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
	EventID      int  // Database alarm_events.id for CLEARED updates
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
	// This allows the parent driver to publish the alarm to the cloud (e.g. on sys/alarms).
	//
	// eventID is the alarm_events row this state change refers to: the row this
	// manager just INSERTed on ACTIVE, or the row tracked since the alarm fired on
	// CLEARED. The driver forwards it so core-api knows the row already exists and
	// skips its own INSERT/UPDATE — otherwise every alarm would be persisted twice.
	// It is 0 only when the DB write failed or no DB is attached (tests).
	OnAlarmEvent func(eventID int, tagID int, alias string, def models.AlarmDefinition, val float64, status string)
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
	m.loadActiveAlarmsFromDB()

	return m
}

// loadActiveAlarmsFromDB loads currently active alarms from database and restores tracking
// This ensures alarms are properly cleared even after driver restart
func (m *Manager) loadActiveAlarmsFromDB() {
	if m.db == nil {
		return
	}

	query := `
		SELECT ae.id, ae.tag_id, ae.definition_id, ae.value_at_trigger, ae.trigger_time
		FROM alarm_events ae
		JOIN tags t ON ae.tag_id = t.id
		WHERE t.gateway_id = $1 AND ae.status = 'ACTIVE'
	`
	rows, err := m.db.Query(query, m.gatewayID)
	if err != nil {
		log.Printf("[ALARM-MANAGER] Error loading active alarms from DB: %v", err)
		return
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for rows.Next() {
		var eventID int
		var tagID int
		var defID int
		var triggerValue float64
		var triggerTime time.Time

		if err := rows.Scan(&eventID, &tagID, &defID, &triggerValue, &triggerTime); err != nil {
			continue
		}

		// Find the definition for this alarm
		defs := m.definitions[tagID]
		for _, def := range defs {
			if def.ID == defID {
				// Restore tracking state with Triggered=true (already fired)
				if m.activeTracks[tagID] == nil {
					m.activeTracks[tagID] = make(map[int]*activeAlarmTrack)
				}
				m.activeTracks[tagID][defID] = &activeAlarmTrack{
					Definition:   def,
					ActiveSince:  triggerTime,
					Triggered:    true, // Already triggered (loaded from DB)
					InitialValue: triggerValue,
					EventID:      eventID, // Critical: load the event ID for proper clearing
				}
				count++
				log.Printf("[ALARM-MANAGER] Restored active alarm tracking: tagID=%d, defID=%d, eventID=%d", tagID, defID, eventID)
				break
			}
		}
	}

	log.Printf("[ALARM-MANAGER] Loaded %d active alarms from database", count)
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
	if m.db == nil {
		log.Printf("[ALARM-MANAGER] Cannot load definitions: db is nil")
		return
	}

	log.Printf("[ALARM-MANAGER] Loading definitions for gateway %d...", m.gatewayID)

	rows, err := m.db.Query(`
		SELECT a.id, a.tag_id, a.alarm_type, a.threshold, a.deadband, a.delay_seconds, a.severity, a.message, a.enabled
		FROM alarm_definitions a
		JOIN tags t ON a.tag_id = t.id
		WHERE t.gateway_id = $1 AND a.enabled = true
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

	m.mu.Lock()
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
					// Update the database record with CLEARED status BEFORE notifying the
					// driver: the callback publishes the event and whoever consumes it
					// (core-api → notifications) must never see a still-ACTIVE row.
					if tracks[defID].EventID > 0 {
						if err := m.updateAlarmEventAsCleared(tracks[defID].EventID); err != nil {
							log.Printf("[ALARM-MANAGER] Failed to update alarm event %d as CLEARED during cleanup: %v", tracks[defID].EventID, err)
						}
					}
					m.fireAlarmEvent(tagID, alias, tracks[defID].Definition, tracks[defID].InitialValue, "CLEARED", tracks[defID].EventID)
				}
				delete(tracks, defID)
			}
		}
	}
	m.mu.Unlock()

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
	defer m.mu.Unlock() // Keep lock for entire operation to prevent race conditions

	now := time.Now()

	// Collect alarms to trigger - we will do everything while holding the lock
	// to prevent race conditions where state could change between operations
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

	// Now trigger the alarms - still holding lock to ensure consistency
	// Note: DB operations are done while holding lock, which is acceptable
	// because tickDelays runs infrequently (once per second)
	for _, pt := range toTrigger {
		// Fire the alarm event - we already set Triggered=true in the collection phase
		// No need to check again since we're iterating over alarms we just collected
		var alias string
		m.db.QueryRow("SELECT alias FROM tags WHERE id = $1", pt.tagID).Scan(&alias)
		eventID := m.fireAlarmEvent(pt.tagID, alias, pt.definition, pt.initialValue, "ACTIVE", 0)

		// Store the event ID in the track for later CLEARED updates
		if tracks, ok := m.activeTracks[pt.tagID]; ok {
			if track, ok := tracks[pt.definition.ID]; ok {
				track.EventID = eventID
			}
		}
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

		log.Printf("[ALARM-MANAGER-DEBUG] tagID=%d, defID=%d, type=%s, val=%v, isViolating=%v, isTracking=%v, Triggered=%v",
			tagID, def.ID, def.AlarmType, floatVal, isViolating, isTracking, track != nil && track.Triggered)

		if isViolating {
			if !isTracking {
				// Condition just met. Start tracking.
				tracks[def.ID] = &activeAlarmTrack{
					Definition:   def,
					ActiveSince:  now,
					Triggered:    false,
					InitialValue: floatVal,
					EventID:      0,
				}
				track = tracks[def.ID]

				// Evaluate immediately if delay is 0
				if def.DelaySeconds == 0 {
					eventID := m.fireAlarmEvent(tagID, alias, def, floatVal, "ACTIVE", 0)
					track.Triggered = true
					track.EventID = eventID
				}
			}
			// Delay ticking is handled by StartTicker() background loop now
		} else {
			// Not violating. Check if we need to clear an active alarm.
			// Important: Use Deadband for clearing to prevent chattering!
			if isTracking {
				if isCleared(def, floatVal) {
					if track.Triggered {
						// It was fired, so we must update the database and publish a CLEAR
						// event. DB first: the callback publishes the event downstream and
						// consumers must never see a still-ACTIVE row for a cleared alarm.
						if track.EventID > 0 {
							if err := m.updateAlarmEventAsCleared(track.EventID); err != nil {
								log.Printf("[ALARM-MANAGER] Failed to update alarm event %d as CLEARED: %v", track.EventID, err)
							}
						}
						m.fireAlarmEvent(tagID, alias, def, floatVal, "CLEARED", track.EventID)
					}
					// Remove from tracking
					delete(tracks, def.ID)
				}
			}
		}
	}
}

// insertAlarmEvent creates a new alarm event record in the database
// Uses ON CONFLICT to prevent duplicate ACTIVE events for the same tag/definition
// Returns the ID of the created event (or existing event if duplicate) or error
func (m *Manager) insertAlarmEvent(tagID int, def models.AlarmDefinition, val float64, status string) (int, error) {
	if m.db == nil {
		return 0, fmt.Errorf("database connection is nil")
	}

	var eventID int
	// For ACTIVE status, use ON CONFLICT to handle duplicates gracefully
	// The unique index alarm_events_active_unique prevents duplicate ACTIVE events
	if status == "ACTIVE" {
		// Try to insert first
		err := m.db.QueryRow(`
			INSERT INTO alarm_events (tag_id, definition_id, status, alarm_type, severity, message, value_at_trigger, trigger_time)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id
		`, tagID, def.ID, status, def.AlarmType, def.Severity, def.Message, val, time.Now()).Scan(&eventID)

		// If it's a duplicate error, fetch the existing event ID and return it (no error)
		if err != nil && strings.Contains(err.Error(), "duplicate key") {
			// Fetch the existing active alarm event
			err = m.db.QueryRow(`
				SELECT id FROM alarm_events
				WHERE tag_id = $1 AND definition_id = $2 AND status = 'ACTIVE' AND clear_time IS NULL
				ORDER BY trigger_time DESC LIMIT 1
			`, tagID, def.ID).Scan(&eventID)
			if err != nil {
				// If we can't find the existing event, log and return the duplicate as a new error
				log.Printf("[ALARM-MANAGER] Duplicate detected but couldn't find existing event for tag %d: %v", tagID, err)
				// Return the original error which will be handled by the caller
				return 0, fmt.Errorf("duplicate key: %w", err)
			}
			// Return the existing event ID with no error - duplicate was handled successfully
			return eventID, nil
		}

		if err != nil {
			return 0, fmt.Errorf("failed to insert alarm event: %w", err)
		}
		return eventID, nil
	}

	// For other statuses (CLEARED), just do a normal insert
	err := m.db.QueryRow(`
		INSERT INTO alarm_events (tag_id, definition_id, status, alarm_type, severity, message, value_at_trigger, trigger_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, tagID, def.ID, status, def.AlarmType, def.Severity, def.Message, val, time.Now()).Scan(&eventID)

	if err != nil {
		return 0, fmt.Errorf("failed to insert alarm event: %w", err)
	}

	return eventID, nil
}

// updateAlarmEventAsCleared updates an existing alarm event with CLEARED status
func (m *Manager) updateAlarmEventAsCleared(eventID int) error {
	if m.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	_, err := m.db.Exec(`
		UPDATE alarm_events
		SET status = 'CLEARED', clear_time = $1
		WHERE id = $2
	`, time.Now(), eventID)

	if err != nil {
		return fmt.Errorf("failed to update alarm event: %w", err)
	}

	return nil
}

// fireAlarmEvent persists the state change and notifies the parent driver.
// existingEventID is the alarm_events row the caller already knows about: 0 on
// ACTIVE (the row is created here), and the tracked row id on CLEARED (created
// when the alarm fired, already updated by the caller). The returned id — also
// handed to OnAlarmEvent — identifies the row this event refers to, so the
// driver can put it on the wire and core-api will not persist it a second time.
func (m *Manager) fireAlarmEvent(tagID int, alias string, def models.AlarmDefinition, val float64, status string, existingEventID int) int {
	// NOTE: MQTT publishing is now handled ONLY by OnAlarmEvent callback in the driver
	// This function handles database persistence and triggers the callback

	// Write to database for history tracking
	eventID := existingEventID
	if status == "ACTIVE" {
		id, err := m.insertAlarmEvent(tagID, def, val, status)
		// Note: Duplicate errors are silently handled in insertAlarmEvent
		if err != nil && !strings.Contains(err.Error(), "duplicate key") {
			log.Printf("[ALARM-MANAGER] Failed to insert alarm event: %v", err)
		}
		if id > 0 {
			eventID = id
		}
	}
	// CLEARED: the row already exists (existingEventID) and is updated by the
	// caller, which owns the track holding its id.

	// Notify parent driver if callback is set
	if m.OnAlarmEvent != nil {
		m.OnAlarmEvent(eventID, tagID, alias, def, val, status)
	}

	return eventID
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
