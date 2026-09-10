package dbhealth_test

import (
	"context"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/dbhealth"
)

// spy records what the operator would have been told.
type spy struct{ said []string }

func (s *spy) Problem(key, severity, message string) { s.said = append(s.said, "problem:"+key) }
func (s *spy) Resolved(key string)                   { s.said = append(s.said, "resolved:"+key) }

// source returns snapshots in order, repeating the last one forever.
func source(snaps ...*dbhealth.Snapshot) func(context.Context) dbhealth.Snapshot {
	i := 0
	return func(context.Context) dbhealth.Snapshot {
		s := snaps[i]
		if i < len(snaps)-1 {
			i++
		}
		return *s
	}
}

func failingJob() *dbhealth.Snapshot {
	s := healthy()
	s.Jobs[0].LastRunStatus = "Failed"
	s.Jobs[0].TotalFailures = 1
	return s
}

// The watcher runs on a timer. A retention job that has been failing since
// Friday must produce one message, not one per minute all weekend.
func TestAProblemIsAnnouncedOnce(t *testing.T) {
	var s spy
	w := dbhealth.NewTestWatcher(source(healthy(), failingJob()), &s, th)

	for i := 0; i < 8; i++ {
		w.Scan(context.Background(), now().Add(time.Duration(i)*time.Minute))
	}

	if len(s.said) != 1 || s.said[0] != "problem:job:1000" {
		t.Fatalf("eight looks at one failing job produced %v", s.said)
	}
}

func TestAProblemThatGoesAwayIsAnnouncedOnce(t *testing.T) {
	var s spy
	w := dbhealth.NewTestWatcher(source(healthy(), failingJob(), failingJob(), healthy()), &s, th)

	for i := 0; i < 6; i++ {
		w.Scan(context.Background(), now().Add(time.Duration(i)*time.Minute))
	}

	want := []string{"problem:job:1000", "resolved:job:1000"}
	if len(s.said) != 2 || s.said[0] != want[0] || s.said[1] != want[1] {
		t.Fatalf("got %v, want %v", s.said, want)
	}
	if w.OpenIssues() != 0 {
		t.Errorf("the watcher still holds %d open issues after the recovery", w.OpenIssues())
	}
}

// The same problem coming back after it was resolved is news again.
func TestAProblemThatReturnsIsAnnouncedAgain(t *testing.T) {
	var s spy
	w := dbhealth.NewTestWatcher(
		source(healthy(), failingJob(), healthy(), failingJob()), &s, th)

	for i := 0; i < 8; i++ {
		w.Scan(context.Background(), now().Add(time.Duration(i)*time.Minute))
	}

	want := []string{"problem:job:1000", "resolved:job:1000", "problem:job:1000"}
	if len(s.said) != len(want) {
		t.Fatalf("got %v, want %v", s.said, want)
	}
	for i := range want {
		if s.said[i] != want[i] {
			t.Fatalf("got %v, want %v", s.said, want)
		}
	}
}

// Problems are independent: a failing job must not hide a stuck query, and
// resolving one must not silence the other.
func TestProblemsAreTrackedSeparately(t *testing.T) {
	both := failingJob()
	both.SlowQueries = []dbhealth.SlowQuery{{PID: 99, Running: time.Hour, Query: "SELECT 1"}}

	onlyTheQuery := healthy()
	onlyTheQuery.SlowQueries = both.SlowQueries

	var s spy
	w := dbhealth.NewTestWatcher(source(healthy(), both, onlyTheQuery), &s, th)

	for i := 0; i < 6; i++ {
		w.Scan(context.Background(), now().Add(time.Duration(i)*time.Minute))
	}

	want := []string{"problem:job:1000", "problem:slow-query:99", "resolved:job:1000"}
	if len(s.said) != len(want) {
		t.Fatalf("got %v, want %v", s.said, want)
	}
	for i := range want {
		if s.said[i] != want[i] {
			t.Fatalf("got %v, want %v", s.said, want)
		}
	}
	if w.OpenIssues() != 1 {
		t.Errorf("open issues = %d, want 1 (the query is still stuck)", w.OpenIssues())
	}
}

// A watcher with nobody listening must still work: the log line is the fallback
// when no channel is configured, and a nil announcer must not take down the
// process that is watching the database.
func TestAWatcherWithNoAnnouncerDoesNotPanic(t *testing.T) {
	w := dbhealth.NewTestWatcher(source(healthy(), failingJob()), nil, th)
	w.Scan(context.Background(), now())
	w.Scan(context.Background(), now().Add(time.Minute))
}
