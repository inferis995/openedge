// export_test.go exposes the watcher's seam for white-box testing.
// Only compiled during `go test`.
package dbhealth

import "context"

// NewTestWatcher builds a Watcher over a snapshot source of the test's choosing.
func NewTestWatcher(look func(context.Context) Snapshot, a Announcer, th Thresholds) *Watcher {
	return newWatcher(look, a, th)
}

// OpenIssues reports how many problems the watcher currently considers open.
func (w *Watcher) OpenIssues() int { return len(w.open) }
