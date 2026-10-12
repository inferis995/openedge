package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewestBackup(t *testing.T) {
	dir := t.TempDir()
	if got := newestBackup(dir); got.Unix() != 0 {
		t.Fatalf("empty dir: %v", got)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	recent := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	files := map[string]time.Time{
		"a.dump":                  old,
		"openedge_x.sql.gz":       recent,
		"notes.txt":               time.Now(),
		"pre-restore_y.sql.gz":    time.Now(), // a safety copy, not a backup
		".openedge_z.sql.gz.tmp1": time.Now(), // a dump still being written
	}
	for name, at := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if got := newestBackup(dir); !got.Equal(recent) {
		t.Fatalf("got %v, want %v (other files ignored)", got, recent)
	}
	if got := newestBackup(filepath.Join(dir, "missing")); got.Unix() != 0 {
		t.Fatalf("missing dir: %v", got)
	}
}
