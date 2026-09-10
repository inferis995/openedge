// export_test.go exposes the watcher's seams for white-box testing.
// Only compiled during `go test`.
package gatewayhealth

import (
	"context"
	"errors"
	"sync"
	"time"
)

// NewTestWatcher builds a Watcher over a fake store.
func NewTestWatcher(st *FakeStore, a Announcer, cfg Config) *Watcher {
	return newWatcher(st, a, cfg)
}

// SeenCount reports how many gateways the watcher still remembers first seeing.
func (w *Watcher) SeenCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.seen)
}

// FakeStore is an in-memory store that can be told to fail its writes.
type FakeStore struct {
	mu sync.Mutex

	States    []State
	failWrite bool

	// Notified records every state written, in order.
	Notified []Written
}

// Written is one recorded call to RecordNotified.
type Written struct {
	GatewayID int
	Notified  string
}

func (s *FakeStore) FailWrites(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failWrite = fail
}

func (s *FakeStore) EnabledGateways(context.Context) ([]State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]State(nil), s.States...), nil
}

func (s *FakeStore) RecordNotified(_ context.Context, gatewayID int, notified string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWrite {
		return errors.New("simulated database failure")
	}
	s.Notified = append(s.Notified, Written{GatewayID: gatewayID, Notified: notified})
	// The next scan reads what was written, exactly as the real one would.
	for i := range s.States {
		if s.States[i].GatewayID == gatewayID {
			s.States[i].NotifiedAs = notified
		}
	}
	return nil
}

// SetStatus updates what a gateway is reporting, as a driver would.
func (s *FakeStore) SetStatus(gatewayID int, status string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.States {
		if s.States[i].GatewayID == gatewayID {
			s.States[i].Status = status
			s.States[i].ReportedAt = at
		}
	}
}

// Remove deletes a gateway, as disabling or deleting one would.
func (s *FakeStore) Remove(gatewayID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.States[:0]
	for _, st := range s.States {
		if st.GatewayID != gatewayID {
			kept = append(kept, st)
		}
	}
	s.States = kept
}
