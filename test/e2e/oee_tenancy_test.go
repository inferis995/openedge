//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// OEE was never made multi-tenant: the factory rollup in oee_history averaged
// every organization's lines into one row that every organization read, any
// profile's history, loss tree and reliability were readable by id, and the
// alert rules were one list for the whole installation.
func TestOEEHistoryAndAlertRulesBelongToTheirOrganization(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)
	suffix := uniqueSuffix()
	a := createOrg(t, admin, "oee-a-"+suffix)
	b := createOrg(t, admin, "oee-b-"+suffix)
	adminA := createOrgAdmin(t, admin, a.ID, "oee-a-"+suffix, "e2e-Password-"+suffix)
	adminB := createOrgAdmin(t, admin, b.ID, "oee-b-"+suffix, "e2e-Password-"+suffix)

	// Disabled, so the hourly job leaves it alone.
	var profileA int
	if err := db.QueryRow(`INSERT INTO oee_profiles (org_id, name, enabled) VALUES ($1, $2, false) RETURNING id`,
		a.ID, "linea-a-"+suffix).Scan(&profileA); err != nil {
		t.Fatal(err)
	}
	const (
		profileMarker = 12.3456
		rollupA       = 23.4567
		rollupB       = 34.5678
		bucket        = "2020-03-04T10:00:00Z"
	)
	insert := func(profileID interface{}, org int, oee float64) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO oee_history (profile_id, org_id, bucket_start, bucket_size, oee, availability, performance, quality)
			VALUES ($1, $2, $3, 'hour', $4, $4, $4, $4)`, profileID, org, bucket, oee); err != nil {
			t.Fatal(err)
		}
	}
	insert(profileA, a.ID, profileMarker)
	insert(nil, a.ID, rollupA)
	insert(nil, b.ID, rollupB)
	has := func(body []byte, v float64) bool { return strings.Contains(string(body), fmt.Sprint(v)) }
	const rng = "from=2020-03-04&to=2020-03-05"

	// The rollup: each organization reads its own.
	status, body := adminB.do(http.MethodGet, "/api/oee/history-v2?bucket=hour&"+rng, nil)
	if status != http.StatusOK {
		t.Fatalf("history-v2 returned %d: %s", status, truncate(body))
	}
	if has(body, rollupA) {
		t.Error("organization B reads organization A's factory rollup")
	}
	if !has(body, rollupB) {
		t.Error("organization B does not read its own factory rollup")
	}
	_, own := adminA.do(http.MethodGet, "/api/oee/history-v2?bucket=hour&"+rng, nil)
	if !has(own, rollupA) || has(own, rollupB) {
		t.Errorf("organization A's rollup is wrong: %s", truncate(own))
	}
	// The CSV rounds to one decimal: 23.5 is organization A's rollup.
	if status, body := adminB.do(http.MethodGet, "/api/oee/export/history.csv?"+rng, nil); status == http.StatusOK &&
		(strings.Contains(string(body), ",23.5,") || strings.Contains(string(body), "linea-a-"+suffix)) {
		t.Error("organization B's history export carries organization A's rows")
	}

	// A profile of another organization is not found, wherever it is asked for.
	for _, path := range []string{
		fmt.Sprintf("/api/oee/history-v2?bucket=hour&profile_id=%d&%s", profileA, rng),
		fmt.Sprintf("/api/oee/by-shift?profile_id=%d&%s", profileA, rng),
		fmt.Sprintf("/api/oee/loss-tree?profile_id=%d&%s", profileA, rng),
		fmt.Sprintf("/api/oee/profiles/%d/reliability?%s", profileA, rng),
		fmt.Sprintf("/api/oee/export/history.csv?profile_id=%d&%s", profileA, rng),
		fmt.Sprintf("/api/oee/export/by-shift.csv?profile_id=%d&%s", profileA, rng),
		fmt.Sprintf("/api/oee/export/losses.csv?profile_id=%d&%s", profileA, rng),
	} {
		status, body := adminB.do(http.MethodGet, path, nil)
		if status != http.StatusNotFound && status != http.StatusForbidden {
			t.Errorf("GET %s by another organization returned %d: %s", path, status, truncate(body))
		}
		if has(body, profileMarker) {
			t.Errorf("GET %s by another organization shows the profile's history", path)
		}
	}
	status, body = adminA.do(http.MethodGet, fmt.Sprintf("/api/oee/history-v2?bucket=hour&profile_id=%d&%s", profileA, rng), nil)
	if status != http.StatusOK || !has(body, profileMarker) {
		t.Errorf("the organization cannot read its own profile's history: %d %s", status, truncate(body))
	}

	// A profile cannot be built on another organization's counters: its OEE
	// would be that organization's production.
	gw := seedInventoryGateway(t, db, a.ID, "oee-plc-"+suffix)
	var tagA int
	if err := db.QueryRow(`INSERT INTO tags (gateway_id, code, alias, data_type) VALUES ($1, 'DB1.DBD8', 'pezzi', 'REAL') RETURNING id`,
		gw).Scan(&tagA); err != nil {
		t.Fatal(err)
	}
	if status, body := adminB.do(http.MethodPost, "/api/oee/profiles", map[string]interface{}{
		"name": "stolen-" + suffix, "produced_tag_id": tagA, "target_pieces_per_hour": 100,
	}); status == http.StatusCreated {
		t.Fatalf("organization B made a profile on organization A's tag: %s", truncate(body))
	}
	if status, body := adminA.do(http.MethodPost, "/api/oee/profiles", map[string]interface{}{
		"name": "own-" + suffix, "produced_tag_id": tagA, "target_pieces_per_hour": 100, "enabled": false,
	}); status != http.StatusCreated {
		t.Errorf("a profile on the organization's own tag was refused: %d %s", status, truncate(body))
	}

	// Alert rules.
	rule := map[string]interface{}{
		"name": "rule-a-" + suffix, "metric": "oee", "op": "<", "threshold": 50, "severity": "warning",
	}
	status, body = adminA.do(http.MethodPost, "/api/oee/alert-rules", rule)
	if status != http.StatusCreated {
		t.Fatalf("creating a factory rule returned %d: %s", status, truncate(body))
	}
	var created struct {
		ID    int  `json:"id"`
		OrgID *int `json:"org_id"`
	}
	_ = json.Unmarshal(body, &created)
	if created.OrgID == nil || *created.OrgID != a.ID {
		t.Errorf("the rule was given organization %v, want %d", created.OrgID, a.ID)
	}
	_, list := adminB.do(http.MethodGet, "/api/oee/alert-rules", nil)
	if strings.Contains(string(list), "rule-a-"+suffix) {
		t.Error("organization B lists organization A's alert rule")
	}
	path := fmt.Sprintf("/api/oee/alert-rules/%d", created.ID)
	hijack := map[string]interface{}{
		"name": "hijacked", "metric": "oee", "op": ">", "threshold": 1, "severity": "critical",
	}
	if status, _ := adminB.do(http.MethodPut, path, hijack); status == http.StatusOK {
		t.Fatal("organization B changed organization A's alert rule")
	}
	if status, _ := adminB.do(http.MethodDelete, path, nil); status == http.StatusNoContent {
		t.Fatal("organization B deleted organization A's alert rule")
	}
	onA := map[string]interface{}{
		"profile_id": profileA, "name": "stolen-" + suffix, "metric": "oee", "op": "<", "threshold": 50, "severity": "info",
	}
	if status, body := adminB.do(http.MethodPost, "/api/oee/alert-rules", onA); status == http.StatusCreated {
		t.Fatalf("organization B made a rule on organization A's profile: %s", truncate(body))
	}
	_, mine := adminA.do(http.MethodGet, "/api/oee/alert-rules", nil)
	if !strings.Contains(string(mine), "rule-a-"+suffix) {
		t.Error("the organization does not list its own alert rule")
	}
	onA["name"] = "own-" + suffix
	if status, body := adminA.do(http.MethodPost, "/api/oee/alert-rules", onA); status != http.StatusCreated {
		t.Errorf("a rule on the organization's own profile was refused: %d %s", status, truncate(body))
	}
}

// An organization without OEE profiles was shown the "legacy" OEE, computed
// from the platform's oee_* settings: tag ids the platform administrator
// chose, possibly another company's line. With more than one organization
// those settings belong to the platform administrator only.
func TestTheLegacyOEEIsNotShownToOtherOrganizations(t *testing.T) {
	admin, _ := adminSession(t)
	suffix := uniqueSuffix()
	b := createOrg(t, admin, "oee-legacy-"+suffix)
	// A second organization, so b is not the platform's only one.
	createOrg(t, admin, "oee-legacy-other-"+suffix)
	adminB := createOrgAdmin(t, admin, b.ID, "oee-legacy-"+suffix, "e2e-Password-"+suffix)

	status, body := adminB.do(http.MethodGet, "/api/oee", nil)
	if status != http.StatusOK {
		t.Fatalf("/api/oee returned %d: %s", status, truncate(body))
	}
	var o struct {
		Mode   string          `json:"mode"`
		Legacy json.RawMessage `json:"legacy"`
	}
	if err := json.Unmarshal(body, &o); err != nil {
		t.Fatal(err)
	}
	if o.Mode != "legacy" || len(o.Legacy) != 0 {
		t.Errorf("an organization without profiles got the platform's legacy OEE: %s", truncate(body))
	}
	if status, body := adminB.do(http.MethodGet, "/api/dashboard/overview", nil); status == http.StatusOK &&
		strings.Contains(string(body), `"legacy":{`) {
		t.Errorf("the dashboard shows organization B the platform's legacy OEE: %s", truncate(body))
	}
}
