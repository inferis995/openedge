//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Shifts, the dashboard and OEE belonged to nobody: one table for the whole
// installation, read whole. On a server with several companies, one
// company's admin changed the shifts every other company's OEE was computed
// on, and every user saw every company's alarms on the dashboard.
func TestShiftsBelongToTheirOrganization(t *testing.T) {
	admin, _ := adminSession(t)
	suffix := uniqueSuffix()
	a := createOrg(t, admin, "shift-a-"+suffix)
	b := createOrg(t, admin, "shift-b-"+suffix)
	adminA := createOrgAdmin(t, admin, a.ID, "shift-a-"+suffix, "e2e-Password-"+suffix)
	adminB := createOrgAdmin(t, admin, b.ID, "shift-b-"+suffix, "e2e-Password-"+suffix)

	name := "Turno A " + suffix
	status, body := adminA.do(http.MethodPost, "/api/shifts", map[string]interface{}{
		"name": name, "start_time": "06:00", "end_time": "14:00", "weekdays": []int{1, 2, 3, 4, 5},
	})
	if status != http.StatusCreated {
		t.Fatalf("creating a shift returned %d: %s", status, truncate(body))
	}
	var created struct{ ID int }
	_ = json.Unmarshal(body, &created)

	_, list := adminB.do(http.MethodGet, "/api/shifts", nil)
	if strings.Contains(string(list), name) {
		t.Error("another organization's admin sees the shift")
	}
	path := fmt.Sprintf("/api/shifts/%d", created.ID)
	if status, _ := adminB.do(http.MethodPut, path, map[string]interface{}{
		"name": "hijacked", "start_time": "00:00", "end_time": "01:00",
	}); status == http.StatusOK {
		t.Fatal("another organization's admin changed the shift")
	}
	if status, _ := adminB.do(http.MethodDelete, path, nil); status == http.StatusNoContent {
		t.Fatal("another organization's admin deleted the shift")
	}

	// The same name is free in the other organization.
	if status, body := adminB.do(http.MethodPost, "/api/shifts", map[string]interface{}{
		"name": name, "start_time": "07:00", "end_time": "15:00",
	}); status != http.StatusCreated {
		t.Errorf("a shift name taken in another organization is refused: %d %s", status, truncate(body))
	}
	_, mine := adminA.do(http.MethodGet, "/api/shifts", nil)
	if !strings.Contains(string(mine), name) {
		t.Error("the organization does not see its own shift")
	}
}

func TestTheDashboardShowsOnlyTheCallersOrganization(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)
	suffix := uniqueSuffix()
	a := createOrg(t, admin, "dash-a-"+suffix)
	b := createOrg(t, admin, "dash-b-"+suffix)
	adminB := createOrgAdmin(t, admin, b.ID, "dash-b-"+suffix, "e2e-Password-"+suffix)
	adminA := createOrgAdmin(t, admin, a.ID, "dash-a-"+suffix, "e2e-Password-"+suffix)

	gw := seedInventoryGateway(t, db, a.ID, "dash-plc-"+suffix)
	var tagID int
	if err := db.QueryRow(`INSERT INTO tags (gateway_id, code, alias, data_type) VALUES ($1, 'DB1.DBD0', $2, 'REAL') RETURNING id`,
		gw, "secret-alias-"+suffix).Scan(&tagID); err != nil {
		t.Fatal(err)
	}
	msg := "secret alarm " + suffix
	if _, err := db.Exec(`INSERT INTO alarm_events (tag_id, status, alarm_type, severity, message) VALUES ($1, 'ACTIVE', 'high', 'critical', $2)`,
		tagID, msg); err != nil {
		t.Fatal(err)
	}

	status, body := adminB.do(http.MethodGet, "/api/dashboard/overview", nil)
	if status != http.StatusOK {
		t.Fatalf("dashboard returned %d: %s", status, truncate(body))
	}
	if strings.Contains(string(body), msg) || strings.Contains(string(body), "secret-alias-"+suffix) {
		t.Fatalf("organization %d's dashboard shows organization %d's alarm", b.ID, a.ID)
	}
	_, own := adminA.do(http.MethodGet, "/api/dashboard/overview", nil)
	if !strings.Contains(string(own), msg) {
		t.Error("the organization's own alarm is missing from its dashboard")
	}
}
