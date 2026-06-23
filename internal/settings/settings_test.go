package settings_test

import (
	"database/sql"
	"testing"

	_ "github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
)

// newManagerWithBadDB creates a Manager whose DB connection will always fail
// (bad host port 1), triggering the default-config fallback in NewManager.
// This avoids the nil-pointer panic that occurs when *sql.DB itself is nil.
func newDefaultManager() *settings.Manager {
	db, _ := sql.Open("postgres", "host=127.0.0.1 port=1 dbname=x sslmode=disable")
	return settings.NewManager(db)
}

// ---------------------------------------------------------------------------
// Get() — default values when DB is unavailable
// ---------------------------------------------------------------------------

func TestGet_DefaultsWhenNoDB(t *testing.T) {
	m := newDefaultManager()
	cfg := m.Get()
	if cfg == nil {
		t.Fatal("Get() returned nil")
	}
	if cfg.PublishMode != models.PublishModeDual {
		t.Errorf("default PublishMode = %q, want %q", cfg.PublishMode, models.PublishModeDual)
	}
	if cfg.RBEHeartbeatSeconds != 60 {
		t.Errorf("default RBEHeartbeatSeconds = %d, want 60", cfg.RBEHeartbeatSeconds)
	}
	if cfg.RBEDeadbandPercent != 0.5 {
		t.Errorf("default RBEDeadbandPercent = %v, want 0.5", cfg.RBEDeadbandPercent)
	}
}

// ---------------------------------------------------------------------------
// PublishMode.IsValid()
// ---------------------------------------------------------------------------

func TestPublishMode_IsValid(t *testing.T) {
	cases := []struct {
		mode  models.PublishMode
		valid bool
	}{
		{models.PublishModeDual, true},
		{models.PublishModeSparkplugOnly, true},
		{models.PublishModeLegacyOnly, true},
		{"", false},
		{"unknown", false},
		{"DUAL", false}, // case-sensitive
	}
	for _, c := range cases {
		if got := c.mode.IsValid(); got != c.valid {
			t.Errorf("PublishMode(%q).IsValid() = %v, want %v", c.mode, got, c.valid)
		}
	}
}

// ---------------------------------------------------------------------------
// ShouldPublish — quality change
// ---------------------------------------------------------------------------

func TestShouldPublish_QualityChange(t *testing.T) {
	m := newDefaultManager()
	// Different quality values must always publish.
	if !m.ShouldPublish(1, 42.0, 42.0, 192, 0) {
		t.Error("ShouldPublish should return true when quality changes")
	}
}

func TestShouldPublish_QualitySame_ValueChanged(t *testing.T) {
	m := newDefaultManager()
	// Same quality, but value changed beyond deadband (0.5%) → publish.
	if !m.ShouldPublish(1, 100.0, 1.0, 192, 192) {
		t.Error("ShouldPublish should return true when value changes significantly")
	}
}

func TestShouldPublish_QualitySame_ValueUnchanged(t *testing.T) {
	m := newDefaultManager()
	// Pre-warm the last-publish time by publishing once so heartbeat timer starts.
	m.ShouldPublish(2, 50.0, 0.0, 192, 192) // first call always publishes (value changed)

	// Now same value, same quality — within deadband (0.5%), heartbeat not elapsed.
	// With default heartbeat=60s the second call within the same second must skip.
	got := m.ShouldPublish(2, 50.0, 50.0, 192, 192)
	if got {
		t.Error("ShouldPublish should return false when value/quality unchanged and heartbeat not elapsed")
	}
}

func TestShouldPublish_FirstPublishAlwaysSucceeds(t *testing.T) {
	m := newDefaultManager()
	// First publish on a new tag ID should succeed because value changed from zero.
	if !m.ShouldPublish(99, 1.0, 0.0, 192, 192) {
		t.Error("First publish on a new tag should always succeed (value changed from zero)")
	}
}

// ---------------------------------------------------------------------------
// ShouldPublish — deadband edge cases
// ---------------------------------------------------------------------------

func TestShouldPublish_DeadbandExact(t *testing.T) {
	m := newDefaultManager()
	// Prime the heartbeat for tag 10.
	m.ShouldPublish(10, 100.0, 0.0, 192, 192)

	// Default deadband = 0.5%.  A change of exactly 0.5% of 100 = 0.5 must be within deadband → skip.
	got := m.ShouldPublish(10, 100.5, 100.0, 192, 192)
	if got {
		t.Errorf("ShouldPublish should skip when change == deadband (0.5%% of 100 = 0.5): got publish=true")
	}
}

func TestShouldPublish_DeadbandExceeded(t *testing.T) {
	m := newDefaultManager()
	// Prime the heartbeat for tag 11.
	m.ShouldPublish(11, 100.0, 0.0, 192, 192)

	// Change of 1.0 > 0.5% of 100 → must publish.
	got := m.ShouldPublish(11, 101.0, 100.0, 192, 192)
	if !got {
		t.Error("ShouldPublish should publish when change exceeds deadband")
	}
}

// ---------------------------------------------------------------------------
// ShouldPublish — nil config (defensive path)
// ---------------------------------------------------------------------------

func TestShouldPublish_ConfigNeverNilAfterNewManager(t *testing.T) {
	m := newDefaultManager()
	cfg := m.Get()
	if cfg == nil {
		t.Fatal("config must never be nil after NewManager")
	}
}

// ---------------------------------------------------------------------------
// GetMetrics — counter accumulation and reset
// ---------------------------------------------------------------------------

func TestGetMetrics_InitialZero(t *testing.T) {
	m := newDefaultManager()
	metrics := m.GetMetrics()
	if metrics.Published != 0 || metrics.Skipped != 0 {
		t.Errorf("initial metrics should be zero: got published=%d skipped=%d", metrics.Published, metrics.Skipped)
	}
}

func TestGetMetrics_ResetsAfterRead(t *testing.T) {
	m := newDefaultManager()

	// Generate a publish event.
	m.ShouldPublish(20, 1.0, 0.0, 192, 192)

	first := m.GetMetrics()
	if first.Published == 0 {
		t.Error("expected at least one published count after ShouldPublish")
	}

	// Second call should be zeroed out.
	second := m.GetMetrics()
	if second.Published != 0 || second.Skipped != 0 {
		t.Errorf("GetMetrics should reset counters: got published=%d skipped=%d", second.Published, second.Skipped)
	}
}

func TestGetMetrics_SavedPercent(t *testing.T) {
	m := newDefaultManager()

	// Cause one publish (value change from zero).
	m.ShouldPublish(30, 100.0, 0.0, 192, 192)
	// Cause one skip (same value, same quality, heartbeat not elapsed).
	m.ShouldPublish(30, 100.0, 100.0, 192, 192)

	metrics := m.GetMetrics()
	total := metrics.Published + metrics.Skipped
	if total == 0 {
		t.Skip("no events recorded — skip SavedPercent check")
	}
	expected := float64(metrics.Skipped) / float64(total) * 100
	if metrics.SavedPercent != expected {
		t.Errorf("SavedPercent = %v, want %v", metrics.SavedPercent, expected)
	}
}

func TestGetMetrics_SavedPercentZeroWhenNoEvents(t *testing.T) {
	m := newDefaultManager()
	metrics := m.GetMetrics()
	if metrics.SavedPercent != 0 {
		t.Errorf("SavedPercent should be 0 when no events: got %v", metrics.SavedPercent)
	}
}

func TestGetMetrics_MultiplePublishes(t *testing.T) {
	m := newDefaultManager()
	// Three different value changes on different tags.
	m.ShouldPublish(50, 1.0, 0.0, 192, 192)
	m.ShouldPublish(51, 2.0, 0.0, 192, 192)
	m.ShouldPublish(52, 3.0, 0.0, 192, 192)

	metrics := m.GetMetrics()
	if metrics.Published < 3 {
		t.Errorf("expected at least 3 published, got %d", metrics.Published)
	}
}

// ---------------------------------------------------------------------------
// ShouldPublish — quality-only change
// ---------------------------------------------------------------------------

func TestShouldPublish_QualityChangedSameValue(t *testing.T) {
	m := newDefaultManager()
	// Same numeric value but quality flipped from good to bad → must publish.
	if !m.ShouldPublish(60, 50.0, 50.0, 0, 192) {
		t.Error("quality change must trigger publish even when value is unchanged")
	}
}

// ---------------------------------------------------------------------------
// ShouldPublish — bool value
// ---------------------------------------------------------------------------

func TestShouldPublish_BoolValueChanged(t *testing.T) {
	m := newDefaultManager()
	if !m.ShouldPublish(70, true, false, 192, 192) {
		t.Error("bool value change should trigger publish")
	}
}

func TestShouldPublish_BoolValueUnchanged(t *testing.T) {
	m := newDefaultManager()
	// Prime so heartbeat is set.
	m.ShouldPublish(71, true, false, 192, 192)
	// Same bool, same quality → skip.
	got := m.ShouldPublish(71, true, true, 192, 192)
	if got {
		t.Error("unchanged bool value should be skipped within heartbeat window")
	}
}
