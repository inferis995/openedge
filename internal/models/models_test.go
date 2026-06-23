package models_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

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
		{"DUAL", false},
		{"unknown", false},
		{"dual ", false}, // trailing space
	}
	for _, c := range cases {
		if got := c.mode.IsValid(); got != c.valid {
			t.Errorf("PublishMode(%q).IsValid() = %v, want %v", c.mode, got, c.valid)
		}
	}
}

// ---------------------------------------------------------------------------
// MQTTBrokerMode.IsValid()
// ---------------------------------------------------------------------------

func TestMQTTBrokerMode_IsValid(t *testing.T) {
	cases := []struct {
		mode  models.MQTTBrokerMode
		valid bool
	}{
		{models.MQTTBrokerModeInternal, true},
		{models.MQTTBrokerModeExternal, true},
		{"", false},
		{"INTERNAL", false},
		{"local", false},
		{"cloud", false},
	}
	for _, c := range cases {
		if got := c.mode.IsValid(); got != c.valid {
			t.Errorf("MQTTBrokerMode(%q).IsValid() = %v, want %v", c.mode, got, c.valid)
		}
	}
}

// ---------------------------------------------------------------------------
// ValidPublishModes slice completeness
// ---------------------------------------------------------------------------

func TestValidPublishModes_ContainsAll(t *testing.T) {
	want := map[models.PublishMode]bool{
		models.PublishModeDual:          true,
		models.PublishModeSparkplugOnly: true,
		models.PublishModeLegacyOnly:    true,
	}
	if len(models.ValidPublishModes) != len(want) {
		t.Errorf("ValidPublishModes has %d entries, want %d", len(models.ValidPublishModes), len(want))
	}
	for _, m := range models.ValidPublishModes {
		if !want[m] {
			t.Errorf("unexpected mode in ValidPublishModes: %q", m)
		}
	}
}

// ---------------------------------------------------------------------------
// UserRole constants
// ---------------------------------------------------------------------------

func TestUserRole_Constants(t *testing.T) {
	if models.RoleAdmin != "admin" {
		t.Errorf("RoleAdmin = %q, want %q", models.RoleAdmin, "admin")
	}
	if models.RoleUser != "user" {
		t.Errorf("RoleUser = %q, want %q", models.RoleUser, "user")
	}
	if models.RoleAdmin == models.RoleUser {
		t.Error("RoleAdmin and RoleUser must be distinct")
	}
}
