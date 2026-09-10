package dbhealth

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// Announcer is told when a problem appears and when it goes away.
//
// An interface, so this package never has to know that notification channels
// exist and can be exercised without one.
type Announcer interface {
	Problem(key, severity, message string)
	Resolved(key string)
}

// Watcher looks at the database on a timer and announces what changed.
type Watcher struct {
	// look takes one snapshot. A field rather than a direct call so the loop
	// and the dedupe can be exercised without a Postgres behind them.
	look       func(context.Context) Snapshot
	announcer  Announcer
	thresholds Thresholds

	prev *Snapshot
	// open is the set of problems the operator has already been told about.
	// In memory on purpose, unlike the gateway watcher's: these are statements
	// about how the database is behaving RIGHT NOW, and after a restart the
	// operator should hear about a database that is still struggling.
	open map[string]bool
}

func NewWatcher(db *sql.DB, a Announcer, th Thresholds) *Watcher {
	return newWatcher(func(ctx context.Context) Snapshot { return Collect(ctx, db) }, a, th)
}

func newWatcher(look func(context.Context) Snapshot, a Announcer, th Thresholds) *Watcher {
	return &Watcher{look: look, announcer: a, thresholds: th, open: make(map[string]bool)}
}

// Run looks every interval until the context is canceled.
func (w *Watcher) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	log.Printf("[DB-HEALTH] watching the database every %s", every)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Scan(ctx, time.Now())
		}
	}
}

// Scan takes one look and announces the difference from the last one.
func (w *Watcher) Scan(ctx context.Context, now time.Time) {
	cur := w.look(ctx)
	issues := Evaluate(w.prev, &cur, w.thresholds, now)
	w.prev = &cur

	seen := make(map[string]bool, len(issues))
	for i := range issues {
		issue := &issues[i]
		seen[issue.Key] = true
		if w.open[issue.Key] {
			continue // already said, and saying it again every minute helps nobody
		}
		w.open[issue.Key] = true
		log.Printf("[DB-HEALTH] %s: %s", issue.Severity, issue.Message)
		if w.announcer != nil {
			w.announcer.Problem(issue.Key, issue.Severity, issue.Message)
		}
	}

	for key := range w.open {
		if seen[key] {
			continue
		}
		delete(w.open, key)
		log.Printf("[DB-HEALTH] resolved: %s", key)
		if w.announcer != nil {
			w.announcer.Resolved(key)
		}
	}
}
