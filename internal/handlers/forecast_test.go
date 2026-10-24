package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/forecast"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// forecastCall runs one handler as a user of org (0 = global admin).
func forecastCall(t *testing.T, h gin.HandlerFunc, method, target string, params gin.Params, org int, body any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(context.Background(), method, target, &buf)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	if org == 0 {
		c.Set(middleware.UserKey, jwt.MapClaims{"role": "admin", "user_id": float64(1)})
	} else {
		c.Set(middleware.UserKey, jwt.MapClaims{"role": "admin", "org_id": float64(org), "user_id": float64(1)})
		c.Set(middleware.ContextKeyOrganizationID, org)
	}
	h(c)
	return w
}

func TestForecastRequestsAreCheckedBeforeTheDatabase(t *testing.T) {
	h := NewForecastHandler(nil, nil) // no database: each case must stop first
	if w := forecastCall(t, h.GetTagForecast, http.MethodGet, "/api/tags/x/forecast",
		gin.Params{{Key: "id", Value: "x"}}, 5, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad tag id: %d", w.Code)
	}
	if w := forecastCall(t, h.GetProfile, http.MethodGet, "/api/forecast/profiles?scope=plant&scope_id=1",
		nil, 5, nil); w.Code != http.StatusBadRequest {
		t.Errorf("unknown scope: %d", w.Code)
	}
	bad := map[string]any{"scope": "tag", "scope_id": 1, "settings": map[string]any{"kind": "magic", "horizon_hours": 0}}
	if w := forecastCall(t, h.PutProfile, http.MethodPut, "/api/forecast/profiles", nil, 5, bad); w.Code != http.StatusBadRequest {
		t.Errorf("invalid settings: %d", w.Code)
	}
}

// Against a real Postgres (OPENEDGE_TEST_DSN, wiped): two organizations, a tag
// each, a stored forecast for the first.
func forecastDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("OPENEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("OPENEDGE_TEST_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`DROP TABLE IF EXISTS tag_forecasts, forecast_profiles, tag_history, tags, gateways, areas, sites, users, organizations CASCADE`,
		`CREATE TABLE organizations (id SERIAL PRIMARY KEY, name TEXT)`,
		`CREATE TABLE users (id SERIAL PRIMARY KEY)`,
		`CREATE TABLE sites (id SERIAL PRIMARY KEY, org_id INT REFERENCES organizations(id), name TEXT)`,
		`CREATE TABLE areas (id SERIAL PRIMARY KEY, site_id INT REFERENCES sites(id), name TEXT)`,
		`CREATE TABLE gateways (id SERIAL PRIMARY KEY, area_id INT REFERENCES areas(id), name TEXT)`,
		`CREATE TABLE tags (id SERIAL PRIMARY KEY, gateway_id INT REFERENCES gateways(id), alias TEXT, data_type TEXT, historize BOOLEAN, eu_unit TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE forecast_profiles (id SERIAL PRIMARY KEY, org_id INT NOT NULL REFERENCES organizations(id),
			scope TEXT NOT NULL, scope_id INT NOT NULL, settings JSONB NOT NULL DEFAULT '{}',
			updated_by INT REFERENCES users(id), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (scope, scope_id))`,
		`CREATE TABLE tag_forecasts (tag_id INT PRIMARY KEY REFERENCES tags(id), status TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', engine TEXT NOT NULL DEFAULT '',
			step_seconds INT NOT NULL DEFAULT 0, start_at TIMESTAMPTZ, quantiles JSONB, skill DOUBLE PRECISION,
			coverage DOUBLE PRECISION, details JSONB NOT NULL DEFAULT '{}',
			generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), next_run_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			risks JSONB NOT NULL DEFAULT '[]', anomaly JSONB)`,
		`INSERT INTO users DEFAULT VALUES`,
		`INSERT INTO organizations (name) VALUES ('A'), ('B')`,
		`INSERT INTO sites (org_id, name) VALUES (1, 'SA'), (2, 'SB')`,
		`INSERT INTO areas (site_id, name) VALUES (1, 'AA'), (2, 'AB')`,
		`INSERT INTO gateways (area_id, name) VALUES (1, 'GA'), (2, 'GB')`,
		`INSERT INTO tags (gateway_id, alias, data_type, historize) VALUES (1, 'ta', 'REAL', true), (2, 'tb', 'REAL', true)`,
		`INSERT INTO tag_forecasts (tag_id, status, kind, engine, step_seconds, start_at, quantiles, skill, coverage, details, next_run_at)
		 VALUES (1, 'ok', 'cyclic', 'timestral', 3600, '2026-10-22T12:00:00Z',
		         '{"q10":[1,null],"q50":[2,null],"q90":[3,null]}', 0.4, 0.8,
		         '{"model":"mistralai/Timestral-28M"}', '2099-01-01')`,
	} {
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

func TestTagForecastAPI(t *testing.T) {
	db := forecastDB(t)
	h := NewForecastHandler(db, nil)
	tag := func(id string) gin.Params { return gin.Params{{Key: "id", Value: id}} }

	w := forecastCall(t, h.GetTagForecast, http.MethodGet, "/api/tags/1/forecast", tag("1"), 1, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("own tag: %d %s", w.Code, w.Body)
	}
	var got TagForecastResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || got.Engine != "timestral" || got.Model != "mistralai/Timestral-28M" || len(got.Points) != 2 {
		t.Fatalf("forecast %+v", got)
	}
	if !got.Points[1].T.Equal(time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)) || got.Points[1].Q50 != nil || *got.Points[0].Q90 != 3 {
		t.Errorf("points %+v: one per step, null where there is no forecast", got.Points)
	}
	if got.Settings != forecast.Defaults {
		t.Errorf("settings %+v, want the defaults (no profile)", got.Settings)
	}

	if w := forecastCall(t, h.GetTagForecast, http.MethodGet, "/api/tags/2/forecast", tag("2"), 1, nil); w.Code != http.StatusNotFound {
		t.Errorf("another organization's tag: %d, want 404", w.Code)
	}
	if w := forecastCall(t, h.GetTagForecast, http.MethodGet, "/api/tags/2/forecast", tag("2"), 0, nil); w.Code != http.StatusOK ||
		!bytes.Contains(w.Body.Bytes(), []byte(`"status":"pending"`)) {
		t.Errorf("global admin, never forecast: %d %s", w.Code, w.Body)
	}
	if w := forecastCall(t, h.RefreshTagForecast, http.MethodPost, "/api/tags/1/forecast/refresh", tag("1"), 1, nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("refresh with forecasting off: %d", w.Code)
	}
}

func TestForecastProfileAPI(t *testing.T) {
	db := forecastDB(t)
	h := NewForecastHandler(db, nil)
	put := func(org int, body any) *httptest.ResponseRecorder {
		return forecastCall(t, h.PutProfile, http.MethodPut, "/api/forecast/profiles", nil, org, body)
	}

	if w := put(1, map[string]any{"scope": "site", "scope_id": 1, "settings": map[string]any{"horizon_hours": 72}}); w.Code != http.StatusOK {
		t.Fatalf("site profile: %d %s", w.Code, w.Body)
	}
	w := put(1, map[string]any{"scope": "tag", "scope_id": 1, "settings": map[string]any{"kind": "degradation"}})
	if w.Code != http.StatusOK {
		t.Fatalf("tag profile: %d %s", w.Code, w.Body)
	}
	var p ProfileResponse
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Inherited.HorizonHours != 72 || p.Inherited.Kind != forecast.KindAuto {
		t.Errorf("inherited %+v: 72 h from the site, kind not yet set", p.Inherited)
	}
	if p.Effective.HorizonHours != 72 || p.Effective.Kind != forecast.KindDegradation {
		t.Errorf("effective %+v", p.Effective)
	}
	var next time.Time
	_ = db.QueryRowContext(context.Background(), `SELECT next_run_at FROM tag_forecasts WHERE tag_id = 1`).Scan(&next)
	if next.Year() == 2099 {
		t.Error("the tag was not rescheduled after its profile changed")
	}

	// Another organization's entities are out of reach, for reading and writing.
	if w := put(1, map[string]any{"scope": "site", "scope_id": 2, "settings": map[string]any{"enabled": false}}); w.Code != http.StatusNotFound {
		t.Errorf("another organization's site: %d, want 404", w.Code)
	}
	if w := forecastCall(t, h.GetProfile, http.MethodGet, "/api/forecast/profiles?scope=tag&scope_id=2", nil, 1, nil); w.Code != http.StatusNotFound {
		t.Errorf("reading another organization's tag: %d, want 404", w.Code)
	}

	// Empty settings: back to inheriting everything.
	if w := put(1, map[string]any{"scope": "tag", "scope_id": 1, "settings": map[string]any{}}); w.Code != http.StatusOK {
		t.Fatalf("clear: %d", w.Code)
	}
	var n int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM forecast_profiles WHERE scope = 'tag'`).Scan(&n)
	if n != 0 {
		t.Error("an emptied profile should be removed")
	}
}

func TestForecastOverview(t *testing.T) {
	db := forecastDB(t)
	h := NewForecastHandler(db, nil)

	w := forecastCall(t, h.Overview, http.MethodGet, "/api/forecast/overview", nil, 1, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("overview: %d %s", w.Code, w.Body)
	}
	var got OverviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 1 || got.Tags[0].TagID != 1 {
		t.Fatalf("tags %+v: only organization 1's tag", got.Tags)
	}
	tag := got.Tags[0]
	if tag.Status != "ok" || tag.SiteName != "SA" || tag.HorizonHours != forecast.Defaults.HorizonHours {
		t.Errorf("row %+v", tag)
	}
	if tag.End == nil || tag.End.Q50 == nil || *tag.End.Q50 != 2 {
		t.Errorf("end %+v: the last step with a value", tag.End)
	}
	if got.Summary["ok"] != 1 || got.Summary["total"] != 1 {
		t.Errorf("summary %v", got.Summary)
	}

	// Organization 2 sees its own tag, never forecast yet.
	w = forecastCall(t, h.Overview, http.MethodGet, "/api/forecast/overview", nil, 2, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Tags) != 1 || got.Tags[0].TagID != 2 || got.Tags[0].Status != "pending" {
		t.Errorf("organization 2: %+v", got.Tags)
	}
}
