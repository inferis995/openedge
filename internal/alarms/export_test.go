// export_test.go exposes unexported alarm helpers for white-box testing.
// Only compiled during `go test`.
package alarms

import (
	"fmt"
	"sync"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

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

// TrackEventID reports the alarm_events row id recorded for a track, or -1 when
// the track no longer exists. 0 means "announced but not durably recorded yet".
func (m *Manager) TrackEventID(tagID, defID int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	tracks, ok := m.activeTracks[tagID]
	if !ok {
		return -1
	}
	t, ok := tracks[defID]
	if !ok {
		return -1
	}
	return t.Event.id
}

// ExpireInsertRetry makes a track's next persistence retry due immediately, so
// tests do not have to wait alarmInsertRetryInterval.
func (m *Manager) ExpireInsertRetry(tagID, defID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tracks, ok := m.activeTracks[tagID]; ok {
		if t, ok := tracks[defID]; ok {
			t.NextInsertRetry = time.Now().Add(-time.Second)
		}
	}
}

// UseFakeStore attaches an in-memory durable store to the manager and returns it.
func (m *Manager) UseFakeStore() *FakeStore {
	s := &FakeStore{aliases: map[int]string{}}
	m.store = s
	return s
}

// FakeStore is an in-memory alarmStore that can be told to fail, so the
// retry / durability semantics can be exercised without a database.
type FakeStore struct {
	mu sync.Mutex

	failActive bool
	nextID     int
	aliases    map[int]string

	ActiveInserts  int   // successful + failed attempts at an ACTIVE insert
	ClearedInserts int   // standalone already-closed rows written
	MarkedCleared  []int // event ids closed via UPDATE
	OpenRows       map[int]bool
}

// FailActive toggles whether ACTIVE inserts fail.
func (s *FakeStore) FailActive(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failActive = fail
}

// Counters returns a snapshot of the store's activity.
func (s *FakeStore) Counters() (activeInserts, clearedInserts, marked int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ActiveInserts, s.ClearedInserts, len(s.MarkedCleared)
}

func (s *FakeStore) InsertActive(tagID int, def models.AlarmDefinition, val float64, triggerTime time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ActiveInserts++
	if s.failActive {
		return 0, fmt.Errorf("simulated database failure")
	}
	s.nextID++
	if s.OpenRows == nil {
		s.OpenRows = map[int]bool{}
	}
	s.OpenRows[s.nextID] = true
	return s.nextID, nil
}

func (s *FakeStore) InsertCleared(tagID int, def models.AlarmDefinition, val float64, triggerTime, clearTime time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ClearedInserts++
	s.nextID++
	return s.nextID, nil
}

func (s *FakeStore) MarkCleared(eventID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MarkedCleared = append(s.MarkedCleared, eventID)
	delete(s.OpenRows, eventID)
	return nil
}

func (s *FakeStore) TagAlias(tagID int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.aliases[tagID]; ok {
		return a, nil
	}
	return fmt.Sprintf("tag-%d", tagID), nil
}
