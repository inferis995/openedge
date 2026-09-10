//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// The same question TestTheInAppBackupContainsTheHistory answers for the button
// an operator presses, asked of the backup that runs unattended.
//
// That distinction turned out to matter. The download endpoint was fixed to
// stop excluding _timescaledb_internal.*, which is where the rows of a
// hypertable physically live; the scheduled backup — the one that runs every
// night, the one nobody watches, the one people actually restore from — kept
// the exclusions and kept producing dumps that carry the schema of tag_history
// and none of its rows.
//
// The path is reachable now because RunBackupNow runs exactly the code the
// scheduler runs. A test that exercised a copy of it would prove nothing.
func TestTheScheduledBackupContainsTheHistory(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	tagID, marker := seedOneHistoryRow(t, db, admin)
	t.Logf("history row seeded for tag %d, marker value %v", tagID, marker)

	status, body := admin.do("POST", "/api/system/backup/run", nil)
	if status != 200 {
		t.Fatalf("POST /api/system/backup/run returned %d: %s", status, truncate(body))
	}

	var result struct {
		Status    string `json:"status"`
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("the run endpoint did not return JSON: %v — %s", err, truncate(body))
	}
	if result.Filename == "" {
		t.Fatalf("the backup ran but named no file: %s", truncate(body))
	}
	t.Logf("scheduled backup produced %s (%d bytes)", result.Filename, result.SizeBytes)

	// Fetch what the scheduler actually left on disk, not a second dump taken
	// through a different code path.
	status, archive := admin.do("GET", "/api/system/backup/files/"+result.Filename, nil)
	if status != 200 {
		t.Fatalf("downloading %s returned %d: %s", result.Filename, status, truncate(archive))
	}

	dump := readOnlyEntryOfZip(t, archive)
	t.Logf("dump extracted from the scheduled backup: %d bytes", len(dump))

	if !strings.Contains(dump, "tag_history") {
		t.Fatal("the scheduled dump does not mention tag_history at all — this is not " +
			"a missing-data problem, pg_dump did not write the schema either")
	}

	if !dumpCarriesHistoryRows(dump, marker) {
		t.Fatalf("the scheduled backup contains the tag_history SCHEMA but not the row "+
			"seeded for this run (value %v). Every unattended backup this installation "+
			"has ever taken restores an empty historian.", marker)
	}
}

// The archive the scheduler publishes has to be one that can be opened and read
// back. It used to be written straight to its final name and never looked at
// again, so a truncated write was indistinguishable from a good backup until
// somebody needed it.
func TestTheScheduledBackupPublishesOnlyAReadableArchive(t *testing.T) {
	admin, _ := adminSession(t)

	status, body := admin.do("POST", "/api/system/backup/run", nil)
	if status != 200 {
		t.Fatalf("POST /api/system/backup/run returned %d: %s", status, truncate(body))
	}
	var result struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Filename == "" {
		t.Fatalf("the run endpoint returned no filename: %s", truncate(body))
	}

	status, listing := admin.do("GET", "/api/system/backup/list", nil)
	if status != 200 {
		t.Fatalf("GET /api/system/backup/list returned %d: %s", status, truncate(listing))
	}

	// The half-written file lives in the same directory under a hidden name
	// while it is being built. If one is ever listed, the atomic rename that is
	// supposed to keep it out of sight has stopped working.
	if strings.Contains(string(listing), ".partial") {
		t.Errorf("a partially written archive is visible in the backup listing: %s",
			truncate(listing))
	}

	status, archive := admin.do("GET", "/api/system/backup/files/"+result.Filename, nil)
	if status != 200 {
		t.Fatalf("downloading %s returned %d", result.Filename, status)
	}
	// readOnlyEntryOfZip fails the test if the archive does not open or the
	// entry does not decompress.
	if dump := readOnlyEntryOfZip(t, archive); len(dump) == 0 {
		t.Fatal("the published archive carries an empty dump")
	}
}
