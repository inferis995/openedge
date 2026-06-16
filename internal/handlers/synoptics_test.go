package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// TestMain sets shared test state for the handlers package.
func TestMain(m *testing.M) {
	os.Setenv("JWT_SECRET", "test-handlers-secret-key-min-32-chars!")
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// synopticAuthRouter mirrors the real synoptic route group with stub handlers
// so that auth tests have no database dependency.
func synopticAuthRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/synoptics")
	g.Use(middleware.RequireAuth, middleware.OrganizationContext())
	g.GET("", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.GET("/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.POST("", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusCreated) })
	g.PUT("/:id", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	g.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func synopticJWT(t *testing.T, role string, orgID int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 1,
		"role":    role,
		"org_id":  float64(orgID),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func synopticReq(t *testing.T, router *gin.Engine, method, path, token, body string) int {
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

const synopticBody = `{"name":"test","background_color":"#0f172a","canvas_w":1280,"canvas_h":720,"layout":[]}`

// TestSynoptics_UserForbiddenOnWrite verifies that a regular user (role=user)
// receives 403 Forbidden on all write operations.
func TestSynoptics_UserForbiddenOnWrite(t *testing.T) {
	r := synopticAuthRouter()
	token := synopticJWT(t, "user", 1)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/synoptics", synopticBody},
		{http.MethodPut, "/api/synoptics/1", synopticBody},
		{http.MethodDelete, "/api/synoptics/1", ""},
	}
	for _, tc := range cases {
		code := synopticReq(t, r, tc.method, tc.path, token, tc.body)
		if code != http.StatusForbidden {
			t.Errorf("%s %s with user role: got %d, want 403", tc.method, tc.path, code)
		}
	}
}

// TestSynoptics_AdminAllowedOnWrite verifies that an admin (role=admin, with
// org_id) passes the authorization layer and reaches the handler.
func TestSynoptics_AdminAllowedOnWrite(t *testing.T) {
	r := synopticAuthRouter()
	token := synopticJWT(t, "admin", 1)

	cases := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{http.MethodPost, "/api/synoptics", synopticBody, http.StatusCreated},
		{http.MethodPut, "/api/synoptics/1", synopticBody, http.StatusOK},
		{http.MethodDelete, "/api/synoptics/1", "", http.StatusOK},
	}
	for _, tc := range cases {
		code := synopticReq(t, r, tc.method, tc.path, token, tc.body)
		if code != tc.want {
			t.Errorf("%s %s with admin role: got %d, want %d", tc.method, tc.path, code, tc.want)
		}
	}
}

// TestSynoptics_UserCanRead verifies that regular users can read synoptics.
func TestSynoptics_UserCanRead(t *testing.T) {
	r := synopticAuthRouter()
	token := synopticJWT(t, "user", 1)

	for _, path := range []string{"/api/synoptics", "/api/synoptics/1"} {
		code := synopticReq(t, r, http.MethodGet, path, token, "")
		if code != http.StatusOK {
			t.Errorf("GET %s with user role: got %d, want 200", path, code)
		}
	}
}

// TestSynoptics_OrgIsolation verifies that a user cannot access synoptics of
// a different organization by spoofing the X-Organization-ID header.
func TestSynoptics_OrgIsolation(t *testing.T) {
	r := synopticAuthRouter()
	token := synopticJWT(t, "user", 1) // user belongs to org 1

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/synoptics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-ID", "99") // attempt to access org 99
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("cross-org header: got %d, want 403", w.Code)
	}
}

// TestSynoptics_UnauthenticatedDenied verifies that unauthenticated requests
// are rejected on all routes.
func TestSynoptics_UnauthenticatedDenied(t *testing.T) {
	r := synopticAuthRouter()

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/synoptics"},
		{http.MethodPost, "/api/synoptics"},
		{http.MethodGet, "/api/synoptics/1"},
		{http.MethodPut, "/api/synoptics/1"},
		{http.MethodDelete, "/api/synoptics/1"},
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
