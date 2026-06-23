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
)

// makeToken creates a signed JWT with the given role, orgID, and userID.
// Pass orgID=nil to simulate a global admin (no org_id claim).
func makeToken(t *testing.T, role string, orgID *int, userID int) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id":  float64(userID),
		"role":     role,
		"username": "testuser",
		"exp":      time.Now().Add(time.Hour).Unix(),
	}
	if orgID != nil {
		claims["org_id"] = float64(*orgID)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("makeToken: sign: %v", err)
	}
	return signed
}

// authRouter mirrors the real auth route structure with stub handlers
// so auth/RBAC tests have no DB dependency.
func authRouter() *gin.Engine {
	r := gin.New()
	// Enable 405 responses for registered routes accessed with the wrong HTTP method.
	r.HandleMethodNotAllowed = true

	// Public routes — no auth required.
	pub := r.Group("/api/auth")
	pub.POST("/login", func(c *gin.Context) { c.Status(http.StatusOK) })
	pub.POST("/mfa/verify", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Authenticated routes — require valid JWT.
	priv := r.Group("/api/auth/me", middleware.RequireAuth)
	priv.GET("/mfa/status", func(c *gin.Context) { c.Status(http.StatusOK) })
	priv.POST("/mfa/setup", func(c *gin.Context) { c.Status(http.StatusOK) })
	priv.DELETE("/mfa/disable", func(c *gin.Context) { c.Status(http.StatusOK) })

	return r
}

// doReq is a small helper for one-shot requests.
func doReq(t *testing.T, router *gin.Engine, method, path, token, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(w, req)
	return w.Code
}

// TestAuth_LoginIsPublic verifies that POST /api/auth/login requires no token.
func TestAuth_LoginIsPublic(t *testing.T) {
	r := authRouter()
	orgID := 1
	token := makeToken(t, "user", &orgID, 42)

	// Authenticated call should succeed (200).
	code := doReq(t, r, http.MethodPost, "/api/auth/login", token, `{"username":"u","password":"p"}`)
	if code != http.StatusOK {
		t.Errorf("POST /api/auth/login with token: got %d, want 200", code)
	}
	// Unauthenticated call should also succeed — the route is public.
	code = doReq(t, r, http.MethodPost, "/api/auth/login", "", `{"username":"u","password":"p"}`)
	if code != http.StatusOK {
		t.Errorf("POST /api/auth/login without token: got %d, want 200", code)
	}
}

// TestAuth_LoginWrongMethod verifies that non-POST methods on /api/auth/login return 405.
func TestAuth_LoginWrongMethod(t *testing.T) {
	r := authRouter()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		code := doReq(t, r, method, "/api/auth/login", "", "")
		if code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/auth/login: got %d, want 405", method, code)
		}
	}
}

// TestAuth_MFAVerifyIsPublic verifies that POST /api/auth/mfa/verify needs no token.
func TestAuth_MFAVerifyIsPublic(t *testing.T) {
	r := authRouter()
	body := `{"mfa_token":"tok","code":"123456"}`

	// Without token — should reach the handler (200 stub).
	code := doReq(t, r, http.MethodPost, "/api/auth/mfa/verify", "", body)
	if code != http.StatusOK {
		t.Errorf("POST /api/auth/mfa/verify without token: got %d, want 200", code)
	}
}

// TestAuth_MFAStatusRequiresAuth verifies that GET /api/auth/me/mfa/status returns
// 401 when no JWT is supplied.
func TestAuth_MFAStatusRequiresAuth(t *testing.T) {
	r := authRouter()
	code := doReq(t, r, http.MethodGet, "/api/auth/me/mfa/status", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/auth/me/mfa/status without token: got %d, want 401", code)
	}
}

// TestAuth_MFAStatusAllowedWithToken verifies that a valid JWT reaches the handler.
func TestAuth_MFAStatusAllowedWithToken(t *testing.T) {
	r := authRouter()
	orgID := 1
	token := makeToken(t, "user", &orgID, 42)
	code := doReq(t, r, http.MethodGet, "/api/auth/me/mfa/status", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/auth/me/mfa/status with token: got %d, want 200", code)
	}
}

// TestAuth_MFASetupRequiresAuth verifies that POST /api/auth/me/mfa/setup returns
// 401 without a token.
func TestAuth_MFASetupRequiresAuth(t *testing.T) {
	r := authRouter()
	code := doReq(t, r, http.MethodPost, "/api/auth/me/mfa/setup", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("POST /api/auth/me/mfa/setup without token: got %d, want 401", code)
	}
}

// TestAuth_MFASetupAllowedWithToken verifies that a valid JWT reaches the handler.
func TestAuth_MFASetupAllowedWithToken(t *testing.T) {
	r := authRouter()
	orgID := 1
	token := makeToken(t, "user", &orgID, 42)
	code := doReq(t, r, http.MethodPost, "/api/auth/me/mfa/setup", token, "")
	if code != http.StatusOK {
		t.Errorf("POST /api/auth/me/mfa/setup with token: got %d, want 200", code)
	}
}

// TestAuth_MFADisableRequiresAuth verifies that DELETE /api/auth/me/mfa/disable returns
// 401 without a token.
func TestAuth_MFADisableRequiresAuth(t *testing.T) {
	r := authRouter()
	code := doReq(t, r, http.MethodDelete, "/api/auth/me/mfa/disable", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("DELETE /api/auth/me/mfa/disable without token: got %d, want 401", code)
	}
}

// TestAuth_MFADisableAllowedWithToken verifies that a valid JWT reaches the handler.
func TestAuth_MFADisableAllowedWithToken(t *testing.T) {
	r := authRouter()
	orgID := 1
	token := makeToken(t, "user", &orgID, 42)
	code := doReq(t, r, http.MethodDelete, "/api/auth/me/mfa/disable", token, "")
	if code != http.StatusOK {
		t.Errorf("DELETE /api/auth/me/mfa/disable with token: got %d, want 200", code)
	}
}

// TestAuth_PrivateRoutesRejectExpiredToken verifies that an expired JWT is rejected
// on all authenticated MFA routes.
func TestAuth_PrivateRoutesRejectExpiredToken(t *testing.T) {
	r := authRouter()
	// Build an already-expired token.
	claims := jwt.MapClaims{
		"user_id":  float64(1),
		"role":     "user",
		"org_id":   float64(1),
		"username": "testuser",
		"exp":      time.Now().Add(-time.Hour).Unix(), // expired
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expired, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/auth/me/mfa/status"},
		{http.MethodPost, "/api/auth/me/mfa/setup"},
		{http.MethodDelete, "/api/auth/me/mfa/disable"},
	}
	for _, tc := range cases {
		code := doReq(t, r, tc.method, tc.path, expired, "")
		if code != http.StatusUnauthorized {
			t.Errorf("%s %s with expired token: got %d, want 401", tc.method, tc.path, code)
		}
	}
}
