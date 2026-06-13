package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func newOrgRouter() *gin.Engine {
	r := gin.New()
	r.Use(RequireAuth)
	r.Use(OrganizationContext())
	r.GET("/data", func(c *gin.Context) {
		orgID, ok := GetOrganizationID(c)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no org"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"org_id": orgID})
	})
	return r
}

func TestOrganizationContext_GlobalAdmin_NoHeader(t *testing.T) {
	// Global admin (role=admin, no org_id) — no header required
	token := makeToken(t, jwt.MapClaims{
		"user_id":  1,
		"username": "admin",
		"role":     "admin",
		// no org_id claim → global admin
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	newOrgRouter().ServeHTTP(w, req)

	// Global admin without header should still reach the handler
	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
		t.Errorf("unexpected status %d", w.Code)
	}
}

func TestOrganizationContext_OrgUser_WithHeader(t *testing.T) {
	orgID := 5
	token := makeToken(t, jwt.MapClaims{
		"user_id":  10,
		"username": "mario",
		"role":     "user",
		"org_id":   float64(orgID), // JWT numbers are float64
		"exp":      time.Now().Add(1 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-ID", strconv.Itoa(orgID))

	newOrgRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestOrganizationContext_OrgUser_WrongOrgHeader(t *testing.T) {
	// User belongs to org 5 but tries to access org 9
	token := makeToken(t, jwt.MapClaims{
		"user_id": 10,
		"role":    "user",
		"org_id":  float64(5),
		"exp":     time.Now().Add(1 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-ID", "9")

	newOrgRouter().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestOrganizationContext_GlobalAdmin_WithHeader(t *testing.T) {
	// Global admin specifying any org should be allowed
	token := makeToken(t, jwt.MapClaims{
		"user_id": 1,
		"role":    "admin",
		"exp":     time.Now().Add(1 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-ID", "9")

	newOrgRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200 (global admin can access any org)", w.Code)
	}
}
