package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// orgAuthRouter mirrors the real /api/organizations route group with stub
// handlers so that auth/authz tests have no database dependency.
func orgAuthRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/organizations")
	g.Use(middleware.RequireAuth, middleware.OrganizationContext())
	g.GET("", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.POST("", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusCreated) })
	g.GET("/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.PUT("/:id", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	g.PUT("/:id/mfa-required", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	g.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

// globalAdminToken builds a JWT with role=admin and NO org_id claim, which is
// how IsGlobalAdmin() identifies a platform-level admin.
func globalAdminToken(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 99,
		"role":    "admin",
		// deliberately no org_id claim
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign global admin token: %v", err)
	}
	return signed
}

// orgUserToken builds a JWT for a plain (non-admin) user scoped to an org.
func orgUserToken(t *testing.T, orgID int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 2,
		"role":    "user",
		"org_id":  float64(orgID),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign user token: %v", err)
	}
	return signed
}

// orgScopedAdminToken builds a JWT with role=admin AND an org_id.
// This admin is NOT a global admin — IsGlobalAdmin() returns false for them.
func orgScopedAdminToken(t *testing.T, orgID int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 3,
		"role":    "admin",
		"org_id":  float64(orgID),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign org-scoped admin token: %v", err)
	}
	return signed
}

func orgDo(t *testing.T, router *gin.Engine, method, path, token, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(w, req)
	return w.Code
}

// TestOrgList_Unauthenticated verifies that GET /api/organizations returns 401
// when no Authorization header is provided.
func TestOrgList_Unauthenticated(t *testing.T) {
	r := orgAuthRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/organizations", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/organizations without auth: got %d, want 401", w.Code)
	}
}

// TestOrgList_AuthenticatedUser verifies that an authenticated user can reach
// the GET /api/organizations handler (auth layer passes; stub returns 200).
func TestOrgList_AuthenticatedUser(t *testing.T) {
	r := orgAuthRouter()
	token := orgUserToken(t, 1)
	code := orgDo(t, r, http.MethodGet, "/api/organizations", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/organizations with user token: got %d, want 200", code)
	}
}

// TestOrgCreate_RequiresAdmin verifies that POST /api/organizations returns 403
// for a regular (non-admin) user.
func TestOrgCreate_RequiresAdmin(t *testing.T) {
	r := orgAuthRouter()
	token := orgUserToken(t, 1)
	body := `{"name":"New Org"}`
	code := orgDo(t, r, http.MethodPost, "/api/organizations", token, body)
	if code != http.StatusForbidden {
		t.Errorf("POST /api/organizations with user token: got %d, want 403", code)
	}
}

// TestOrgCreate_GlobalAdminAllowed verifies that a global admin (role=admin,
// no org_id) can reach POST /api/organizations.
func TestOrgCreate_GlobalAdminAllowed(t *testing.T) {
	r := orgAuthRouter()
	token := globalAdminToken(t)
	body := `{"name":"New Org"}`
	code := orgDo(t, r, http.MethodPost, "/api/organizations", token, body)
	if code != http.StatusCreated {
		t.Errorf("POST /api/organizations with global admin: got %d, want 201", code)
	}
}

// TestOrgCreate_OrgScopedAdminAllowed verifies that even an org-scoped admin
// passes RequireRole(admin) and reaches the handler.
// NOTE: In production the handler itself would enforce global-admin semantics;
// here we are testing the route middleware layer only.
func TestOrgCreate_OrgScopedAdminAllowed(t *testing.T) {
	r := orgAuthRouter()
	token := orgScopedAdminToken(t, 1)
	body := `{"name":"New Org"}`
	code := orgDo(t, r, http.MethodPost, "/api/organizations", token, body)
	// RequireRole(admin) allows org-scoped admins through; the stub returns 201.
	if code != http.StatusCreated {
		t.Errorf("POST /api/organizations with org-scoped admin: got %d, want 201", code)
	}
}

// TestOrgCreate_Unauthenticated verifies that POST /api/organizations returns 401
// when no token is supplied.
func TestOrgCreate_Unauthenticated(t *testing.T) {
	r := orgAuthRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/organizations", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/organizations without auth: got %d, want 401", w.Code)
	}
}

// TestOrgMFARequired_RequiresAdminRole verifies that PUT /api/organizations/:id/mfa-required
// returns 403 for a regular user and 200 for an admin.
func TestOrgMFARequired_RequiresAdminRole(t *testing.T) {
	r := orgAuthRouter()
	body := `{"mfa_required":true}`

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"unauthenticated", "", http.StatusUnauthorized},
		{"regular user", orgUserToken(t, 1), http.StatusForbidden},
		{"org-scoped admin", orgScopedAdminToken(t, 1), http.StatusOK},
		{"global admin", globalAdminToken(t), http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			if tc.token == "" {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPut, "/api/organizations/1/mfa-required", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				code = w.Code
			} else {
				code = orgDo(t, r, http.MethodPut, "/api/organizations/1/mfa-required", tc.token, body)
			}
			if code != tc.want {
				t.Errorf("PUT /api/organizations/1/mfa-required (%s): got %d, want %d", tc.name, code, tc.want)
			}
		})
	}
}

// TestOrgRoutes_AllUnauthenticated verifies every organization route returns 401
// when the Authorization header is absent.
func TestOrgRoutes_AllUnauthenticated(t *testing.T) {
	r := orgAuthRouter()
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/organizations"},
		{http.MethodPost, "/api/organizations"},
		{http.MethodGet, "/api/organizations/1"},
		{http.MethodPut, "/api/organizations/1"},
		{http.MethodPut, "/api/organizations/1/mfa-required"},
		{http.MethodDelete, "/api/organizations/1"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without auth: got %d, want 401", tc.method, tc.path, w.Code)
		}
	}
}
