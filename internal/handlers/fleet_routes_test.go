package handlers

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// fleetRouter mirrors the real fleet/user-permissions/org-mfa route groups
// with stub handlers — no DB dependency.
//
// Real route structure (from services/core-api/main.go):
//
//	fleetGlobal := api.Group("/fleet")
//	fleetGlobal.Use(RequireAuth, RequireRole(admin))
//	  GET /fleet/status
//
//	users := api.Group("/users")
//	users.Use(RequireAuth, RequireRole(admin))
//	  GET  /users/:id/permissions
//	  PUT  /users/:id/permissions
//
//	orgs := api.Group("/organizations")
//	orgs.Use(RequireAuth, RequireRole(admin))
//	  PUT /organizations/:id/mfa-required
func fleetRouter() *gin.Engine {
	r := gin.New()

	// Fleet status — admin role required.
	fleet := r.Group("/api/fleet")
	fleet.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
	fleet.GET("/status", func(c *gin.Context) { c.Status(http.StatusOK) })

	// User permissions — admin role required (users group already has admin middleware).
	users := r.Group("/api/users")
	users.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
	users.GET("/:id/permissions", func(c *gin.Context) { c.Status(http.StatusOK) })
	users.PUT("/:id/permissions", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Org MFA required — admin role required.
	orgs := r.Group("/api/organizations")
	orgs.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
	orgs.PUT("/:id/mfa-required", func(c *gin.Context) { c.Status(http.StatusOK) })

	return r
}

// intPtr is a tiny helper to take the address of an int literal.
func intPtr(v int) *int { return &v }

// ---------------------------------------------------------------------------
// Fleet status tests
// ---------------------------------------------------------------------------

// TestFleet_StatusRequiresAdmin verifies that a regular user (role=user) is
// rejected with 403 on GET /api/fleet/status.
func TestFleet_StatusRequiresAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "user", intPtr(1), 42)
	code := doReq(t, r, http.MethodGet, "/api/fleet/status", token, "")
	if code != http.StatusForbidden {
		t.Errorf("GET /api/fleet/status with user role: got %d, want 403", code)
	}
}

// TestFleet_StatusUnauthenticated verifies that a missing token returns 401.
func TestFleet_StatusUnauthenticated(t *testing.T) {
	r := fleetRouter()
	code := doReq(t, r, http.MethodGet, "/api/fleet/status", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/fleet/status without token: got %d, want 401", code)
	}
}

// TestFleet_StatusAllowedForOrgAdmin verifies that an org-scoped admin
// (role=admin WITH org_id) passes the middleware layer and reaches the handler.
// The real handler enforces IsGlobalAdmin via DB, but the RBAC gate itself
// should not block org admins.
func TestFleet_StatusAllowedForOrgAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "admin", intPtr(1), 10)
	code := doReq(t, r, http.MethodGet, "/api/fleet/status", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/fleet/status with org admin: got %d, want 200", code)
	}
}

// TestFleet_StatusAllowedForGlobalAdmin verifies that a global admin
// (role=admin, no org_id) passes the middleware layer.
func TestFleet_StatusAllowedForGlobalAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "admin", nil, 1) // nil orgID = global admin
	code := doReq(t, r, http.MethodGet, "/api/fleet/status", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/fleet/status with global admin: got %d, want 200", code)
	}
}

// ---------------------------------------------------------------------------
// User permissions tests
// ---------------------------------------------------------------------------

// TestFleet_GetPermissionsRequiresAdmin verifies that a regular user cannot read
// another user's permissions.
func TestFleet_GetPermissionsRequiresAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "user", intPtr(1), 42)
	code := doReq(t, r, http.MethodGet, "/api/users/5/permissions", token, "")
	if code != http.StatusForbidden {
		t.Errorf("GET /api/users/5/permissions with user role: got %d, want 403", code)
	}
}

// TestFleet_GetPermissionsAllowedForAdmin verifies that an admin can read
// a user's permissions.
func TestFleet_GetPermissionsAllowedForAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "admin", intPtr(1), 10)
	code := doReq(t, r, http.MethodGet, "/api/users/5/permissions", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/users/5/permissions with admin role: got %d, want 200", code)
	}
}

// TestFleet_GetPermissionsUnauthenticated verifies that a missing token returns 401.
func TestFleet_GetPermissionsUnauthenticated(t *testing.T) {
	r := fleetRouter()
	code := doReq(t, r, http.MethodGet, "/api/users/5/permissions", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/users/5/permissions without token: got %d, want 401", code)
	}
}

// TestFleet_UpdatePermissionsRequiresAdmin verifies that a regular user cannot
// update another user's permissions.
func TestFleet_UpdatePermissionsRequiresAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "user", intPtr(1), 42)
	body := `{"can_write_tags":true}`
	code := doReq(t, r, http.MethodPut, "/api/users/5/permissions", token, body)
	if code != http.StatusForbidden {
		t.Errorf("PUT /api/users/5/permissions with user role: got %d, want 403", code)
	}
}

// TestFleet_UpdatePermissionsAllowedForAdmin verifies that an admin can update
// a user's permissions.
func TestFleet_UpdatePermissionsAllowedForAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "admin", intPtr(1), 10)
	body := `{"can_write_tags":true}`
	code := doReq(t, r, http.MethodPut, "/api/users/5/permissions", token, body)
	if code != http.StatusOK {
		t.Errorf("PUT /api/users/5/permissions with admin role: got %d, want 200", code)
	}
}

// TestFleet_UpdatePermissionsUnauthenticated verifies that a missing token returns 401.
func TestFleet_UpdatePermissionsUnauthenticated(t *testing.T) {
	r := fleetRouter()
	body := `{"can_write_tags":true}`
	code := doReq(t, r, http.MethodPut, "/api/users/5/permissions", "", body)
	if code != http.StatusUnauthorized {
		t.Errorf("PUT /api/users/5/permissions without token: got %d, want 401", code)
	}
}

// ---------------------------------------------------------------------------
// Organization MFA required tests
// ---------------------------------------------------------------------------

// TestOrg_SetMFARequiredRequiresAdmin verifies that a regular user cannot change
// the MFA policy for an organization.
func TestOrg_SetMFARequiredRequiresAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "user", intPtr(1), 42)
	body := `{"mfa_required":true}`
	code := doReq(t, r, http.MethodPut, "/api/organizations/1/mfa-required", token, body)
	if code != http.StatusForbidden {
		t.Errorf("PUT /api/organizations/1/mfa-required with user role: got %d, want 403", code)
	}
}

// TestOrg_SetMFARequiredAllowedForAdmin verifies that an admin can update the
// MFA policy.
func TestOrg_SetMFARequiredAllowedForAdmin(t *testing.T) {
	r := fleetRouter()
	token := makeToken(t, "admin", intPtr(1), 10)
	body := `{"mfa_required":true}`
	code := doReq(t, r, http.MethodPut, "/api/organizations/1/mfa-required", token, body)
	if code != http.StatusOK {
		t.Errorf("PUT /api/organizations/1/mfa-required with admin role: got %d, want 200", code)
	}
}

// TestOrg_SetMFARequiredUnauthenticated verifies that a missing token returns 401.
func TestOrg_SetMFARequiredUnauthenticated(t *testing.T) {
	r := fleetRouter()
	body := `{"mfa_required":true}`
	code := doReq(t, r, http.MethodPut, "/api/organizations/1/mfa-required", "", body)
	if code != http.StatusUnauthorized {
		t.Errorf("PUT /api/organizations/1/mfa-required without token: got %d, want 401", code)
	}
}
