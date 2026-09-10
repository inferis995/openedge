package dbhealth

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"
)

// collectTimeout bounds every query this package runs. A health check that
// blocks on a sick database is the one thing worse than no health check: it
// holds a connection from the very pool it is supposed to be reporting on.
const collectTimeout = 5 * time.Second

// Collect takes one look at the database.
//
// A failure to read one part is logged and left empty rather than failing the
// whole snapshot: a Postgres that will not answer a question about background
// jobs is exactly the Postgres whose connection pool is worth reporting on.
func Collect(ctx context.Context, db *sql.DB) Snapshot {
	stats := db.Stats()
	snap := Snapshot{Pool: Pool{
		MaxOpen:      stats.MaxOpenConnections,
		InUse:        stats.InUse,
		WaitCount:    stats.WaitCount,
		WaitDuration: stats.WaitDuration,
	}}

	jobs, err := collectJobs(ctx, db)
	if err != nil {
		log.Printf("[DB-HEALTH] could not read the background jobs: %v", err)
	} else {
		snap.Jobs = jobs
	}

	slow, err := collectSlowQueries(ctx, db)
	if err != nil {
		log.Printf("[DB-HEALTH] could not read the running statements: %v", err)
	} else {
		snap.SlowQueries = slow
	}

	return snap
}

// collectJobs reads the TimescaleDB background jobs.
//
// The view is absent when the extension is not installed, which is a legitimate
// configuration (Postgres without TimescaleDB), so the error is reported to the
// caller and swallowed there rather than being treated as a fault.
func collectJobs(ctx context.Context, db *sql.DB) ([]Job, error) {
	ctx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT j.job_id,
		       COALESCE(j.application_name, 'job ' || j.job_id::text),
		       COALESCE(s.last_run_status, ''),
		       COALESCE(s.total_failures, 0),
		       s.last_successful_finish
		FROM timescaledb_information.jobs j
		LEFT JOIN timescaledb_information.job_stats s ON s.job_id = j.job_id
		WHERE j.job_id >= 1000`) // below 1000 are TimescaleDB's own internal jobs
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Job
	for rows.Next() {
		var j Job
		var lastSuccess sql.NullTime
		if scanErr := rows.Scan(&j.ID, &j.Name, &j.LastRunStatus, &j.TotalFailures, &lastSuccess); scanErr != nil {
			log.Printf("[DB-HEALTH] skipping a job row: %v", scanErr)
			continue
		}
		if lastSuccess.Valid {
			// TimescaleDB reports -infinity for a job that has never finished
			// successfully. Left as it comes, that is a timestamp in the year
			// -4713 and every fresh policy is instantly "stale for 6 million
			// years"; as a zero time it means what it is, which is "never".
			if lastSuccess.Time.Year() > 1 {
				j.LastSuccess = lastSuccess.Time
			}
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// collectSlowQueries reads the statements that are currently running.
//
// It excludes this backend and the background workers: autovacuum on a large
// hypertable legitimately runs for hours, and reporting it would train the
// operator to ignore the whole check.
func collectSlowQueries(ctx context.Context, db *sql.DB) ([]SlowQuery, error) {
	ctx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT pid,
		       EXTRACT(EPOCH FROM (NOW() - query_start))::bigint,
		       query
		FROM pg_stat_activity
		WHERE state = 'active'
		  AND pid <> pg_backend_pid()
		  AND backend_type = 'client backend'
		  AND query_start IS NOT NULL
		ORDER BY query_start
		LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []SlowQuery
	for rows.Next() {
		var q SlowQuery
		var seconds int64
		if scanErr := rows.Scan(&q.PID, &seconds, &q.Query); scanErr != nil {
			log.Printf("[DB-HEALTH] skipping an activity row: %v", scanErr)
			continue
		}
		q.Running = time.Duration(seconds) * time.Second
		q.Query = strings.Join(strings.Fields(q.Query), " ")
		out = append(out, q)
	}
	return out, rows.Err()
}
