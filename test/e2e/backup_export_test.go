//go:build e2e

package e2e

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// The backup button in the web UI takes a different route from the script, and
// nothing was checking that the history comes out of it.
//
// scripts/backup.sh dumps the whole database and scripts/restore.sh replays it
// between timescaledb_pre_restore() and post_restore(); TestBackupCanActuallyBeRestored
// covers that pair end to end. GET /api/system/backup does something else: it
// runs pg_dump with --exclude-table=_timescaledb_internal.*, and in TimescaleDB
// the rows of a hypertable physically live in chunk tables under exactly that
// schema.
//
// So the question this test exists to answer is narrow and worth answering
// before a customer answers it for us: does an operator who presses the backup
// button get their history, or a schema with the data missing?
//
// It answers it without restoring anything. Restoring into the live database to
// find out would be a destructive test of a destructive path; reading the dump
// out of the ZIP and looking for the rows says the same thing and costs nothing.
func TestTheInAppBackupContainsTheHistory(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	tagID, marker := seedOneHistoryRow(t, db, admin)
	t.Logf("history row seeded for tag %d, marker value %v", tagID, marker)

	status, body := admin.do("GET", "/api/system/backup", nil)
	if status != 200 {
		t.Fatalf("GET /api/system/backup returned %d: %s", status, truncate(body))
	}
	if len(body) == 0 {
		t.Fatal("the backup endpoint returned an empty body")
	}

	dump := readOnlyEntryOfZip(t, body)
	t.Logf("dump extracted from the ZIP: %d bytes", len(dump))

	// The schema always makes it: it is at the top of the file and lands long
	// before any data does. Checking for it separately is what tells a failure
	// of the dump apart from a dump that simply carries no rows.
	if !strings.Contains(dump, "tag_history") {
		t.Fatal("the dump does not mention tag_history at all — this is not a " +
			"missing-data problem, the export did not dump the schema either")
	}

	if !dumpCarriesHistoryRows(dump, marker) {
		t.Fatalf("the dump contains the tag_history SCHEMA but not the row seeded for "+
			"this run (value %v). An operator pressing the backup button in the web UI "+
			"gets a file that restores an empty historian, and validateRestoreIntegrity "+
			"used to call that success. The script path (scripts/backup.sh) is unaffected.",
			marker)
	}
}

// seedOneHistoryRow builds org -> site -> area -> gateway -> tag through the API
// and writes one history row with a value unique to this run, so a dump taken
// from a shared database cannot pass by carrying somebody else's data.
func seedOneHistoryRow(t *testing.T, db *sql.DB, admin *apiClient) (int, float64) {
	t.Helper()

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "backup-export-"+suffix)

	var siteID, areaID, gatewayID, tagID int
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
		areaID, "gw-"+suffix).Scan(&gatewayID); err != nil {
		t.Fatalf("creating gateway: %v", err)
	}
	if err := db.QueryRow(
		`INSERT INTO tags (gateway_id, code, alias, data_type, historize)
		 VALUES ($1, $2, $3, 'REAL', true) RETURNING id`,
		gatewayID, "40001", "probe-"+suffix).Scan(&tagID); err != nil {
		t.Fatalf("creating tag: %v", err)
	}

	// A value no other row in the database will hold, so finding it in the dump
	// proves this run's row travelled and not merely that some row did.
	marker := 424242.0 + float64(tagID)
	if _, err := db.Exec(
		`INSERT INTO tag_history (time, tag_id, value, source) VALUES ($1, $2, $3, 'e2e')`,
		time.Now(), tagID, marker); err != nil {
		t.Fatalf("seeding the history row: %v", err)
	}

	t.Cleanup(func() {
		// tag_history cascades from tags where the foreign key is present; where
		// it is not, this leaves the row behind, which is itself the defect the
		// constraint repair now covers. Delete explicitly so the test does not
		// depend on which shape the table has.
		if _, err := db.Exec(`DELETE FROM tag_history WHERE tag_id = $1`, tagID); err != nil {
			t.Logf("cleaning history rows: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM sites WHERE id = $1`, siteID); err != nil {
			t.Logf("cleaning site: %v", err)
		}
	})

	return tagID, marker
}

func readOnlyEntryOfZip(t *testing.T, payload []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("the backup endpoint did not return a readable ZIP: %v", err)
	}
	if len(zr.File) != 1 {
		names := make([]string, 0, len(zr.File))
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		t.Fatalf("expected exactly one entry in the backup ZIP, found %d: %v", len(zr.File), names)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("opening %s inside the ZIP: %v", zr.File[0].Name, err)
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading %s inside the ZIP: %v", zr.File[0].Name, err)
	}
	return string(content)
}

// dumpCarriesHistoryRows looks for the marker value in the dump.
//
// pg_dump writes data as COPY blocks by default and as INSERT statements with
// --inserts; the marker is searched as plain text so the test does not depend on
// which of the two the handler happens to use, now or later.
func dumpCarriesHistoryRows(dump string, marker float64) bool {
	for _, form := range []string{
		fmt.Sprintf("%g", marker),
		fmt.Sprintf("%.1f", marker),
		fmt.Sprintf("%.0f", marker),
	} {
		if strings.Contains(dump, form) {
			return true
		}
	}
	return false
}
