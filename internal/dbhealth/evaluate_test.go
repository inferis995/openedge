package dbhealth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/dbhealth"
)

var th = dbhealth.DefaultThresholds()

func now() time.Time { return time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC) }

// keys returns the issue keys, which is what dedupe is built on.
func keys(issues []dbhealth.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Key)
	}
	return out
}

func has(issues []dbhealth.Issue, key string) *dbhealth.Issue {
	for i := range issues {
		if issues[i].Key == key {
			return &issues[i]
		}
	}
	return nil
}

func healthy() *dbhealth.Snapshot {
	return &dbhealth.Snapshot{
		Pool: dbhealth.Pool{MaxOpen: 25, InUse: 3},
		Jobs: []dbhealth.Job{
			{ID: 1000, Name: "Retention Policy [1000]", LastRunStatus: "Success", LastSuccess: now().Add(-time.Hour)},
			{ID: 1001, Name: "Compression Policy [1001]", LastRunStatus: "Success", LastSuccess: now().Add(-2 * time.Hour)},
		},
	}
}

func TestAHealthyDatabaseProducesNothing(t *testing.T) {
	prev, cur := healthy(), healthy()
	if got := dbhealth.Evaluate(prev, cur, th, now()); len(got) != 0 {
		t.Fatalf("a healthy database produced %v", keys(got))
	}
}

// ---------------------------------------------------------------------------
// Background jobs — the failure that fills a disk in silence
// ---------------------------------------------------------------------------

func TestAFailingRetentionJobIsReported(t *testing.T) {
	// The failure counter is deliberately unchanged between the two looks, so
	// the only thing that can raise this is the reported status. The first
	// version of this test left it at zero in prev and four in cur, and went on
	// passing with the status check removed — it was exercising the delta.
	prev := healthy()
	prev.Jobs[0].TotalFailures = 4

	cur := healthy()
	cur.Jobs[0].LastRunStatus = "Failed"
	cur.Jobs[0].TotalFailures = 4

	issues := dbhealth.Evaluate(prev, cur, th, now())
	got := has(issues, "job:1000")
	if got == nil {
		t.Fatalf("a failing retention job produced %v", keys(issues))
	}
	if got.Severity != "critical" {
		t.Errorf("severity = %q; retention stopping means the disk fills up", got.Severity)
	}
	if !strings.Contains(got.Message, "Retention Policy [1000]") {
		t.Errorf("the message should name the job, got %q", got.Message)
	}
}

// A job can report its last run as a success and still be failing repeatedly in
// between. The counter is what shows it.
func TestAJobWhoseFailureCountGrowsIsReported(t *testing.T) {
	prev := healthy()
	prev.Jobs[0].TotalFailures = 2

	cur := healthy()
	cur.Jobs[0].TotalFailures = 3 // one more since the last look

	if has(dbhealth.Evaluate(prev, cur, th, now()), "job:1000") == nil {
		t.Fatal("a job that failed again between two looks was not reported")
	}
}

// The failure with no error anywhere: the job is not failing, it is simply not
// running. Nothing in Postgres raises anything, and the disk grows.
func TestAJobThatSimplyStoppedRunningIsReported(t *testing.T) {
	cur := healthy()
	cur.Jobs[0].LastSuccess = now().Add(-72 * time.Hour)

	issues := dbhealth.Evaluate(healthy(), cur, th, now())
	got := has(issues, "job-stale:1000")
	if got == nil {
		t.Fatalf("a job that has not succeeded in three days produced %v", keys(issues))
	}
	if !strings.Contains(got.Message, "on sta segnalando errori") {
		t.Errorf("the message should say that silence is the symptom, got %q", got.Message)
	}
}

// A job that has never run is not the same as one that stopped: a freshly
// created policy has no successes yet and is not a fault.
func TestAJobThatHasNeverRunIsNotAFault(t *testing.T) {
	cur := healthy()
	cur.Jobs[0].LastSuccess = time.Time{}
	cur.Jobs[0].LastRunStatus = ""

	if issues := dbhealth.Evaluate(healthy(), cur, th, now()); len(issues) != 0 {
		t.Fatalf("a policy that has not had its first run yet produced %v", keys(issues))
	}
}

// A failing job is reported as failing, not additionally as stale: two messages
// about one job is one message too many.
func TestAFailingJobIsReportedOnce(t *testing.T) {
	cur := healthy()
	cur.Jobs[0].LastRunStatus = "Failed"
	cur.Jobs[0].LastSuccess = now().Add(-72 * time.Hour)

	issues := dbhealth.Evaluate(healthy(), cur, th, now())
	if has(issues, "job:1000") == nil {
		t.Fatal("the failure was not reported")
	}
	if has(issues, "job-stale:1000") != nil {
		t.Errorf("the same job was reported twice: %v", keys(issues))
	}
}

// ---------------------------------------------------------------------------
// Connection pool
// ---------------------------------------------------------------------------

func TestASaturatedPoolIsReported(t *testing.T) {
	prev := healthy()
	cur := healthy()
	cur.Pool.WaitCount = 50
	cur.Pool.WaitDuration = 20 * time.Second // 400ms each

	issues := dbhealth.Evaluate(prev, cur, th, now())
	got := has(issues, "pool")
	if got == nil {
		t.Fatalf("fifty requests queueing for 400ms each produced %v", keys(issues))
	}
	if !strings.Contains(got.Message, "400ms") {
		t.Errorf("the message should carry the average wait, got %q", got.Message)
	}
}

// Many very short waits are a pool doing its job, not one that has run out.
func TestManyBriefWaitsAreNotSaturation(t *testing.T) {
	prev := healthy()
	cur := healthy()
	cur.Pool.WaitCount = 5000
	cur.Pool.WaitDuration = 5 * time.Millisecond // a microsecond each

	if issues := dbhealth.Evaluate(prev, cur, th, now()); has(issues, "pool") != nil {
		t.Fatalf("waits of a microsecond were called saturation: %v", keys(issues))
	}
}

// WaitCount is cumulative since the process started. Reading it as a rate on
// the first look would announce, at every restart, a saturation that ended
// hours ago.
func TestTheFirstLookDoesNotReportHistoryAsNews(t *testing.T) {
	cur := healthy()
	cur.Pool.WaitCount = 900_000
	cur.Pool.WaitDuration = 40 * time.Hour

	if issues := dbhealth.Evaluate(nil, cur, th, now()); has(issues, "pool") != nil {
		t.Fatalf("the counters accumulated over the life of the process were announced "+
			"as something that just happened: %v", keys(issues))
	}
}

// But the first look must still report what is true right now.
func TestTheFirstLookStillReportsWhatIsTrueNow(t *testing.T) {
	cur := healthy()
	cur.Jobs[0].LastRunStatus = "Failed"

	if has(dbhealth.Evaluate(nil, cur, th, now()), "job:1000") == nil {
		t.Fatal("a job that is failing right now was not reported on the first look")
	}
}

// ---------------------------------------------------------------------------
// Long-running queries
// ---------------------------------------------------------------------------

func TestAQueryThatHasBeenRunningForHoursIsReported(t *testing.T) {
	cur := healthy()
	cur.SlowQueries = []dbhealth.SlowQuery{
		{PID: 4242, Running: 3 * time.Hour, Query: "SELECT * FROM tag_history"},
	}

	issues := dbhealth.Evaluate(healthy(), cur, th, now())
	got := has(issues, "slow-query:4242")
	if got == nil {
		t.Fatalf("a three-hour query produced %v", keys(issues))
	}
	if !strings.Contains(got.Message, "tag_history") {
		t.Errorf("the message should carry the statement, got %q", got.Message)
	}
}

func TestANormalQueryIsNotReported(t *testing.T) {
	cur := healthy()
	cur.SlowQueries = []dbhealth.SlowQuery{
		{PID: 4242, Running: 2 * time.Second, Query: "SELECT 1"},
	}
	if issues := dbhealth.Evaluate(healthy(), cur, th, now()); len(issues) != 0 {
		t.Fatalf("a two-second query produced %v", keys(issues))
	}
}

// The key carries the backend PID, so one stuck statement is one issue however
// many times it is looked at, and a different one is a different issue.
func TestEachStuckStatementIsItsOwnIssue(t *testing.T) {
	cur := healthy()
	cur.SlowQueries = []dbhealth.SlowQuery{
		{PID: 1, Running: time.Hour, Query: "A"},
		{PID: 2, Running: time.Hour, Query: "B"},
	}
	issues := dbhealth.Evaluate(healthy(), cur, th, now())
	if len(issues) != 2 {
		t.Fatalf("two stuck statements produced %v", keys(issues))
	}
}

// A very long statement must not be pasted whole into a message somebody reads
// on a phone.
func TestAHugeStatementIsTrimmed(t *testing.T) {
	cur := healthy()
	cur.SlowQueries = []dbhealth.SlowQuery{
		{PID: 1, Running: time.Hour, Query: strings.Repeat("x", 10_000)},
	}
	got := has(dbhealth.Evaluate(healthy(), cur, th, now()), "slow-query:1")
	if got == nil {
		t.Fatal("not reported")
	}
	if len(got.Message) > 500 {
		t.Errorf("the message is %d characters long", len(got.Message))
	}
}
