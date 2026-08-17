//go:build e2e

package e2e

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// What these tests defend.
//
// migrations/20250308_schema.sql declared add_compression_policy and
// add_retention_policy on tag_history. Nothing ever executed that file — the
// hypertables are created by EnsureTimescaleDBStructures, which called
// create_hypertable and stopped there. So on every real install the historian
// was an uncompressed hypertable with no retention, and the only thing ageing
// it was a Go worker issuing DELETE once a day: a statement that rewrites rows,
// leaves dead tuples in every chunk, and hands no space back to the filesystem.
//
// None of that fails a test or a demo. It fails a plant, months in, when the
// disk fills and deleting data does not free it.
//
// Asserting the policies exist in the database is the only check that means
// anything here: the Go code "calling add_retention_policy" is exactly what the
// unexecuted SQL file also appeared to do.

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	// The cloud overlay strips Postgres' host port — a database on the public
	// internet is the thing that overlay exists to prevent — so these tests run
	// in the direct job, against the same image and the same migrations.
	requireDirectAccess(t, "Postgres")

	dsn := os.Getenv("E2E_DB_DSN")
	if dsn == "" {
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			env("E2E_DB_HOST", "127.0.0.1"),
			env("E2E_DB_PORT", "5432"),
			env("E2E_DB_USER", "industrial_user"),
			env("E2E_DB_PASSWORD", os.Getenv("POSTGRES_PASSWORD")),
			env("E2E_DB_NAME", "industrial_edge"),
		)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	db.SetConnMaxLifetime(time.Minute)
	if err := db.Ping(); err != nil {
		t.Fatalf("connecting to the database: %v\n"+
			"Set E2E_DB_DSN, or POSTGRES_PASSWORD to match the running stack.", err)
	}
	return db
}

// retentionInterval returns the drop_after interval of the retention policy on
// a hypertable, or "" when no policy exists.
func retentionInterval(t *testing.T, db *sql.DB, table string) string {
	t.Helper()

	// The column carrying the window moved between TimescaleDB versions
	// (drop_after in 2.x, config->>'drop_after' in the jobs view). Reading the
	// jobs config is the form that holds across both.
	var iv sql.NullString
	err := db.QueryRow(`
		SELECT j.config->>'drop_after'
		FROM timescaledb_information.jobs j
		WHERE j.proc_name = 'policy_retention'
		  AND j.hypertable_name = $1
		LIMIT 1`, table).Scan(&iv)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("reading the retention policy for %s: %v", table, err)
	}
	if !iv.Valid {
		return ""
	}
	return iv.String
}

// The historian must have a retention policy, or it grows until the disk stops
// the plant.
func TestHistorianHasARetentionPolicy(t *testing.T) {
	db := openDB(t)

	if iv := retentionInterval(t, db, "tag_history"); iv == "" {
		t.Fatal("tag_history has no TimescaleDB retention policy — the historian grows " +
			"without bound and ageing falls back to DELETE, which never returns disk space")
	} else {
		t.Logf("tag_history retention: %s", iv)
	}
}

// The policy must match the CONFIGURED window, not merely exist.
//
// This is the test that would have caught the defect. Both mechanisms were
// present and neither was broken on its own: initdb installed a 90-day policy
// from the schema file, the application seeded historian_retention_days = 365
// and showed that in the UI. The database dropped chunks at 90 days while every
// operator-facing surface said a year, and nothing reported the difference —
// the worker's DELETE at 365 days returned zero rows, which is also what a
// correctly-aged table returns.
//
// Asserting a policy exists would have passed throughout. Only comparing it to
// the setting fails.
func TestRetentionMatchesTheConfiguredWindow(t *testing.T) {
	db := openDB(t)

	var configured int
	if err := db.QueryRow(`
		SELECT value::int FROM global_settings
		WHERE key = 'historian_retention_days'`).Scan(&configured); err != nil {
		t.Fatalf("reading historian_retention_days: %v", err)
	}
	if configured <= 0 {
		t.Skipf("retention disabled in settings (%d days) — nothing to compare", configured)
	}

	iv := retentionInterval(t, db, "tag_history")
	if iv == "" {
		t.Fatalf("historian_retention_days is %d but tag_history has no retention policy", configured)
	}

	// Compare as intervals in the database rather than parsing Postgres's
	// interval text in Go, where '3 mons' and '90 days' are equal to Postgres
	// and unequal to strings.Compare.
	var equal bool
	if err := db.QueryRow(
		`SELECT $1::interval = make_interval(days => $2)`, iv, configured).Scan(&equal); err != nil {
		t.Fatalf("comparing intervals: %v", err)
	}
	if !equal {
		t.Fatalf("tag_history is aged at %s but historian_retention_days says %d days — "+
			"the database is silently discarding data the operator believes is retained",
			iv, configured)
	}
	t.Logf("retention policy and setting agree: %d days", configured)
}

// Alarm and event history age too. These were hypertables that nothing at all
// cleaned up: the Go worker only ever touched tag_history.
func TestEventHypertablesHaveRetentionPolicies(t *testing.T) {
	db := openDB(t)

	for _, table := range []string{"alarm_events", "system_events"} {
		if iv := retentionInterval(t, db, table); iv == "" {
			t.Errorf("%s has no retention policy — before this change nothing aged it at all, "+
				"since the cleanup worker only handled tag_history", table)
		} else {
			t.Logf("%s retention: %s", table, iv)
		}
	}
}

// Compression is where the storage actually goes. Without it the historian
// occupies several times what it needs for append-only numeric series.
func TestHistorianIsCompressed(t *testing.T) {
	db := openDB(t)

	var enabled sql.NullBool
	err := db.QueryRow(`
		SELECT compression_enabled
		FROM timescaledb_information.hypertables
		WHERE hypertable_name = 'tag_history'`).Scan(&enabled)
	if err != nil {
		t.Fatalf("reading hypertable metadata for tag_history: %v", err)
	}
	if !enabled.Valid || !enabled.Bool {
		t.Fatal("compression is not enabled on tag_history")
	}

	var jobs int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_compression' AND hypertable_name = 'tag_history'`).Scan(&jobs); err != nil {
		t.Fatalf("reading compression jobs: %v", err)
	}
	if jobs == 0 {
		t.Fatal("compression is enabled on tag_history but no compression POLICY exists — " +
			"nothing will ever actually compress a chunk")
	}
}

// Segmenting by tag_id is what makes a single-tag trend query read one
// compressed batch instead of decompressing a whole chunk. Getting compression
// on but segmenting wrongly is a performance trap that only appears once the
// first chunks compress, a week after install.
func TestCompressionIsSegmentedByTag(t *testing.T) {
	db := openDB(t)

	var segmentBy sql.NullString
	err := db.QueryRow(`
		SELECT attname
		FROM timescaledb_information.compression_settings
		WHERE hypertable_name = 'tag_history' AND segmentby_column_index IS NOT NULL
		LIMIT 1`).Scan(&segmentBy)
	if err == sql.ErrNoRows {
		t.Fatal("tag_history compression has no segmentby column — every single-tag query " +
			"will decompress entire chunks")
	}
	if err != nil {
		t.Fatalf("reading compression settings: %v", err)
	}
	if segmentBy.String != "tag_id" {
		t.Fatalf("tag_history is segmented by %q, expected tag_id", segmentBy.String)
	}
}
