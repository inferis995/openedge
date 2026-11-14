package handlers

import (
	"context"
	"database/sql"
	"math"
	"os"
	"testing"
	"time"
)

var availEnd = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := availEnd.Add(-d); return &t }

// A tag that last reported within twice its usual gap still holds its state:
// a datalogger is not down in the hours between two sends.
func TestValuesAvailabilityGraceForSlowReporters(t *testing.T) {
	start := availEnd.Add(-time.Hour)
	tags := []bandTagWindow{
		// Every 30 min, last in band 40 min ago: within 60 min.
		{LastBefore: at(40 * time.Minute), LastInBand: true, MedianGap: 30 * time.Minute},
		// Every 8 h, last in band 5 h ago.
		{LastBefore: at(5 * time.Hour), LastInBand: true, MedianGap: 8 * time.Hour},
	}
	a, src, down := valuesAvailability(tags, start, availEnd)
	if a != 100 || src != "values" || down != 0 {
		t.Errorf("availability %.1f %s down %.1f, want 100", a, src, down)
	}
	// The same tag holding an out-of-band reading is down.
	tags[0].LastInBand = false
	if a, _, _ := valuesAvailability(tags, start, availEnd); a != 50 {
		t.Errorf("one of two held out of band: %.1f, want 50", a)
	}
}

// Past its grace, silence is downtime again.
func TestValuesAvailabilityOverdueSilenceIsDowntime(t *testing.T) {
	start := availEnd.Add(-time.Hour)
	tags := []bandTagWindow{
		{Readings: 4, InBand: 4},
		// Every 30 min, but nothing for 3 h.
		{LastBefore: at(3 * time.Hour), LastInBand: true, MedianGap: 30 * time.Minute},
	}
	if a, _, _ := valuesAvailability(tags, start, availEnd); a != 50 {
		t.Errorf("availability %.1f, want 50 (the silent tag at the others' rate)", a)
	}
	// No gap known (one reading ever): no grace.
	tags[1].MedianGap = 0
	tags[1].LastBefore = at(10 * time.Minute)
	if a, _, _ := valuesAvailability(tags, start, availEnd); a != 50 {
		t.Errorf("unknown gap: %.1f, want 50", a)
	}
	if a, src, _ := valuesAvailability([]bandTagWindow{{}}, start, availEnd); a != 0 || src != "values" {
		t.Errorf("every tag silent: %.1f %s, want 0", a, src)
	}
	if a, src, _ := valuesAvailability(nil, start, availEnd); a != 100 || src != "no-data" {
		t.Errorf("no banded tag: %.1f %s", a, src)
	}
}

// Against a real Postgres (OPENEDGE_TEST_DSN, wiped): a pressure point that
// reports every 30 minutes, always in band, is available every hour of the
// day — including hours whose one reading fell just outside.
func TestValuesAvailabilityThirtyMinuteReporterAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("OPENEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("OPENEDGE_TEST_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	for _, q := range []string{
		`DROP TABLE IF EXISTS tag_bands, tag_forecasts, forecast_profiles, tag_history, tags, gateways, areas, sites, users, organizations CASCADE`,
		`CREATE TABLE organizations (id SERIAL PRIMARY KEY, name TEXT)`,
		`CREATE TABLE sites (id SERIAL PRIMARY KEY, org_id INT REFERENCES organizations(id), name TEXT)`,
		`CREATE TABLE areas (id SERIAL PRIMARY KEY, site_id INT REFERENCES sites(id), name TEXT)`,
		`CREATE TABLE gateways (id SERIAL PRIMARY KEY, area_id INT REFERENCES areas(id), name TEXT)`,
		`CREATE TABLE tags (id SERIAL PRIMARY KEY, gateway_id INT REFERENCES gateways(id), alias TEXT)`,
		`CREATE TABLE tag_bands (tag_id INT PRIMARY KEY REFERENCES tags(id), band_low FLOAT8, band_high FLOAT8)`,
		`CREATE TABLE tag_history (time TIMESTAMPTZ NOT NULL, tag_id INT REFERENCES tags(id), value FLOAT8)`,
		`INSERT INTO organizations (name) VALUES ('Acque')`,
		`INSERT INTO sites (org_id, name) VALUES (1, 'S')`,
		`INSERT INTO areas (site_id, name) VALUES (1, 'A')`,
		`INSERT INTO gateways (area_id, name) VALUES (1, 'G')`,
		`INSERT INTO tags (gateway_id, alias) VALUES (1, 'Pressione'), (1, 'Pressione 8h')`,
		`INSERT INTO tag_bands VALUES (1, 2, 6), (2, 2, 6)`,
		// Tag 1 every 30 min with some jitter, tag 2 every 8 h, both in band,
		// for three days up to the end of the test day.
		`INSERT INTO tag_history (time, tag_id, value)
		 SELECT ts + (CASE WHEN extract(hour FROM ts)::int % 5 = 0 THEN interval '29 min' ELSE interval '0' END), 1, 4
		 FROM generate_series('2026-11-11T00:10:00Z'::timestamptz, '2026-11-14T23:40:00Z', interval '30 min') ts`,
		`INSERT INTO tag_history (time, tag_id, value)
		 SELECT ts, 2, 3.5 FROM generate_series('2026-11-11T03:00:00Z'::timestamptz, '2026-11-14T23:00:00Z', interval '8 hours') ts`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	h := &OEEHandler{db: db}
	cfg := oeeConfig{OrgID: 1, AreaID: 1, WindowMin: 60}
	day := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	for hr := 1; hr <= 24; hr++ {
		end := day.Add(time.Duration(hr) * time.Hour)
		a, src, _ := h.computeAvailability(ctx, end.Add(-time.Hour), end, &cfg)
		if math.Abs(a-100) > 0.01 || src != "values" {
			t.Errorf("hour ending %s: availability %.1f (%s), want 100", end.Format("15:04"), a, src)
		}
	}
	// After four hours of silence the 30-minute tag is down, the 8-hour one
	// (last at 23:00) still holds.
	end := time.Date(2026, 11, 15, 3, 50, 0, 0, time.UTC)
	if a, _, _ := h.computeAvailability(ctx, end.Add(-time.Hour), end, &cfg); a >= 100 || a <= 0 {
		t.Errorf("overdue tag: availability %.1f, want between 0 and 100", a)
	}
}
