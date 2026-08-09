package alarms

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

const (
	// alarmDBTimeout bounds every DB call on the alarm firing path. The pool is
	// opened with statement_timeout=30s, which is far too long for a loop that
	// has to scan every tag of a gateway once per poll cycle.
	alarmDBTimeout = 5 * time.Second

	// alarmInsertRetryInterval is the minimum spacing between attempts to
	// persist an alarm whose INSERT failed. tickDelays runs once per second;
	// without this floor an unreachable database would be hammered every tick.
	alarmInsertRetryInterval = 30 * time.Second
)

// MQTT publisher interface expected by AlarmManager
type MQTTPublisher interface {
	PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error
}

// eventRef carries the alarm_events row id of ONE alarm occurrence. The live
// track and every queued I/O action for that occurrence share the same pointer:
// the ACTIVE write fills the id in, the CLEARED write reads it back when it
// executes. That indirection is what lets a CLEARED transition be decided (and
// the track deleted) while the ACTIVE row is still being written — the clear
// still finds the right row instead of orphaning an ACTIVE event.
//
// Always read/written with Manager.mu held.
type eventRef struct{ id int }

// ActiveAlarmTracks state of an alarm rule that is currently active or in delay
type activeAlarmTrack struct {
	Definition   models.AlarmDefinition
	ActiveSince  time.Time
	Triggered    bool // True if it has passed the delay phase and fired via MQTT
	InitialValue float64
	FiredAt      time.Time // When Triggered became true — the alarm_events.trigger_time
	Event        *eventRef // Database alarm_events.id for CLEARED updates (id==0: not persisted yet)

	// NextInsertRetry throttles re-attempts of a failed ACTIVE insert. Zero
	// means "no write has been attempted yet".
	NextInsertRetry time.Time
}

// alarmIO is one durable side-effect plus notification that a state transition
// decided on. Transitions are computed under Manager.mu and appended to
// Manager.pendingIO; the DB writes and the OnAlarmEvent publish then happen in
// drainIO() with NO lock held, so a Postgres stall or a full MQTT publish queue
// can no longer freeze the scan of the remaining tags on the gateway.
type alarmIO struct {
	tagID       int
	alias       string
	lookupAlias bool // resolve alias from the DB (callers that do not have it)
	def         models.AlarmDefinition
	value       float64
	status      string // "ACTIVE" or "CLEARED"
	ref         *eventRef
	triggeredAt time.Time
	// notify is false for insert retries: the operator has already been told
	// about this alarm, only the durable record was missing.
	notify bool
}

type Manager struct {
	db           *sql.DB
	store        alarmStore // durable writes; nil when no DB is attached (tests)
	mqttClient   MQTTPublisher
	gatewayID    int
	orgID        int
	siteName     string
	areaName     string
	gatewayName  string
	definitions  map[int][]models.AlarmDefinition  // tag_id -> definitions
	activeTracks map[int]map[int]*activeAlarmTrack // tag_id -> definition_id -> track
	pendingIO    []alarmIO                         // ordered queue drained outside the lock
	ioDraining   bool                              // a goroutine is inside drainIO
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
	if db != nil {
		m.store = &sqlStore{db: db}
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
					FiredAt:      triggerTime,
					Event:        &eventRef{id: eventID}, // Critical: load the event ID for proper clearing
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
				// We must fire CLEAR if the track was triggered but definition is gone.
				// The alias lookup, the UPDATE and the callback all run in drainIO
				// once the lock is released — none of them belongs under m.mu.
				if tracks[defID].Triggered {
					m.queueIO(alarmIO{
						tagID:       tagID,
						lookupAlias: true,
						def:         tracks[defID].Definition,
						value:       tracks[defID].InitialValue,
						status:      "CLEARED",
						ref:         tracks[defID].Event,
						triggeredAt: tracks[defID].FiredAt,
						notify:      true,
					})
				}
				delete(tracks, defID)
			}
		}
	}
	m.mu.Unlock()

	m.drainIO()

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

// tickDelays evaluates all activeTracks to see if they reached their fire
// duration, and re-attempts the persistence of alarms whose INSERT failed.
//
// Only the state transitions happen under the lock: the writes and the MQTT
// publish are queued and executed by drainIO() afterwards, so a slow database
// can no longer stall the 1 s ticker (and, through it, every other alarm).
func (m *Manager) tickDelays() {
	m.mu.Lock()

	now := time.Now()

	for tagID, tracks := range m.activeTracks {
		for _, track := range tracks {
			switch {
			case !track.Triggered:
				durationSecs := int(now.Sub(track.ActiveSince).Seconds())
				if durationSecs >= track.Definition.DelaySeconds {
					track.Triggered = true
					track.FiredAt = now
					track.NextInsertRetry = now.Add(alarmInsertRetryInterval)
					m.queueIO(alarmIO{
						tagID:       tagID,
						lookupAlias: true,
						def:         track.Definition,
						value:       track.InitialValue,
						status:      "ACTIVE",
						ref:         track.Event,
						triggeredAt: now,
						notify:      true,
					})
				}

			case track.Event.id == 0 && !now.Before(track.NextInsertRetry):
				// The alarm was announced but its INSERT failed, so right now it
				// exists nowhere durable: a restart would lose it and no CLEARED
				// row could ever be written for it. Retry the write.
				//
				// notify=false — the operator was already told when the alarm
				// fired, and a retry must never produce a second notification.
				// NextInsertRetry is pushed forward here, under the lock, so a
				// dead database is retried at most every alarmInsertRetryInterval
				// instead of on every tick.
				track.NextInsertRetry = now.Add(alarmInsertRetryInterval)
				m.queueIO(alarmIO{
					tagID: tagID,
					// No alias lookup: nothing is published for a retry, so the
					// extra SELECT would be pure load on an already sick DB.
					def:         track.Definition,
					value:       track.InitialValue,
					status:      "ACTIVE",
					ref:         track.Event,
					triggeredAt: track.FiredAt,
					notify:      false,
				})
			}
		}
	}

	m.mu.Unlock()

	m.drainIO()
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

	defs, exists := m.definitions[tagID]
	if !exists || len(defs) == 0 {
		m.mu.Unlock()
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
					Event:        &eventRef{},
				}
				track = tracks[def.ID]

				// Evaluate immediately if delay is 0
				if def.DelaySeconds == 0 {
					track.Triggered = true
					track.FiredAt = now
					track.NextInsertRetry = now.Add(alarmInsertRetryInterval)
					m.queueIO(alarmIO{
						tagID:       tagID,
						alias:       alias,
						def:         def,
						value:       floatVal,
						status:      "ACTIVE",
						ref:         track.Event,
						triggeredAt: now,
						notify:      true,
					})
				}
			}
			// Delay ticking is handled by StartTicker() background loop now
		} else {
			// Not violating. Check if we need to clear an active alarm.
			// Important: Use Deadband for clearing to prevent chattering!
			if isTracking {
				// Still inside the delay window and the condition has already
				// stopped being violated: cancel the pending alarm.
				//
				// A delay exists precisely to require the condition to PERSIST,
				// but the clearing test uses the deadband, so a value between
				// (threshold - deadband) and threshold was neither "violating"
				// nor "cleared" and left the track in place — tickDelays then
				// raised a full alarm on a process that had been back in spec
				// for almost the whole delay. Hysteresis is only meaningful
				// once an alarm has actually been announced.
				if !track.Triggered {
					delete(tracks, def.ID)
					continue
				}
				if isCleared(def, floatVal) {
					if track.Triggered {
						// It was fired, so we must update the database and publish a
						// CLEAR event. Both happen in drainIO, DB first: the callback
						// publishes downstream and consumers must never see a
						// still-ACTIVE row for a cleared alarm.
						m.queueIO(alarmIO{
							tagID:       tagID,
							alias:       alias,
							def:         def,
							value:       floatVal,
							status:      "CLEARED",
							ref:         track.Event,
							triggeredAt: track.FiredAt,
							notify:      true,
						})
					}
					// Remove from tracking
					delete(tracks, def.ID)
				}
			}
		}
	}

	m.mu.Unlock()

	// Everything that can block — the DB writes and the MQTT publish done by
	// OnAlarmEvent — happens here, with the lock released.
	m.drainIO()
}

// queueIO appends a durable side-effect to the pending queue.
// MUST be called with m.mu held.
func (m *Manager) queueIO(io alarmIO) {
	m.pendingIO = append(m.pendingIO, io)
	// The queue is deliberately unbounded — dropping an industrial alarm to save
	// memory is never the right trade — but a backlog this size means the DB or
	// the broker has been unresponsive for a long time and the operator should
	// know before the process runs out of memory.
	if n := len(m.pendingIO); n == 1000 || n%10000 == 0 {
		log.Printf("[ALARM-MANAGER] %d alarm writes/publishes are backed up — database or broker is not keeping up", n)
	}
}

// drainIO executes the queued alarm I/O with m.mu released.
//
// Ordering: actions are appended under m.mu in the exact order the transitions
// were decided, popped FIFO here, and at most one goroutine drains at a time
// (m.ioDraining). So the ACTIVE and CLEARED writes of the same alarm can never
// overtake each other, and a CLEARED row is always written before its callback
// fires — the guarantees that used to come from holding m.mu across the I/O.
//
// A caller that finds another goroutine already draining returns immediately
// instead of blocking: its actions are already in the queue, and the active
// drainer re-checks the queue under m.mu before clearing the flag, so there is
// no window in which work is left stranded. That is what keeps a stalled
// database off the poll loop, rather than merely moving the stall from m.mu
// onto another mutex.
func (m *Manager) drainIO() {
	m.mu.Lock()
	if m.ioDraining {
		m.mu.Unlock()
		return
	}
	m.ioDraining = true

	for len(m.pendingIO) > 0 {
		act := m.pendingIO[0]
		m.pendingIO = m.pendingIO[1:]
		m.mu.Unlock()

		m.executeIO(act)

		m.mu.Lock()
	}

	m.ioDraining = false
	m.mu.Unlock()
}

// executeIO performs the durable write and the notification for one action.
// It runs with NO lock held; m.mu is taken only for the short reads/writes of
// the shared eventRef and the track.
func (m *Manager) executeIO(act alarmIO) {
	alias := act.alias
	if act.lookupAlias && m.store != nil {
		if resolved, err := m.store.TagAlias(act.tagID); err != nil {
			log.Printf("[ALARM-MANAGER] Could not resolve alias for tag %d: %v", act.tagID, err)
		} else {
			alias = resolved
		}
	}

	if m.store != nil {
		switch act.status {
		case "ACTIVE":
			m.persistActive(act)
		case "CLEARED":
			m.persistCleared(act)
		}
	}

	if act.notify && m.OnAlarmEvent != nil {
		m.mu.Lock()
		eventID := act.ref.id
		m.mu.Unlock()
		m.OnAlarmEvent(eventID, act.tagID, alias, act.def, act.value, act.status)
	}
}

// persistActive writes (or re-writes) the ACTIVE alarm_events row.
func (m *Manager) persistActive(act alarmIO) {
	m.mu.Lock()
	alreadyPersisted := act.ref.id > 0
	m.mu.Unlock()
	if alreadyPersisted {
		// A retry was queued while the first INSERT was still in flight against
		// a slow database and that INSERT has since landed. Writing again here
		// would create the duplicate ACTIVE row we are trying to prevent.
		return
	}

	id, err := m.store.InsertActive(act.tagID, act.def, act.value, act.triggeredAt)
	if err != nil {
		// The alarm is announced but NOT durably recorded. ref.id stays 0, which
		// is precisely what makes tickDelays retry it: the failure is no longer
		// swallowed and the event is not lost. NextInsertRetry was already
		// pushed forward by the caller, so the retry is throttled.
		log.Printf("[ALARM-MANAGER] Failed to persist ACTIVE alarm (tag=%d def=%d) — will retry: %v",
			act.tagID, act.def.ID, err)
		return
	}

	m.mu.Lock()
	act.ref.id = id
	m.mu.Unlock()
}

// persistCleared closes the alarm_events row of an alarm that has cleared.
// The DB write happens BEFORE the caller publishes the event downstream.
func (m *Manager) persistCleared(act alarmIO) {
	m.mu.Lock()
	eventID := act.ref.id
	m.mu.Unlock()

	if eventID > 0 {
		if err := m.store.MarkCleared(eventID); err != nil {
			log.Printf("[ALARM-MANAGER] Failed to update alarm event %d as CLEARED: %v", eventID, err)
		}
		return
	}

	// The ACTIVE insert never succeeded and the condition cleared before a retry
	// could land. Record the whole occurrence as a single already-closed row so
	// the operator still has a history entry, instead of no trace at all.
	id, err := m.store.InsertCleared(act.tagID, act.def, act.value, act.triggeredAt, time.Now())
	if err != nil {
		log.Printf("[ALARM-MANAGER] Failed to record unpersisted alarm as CLEARED (tag=%d def=%d): %v",
			act.tagID, act.def.ID, err)
		return
	}

	m.mu.Lock()
	act.ref.id = id
	m.mu.Unlock()
}

// alarmStore is the durable side of the alarm pipeline. It is an interface so
// the retry/durability semantics can be exercised against a store that fails on
// demand; production always uses sqlStore.
type alarmStore interface {
	// InsertActive creates the ACTIVE row for an alarm occurrence, or returns
	// the id of the one that is already open for this tag+definition.
	InsertActive(tagID int, def models.AlarmDefinition, val float64, triggerTime time.Time) (int, error)
	// InsertCleared records an already-closed occurrence in a single row.
	InsertCleared(tagID int, def models.AlarmDefinition, val float64, triggerTime, clearTime time.Time) (int, error)
	// MarkCleared closes an existing ACTIVE row.
	MarkCleared(eventID int) error
	// TagAlias resolves the human-readable name of a tag.
	TagAlias(tagID int) (string, error)
}

type sqlStore struct{ db *sql.DB }

// InsertActive creates a new ACTIVE alarm event record in the database.
//
// The INSERT is conditional (WHERE NOT EXISTS) rather than a plain INSERT whose
// error string is inspected afterwards: the partial unique index this code used
// to rely on (alarm_events_active_unique) existed in no migration, so nothing at
// all prevented duplicate open rows for the same tag+definition. The index is
// now created by runAutoMigrations, but only as a backstop — on TimescaleDB
// alarm_events is a hypertable and a unique index there must contain the
// partitioning column, so the index cannot always be installed. The conditional
// INSERT enforces the invariant regardless of which of the two the deployment
// got.
//
// If a row is already open, its id is returned with no error: that is exactly
// what the caller needs in order to CLEAR it later.
func (s *sqlStore) InsertActive(tagID int, def models.AlarmDefinition, val float64, triggerTime time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), alarmDBTimeout)
	defer cancel()

	var eventID int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO alarm_events (tag_id, definition_id, status, alarm_type, severity, message, value_at_trigger, trigger_time)
		SELECT $1, $2, 'ACTIVE', $3, $4, $5, $6, $7
		WHERE NOT EXISTS (
			SELECT 1 FROM alarm_events
			WHERE tag_id = $1 AND definition_id = $2 AND status = 'ACTIVE' AND clear_time IS NULL
		)
		RETURNING id
	`, tagID, def.ID, def.AlarmType, def.Severity, def.Message, val, triggerTime).Scan(&eventID)

	switch {
	case err == nil:
		return eventID, nil

	case errors.Is(err, sql.ErrNoRows):
		// Nothing inserted: an ACTIVE row for this tag+definition is already
		// open (e.g. left behind by a restart). Adopt it.
		lookupCtx, lookupCancel := context.WithTimeout(context.Background(), alarmDBTimeout)
		defer lookupCancel()

		err = s.db.QueryRowContext(lookupCtx, `
			SELECT id FROM alarm_events
			WHERE tag_id = $1 AND definition_id = $2 AND status = 'ACTIVE' AND clear_time IS NULL
			ORDER BY trigger_time DESC LIMIT 1
		`, tagID, def.ID).Scan(&eventID)
		if err != nil {
			return 0, fmt.Errorf("open alarm event not found for tag %d def %d: %w", tagID, def.ID, err)
		}
		return eventID, nil

	default:
		return 0, fmt.Errorf("failed to insert alarm event: %w", err)
	}
}

// InsertCleared writes a single already-closed alarm_events row.
func (s *sqlStore) InsertCleared(tagID int, def models.AlarmDefinition, val float64, triggerTime, clearTime time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), alarmDBTimeout)
	defer cancel()

	var eventID int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO alarm_events (tag_id, definition_id, status, alarm_type, severity, message, value_at_trigger, trigger_time, clear_time)
		VALUES ($1, $2, 'CLEARED', $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, tagID, def.ID, def.AlarmType, def.Severity, def.Message, val, triggerTime, clearTime).Scan(&eventID)
	if err != nil {
		return 0, fmt.Errorf("failed to insert cleared alarm event: %w", err)
	}
	return eventID, nil
}

// MarkCleared updates an existing alarm event with CLEARED status
func (s *sqlStore) MarkCleared(eventID int) error {
	ctx, cancel := context.WithTimeout(context.Background(), alarmDBTimeout)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		UPDATE alarm_events
		SET status = 'CLEARED', clear_time = $1
		WHERE id = $2
	`, time.Now(), eventID)

	if err != nil {
		return fmt.Errorf("failed to update alarm event: %w", err)
	}

	return nil
}

// TagAlias resolves the display name of a tag.
func (s *sqlStore) TagAlias(tagID int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), alarmDBTimeout)
	defer cancel()

	var alias string
	if err := s.db.QueryRowContext(ctx, `SELECT alias FROM tags WHERE id = $1`, tagID).Scan(&alias); err != nil {
		return "", err
	}
	return alias, nil
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
