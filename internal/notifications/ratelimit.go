package notifications

import (
	"sync"
	"time"
)

// rateLimiter is a tiny token bucket: at most `max` events per `window`,
// shared across every channel and severity. Keeps a flapping sensor
// from turning into 100 emails / 5 minutes during an alarm storm.
type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	events []time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window}
}

// allow returns true and records the event when capacity is available.
// Returns false (and does not record) when over budget; the caller
// drops the notification and logs it.
func (r *rateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-r.window)
	// Prune expired events.
	kept := r.events[:0]
	for _, t := range r.events {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	r.events = kept
	if len(r.events) >= r.max {
		return false
	}
	r.events = append(r.events, now)
	return true
}
