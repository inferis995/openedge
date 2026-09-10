// Package dbhealth watches the database the historian depends on and says
// something when it starts to struggle.
//
// The three failures it looks for are the ones that are silent by nature. A
// retention job that stops running does not raise an error anywhere: the disk
// simply grows until it is full, weeks later, and takes the whole plant with
// it. A saturated connection pool makes every request slower without failing
// any of them. A query that has been running for an hour holds locks that
// nothing else can see.
package dbhealth

import (
	"fmt"
	"time"
)

// Pool is what database/sql knows about its own connections.
type Pool struct {
	MaxOpen      int
	InUse        int
	WaitCount    int64         // cumulative since the process started
	WaitDuration time.Duration // cumulative
}

// Job is one TimescaleDB background job — retention, compression, reorder.
type Job struct {
	ID            int
	Name          string
	LastRunStatus string // "Success", "Failed", or empty when it has never run
	TotalFailures int64
	LastSuccess   time.Time // zero when it has never succeeded
}

// SlowQuery is a statement that has been running for a while.
type SlowQuery struct {
	PID     int
	Running time.Duration
	Query   string
}

// Snapshot is one look at the database.
type Snapshot struct {
	Pool        Pool
	Jobs        []Job
	SlowQueries []SlowQuery
}

// Issue is one thing worth telling the operator, keyed so the same problem is
// only ever announced once.
type Issue struct {
	Key      string
	Severity string
	Message  string
}

// Thresholds is how much trouble counts as trouble.
type Thresholds struct {
	// PoolWaits is how many requests may queue for a connection between two
	// looks before it counts as saturation. Zero would fire on the single
	// unlucky request every busy system has.
	PoolWaits int64

	// PoolAverageWait is how long those waits have to average. A hundred waits
	// of a microsecond are a pool doing its job; ten of half a second are a
	// pool that has run out.
	PoolAverageWait time.Duration

	// SlowQuery is how long a single statement may run.
	SlowQuery time.Duration

	// JobSilence is how long a background job may go without succeeding. It
	// catches the job that is not failing but not running either — the state
	// that produces no error row anywhere.
	JobSilence time.Duration
}

// DefaultThresholds are tuned for an industrial historian, where the scan loops
// are steady and a genuine queue means something is wrong.
func DefaultThresholds() Thresholds {
	return Thresholds{
		PoolWaits:       20,
		PoolAverageWait: 50 * time.Millisecond,
		SlowQuery:       5 * time.Minute,
		JobSilence:      48 * time.Hour,
	}
}

// Evaluate compares two looks at the database and reports what is wrong now.
//
// prev may be nil, on the first look. The checks that need a delta simply do
// not fire then, rather than treating "everything that ever happened" as
// something that just happened: WaitCount is cumulative since the process
// started, so reading it as a rate on the first scan would announce a
// saturation that ended hours ago.
func Evaluate(prev, cur *Snapshot, th Thresholds, now time.Time) []Issue {
	var issues []Issue

	issues = append(issues, poolIssue(prev, cur, th)...)
	issues = append(issues, jobIssues(prev, cur, th, now)...)
	issues = append(issues, queryIssues(cur, th)...)

	return issues
}

func poolIssue(prev, cur *Snapshot, th Thresholds) []Issue {
	if prev == nil {
		return nil
	}
	waits := cur.Pool.WaitCount - prev.Pool.WaitCount
	if waits < th.PoolWaits {
		return nil
	}
	waited := cur.Pool.WaitDuration - prev.Pool.WaitDuration
	average := waited / time.Duration(waits)
	if average < th.PoolAverageWait {
		return nil
	}
	return []Issue{{
		Key:      "pool",
		Severity: "warning",
		Message: fmt.Sprintf(
			"Database sotto sforzo: %d richieste hanno dovuto aspettare una connessione "+
				"(attesa media %s, pool da %d). Le risposte stanno rallentando per tutti.",
			waits, average.Round(time.Millisecond), cur.Pool.MaxOpen),
	}}
}

func jobIssues(prev, cur *Snapshot, th Thresholds, now time.Time) []Issue {
	previousFailures := make(map[int]int64, len(cur.Jobs))
	if prev != nil {
		for i := range prev.Jobs {
			previousFailures[prev.Jobs[i].ID] = prev.Jobs[i].TotalFailures
		}
	}

	var issues []Issue
	for i := range cur.Jobs {
		j := &cur.Jobs[i]

		failing := j.LastRunStatus == "Failed"
		if before, seen := previousFailures[j.ID]; seen && j.TotalFailures > before {
			failing = true
		}
		if failing {
			issues = append(issues, Issue{
				Key:      fmt.Sprintf("job:%d", j.ID),
				Severity: "critical",
				Message: fmt.Sprintf(
					"Il job TimescaleDB %q sta fallendo (%d fallimenti in totale). "+
						"Se è la retention, lo storico cresce senza limite e il disco si "+
						"riempie; se è la compressione, si riempie più in fretta.",
					j.Name, j.TotalFailures),
			})
			continue
		}

		// A job that is not failing and not running either leaves no error row
		// anywhere: the only trace is a success that keeps getting older.
		if th.JobSilence > 0 && !j.LastSuccess.IsZero() && now.Sub(j.LastSuccess) >= th.JobSilence {
			issues = append(issues, Issue{
				Key:      fmt.Sprintf("job-stale:%d", j.ID),
				Severity: "warning",
				Message: fmt.Sprintf(
					"Il job TimescaleDB %q non va a buon fine da %s. Non sta segnalando errori: "+
						"semplicemente non gira.",
					j.Name, roughly(now.Sub(j.LastSuccess))),
			})
		}
	}
	return issues
}

func queryIssues(cur *Snapshot, th Thresholds) []Issue {
	if th.SlowQuery <= 0 {
		return nil
	}
	var issues []Issue
	for i := range cur.SlowQueries {
		q := &cur.SlowQueries[i]
		if q.Running < th.SlowQuery {
			continue
		}
		issues = append(issues, Issue{
			// Keyed on the backend PID: the same stuck statement is one issue,
			// and a new one is a new issue.
			Key:      fmt.Sprintf("slow-query:%d", q.PID),
			Severity: "warning",
			Message: fmt.Sprintf(
				"Una query è in esecuzione da %s e tiene lock che rallentano tutto il resto: %s",
				roughly(q.Running), shorten(q.Query, 200)),
		})
	}
	return issues
}

// shorten trims a statement to something that fits in a message.
func shorten(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// roughly renders a duration the way somebody says it out loud.
func roughly(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d secondi", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minuti", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d ore", int(d.Hours()))
	}
	return fmt.Sprintf("%d giorni", int(d.Hours()/24))
}
