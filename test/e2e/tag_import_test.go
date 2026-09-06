//go:build e2e

package e2e

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// A tag import is all or nothing, and the file is judged before anything lands.
//
// It used to write as it walked: a thousand-line file that failed at line five
// hundred left four hundred and ninety-nine tags in the database, the rest
// missing, and then sent the reload command anyway. The driver restarted onto a
// half-configured gateway and began polling addresses that no longer matched the
// tag list — which does not present as a failed import, it presents as a plant
// reading wrong.
func TestABadLineImportsNothingAtAll(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	gatewayID, cleanup := seedGateway(t, db, admin)
	defer cleanup()

	// Three good lines around one that cannot parse.
	content := "" +
		"Temp_1 : REAL AT 40001;\n" +
		"Temp_2 : REAL AT 40002;\n" +
		"this line is not a tag at all\n" +
		"Temp_3 : REAL AT 40003;\n"

	status, body := admin.do(http.MethodPost, "/api/tags/import", map[string]interface{}{
		"gateway_id": gatewayID,
		"content":    content,
	})
	if status != http.StatusOK {
		t.Fatalf("import returned %d: %s", status, truncate(body))
	}

	var res struct {
		Created int      `json:"created"`
		Updated int      `json:"updated"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decoding the import result: %v — %s", err, truncate(body))
	}
	if len(res.Errors) == 0 {
		t.Fatal("the malformed line produced no error: the parser accepted a line it should not have")
	}
	if res.Created != 0 || res.Updated != 0 {
		t.Errorf("the import reports created=%d updated=%d on a file with a bad line — "+
			"it should report zero and write nothing", res.Created, res.Updated)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tags WHERE gateway_id = $1`, gatewayID).Scan(&n); err != nil {
		t.Fatalf("counting tags: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d tag(s) landed in the database from a file that was rejected — "+
			"the gateway is now half configured, which is the state this behavior exists to prevent", n)
	}
}

// A clean file lands whole, and does not carry a deadband nobody asked for.
//
// The import used to hardcode historize_deadband = 0.1 while the column default
// is 0 and a tag created through the UI gets 0. On a temperature that lives
// inside a narrow band that silently drops the readings the tag was imported to
// show, and nothing in the interface said so.
func TestACleanImportLandsWholeWithNoSurpriseDeadband(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	gatewayID, cleanup := seedGateway(t, db, admin)
	defer cleanup()

	content := "" +
		"Temp_1 : REAL AT 40001;\n" +
		"Temp_2 : REAL AT 40002;\n" +
		"Flag_1 : BOOL AT 40003.0;\n"

	status, body := admin.do(http.MethodPost, "/api/tags/import", map[string]interface{}{
		"gateway_id": gatewayID,
		"content":    content,
		"historize":  true,
	})
	if status != http.StatusOK {
		t.Fatalf("import returned %d: %s", status, truncate(body))
	}

	var res struct {
		Created int      `json:"created"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decoding the import result: %v — %s", err, truncate(body))
	}
	if len(res.Errors) > 0 {
		t.Fatalf("a clean file produced errors: %v", res.Errors)
	}
	if res.Created != 3 {
		t.Errorf("created=%d, expected 3", res.Created)
	}

	rows, err := db.Query(
		`SELECT alias, historize, historize_deadband FROM tags WHERE gateway_id = $1 ORDER BY code`,
		gatewayID)
	if err != nil {
		t.Fatalf("reading back the imported tags: %v", err)
	}
	defer rows.Close()

	var seen int
	for rows.Next() {
		var alias string
		var historize bool
		var deadband float64
		if err := rows.Scan(&alias, &historize, &deadband); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		seen++
		if !historize {
			t.Errorf("%s: historize is false although the import asked for true", alias)
		}
		if deadband != 0 {
			t.Errorf("%s: historize_deadband is %v, expected 0 — the import is choosing "+
				"a change filter the operator never asked for, and small real movements "+
				"will never reach the historian", alias, deadband)
		}
	}
	if seen != 3 {
		t.Errorf("%d tags in the database, expected 3", seen)
	}
}

// seedGateway builds org -> site -> area -> gateway and returns the gateway id.
func seedGateway(t *testing.T, db *sql.DB, admin *apiClient) (int, func()) {
	t.Helper()

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "tag-import-"+suffix)

	var siteID, areaID, gatewayID int
	if err := db.QueryRow(
		`INSERT INTO sites (org_id, name) VALUES ($1, $2) RETURNING id`,
		org.ID, "site-"+suffix).Scan(&siteID); err != nil {
		t.Fatalf("creating site: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO areas (site_id, name) VALUES ($1, $2) RETURNING id`,
		siteID, "area-"+suffix).Scan(&areaID); err != nil {
		t.Fatalf("creating area: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO gateways (area_id, name, driver_type, enabled)
		 VALUES ($1, $2, 'modbus', false) RETURNING id`,
		areaID, fmt.Sprintf("gw-%s", suffix)).Scan(&gatewayID); err != nil {
		t.Fatalf("creating gateway: %v", err)
	}

	return gatewayID, func() {
		if _, err := db.Exec(`DELETE FROM sites WHERE id = $1`, siteID); err != nil {
			t.Logf("cleaning site: %v", err)
		}
	}
}
