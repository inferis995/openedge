package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
)

func TestMergeForecastSettings(t *testing.T) {
	own := map[string]interface{}{"kind": "cyclic", "horizon_hours": float64(48)}
	got, err := mergeForecastSettings(own, map[string]interface{}{
		"enabled": "off", "kind": "inherit", "context_days": float64(14),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["enabled"] != false || got["context_days"] != 14 {
		t.Errorf("merged %v", got)
	}
	if _, ok := got["kind"]; ok {
		t.Error("kind=inherit should remove the field")
	}
	if got["horizon_hours"] != float64(48) {
		t.Errorf("a field not mentioned must keep its value: %v", got)
	}
	if got, _ := mergeForecastSettings(own, map[string]interface{}{"horizon_hours": 0}); got["horizon_hours"] != nil {
		t.Error("horizon 0 should inherit")
	}
	if _, err := mergeForecastSettings(own, map[string]interface{}{"enabled": "maybe"}); err == nil {
		t.Error("enabled=maybe accepted")
	}
	if own["kind"] != "cyclic" {
		t.Error("the input profile was modified")
	}
}

func TestFilterForecastOverview(t *testing.T) {
	rows := []forecastRowOut{
		{TagID: 1, Status: "ok", SiteName: "Sorical"},
		{TagID: 2, Status: "unreliable", SiteName: "Sorical"},
		{TagID: 3, Status: "ok", SiteName: "Altro"},
	}
	if got := filterForecastOverview(rows, "ok", "sorical"); len(got) != 1 || got[0].TagID != 1 {
		t.Errorf("filtered %v", got)
	}
	if got := filterForecastOverview(rows, "", ""); len(got) != 3 {
		t.Errorf("no filter: %d rows", len(got))
	}
	rows[2].Risks = []forecastRisk{{Likelihood: "likely"}}
	rows[1].Risks = []forecastRisk{{Likelihood: "possible"}}
	if got := filterForecastOverview(rows, "at_risk", ""); len(got) != 1 || got[0].TagID != 3 {
		t.Errorf("at_risk: %v (a possible risk is not at risk)", got)
	}
}

// The MCP tool reads the level's profile, changes only what was asked, and
// writes it back.
func TestSetForecastSettingsTool(t *testing.T) {
	var put map[string]interface{}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/forecast/profiles":
			if r.URL.Query().Get("scope") != "site" || r.URL.Query().Get("scope_id") != "3" {
				http.Error(w, "wrong level", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"scope":"site","scope_id":3,"settings":{"kind":"cyclic","horizon_hours":48}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/forecast/profiles":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &put)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer core.Close()

	s := &mcpServer{client: api.New(core.URL, "token", 1)}
	if _, err := s.executeTool("set_forecast_settings", map[string]interface{}{
		"scope": "site", "scope_id": float64(3), "enabled": "off", "horizon_hours": float64(0),
	}); err != nil {
		t.Fatal(err)
	}
	settings, _ := put["settings"].(map[string]interface{})
	if put["scope"] != "site" || settings["enabled"] != false || settings["kind"] != "cyclic" {
		t.Errorf("saved %v", put)
	}
	if _, ok := settings["horizon_hours"]; ok {
		t.Errorf("horizon_hours 0 should inherit: %v", settings)
	}

	if _, err := s.executeTool("set_forecast_settings", map[string]interface{}{
		"scope": "site", "scope_id": float64(3), "clear": true,
	}); err != nil {
		t.Fatal(err)
	}
	if settings, _ := put["settings"].(map[string]interface{}); len(settings) != 0 {
		t.Errorf("clear saved %v", settings)
	}

	if _, err := s.executeTool("set_forecast_settings", map[string]interface{}{"scope": "site"}); err == nil ||
		!strings.Contains(err.Error(), "scope_id") {
		t.Errorf("missing scope_id: %v", err)
	}
}
