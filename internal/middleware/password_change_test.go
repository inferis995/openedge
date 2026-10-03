package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
)

// A session opened with the default password may change the password, read
// who it is and sign out — nothing else.
func TestRequireAuth_DefaultPasswordSessionIsConfined(t *testing.T) {
	r := gin.New()
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	g := r.Group("/api", RequireAuth)
	g.GET("/auth/me", ok)
	g.PUT("/auth/me/password", ok)
	g.POST("/auth/logout", ok)
	g.GET("/tags", ok)
	g.POST("/users", ok)
	g.GET("/auth/me/other", ok)
	g.PUT("/auth/me", ok)

	claims := func(pc bool) jwt.MapClaims {
		c := jwt.MapClaims{"user_id": 1, "username": "admin", "role": "admin", "exp": time.Now().Add(time.Hour).Unix()}
		if pc {
			c[auth.PasswordChangeClaim] = true
		}
		return c
	}
	confined, free := makeToken(t, claims(true)), makeToken(t, claims(false))

	cases := []struct {
		method, path string
		confinedOK   bool
	}{
		{http.MethodGet, "/api/auth/me", true},
		{http.MethodPut, "/api/auth/me/password", true},
		{http.MethodPost, "/api/auth/logout", true},
		{http.MethodGet, "/api/tags", false},
		{http.MethodPost, "/api/users", false},
		{http.MethodGet, "/api/auth/me/other", false},
		{http.MethodPut, "/api/auth/me", false}, // right path, wrong method
	}
	for _, tc := range cases {
		do := func(token string) *httptest.ResponseRecorder {
			req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w
		}
		w := do(confined)
		if tc.confinedOK && w.Code != http.StatusOK {
			t.Errorf("%s %s with the default password: %d, want 200", tc.method, tc.path, w.Code)
		}
		if !tc.confinedOK {
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), PasswordChangeRequired) {
				t.Errorf("%s %s with the default password: %d %s, want 403 %s", tc.method, tc.path, w.Code, w.Body.String(), PasswordChangeRequired)
			}
		}
		if w := do(free); w.Code == http.StatusForbidden {
			t.Errorf("%s %s with a changed password was refused", tc.method, tc.path)
		}
	}
}
