// export_test.go exposes unexported alarm helpers for white-box testing.
// Only compiled during `go test`.
package alarms

import "github.com/ralph/industrial-edge-middleware/internal/models"

// IsConditionViolated exposes the unexported isConditionViolated for testing.
func IsConditionViolated(def models.AlarmDefinition, val float64) bool {
	return isConditionViolated(def, val)
}

// IsCleared exposes the unexported isCleared for testing.
func IsCleared(def models.AlarmDefinition, val float64) bool {
	return isCleared(def, val)
}

// ToFloat exposes the unexported toFloat for testing.
func ToFloat(val interface{}) (float64, bool) {
	return toFloat(val)
}

// NewTestManager builds a Manager with no DB or broker, suitable for exercising
// the pure in-memory tracking logic (NewManager would hit the database).
func NewTestManager() *Manager {
	return &Manager{
		definitions:  make(map[int][]models.AlarmDefinition),
		activeTracks: make(map[int]map[int]*activeAlarmTrack),
	}
}

// SetDefinitions installs alarm definitions for a tag.
func (m *Manager) SetDefinitions(tagID int, defs []models.AlarmDefinition) {
	m.definitions[tagID] = defs
}

// PendingCount reports how many not-yet-fired tracks exist for a tag.
func (m *Manager) PendingCount(tagID int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.activeTracks[tagID] {
		if !t.Triggered {
			n++
		}
	}
	return n
}

// TickDelays exposes the delay ticker for testing.
func (m *Manager) TickDelays() { m.tickDelays() }
