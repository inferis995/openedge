package db_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/db"
)

// ---------------------------------------------------------------------------
// Config struct — zero-value and field assignment
// ---------------------------------------------------------------------------

func TestConfig_ZeroValue(t *testing.T) {
	var cfg db.Config
	if cfg.Host != "" || cfg.Port != 0 || cfg.User != "" || cfg.Password != "" || cfg.Database != "" {
		t.Errorf("zero Config has unexpected non-zero fields: %+v", cfg)
	}
}

func TestConfig_Fields(t *testing.T) {
	cfg := db.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "openedge",
		Password: "secret",
		Database: "openedge_db",
	}
	if cfg.Host != "localhost" { t.Errorf("Host = %q", cfg.Host) }
	if cfg.Port != 5432        { t.Errorf("Port = %d", cfg.Port) }
	if cfg.User != "openedge"  { t.Errorf("User = %q", cfg.User) }
}

// ---------------------------------------------------------------------------
// Connect — returns error when no real DB is available
// ---------------------------------------------------------------------------

func TestConnect_ErrorWithoutServer(t *testing.T) {
	// Port 1 is not a real Postgres — Connect must return a non-nil error.
	_, err := db.Connect(db.Config{
		Host:     "127.0.0.1",
		Port:     1,
		User:     "x",
		Password: "x",
		Database: "x",
	})
	if err == nil {
		t.Error("Connect() with unreachable host returned nil error, want error")
	}
}

// ---------------------------------------------------------------------------
// Close — nil DB must not panic
// ---------------------------------------------------------------------------

func TestClose_NilDB(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Close(nil) panicked: %v", r)
		}
	}()
	if err := db.Close(nil); err != nil {
		t.Errorf("Close(nil) returned unexpected error: %v", err)
	}
}
