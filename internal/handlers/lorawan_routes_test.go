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

// lorawanAuthRouter mirrors the real LoRaWAN route group with stub handlers
// so that auth tests have no database dependency.
func lorawanAuthRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/gateways/:id/lorawan")
	g.Use(middleware.RequireAuth, middleware.OrganizationContext())
	g.GET("/devices", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.POST("/devices/import", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	g.POST("/downlink", func(c *gin.Context) {
		var req DownlinkRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

// healthRouter mirrors the real health route — no auth required.
func healthRouter() *gin.Engine {
	r := gin.New()
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func lorawanJWT(t *testing.T, role string, orgID int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 2,
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

func lorawanReq(t *testing.T, router *gin.Engine, method, path, token, body string) int {
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

// TestLoRa_ListDevices_RequiresAuth verifies that GET /api/gateways/:id/lorawan/devices
// returns 401 when no token is provided.
func TestLoRa_ListDevices_RequiresAuth(t *testing.T) {
	r := lorawanAuthRouter()
	code := lorawanReq(t, r, http.MethodGet, "/api/gateways/1/lorawan/devices", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /lorawan/devices without token: got %d, want 401", code)
	}
}

// TestLoRa_ListDevices_UserAllowed verifies that a regular user with a valid token
// can reach the list endpoint.
func TestLoRa_ListDevices_UserAllowed(t *testing.T) {
	r := lorawanAuthRouter()
	token := lorawanJWT(t, "user", 1)
	code := lorawanReq(t, r, http.MethodGet, "/api/gateways/1/lorawan/devices", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /lorawan/devices with user token: got %d, want 200", code)
	}
}

// TestLoRa_ImportTags_RequiresAdmin verifies that POST /api/gateways/:id/lorawan/devices/import
// returns 403 for a regular user role.
func TestLoRa_ImportTags_RequiresAdmin(t *testing.T) {
	r := lorawanAuthRouter()
	token := lorawanJWT(t, "user", 1)
	body := `{"devices":[]}`
	code := lorawanReq(t, r, http.MethodPost, "/api/gateways/1/lorawan/devices/import", token, body)
	if code != http.StatusForbidden {
		t.Errorf("POST /lorawan/devices/import with user role: got %d, want 403", code)
	}
}

// TestLoRa_ImportTags_AdminAllowed verifies that an admin reaches the import handler.
func TestLoRa_ImportTags_AdminAllowed(t *testing.T) {
	r := lorawanAuthRouter()
	token := lorawanJWT(t, "admin", 1)
	body := `{"devices":[]}`
	code := lorawanReq(t, r, http.MethodPost, "/api/gateways/1/lorawan/devices/import", token, body)
	if code != http.StatusOK {
		t.Errorf("POST /lorawan/devices/import with admin role: got %d, want 200", code)
	}
}

// TestLoRa_Downlink_EmptyBodyReturns400 verifies that POST /api/gateways/:id/lorawan/downlink
// returns 400 when the request body is empty (required fields device_id and payload_hex missing).
func TestLoRa_Downlink_EmptyBodyReturns400(t *testing.T) {
	r := lorawanAuthRouter()
	token := lorawanJWT(t, "user", 1)
	code := lorawanReq(t, r, http.MethodPost, "/api/gateways/1/lorawan/downlink", token, "")
	if code != http.StatusBadRequest {
		t.Errorf("POST /lorawan/downlink with empty body: got %d, want 400", code)
	}
}

// TestLoRa_Downlink_RequiresAuth verifies that the downlink endpoint requires a token.
func TestLoRa_Downlink_RequiresAuth(t *testing.T) {
	r := lorawanAuthRouter()
	body := `{"device_id":"dev1","payload_hex":"AABB"}`
	code := lorawanReq(t, r, http.MethodPost, "/api/gateways/1/lorawan/downlink", "", body)
	if code != http.StatusUnauthorized {
		t.Errorf("POST /lorawan/downlink without token: got %d, want 401", code)
	}
}

// TestHealth_LiveEndpoint verifies that GET /health returns 200 with status ok.
func TestHealth_LiveEndpoint(t *testing.T) {
	r := healthRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// No auth header — health is a public endpoint
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /health: got %d, want 200", w.Code)
	}
}

// TestHealth_AccessibleWithoutAuth verifies that the health endpoint does not
// require an Authorization header — no token yields 200 (not 401).
func TestHealth_AccessibleWithoutAuth(t *testing.T) {
	r := healthRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)
	if w.Code == http.StatusUnauthorized {
		t.Errorf("GET /health should be public; got 401 Unauthorized")
	}
}
