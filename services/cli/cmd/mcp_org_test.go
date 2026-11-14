package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
)

// org_id on a call names the organization in X-Organization-ID for that call
// only; the server's default applies otherwise.
func TestToolOrgIDSetsTheHeaderForOneCall(t *testing.T) {
	var seen []string
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+" org="+r.Header.Get("X-Organization-ID"))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer core.Close()

	s := &mcpServer{client: api.New(core.URL, "token", 1)}
	calls := []struct {
		name string
		args map[string]interface{}
	}{
		{"list_tags", map[string]interface{}{"org_id": float64(5)}},
		{"list_tags", map[string]interface{}{}},
		{"list_oee_profiles", map[string]interface{}{"org_id": float64(7)}},
		// Not about one organization: the argument is ignored.
		{"list_organizations", map[string]interface{}{"org_id": float64(9)}},
	}
	for _, c := range calls {
		if _, err := s.executeTool(c.name, c.args); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	want := []string{"/api/tags org=5", "/api/tags org=1", "/api/oee/profiles org=7", "/api/organizations org=1"}
	if len(seen) != len(want) {
		t.Fatalf("calls %v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("call %d: got %q, want %q", i, seen[i], want[i])
		}
	}
	if s.client.OrgID() != 1 {
		t.Errorf("the server's client was changed: org %d", s.client.OrgID())
	}
}

// Every organization-scoped tool offers org_id; list_organizations does not.
func TestToolsListOffersOrgID(t *testing.T) {
	s := &mcpServer{client: api.New("http://core", "token", 0)}
	resp := s.handleToolsList(&jsonRPCRequest{ID: 1})
	raw, _ := json.Marshal(resp.Result)
	var res struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]interface{} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		_, has := tool.InputSchema.Properties["org_id"]
		if orgFreeTools[tool.Name] == has && tool.Name != "fleet_restart" {
			t.Errorf("%s: org_id offered=%v", tool.Name, has)
		}
	}
}
