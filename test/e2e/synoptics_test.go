//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The synoptic API: saving twice from one editor, metadata-only updates,
// names, layouts and sizes that used to fail badly, and another
// organization's site.
func TestSynopticSavesAndValidation(t *testing.T) {
	requireDirectAccess(t, "Postgres")
	admin, _ := adminSession(t)
	db := openDB(t)
	suffix := uniqueSuffix()
	a := createOrg(t, admin, "syn-a-"+suffix)
	b := createOrg(t, admin, "syn-b-"+suffix)
	orgA := createOrgAdmin(t, admin, a.ID, "syn-a-"+suffix, "e2e-Password-"+suffix)

	var foreignSite int
	if err := db.QueryRow(`INSERT INTO sites (org_id, name) VALUES ($1, $2) RETURNING id`, b.ID, "site-b-"+suffix).Scan(&foreignSite); err != nil {
		t.Fatal(err)
	}

	layout := []map[string]interface{}{{"id": "w1", "type": "label", "x": 10, "y": 10, "w": 100, "h": 30, "label": "kept"}}
	raw := orgA.mustDo(http.MethodPost, "/api/synoptics", map[string]interface{}{"name": "Linea " + suffix, "layout": layout}, http.StatusCreated)
	var created struct{ ID int `json:"id"` }
	_ = json.Unmarshal(raw, &created)
	path := fmt.Sprintf("/api/synoptics/%d", created.ID)

	var s struct {
		UpdatedAt string          `json:"updated_at"`
		Layout    json.RawMessage `json:"layout"`
	}
	_ = json.Unmarshal(orgA.mustDo(http.MethodGet, path, nil, http.StatusOK), &s)

	// Two saves from the same editor: the second echoes what the first returned.
	save := func(expected string) string {
		t.Helper()
		status, body := orgA.do(http.MethodPut, path, map[string]interface{}{
			"name": "Linea " + suffix, "layout": layout, "expected_updated_at": expected,
		})
		if status != http.StatusOK {
			t.Fatalf("save with expected_updated_at=%s: %d %s", expected, status, truncate(body))
		}
		var r struct{ UpdatedAt string `json:"updated_at"` }
		_ = json.Unmarshal(body, &r)
		if r.UpdatedAt == "" {
			t.Fatalf("the save did not return the new updated_at: %s", truncate(body))
		}
		return r.UpdatedAt
	}
	first := save(s.UpdatedAt)
	second := save(first)
	// The stale version is still refused.
	if status, _ := orgA.do(http.MethodPut, path, map[string]interface{}{
		"name": "Linea " + suffix, "layout": layout, "expected_updated_at": first,
	}); status != http.StatusConflict || second == first {
		t.Errorf("a save from a stale copy was not refused: %d", status)
	}

	// A rename without a layout keeps the drawing.
	orgA.mustDo(http.MethodPut, path, map[string]interface{}{"name": "Rinominato " + suffix}, http.StatusOK)
	_ = json.Unmarshal(orgA.mustDo(http.MethodGet, path, nil, http.StatusOK), &s)
	if !strings.Contains(string(s.Layout), `"kept"`) {
		t.Errorf("a metadata-only update erased the layout: %s", truncate(s.Layout))
	}

	// What used to be stored, or answered with a 500.
	orgA.mustDo(http.MethodPost, "/api/synoptics", map[string]interface{}{"name": "Doppio " + suffix}, http.StatusCreated)
	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"duplicate name", map[string]interface{}{"name": "Doppio " + suffix}, http.StatusConflict},
		{"layout object", map[string]interface{}{"name": "X1 " + suffix, "layout": map[string]int{"a": 1}}, http.StatusBadRequest},
		{"layout string", map[string]interface{}{"name": "X2 " + suffix, "layout": "x"}, http.StatusBadRequest},
		{"huge canvas", map[string]interface{}{"name": "X3 " + suffix, "canvas_w": 2000000000}, http.StatusBadRequest},
		{"bad color", map[string]interface{}{"name": "X4 " + suffix, "background_color": "red; background:url(x)"}, http.StatusBadRequest},
		{"blank name", map[string]interface{}{"name": "   "}, http.StatusBadRequest},
		{"another org's site", map[string]interface{}{"name": "X5 " + suffix, "site_id": foreignSite}, http.StatusBadRequest},
	}
	for _, c := range cases {
		if status, body := orgA.do(http.MethodPost, "/api/synoptics", c.body); status != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, status, truncate(body), c.want)
		}
	}
}
