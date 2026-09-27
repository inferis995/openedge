//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// The platform settings — broker, cloud sync, retention, notification
// channels — belong to the installation, not to one organization. They were
// behind RequireRole(admin), which an organization's admin passes: one
// tenant could point the cloud sync at their own broker and receive every
// other tenant's data.
func TestAnOrganizationAdminCannotChangeThePlatformSettings(t *testing.T) {
	admin, _ := adminSession(t)
	suffix := uniqueSuffix()
	org := createOrg(t, admin, "settings-"+suffix)
	orgAdmin := createOrgAdmin(t, admin, org.ID, "settings-"+suffix, "e2e-Password-"+suffix)

	if status, _ := orgAdmin.do(http.MethodGet, "/api/system/settings", nil); status != http.StatusForbidden {
		t.Errorf("an organization's admin read the platform settings: %d", status)
	}
	status, _ := orgAdmin.do(http.MethodPut, "/api/system/settings",
		map[string]interface{}{"cloud_mqtt_host": "attacker.example", "cloud_sync_enabled": true})
	if status != http.StatusForbidden {
		t.Fatalf("an organization's admin changed the cloud sync target: %d", status)
	}
	if status, _ := orgAdmin.do(http.MethodPost, "/api/system/mqtt/test",
		map[string]interface{}{"target": "cloud", "host": "127.0.0.1", "port": 1883}); status != http.StatusForbidden {
		t.Errorf("an organization's admin used the broker probe: %d", status)
	}

	// The global admin still can.
	if status, body := admin.do(http.MethodGet, "/api/system/settings", nil); status != http.StatusOK {
		t.Errorf("the global admin cannot read the settings: %d %s", status, truncate(body))
	}
}
