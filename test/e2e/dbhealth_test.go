//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/dbhealth"
)

// The evaluation logic is unit-tested against snapshots somebody typed. What no
// unit test can tell you is whether the SQL that produces those snapshots runs:
// a view that was renamed between TimescaleDB versions, or a column that never
// existed, fails at run time inside a goroutine, gets logged once, and leaves a
// watcher that reports a perfectly healthy database forever.
//
// So this runs the real collector against the real database.
func TestTheDatabaseHealthCollectorRunsAgainstTheRealDatabase(t *testing.T) {
	db := openDB(t)

	snap := dbhealth.Collect(context.Background(), db)

	// Nothing is asserted about the pool here. Those numbers come from
	// database/sql, not from Postgres, and they describe THIS test's connection
	// rather than the one core-api runs — a first version checked MaxOpen and
	// failed, because a pool with no limit reports zero and this test never set
	// one. The pool is covered by the unit tests; what only a real database can
	// answer is whether the queries below run at all.

	// The jobs come from timescaledb_information, and this deployment has
	// retention and compression policies — TestHistorianHasARetentionPolicy
	// asserts they exist. Finding none here means the query is not reaching
	// them, which is precisely the failure this test is for.
	if len(snap.Jobs) == 0 {
		t.Fatal("the collector found no TimescaleDB background jobs, yet this installation " +
			"has retention and compression policies. The query is not reading what it thinks " +
			"it is reading, and a watcher built on it would report a healthy database forever.")
	}
	for _, j := range snap.Jobs {
		t.Logf("job %d %q: last=%q failures=%d lastSuccess=%v",
			j.ID, j.Name, j.LastRunStatus, j.TotalFailures, j.LastSuccess)
		if j.Name == "" {
			t.Errorf("job %d came back with no name; the message would not say what broke", j.ID)
		}
	}

	// pg_stat_activity always has at least this connection's own siblings; what
	// matters is that the query runs and the durations are sane rather than the
	// six-million-year figures a mishandled -infinity produces.
	for _, q := range snap.SlowQueries {
		t.Logf("running for %s: %s", q.Running, q.Query)
		if q.Running < 0 || q.Running > 365*24*time.Hour {
			t.Errorf("a statement reports a runtime of %s", q.Running)
		}
	}
}

// A freshly created policy has never finished successfully. TimescaleDB reports
// that as -infinity, which as a plain timestamp is the year -4713: left alone,
// every new policy would immediately be announced as "not running for six
// million years".
func TestAPolicyThatHasNeverRunIsNotReportedAsAncient(t *testing.T) {
	db := openDB(t)

	snap := dbhealth.Collect(context.Background(), db)
	issues := dbhealth.Evaluate(nil, &snap, dbhealth.DefaultThresholds(), time.Now())

	for _, i := range issues {
		t.Logf("issue %s: %s", i.Key, i.Message)
	}

	for _, j := range snap.Jobs {
		if j.LastSuccess.IsZero() {
			continue // never finished successfully, which is what zero means here
		}
		if j.LastSuccess.Year() < 2000 {
			t.Errorf("job %d reports its last success as %v — that is TimescaleDB's "+
				"-infinity leaking through, and it would be announced as a fault",
				j.ID, j.LastSuccess)
		}
	}
}

// The defect the first run of this file actually found: a job whose
// last_successful_finish is -infinity — every policy that has not yet had a
// successful run — came back from lib/pq as raw bytes, the row failed to scan,
// and the collector logged one line and moved on. The jobs that were most worth
// looking at were the ones it could not see.
//
// A row that cannot be read is now an error, not a shrug, so this asserts the
// whole set comes back rather than whatever survived.
func TestEveryBackgroundJobIsRead(t *testing.T) {
	db := openDB(t)

	var expected int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM timescaledb_information.jobs WHERE job_id >= 1000`).
		Scan(&expected); err != nil {
		t.Fatalf("counting the jobs: %v", err)
	}
	if expected == 0 {
		t.Skip("this database has no background jobs to read")
	}

	snap := dbhealth.Collect(context.Background(), db)

	if len(snap.Jobs) != expected {
		t.Fatalf("the collector read %d of %d background jobs. The ones it drops are the "+
			"ones whose columns it cannot parse — which is where a policy that has never "+
			"run lives, and that is exactly the policy worth watching.",
			len(snap.Jobs), expected)
	}
}
