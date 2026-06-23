package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// shadowAuthRouter mirrors the real /api/tags shadow sub-routes with stub
// handlers so that auth tests have no Redis or database dependency.
// It also replicates the numeric-ID validation present in GetShadow.
func shadowAuthRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/tags")
	g.Use(middleware.RequireAuth, middleware.OrganizationContext())

	// GET /api/tags/:id/shadow — replicates the Atoi check from GetShadow.
	g.GET("/:id/shadow", func(c *gin.Context) {
		if _, err := strconv.Atoi(c.Param("id")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tag id"})
			return
		}
		c.Status(http.StatusOK)
	})

	// GET /api/tags/shadows — requires gateway_id query param (mirrors GetShadowBatch).
	g.GET("/shadows", func(c *gin.Context) {
		if c.Query("gateway_id") == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "gateway_id required"})
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

// shadowDo fires a request on the stub router and returns the status code.
func shadowDo(t *testing.T, router *gin.Engine, method, path, token string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Organization-ID", "1")
	router.ServeHTTP(w, req)
	return w.Code
}

// TestShadow_GetSingle_Unauthenticated verifies that GET /api/tags/:id/shadow
// returns 401 when no Authorization header is provided.
func TestShadow_GetSingle_Unauthenticated(t *testing.T) {
	r := shadowAuthRouter()
	code := shadowDo(t, r, http.MethodGet, "/api/tags/1/shadow", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/tags/1/shadow without auth: got %d, want 401", code)
	}
}

// TestShadow_GetSingle_ValidToken verifies that an authenticated request to
// GET /api/tags/:id/shadow passes auth and reaches the stub handler (200).
func TestShadow_GetSingle_ValidToken(t *testing.T) {
	r := shadowAuthRouter()
	token := adminToken(t, 1)
	code := shadowDo(t, r, http.MethodGet, "/api/tags/42/shadow", token)
	if code != http.StatusOK {
		t.Errorf("GET /api/tags/42/shadow with valid token: got %d, want 200", code)
	}
}

// TestShadow_GetSingle_NonNumericID verifies that a non-numeric tag ID in
// GET /api/tags/:id/shadow is rejected with 400.
func TestShadow_GetSingle_NonNumericID(t *testing.T) {
	r := shadowAuthRouter()
	token := adminToken(t, 1)

	cases := []struct {
		id   string
		want int
	}{
		{"abc", http.StatusBadRequest},
		{"1.5", http.StatusBadRequest},
		{"12abc", http.StatusBadRequest},
		{"1", http.StatusOK},
		{"99", http.StatusOK},
	}

	for _, tc := range cases {
		code := shadowDo(t, r, http.MethodGet, "/api/tags/"+tc.id+"/shadow", token)
		if code != tc.want {
			t.Errorf("GET /api/tags/%s/shadow: got %d, want %d", tc.id, code, tc.want)
		}
	}
}

// TestShadow_Batch_Unauthenticated verifies that GET /api/tags/shadows
// returns 401 when no Authorization header is provided.
func TestShadow_Batch_Unauthenticated(t *testing.T) {
	r := shadowAuthRouter()
	code := shadowDo(t, r, http.MethodGet, "/api/tags/shadows?gateway_id=1", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/tags/shadows without auth: got %d, want 401", code)
	}
}

// TestShadow_Batch_ValidToken verifies that an authenticated request to
// GET /api/tags/shadows passes auth and the stub handler returns 200.
func TestShadow_Batch_ValidToken(t *testing.T) {
	r := shadowAuthRouter()
	token := adminToken(t, 1)
	code := shadowDo(t, r, http.MethodGet, "/api/tags/shadows?gateway_id=1", token)
	if code != http.StatusOK {
		t.Errorf("GET /api/tags/shadows with valid token: got %d, want 200", code)
	}
}

// TestShadow_Batch_MissingGatewayID verifies that omitting gateway_id query
// param returns 400.
func TestShadow_Batch_MissingGatewayID(t *testing.T) {
	r := shadowAuthRouter()
	token := adminToken(t, 1)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tags/shadows", nil) // no gateway_id
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-ID", "1")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/tags/shadows without gateway_id: got %d, want 400", w.Code)
	}
}
