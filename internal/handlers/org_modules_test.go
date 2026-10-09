package handlers

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/modules"
)

// An unknown module key must never be written: it would be a silent typo.
func TestValidateModuleKeys(t *testing.T) {
	for _, k := range []string{"oee", "synoptics", "recipes", "shifts", "kpis", "trend", "alarms", "tags", "maintenance", "reports", "users", "dashboard", "setup", "sites", "areas", "gateways", "inventory", "historian", "i3x", "connected-apps"} {
		if !modules.Valid(k) {
			t.Errorf("known module %q rejected", k)
		}
	}
	if modules.Valid("oee_profiles") {
		t.Error("nav label oee_profiles accepted as module key")
	}
	if modules.Valid("nope") {
		t.Error("unknown module accepted")
	}
}

// Backend groups and the frontend menu must resolve to the same modules.
func TestModuleForGroupCoverage(t *testing.T) {
	for _, g := range []string{"synoptics", "recipes", "reports", "trend-views", "shifts", "custom-kpis", "maintenance", "oee", "i3x/v1"} {
		if modules.ModuleForGroup("/"+g) == "" {
			t.Errorf("API group %q has no module mapping", g)
		}
	}
	for _, m := range modules.All {
		if m == "" {
			t.Error("empty module key")
		}
	}
}
