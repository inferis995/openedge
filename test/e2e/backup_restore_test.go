//go:build e2e

package e2e

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A backup nobody has ever restored is not a backup.
//
// This repo had two restore scripts and shipped the wrong one. scripts/backup.sh
// writes openedge_<ts>.sql.gz — plain SQL through gzip. scripts/restore-backup.sh,
// which `make restore` invoked, expected a .dump and ran pg_restore, a tool that
// does not read plain SQL at all. No producer in the repo has ever written a
// .dump file.
//
// The ordering is what made it unrecoverable rather than merely broken: that
// script stopped the services, DROPPED the database — which succeeds — and only
// then reached the pg_restore that could not read the file. An operator working
// through a real incident, following the documented command, would be left with
// no database and a backup the tool in front of them refused to load.
//
// Nothing in the suite caught it, because backup.sh verifies its own output
// (gzip -t, minimum size) and passes. Verifying the dump is not verifying the
// restore.
//
// This test therefore does the only thing that settles it: writes a row, takes
// a real backup, restores it into a scratch database, and reads the row back.
// It restores into a COPY rather than over the live database — an acceptance
// test that drops the database the rest of the suite is using would be its own
// kind of disaster.

func repoRoot(t *testing.T) string {
	t.Helper()
	if r := os.Getenv("E2E_REPO_ROOT"); r != "" {
		return r
	}
	// test/e2e → repository root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func TestBackupCanActuallyBeRestored(t *testing.T) {
	if os.Getenv("E2E_SKIP_DOCKER") != "" {
		t.Skip("E2E_SKIP_DOCKER set — this test drives docker compose directly")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}

	admin, _ := adminSession(t)
	db := openDB(t)

	// A row that must survive the round trip, unique to this run so a stale
	// backup in the directory cannot make the test pass by accident.
	marker := "restore-probe-" + uniqueSuffix()
	org := createOrg(t, admin, marker)
	t.Logf("marker organisation %q (id %d)", marker, org.ID)

	// Give Postgres a moment to have the row durably in the dump we are about
	// to take; the API call has already returned, so this is belt and braces.
	time.Sleep(time.Second)

	// ── Take a real backup with the real script ────────────────────────────
	root := repoRoot(t)
	backupCmd := exec.Command("./scripts/backup.sh")
	backupCmd.Dir = root
	if out, err := backupCmd.CombinedOutput(); err != nil {
		t.Fatalf("scripts/backup.sh failed: %v\n%s", err, out)
	}

	matches, err := filepath.Glob(filepath.Join(root, "backups", "openedge_*.sql.gz"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("backup.sh reported success but wrote no openedge_*.sql.gz into backups/ (err=%v)", err)
	}
	// Newest wins.
	newest := matches[0]
	newestInfo, _ := os.Stat(newest)
	for _, m := range matches[1:] {
		if info, err := os.Stat(m); err == nil && info.ModTime().After(newestInfo.ModTime()) {
			newest, newestInfo = m, info
		}
	}
	t.Logf("backup written: %s (%d bytes)", filepath.Base(newest), newestInfo.Size())

	// ── Restore into a scratch database ───────────────────────────────────
	scratch := "restore_probe_" + strings.ReplaceAll(uniqueSuffix(), "-", "")
	if _, err := db.Exec(fmt.Sprintf(`CREATE DATABASE %s`, scratch)); err != nil {
		t.Fatalf("creating scratch database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, scratch)); err != nil {
			t.Logf("could not drop scratch database %s: %v", scratch, err)
		}
	})

	// The restore path under test: gunzip piped into psql, exactly what
	// scripts/restore.sh does — as opposed to pg_restore, which cannot read
	// this format and which `make restore` used to invoke after dropping the
	// live database.
	pgUser := env("E2E_DB_USER", "industrial_user")
	pgPass := env("E2E_DB_PASSWORD", os.Getenv("POSTGRES_PASSWORD"))

	// The sequence scripts/restore.sh performs. The pre/post pair is not
	// optional decoration: a pg_dump of this database contains rows for
	// TimescaleDB's own catalog, and replaying them into a live extension
	// either fails partway or completes with tag_history as an ORDINARY table
	// — historian present, queryable, quietly no longer a hypertable, so
	// compression and retention never run again.
	psql := func(sqlText string) (string, error) {
		c := exec.Command("docker", "compose", "exec", "-T",
			"-e", "PGPASSWORD="+pgPass, "postgres",
			"psql", "-q", "-v", "ON_ERROR_STOP=1", "-U", pgUser, "-d", scratch, "-c", sqlText)
		c.Dir = root
		out, err := c.CombinedOutput()
		return string(out), err
	}

	if out, err := psql("CREATE EXTENSION IF NOT EXISTS timescaledb;"); err != nil {
		t.Fatalf("creating the extension in the scratch database: %v\n%s", err, out)
	}
	if out, err := psql("SELECT timescaledb_pre_restore();"); err != nil {
		t.Fatalf("timescaledb_pre_restore failed: %v\n%s", err, out)
	}

	restore := exec.Command("sh", "-c", fmt.Sprintf(
		"gunzip -c %q | docker compose exec -T -e PGPASSWORD=%q postgres psql -q -U %q -d %q",
		newest, pgPass, pgUser, scratch))
	restore.Dir = root
	if out, err := restore.CombinedOutput(); err != nil {
		t.Fatalf("restoring the backup failed: %v\n%s", err, truncate(out))
	}

	if out, err := psql("SELECT timescaledb_post_restore();"); err != nil {
		t.Fatalf("timescaledb_post_restore failed: %v\n%s", err, out)
	}

	// ── Read the marker back out of the restored copy ─────────────────────
	scratchDSN := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		env("E2E_DB_HOST", "127.0.0.1"), env("E2E_DB_PORT", "5432"),
		pgUser, pgPass, scratch)

	restored, err := sql.Open("postgres", scratchDSN)
	if err != nil {
		t.Fatalf("opening the restored copy: %v", err)
	}
	defer restored.Close()
	var name string
	if err := restored.QueryRow(
		`SELECT name FROM organizations WHERE name = $1`, marker).Scan(&name); err != nil {
		t.Fatalf("the marker organisation %q is not in the restored database: %v\n"+
			"The backup was produced and verified, and its contents did not survive a restore.",
			marker, err)
	}

	// A restore that returns one row but an empty schema would also pass the
	// check above by luck; assert the historian table came back too.
	var tables int
	if err := restored.QueryRow(`
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public'`).Scan(&tables); err != nil {
		t.Fatalf("counting restored tables: %v", err)
	}
	if tables < 10 {
		t.Fatalf("only %d tables in the restored database — the dump is partial", tables)
	}

	// The historian must come back as a HYPERTABLE. Restoring it as an ordinary
	// table looks like success from every angle an operator can see — the rows
	// are there and the UI charts them — while compression and retention are
	// silently gone, and the disk problem that prompted the restore returns.
	var isHyper bool
	if err := restored.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM timescaledb_information.hypertables
			WHERE hypertable_name = 'tag_history'
		)`).Scan(&isHyper); err != nil {
		t.Fatalf("checking hypertables in the restored database: %v", err)
	}
	if !isHyper {
		t.Fatal("tag_history restored as an ordinary table, not a hypertable — the data is " +
			"there and compression and retention will never run again")
	}

	t.Logf("restore verified: marker present, %d tables, tag_history is a hypertable", tables)
}

// The USB copy script must find the backups the backup script writes.
//
// It globbed openedge-*.dump* — a hyphen, and a format nothing in this repo
// produces — so on a machine with a directory full of backups it reported "no
// backups found" and copied nothing. An offsite copy that silently copies
// nothing is the failure you discover at the same moment you need it.
func TestUSBBackupScriptMatchesTheBackupNaming(t *testing.T) {
	root := repoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, "scripts", "backup-to-usb.sh"))
	if err != nil {
		t.Fatalf("reading backup-to-usb.sh: %v", err)
	}
	produced, err := os.ReadFile(filepath.Join(root, "scripts", "backup.sh"))
	if err != nil {
		t.Fatalf("reading backup.sh: %v", err)
	}

	if !strings.Contains(string(produced), "openedge_${TIMESTAMP}.sql.gz") {
		t.Fatal("backup.sh no longer writes openedge_<ts>.sql.gz — update this test and " +
			"every consumer of that name")
	}
	if !strings.Contains(string(script), "openedge_*.sql.gz") {
		t.Fatal("backup-to-usb.sh does not glob openedge_*.sql.gz — it will find none of " +
			"the files backup.sh writes and copy nothing offsite")
	}
}

// `make restore` must point at the script that can read what backup.sh writes.
func TestMakeRestoreUsesTheWorkingScript(t *testing.T) {
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}
	if strings.Contains(string(mk), "restore-backup.sh") {
		t.Fatal("make restore still calls restore-backup.sh, which runs pg_restore on a " +
			"plain-SQL dump — it drops the database first and fails afterwards")
	}
	if !strings.Contains(string(mk), "scripts/restore.sh") {
		t.Fatal("make restore does not call scripts/restore.sh")
	}
}
