//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// A signed token cannot be recalled, so users.token_version retires it: a
// password change ends the user's other sessions, a new role ends all of
// them, and a deleted user's token stops working at once.
func TestChangedPasswordOrRoleRetiresOpenSessions(t *testing.T) {
	admin, _ := adminSession(t)
	suffix := uniqueSuffix()
	org := createOrg(t, admin, "revoke-"+suffix)
	username, password := "revoke-"+suffix, "e2e-Password-"+suffix
	first := createOrgAdmin(t, admin, org.ID, username, password)
	second, lr := login(t, username, password)

	status, body := first.do(http.MethodPut, "/api/auth/me/password",
		map[string]string{"old_password": password, "new_password": "e2e-Changed-" + suffix})
	if status != http.StatusOK {
		t.Fatalf("changing the password: %d %s", status, truncate(body))
	}
	var res struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Token == "" {
		t.Fatalf("the password change returned no fresh token: %s", truncate(body))
	}
	current := &apiClient{t: t, token: res.Token}

	if status, _ := second.do(http.MethodGet, "/api/sites", nil); status != http.StatusUnauthorized {
		t.Errorf("a session opened before the password change still works: %d", status)
	}
	if status, _ := first.do(http.MethodGet, "/api/sites", nil); status != http.StatusUnauthorized {
		t.Errorf("the token that changed the password still works: %d", status)
	}
	if status, body := current.do(http.MethodGet, "/api/sites", nil); status != http.StatusOK {
		t.Fatalf("the fresh token does not work: %d %s", status, truncate(body))
	}

	// Demoted from admin: the admin token must not outlive the demotion.
	admin.mustDo(http.MethodPut, "/api/users/"+strconv.Itoa(lr.User.ID), map[string]interface{}{
		"role": "user", "full_name": username, "org_id": org.ID,
	}, http.StatusOK)
	if status, _ := current.do(http.MethodGet, "/api/sites", nil); status != http.StatusUnauthorized {
		t.Errorf("an admin token survived the demotion: %d", status)
	}
}
