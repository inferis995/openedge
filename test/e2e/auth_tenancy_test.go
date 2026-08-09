//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Every test in this file corresponds to a defect that shipped. The pattern was
// always the same: RequireRole(admin) is satisfied by an ORG-SCOPED admin, but
// the route or handler treated it as a platform administrator. Unit tests could
// not catch it because each half was individually correct.

// TestLoginWorks is the smoke test everything else depends on.
func TestLoginWorks(t *testing.T) {
	user, pass := adminCredentials()
	c, lr := login(t, user, pass)

	if lr.User.Role != "admin" {
		t.Errorf("bootstrap user role = %q, want admin", lr.User.Role)
	}
	if lr.User.OrgID != nil {
		t.Errorf("bootstrap user org_id = %v, want nil (global admin)", *lr.User.OrgID)
	}
	c.mustDo(http.MethodGet, "/api/auth/me", nil, http.StatusOK)
}

// TestUnauthenticatedRequestsAreRejected guards the whole API surface: a
// missing or malformed token must never reach a handler.
func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	anon := &apiClient{t: t}

	for _, path := range []string{
		"/api/tags",
		"/api/gateways",
		"/api/users",
		"/api/organizations",
		"/api/system/backup",
	} {
		if status, _ := anon.do(http.MethodGet, path, nil); status != http.StatusUnauthorized {
			t.Errorf("GET %s without a token: status %d, want 401", path, status)
		}
	}

	bogus := &apiClient{t: t, token: "not.a.jwt"}
	if status, _ := bogus.do(http.MethodGet, "/api/tags", nil); status != http.StatusUnauthorized {
		t.Errorf("GET /api/tags with a malformed token: status %d, want 401", status)
	}
}

// TestWebSocketRequiresAuth covers the worst leak found: /api/ws/realtime sat on
// a group with NO authentication and took org_id from a query parameter, so
// anyone who could reach the port could stream any tenant's live process values
// by incrementing a number.
func TestWebSocketRequiresAuth(t *testing.T) {
	anon := &apiClient{t: t}

	// A plain GET (no upgrade) must be rejected before the handler runs.
	// 401 is the contract; anything 2xx means the endpoint is open again.
	status, body := anon.do(http.MethodGet, "/api/ws/realtime?organization_id=1", nil)
	if status == http.StatusOK {
		t.Fatalf("unauthenticated WebSocket endpoint returned 200 — it is open: %s", truncate(body))
	}
	if status != http.StatusUnauthorized {
		t.Errorf("unauthenticated /api/ws/realtime: status %d, want 401", status)
	}
}

// TestMFAChallengeTokenIsNotASession covers the MFA bypass: the step-up token is
// issued after the password check but BEFORE the second factor, and RequireAuth
// used to accept it — so knowing only the password was enough to reach
// /api/auth/mfa/disable and turn MFA off.
//
// Skipped unless E2E_MFA_USER/E2E_MFA_PASSWORD identify an MFA-enabled account,
// since the bootstrap admin has no MFA.
func TestMFAChallengeTokenIsNotASession(t *testing.T) {
	user := env("E2E_MFA_USER", "")
	pass := env("E2E_MFA_PASSWORD", "")
	if user == "" || pass == "" {
		t.Skip("set E2E_MFA_USER/E2E_MFA_PASSWORD to an MFA-enabled account to run this")
	}

	anon := &apiClient{t: t}
	raw := anon.mustDo(http.MethodPost, "/api/auth/login",
		map[string]string{"username": user, "password": pass}, http.StatusOK)

	var lr loginResponse
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if !lr.MFARequired || lr.MFAToken == "" {
		t.Fatalf("account %q did not present an MFA challenge — check the fixture", user)
	}

	// The challenge token must be useless as a session.
	stepUp := &apiClient{t: t, token: lr.MFAToken}
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/auth/me"},
		{http.MethodPost, "/api/auth/mfa/setup"},
		{http.MethodDelete, "/api/auth/mfa/disable"},
	} {
		if status, _ := stepUp.do(tc.method, tc.path, nil); status != http.StatusUnauthorized {
			t.Errorf("%s %s with the MFA challenge token: status %d, want 401 — MFA is bypassable",
				tc.method, tc.path, status)
		}
	}
}

// ── Multi-tenancy ───────────────────────────────────────────────────────────

type orgRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// createOrg makes an organization and returns it.
func createOrg(t *testing.T, admin *apiClient, name string) orgRef {
	t.Helper()
	raw := admin.mustDo(http.MethodPost, "/api/organizations",
		map[string]string{"name": name}, http.StatusCreated)

	var o orgRef
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatalf("decode created org: %v — body: %s", err, truncate(raw))
	}
	if o.ID == 0 {
		t.Fatalf("created org has id 0 — body: %s", truncate(raw))
	}
	return o
}

// createOrgAdmin makes an admin scoped to one organization — the actor at the
// centre of every tenancy defect found.
func createOrgAdmin(t *testing.T, admin *apiClient, orgID int, username, password string) *apiClient {
	t.Helper()
	admin.mustDo(http.MethodPost, "/api/users", map[string]interface{}{
		"username":  username,
		"password":  password,
		"role":      "admin",
		"full_name": username,
		"org_id":    orgID,
	}, http.StatusCreated)

	c, lr := login(t, username, password)
	if lr.User.OrgID == nil || *lr.User.OrgID != orgID {
		t.Fatalf("user %q has org_id %v, want %d", username, lr.User.OrgID, orgID)
	}
	return c
}

// TestOrgAdminCannotReachAnotherTenant is the core tenancy contract. Each
// endpoint here was, at some point, reachable across tenants: API keys and the
// edge installer leaked another org's credentials, edge-update pushed an
// arbitrary container image to another customer's plant, and the SSO provider
// could be overwritten to hijack their logins.
func TestOrgAdminCannotReachAnotherTenant(t *testing.T) {
	user, pass := adminCredentials()
	admin, _ := login(t, user, pass)

	suffix := uniqueSuffix()
	victim := createOrg(t, admin, "e2e-victim-"+suffix)
	attackerOrg := createOrg(t, admin, "e2e-attacker-"+suffix)

	attacker := createOrgAdmin(t, admin, attackerOrg.ID,
		"e2e-attacker-"+suffix, "e2e-Password-"+suffix)

	cases := []struct {
		name         string
		method, path string
		body         interface{}
	}{
		{"mint the victim's API key", http.MethodPost,
			fmt.Sprintf("/api/organizations/%d/api-keys", victim.ID),
			map[string]string{"name": "stolen"}},
		{"list the victim's API keys", http.MethodGet,
			fmt.Sprintf("/api/organizations/%d/api-keys", victim.ID), nil},
		{"download the victim's edge installer", http.MethodGet,
			fmt.Sprintf("/api/organizations/%d/edge-installer", victim.ID), nil},
		{"restart the victim's plant", http.MethodPost,
			fmt.Sprintf("/api/organizations/%d/edge-restart", victim.ID), map[string]string{}},
		{"push an image to the victim's plant", http.MethodPost,
			fmt.Sprintf("/api/organizations/%d/edge-update", victim.ID),
			map[string]string{"version": "x", "image": "attacker/evil:latest"}},
		{"read the victim's SSO config", http.MethodGet,
			fmt.Sprintf("/api/organizations/%d/sso-providers", victim.ID), nil},
		{"delete the victim organization", http.MethodDelete,
			fmt.Sprintf("/api/organizations/%d", victim.ID), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := attacker.do(tc.method, tc.path, tc.body)
			if status < 400 {
				t.Fatalf("an admin of org %d could %s (status %d) — cross-tenant access: %s",
					attackerOrg.ID, tc.name, status, truncate(body))
			}
		})
	}
}

// TestOrgAdminCannotEscalateToGlobalAdmin covers the platform-takeover path:
// /api/users had no org scoping, so an org admin could create a user with
// role=admin and org_id=null — a global admin — or reset the real one's password.
func TestOrgAdminCannotEscalateToGlobalAdmin(t *testing.T) {
	user, pass := adminCredentials()
	admin, adminLogin := login(t, user, pass)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "e2e-escalate-"+suffix)
	orgAdmin := createOrgAdmin(t, admin, org.ID, "e2e-esc-"+suffix, "e2e-Password-"+suffix)

	t.Run("cannot create a global admin", func(t *testing.T) {
		status, body := orgAdmin.do(http.MethodPost, "/api/users", map[string]interface{}{
			"username":  "e2e-ghost-" + suffix,
			"password":  "e2e-Password-" + suffix,
			"role":      "admin",
			"full_name": "ghost",
			"org_id":    nil, // null org_id + admin role == global admin
		})
		if status < 400 {
			t.Fatalf("an org admin created a GLOBAL admin (status %d): %s", status, truncate(body))
		}
	})

	t.Run("cannot reset the global admin's password", func(t *testing.T) {
		status, body := orgAdmin.do(http.MethodPut,
			fmt.Sprintf("/api/users/%d", adminLogin.User.ID),
			map[string]interface{}{"password": "e2e-pwned-" + suffix})
		if status < 400 {
			t.Fatalf("an org admin reset the GLOBAL admin's password (status %d): %s", status, truncate(body))
		}
	})

	t.Run("user list is scoped to the caller's org", func(t *testing.T) {
		raw := orgAdmin.mustDo(http.MethodGet, "/api/users", nil, http.StatusOK)
		var users []struct {
			Username string `json:"username"`
			OrgID    *int   `json:"org_id"`
		}
		if err := json.Unmarshal(raw, &users); err != nil {
			t.Fatalf("decode user list: %v — body: %s", err, truncate(raw))
		}
		for _, u := range users {
			if u.OrgID == nil || *u.OrgID != org.ID {
				t.Errorf("org admin sees user %q of org %v — the list is not scoped", u.Username, u.OrgID)
			}
		}
	})
}

// TestPlatformWideRoutesRequireGlobalAdmin: the full-database backup runs an
// unfiltered pg_dump (every tenant's data, users, API-key hashes and SSO client
// secrets) and the audit CSV spans every user on the platform. RequireRole(admin)
// let an org admin download both.
func TestPlatformWideRoutesRequireGlobalAdmin(t *testing.T) {
	user, pass := adminCredentials()
	admin, _ := login(t, user, pass)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "e2e-platform-"+suffix)
	orgAdmin := createOrgAdmin(t, admin, org.ID, "e2e-plat-"+suffix, "e2e-Password-"+suffix)

	for _, path := range []string{
		"/api/system/backup",
		"/api/system/backup/list",
		"/api/reports/audit.csv?start=2020-01-01&end=2030-01-01",
	} {
		if status, body := orgAdmin.do(http.MethodGet, path, nil); status < 400 {
			t.Errorf("an org admin reached %s (status %d) — platform-wide data exposed: %s",
				path, status, truncate(body))
		}
	}
}
