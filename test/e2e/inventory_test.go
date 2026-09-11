//go:build e2e

package e2e

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// The inventory lists every address in a plant, which makes cross-tenant
// leakage the worst thing that could go wrong with it. Unlike the endpoints in
// TestOrgAdminCannotReachAnotherTenant there is no organisation id in the path
// to refuse: the scope comes from the token, so a leak would not look like an
// authorisation failure. It would look like a longer list.
func TestTheInventoryShowsOnlyTheCallersOwnPlant(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	victim := createOrg(t, admin, "inv-victim-"+suffix)
	attackerOrg := createOrg(t, admin, "inv-attacker-"+suffix)

	victimGateway := seedInventoryGateway(t, db, victim.ID, "victim-plc-"+suffix)
	ownGateway := seedInventoryGateway(t, db, attackerOrg.ID, "own-plc-"+suffix)
	t.Logf("victim gateway %d, attacker's own gateway %d", victimGateway, ownGateway)

	attacker := createOrgAdmin(t, admin, attackerOrg.ID,
		"inv-attacker-"+suffix, "e2e-Password-"+suffix)

	status, body := attacker.do("GET", "/api/inventory", nil)
	if status != 200 {
		t.Fatalf("GET /api/inventory returned %d: %s", status, truncate(body))
	}

	var result struct {
		Devices []struct {
			GatewayID int    `json:"gateway_id"`
			Name      string `json:"name"`
			Endpoint  string `json:"endpoint"`
		} `json:"devices"`
		Summary struct {
			Devices int `json:"devices"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("the inventory is not JSON: %v — %s", err, truncate(body))
	}

	var sawOwn bool
	for _, d := range result.Devices {
		if d.GatewayID == victimGateway {
			t.Errorf("the inventory of org %d contains org %d's gateway %q (%s)",
				attackerOrg.ID, victim.ID, d.Name, d.Endpoint)
		}
		if d.GatewayID == ownGateway {
			sawOwn = true
		}
	}
	if !sawOwn {
		t.Errorf("the caller's own gateway %d is missing from their inventory; scoping that "+
			"shows nothing is not scoping, it is a broken endpoint", ownGateway)
	}
	if result.Summary.Devices != len(result.Devices) {
		t.Errorf("the summary counts %d devices and the list has %d",
			result.Summary.Devices, len(result.Devices))
	}
}

// The export is the file that gets attached to a report and emailed. It must
// carry the same scope as the list, and the same absence of credentials.
func TestTheInventoryExportIsScopedAndCarriesNoPasswords(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	victim := createOrg(t, admin, "invcsv-victim-"+suffix)
	attackerOrg := createOrg(t, admin, "invcsv-attacker-"+suffix)

	const canary = "SuperSegreta123!"
	victimGateway := seedInventoryGatewayWithConfig(t, db, victim.ID, "victim-plc-"+suffix,
		`{"broker_host":"broker.example.com","broker_password":"`+canary+`","password":"`+canary+`"}`)
	seedInventoryGatewayWithConfig(t, db, attackerOrg.ID, "own-plc-"+suffix,
		`{"broker_host":"own.example.com","broker_password":"`+canary+`"}`)
	t.Logf("victim gateway %d", victimGateway)

	attacker := createOrgAdmin(t, admin, attackerOrg.ID,
		"invcsv-attacker-"+suffix, "e2e-Password-"+suffix)

	status, body := attacker.do("GET", "/api/inventory/export.csv", nil)
	if status != 200 {
		t.Fatalf("GET /api/inventory/export.csv returned %d: %s", status, truncate(body))
	}
	csv := string(body)

	if strings.Contains(csv, "victim-plc-"+suffix) {
		t.Errorf("another tenant's gateway is in the exported file:\n%s", truncate(body))
	}
	if !strings.Contains(csv, "own-plc-"+suffix) {
		t.Errorf("the caller's own gateway is missing from the export:\n%s", truncate(body))
	}
	if strings.Contains(csv, canary) {
		t.Fatalf("a broker password reached the exported inventory. This file is emailed " +
			"and printed.")
	}
	if !strings.HasPrefix(csv, "\ufeff") {
		t.Errorf("the export has no UTF-8 byte order mark and will open as mojibake in Excel")
	}
}

// seedInventoryGateway creates site → area → gateway under an organisation and
// returns the gateway id. Disabled, so driver-manager does not try to start a
// container for a PLC that does not exist.
func seedInventoryGateway(t *testing.T, db *sql.DB, orgID int, name string) int {
	t.Helper()
	return seedInventoryGatewayWithConfig(t, db, orgID, name, `{"ip":"10.9.9.9","port":502,"slave_id":3}`)
}

func seedInventoryGatewayWithConfig(t *testing.T, db *sql.DB, orgID int, name, config string) int {
	t.Helper()

	var siteID, areaID, gatewayID int
	if err := db.QueryRow(
		`INSERT INTO sites (org_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, "site-"+name).Scan(&siteID); err != nil {
		t.Fatalf("creating site: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO areas (site_id, name) VALUES ($1, $2) RETURNING id`,
		siteID, "area-"+name).Scan(&areaID); err != nil {
		t.Fatalf("creating area: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO gateways (area_id, name, driver_type, connection_config, enabled)
		 VALUES ($1, $2, 'MODBUS_TCP', $3::jsonb, false) RETURNING id`,
		areaID, name, config).Scan(&gatewayID); err != nil {
		t.Fatalf("creating gateway: %v", err)
	}

	// Bottom-up: areas and gateways reference their parent with ON DELETE
	// RESTRICT, so deleting the site first fails and leaves the tree behind for
	// every later run to trip over.
	t.Cleanup(func() {
		for _, step := range []struct {
			q   string
			arg interface{}
		}{
			{`DELETE FROM tags WHERE gateway_id = $1`, gatewayID},
			{`DELETE FROM gateways WHERE id = $1`, gatewayID},
			{`DELETE FROM areas WHERE id = $1`, areaID},
			{`DELETE FROM sites WHERE id = $1`, siteID},
		} {
			if _, err := db.Exec(step.q, step.arg); err != nil {
				t.Logf("cleanup: %v", err)
			}
		}
	})

	return gatewayID
}
