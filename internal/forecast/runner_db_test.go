package forecast

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// The runner against a real Postgres. Set OPENEDGE_TEST_DSN to a database
// that may be wiped, e.g.
//
//	OPENEDGE_TEST_DSN='postgres://test@127.0.0.1:55432/postgres?sslmode=disable'
//
// It builds the few tables it needs itself and uses no TimescaleDB, so it also
// covers the path without continuous aggregates.
func testDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("OPENEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("OPENEDGE_TEST_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range schema {
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

var schema = []string{
	`DROP TABLE IF EXISTS alarm_definitions, tag_forecasts, forecast_profiles, tag_history, tags, gateways, areas, sites, users, organizations CASCADE`,
	`CREATE TABLE organizations (id SERIAL PRIMARY KEY, name TEXT)`,
	`CREATE TABLE users (id SERIAL PRIMARY KEY)`,
	`CREATE TABLE sites (id SERIAL PRIMARY KEY, org_id INT REFERENCES organizations(id), name TEXT)`,
	`CREATE TABLE areas (id SERIAL PRIMARY KEY, site_id INT REFERENCES sites(id), name TEXT)`,
	`CREATE TABLE gateways (id SERIAL PRIMARY KEY, area_id INT REFERENCES areas(id), name TEXT)`,
	`CREATE TABLE tags (id SERIAL PRIMARY KEY, gateway_id INT REFERENCES gateways(id) ON DELETE CASCADE,
		alias TEXT, data_type TEXT, historize BOOLEAN)`,
	`CREATE TABLE tag_history (time TIMESTAMPTZ NOT NULL, tag_id INT REFERENCES tags(id) ON DELETE CASCADE, value FLOAT8)`,
	// As in internal/db/db.go.
	`CREATE TABLE forecast_profiles (
		id SERIAL PRIMARY KEY,
		org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		scope TEXT NOT NULL CHECK (scope IN ('org','site','area','gateway','tag')),
		scope_id INT NOT NULL, settings JSONB NOT NULL DEFAULT '{}',
		updated_by INT REFERENCES users(id) ON DELETE SET NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (scope, scope_id))`,
	`CREATE TABLE tag_forecasts (
		tag_id INT PRIMARY KEY REFERENCES tags(id) ON DELETE CASCADE,
		status TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
		engine TEXT NOT NULL DEFAULT '', step_seconds INT NOT NULL DEFAULT 0, start_at TIMESTAMPTZ,
		quantiles JSONB, skill DOUBLE PRECISION, coverage DOUBLE PRECISION,
		details JSONB NOT NULL DEFAULT '{}', generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		next_run_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		risks JSONB NOT NULL DEFAULT '[]', anomaly JSONB)`,
	`CREATE TABLE alarm_definitions (id SERIAL PRIMARY KEY, tag_id INT REFERENCES tags(id) ON DELETE CASCADE,
		alarm_type TEXT, threshold DOUBLE PRECISION, severity TEXT, message TEXT, enabled BOOLEAN DEFAULT TRUE)`,
	`INSERT INTO organizations (name) VALUES ('A')`,
	`INSERT INTO sites (org_id, name) VALUES (1, 'S')`,
	`INSERT INTO areas (site_id, name) VALUES (1, 'Ar')`,
	`INSERT INTO gateways (area_id, name) VALUES (1, 'G1'), (1, 'G2')`,
	`INSERT INTO tags (gateway_id, alias, data_type, historize) VALUES
		(1, 'cycle',   'REAL', true),   -- 1: three weeks of a daily cycle
		(1, 'young',   'REAL', true),   -- 2: five values
		(1, 'running', 'BOOL', true),   -- 3: on in the day
		(1, 'notkept', 'REAL', false),  -- 4: not historized: never forecast
		(2, 'off',     'REAL', true)    -- 5: on a gateway switched off
	`,
	// In the 12 h after noon the daily cycle falls to 7 at 18:00: a low limit
	// at 7.5 is crossed this evening, one at 4 never.
	`INSERT INTO alarm_definitions (tag_id, alarm_type, threshold, severity, message) VALUES
		(1, 'low', 7.5, 'warning', 'cycle low'), (1, 'low', 4, 'critical', 'never')`,
	`INSERT INTO forecast_profiles (org_id, scope, scope_id, settings)
		VALUES (1, 'gateway', 2, '{"enabled": false}'), (1, 'org', 1, '{"horizon_hours": 12}')`,
}

var dbNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// seed writes three weeks every 15 minutes for tags 1, 3, 4, 5 and five
// hourly values for tag 2.
func seed(t *testing.T, db *sql.DB) {
	ctx := context.Background()
	ins, err := db.PrepareContext(ctx, `INSERT INTO tag_history (time, tag_id, value) VALUES ($1, $2, $3)`)
	if err != nil {
		t.Fatal(err)
	}
	defer ins.Close()
	put := func(ts time.Time, tag int, v float64) {
		if _, err := ins.ExecContext(ctx, ts, tag, v); err != nil {
			t.Fatal(err)
		}
	}
	for ts := dbNow.Add(-21 * 24 * time.Hour); ts.Before(dbNow); ts = ts.Add(15 * time.Minute) {
		h := float64(ts.Hour()) + float64(ts.Minute())/60
		put(ts, 1, 10+3*math.Sin(2*math.Pi*h/24))
		running := 0.0
		if ts.Hour() >= 8 && ts.Hour() < 18 {
			running = 1
		}
		put(ts, 3, running)
		put(ts, 4, 1)
		put(ts, 5, 2)
	}
	for i := 0; i < 5; i++ {
		put(dbNow.Add(-time.Duration(i)*time.Hour), 2, float64(i))
	}
}

type storedForecast struct {
	status, kind, engine string
	step                 int
	start                sql.NullTime
	q                    []byte
	next                 time.Time
}

func stored(t *testing.T, db *sql.DB, tag int) storedForecast {
	t.Helper()
	var x storedForecast
	if err := db.QueryRowContext(context.Background(), `
		SELECT status, kind, engine, step_seconds, start_at, quantiles, next_run_at
		FROM tag_forecasts WHERE tag_id = $1`, tag).Scan(
		&x.status, &x.kind, &x.engine, &x.step, &x.start, &x.q, &x.next); err != nil {
		t.Fatalf("tag %d: %v", tag, err)
	}
	return x
}

func TestRunnerAgainstPostgres(t *testing.T) {
	db := testDB(t)
	seed(t, db)
	r := NewRunner(db, "")
	r.Now = func() time.Time { return dbNow }
	var notices []RiskNotice
	r.OnRisk = func(n RiskNotice) { notices = append(notices, n) }
	n, err := r.RunDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("%d tags forecast, want 4 (not the one that is not historized)", n)
	}

	t.Run("daily cycle", func(t *testing.T) {
		cyc := stored(t, db, 1)
		if cyc.status != StatusOK || cyc.kind != string(KindCyclic) || cyc.engine != "statistical" {
			t.Errorf("%+v", cyc)
		}
		if cyc.step != 900 {
			t.Errorf("step %ds, want 900 (measured every 15 min)", cyc.step)
		}
		var q Quantiles
		if err := json.Unmarshal(cyc.q, &q); err != nil {
			t.Fatal(err)
		}
		if len(q.Q50) != 12*4 {
			t.Errorf("%d steps, want 48 (12 h from the org profile at 15 min)", len(q.Q50))
		}
		if !cyc.start.Valid || !cyc.start.Time.Equal(dbNow) {
			t.Errorf("starts %v, want %v", cyc.start.Time, dbNow)
		}
		if !cyc.next.Equal(dbNow.Add(time.Hour)) {
			t.Errorf("next run %v, want in 1 h (4 steps)", cyc.next)
		}
	})

	t.Run("statuses", func(t *testing.T) {
		if y := stored(t, db, 2); y.status != StatusLearning {
			t.Errorf("young: %+v", y)
		}
		if s := stored(t, db, 3); s.kind != string(KindState) {
			t.Errorf("running: %+v", s)
		}
		if o := stored(t, db, 5); o.status != StatusDisabled {
			t.Errorf("off: %+v", o)
		}
		var none int
		_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tag_forecasts WHERE tag_id = 4`).Scan(&none)
		if none != 0 {
			t.Error("a tag that is not historized was forecast")
		}
	})

	t.Run("predicted alarm", func(t *testing.T) {
		var raw []byte
		if err := db.QueryRowContext(context.Background(), `SELECT risks FROM tag_forecasts WHERE tag_id = 1`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var risks []Risk
		_ = json.Unmarshal(raw, &risks)
		if len(risks) != 1 || risks[0].Value != 7.5 || risks[0].Likelihood == "" {
			t.Fatalf("risks %s: the 7.5 limit, not the 4 one", raw)
		}
		if h := risks[0].At.Sub(dbNow).Hours(); h < 3 || h > 7 {
			t.Errorf("crossed %.1f h from now, want this evening", h)
		}
		if len(notices) != 1 || notices[0].TagID != 1 || notices[0].Alias != "cycle" || notices[0].OrgID != 1 {
			t.Errorf("notices %+v", notices)
		}
	})

	t.Run("scheduling", func(t *testing.T) { scheduling(t, db, r) })
}

func scheduling(t *testing.T, db *sql.DB, r *Runner) {
	ctx := context.Background()
	// Nothing is due until next_run_at.
	if n, err := r.RunDue(ctx); err != nil || n != 0 {
		t.Errorf("second run: %d tags, %v", n, err)
	}
	// RunTags does it anyway.
	if err := r.RunTags(ctx, []int{1}); err != nil {
		t.Fatal(err)
	}
	// Another replica holding the lock: this one does nothing.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey) }()
	if _, err := db.ExecContext(ctx, `UPDATE tag_forecasts SET next_run_at = $1`, dbNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n, err := r.RunDue(ctx); err != nil || n != 0 {
		t.Errorf("while locked: %d tags, %v", n, err)
	}
}
