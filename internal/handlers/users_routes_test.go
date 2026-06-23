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

// usersAuthRouter mirrors the real users route group with stub handlers
// so that auth tests have no database dependency.
func usersAuthRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api", middleware.RequireAuth, middleware.OrganizationContext())
	g.GET("/users", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.POST("/users", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusCreated) })
	g.PUT("/users/:id", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	g.DELETE("/users/:id", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func userJWT(t *testing.T, orgID int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 99,
		"role":    "user",
		"org_id":  float64(orgID),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func usersReq(t *testing.T, router *gin.Engine, method, path, token, body string) int {
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

// TestUser_ListRequiresAuth verifies that GET /api/users returns 401 without a token.
func TestUser_ListRequiresAuth(t *testing.T) {
	r := usersAuthRouter()
	code := usersReq(t, r, http.MethodGet, "/api/users", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/users without token: got %d, want 401", code)
	}
}

// TestUser_ListAllowedForAuthenticatedUser verifies that a regular user can reach
// the list endpoint (read-only, no admin check).
func TestUser_ListAllowedForAuthenticatedUser(t *testing.T) {
	r := usersAuthRouter()
	token := userJWT(t, 1)
	code := usersReq(t, r, http.MethodGet, "/api/users", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/users with user token: got %d, want 200", code)
	}
}

// TestUser_CreateRequiresAdmin verifies that POST /api/users returns 403 for a regular user.
func TestUser_CreateRequiresAdmin(t *testing.T) {
	r := usersAuthRouter()
	token := userJWT(t, 1)
	body := `{"username":"newuser","password":"secret","role":"user"}`
	code := usersReq(t, r, http.MethodPost, "/api/users", token, body)
	if code != http.StatusForbidden {
		t.Errorf("POST /api/users with user role: got %d, want 403", code)
	}
}

// TestUser_CreateAllowedForAdmin verifies that an admin can reach the create endpoint.
func TestUser_CreateAllowedForAdmin(t *testing.T) {
	r := usersAuthRouter()
	token := adminToken(t, 1)
	body := `{"username":"newuser","password":"secret","role":"user"}`
	code := usersReq(t, r, http.MethodPost, "/api/users", token, body)
	if code != http.StatusCreated {
		t.Errorf("POST /api/users with admin role: got %d, want 201", code)
	}
}

// TestUser_DeleteRequiresAdmin verifies that DELETE /api/users/:id returns 403 for a regular user.
func TestUser_DeleteRequiresAdmin(t *testing.T) {
	r := usersAuthRouter()
	token := userJWT(t, 1)
	code := usersReq(t, r, http.MethodDelete, "/api/users/5", token, "")
	if code != http.StatusForbidden {
		t.Errorf("DELETE /api/users/5 with user role: got %d, want 403", code)
	}
}

// TestUser_DeleteAllowedForAdmin verifies that an admin can reach the delete endpoint.
func TestUser_DeleteAllowedForAdmin(t *testing.T) {
	r := usersAuthRouter()
	token := adminToken(t, 1)
	code := usersReq(t, r, http.MethodDelete, "/api/users/5", token, "")
	if code != http.StatusOK {
		t.Errorf("DELETE /api/users/5 with admin role: got %d, want 200", code)
	}
}

// TestUser_DeleteRequiresAuth verifies that DELETE /api/users/:id returns 401 without a token.
func TestUser_DeleteRequiresAuth(t *testing.T) {
	r := usersAuthRouter()
	code := usersReq(t, r, http.MethodDelete, "/api/users/5", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("DELETE /api/users/5 without token: got %d, want 401", code)
	}
}
