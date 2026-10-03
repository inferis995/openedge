//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// An account still on the default password (users.must_change_password) may
// read who it is and change the password; anything else is refused until it
// does, and the change hands back a session without the restriction.
func TestADefaultPasswordSessionCanOnlyChangeThePassword(t *testing.T) {
	requireDirectAccess(t, "Postgres")
	admin, _ := adminSession(t)
	db := openDB(t)
	suffix := uniqueSuffix()
	org := createOrg(t, admin, "pwchange-"+suffix)
	username, password := "pwchange-"+suffix, "e2e-Password-"+suffix
	createOrgAdmin(t, admin, org.ID, username, password)
	if _, err := db.Exec(`UPDATE users SET must_change_password = true WHERE username = $1`, username); err != nil {
		t.Fatal(err)
	}

	c, _ := login(t, username, password)
	status, body := c.do(http.MethodGet, "/api/sites", nil)
	if status != http.StatusForbidden || !strings.Contains(string(body), "password_change_required") {
		t.Fatalf("a default-password session read the sites: %d %s", status, truncate(body))
	}
	if status, body := c.do(http.MethodGet, "/api/auth/me", nil); status != http.StatusOK {
		t.Errorf("a default-password session cannot read who it is: %d %s", status, truncate(body))
	}

	newPassword := "e2e-Changed-" + suffix
	status, body = c.do(http.MethodPut, "/api/auth/me/password",
		map[string]string{"old_password": password, "new_password": newPassword})
	if status != http.StatusOK {
		t.Fatalf("changing the password: %d %s", status, truncate(body))
	}
	var res struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Token == "" {
		t.Fatalf("the password change returned no fresh token: %s", truncate(body))
	}
	fresh := &apiClient{t: t, token: res.Token}
	if status, body := fresh.do(http.MethodGet, "/api/sites", nil); status != http.StatusOK {
		t.Errorf("the fresh session is still confined: %d %s", status, truncate(body))
	}

	var flag bool
	if err := db.QueryRow(`SELECT must_change_password FROM users WHERE username = $1`, username).Scan(&flag); err != nil || flag {
		t.Errorf("must_change_password still set after the change (err %v)", err)
	}
}
