package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// alarmTagRouter mirrors the /api/tags/:id/alarms sub-routes.
// GET /api/tags/:id/alarms — requires auth only (no admin role).
// PUT /api/tags/:id/alarms — requires admin role.
func alarmTagRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/tags")
	g.Use(middleware.RequireAuth, middleware.OrganizationContext())
	g.GET("/:id/alarms", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.PUT("/:id/alarms", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

// alarmEventRouter mirrors the /api/alarms group routes.
// Active alarms, history — auth only.
// POST /:id/ack — requires admin.
func alarmEventRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/alarms")
	g.Use(middleware.RequireAuth, middleware.OrganizationContext())
	g.GET("/active", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.GET("/history", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.POST("/:id/ack", middleware.RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func alarmDo(t *testing.T, router *gin.Engine, method, path, token, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	// Provide org context header so OrganizationContext() can resolve the org.
	if token != "" {
		req.Header.Set("X-Organization-ID", "1")
	}
	router.ServeHTTP(w, req)
	return w.Code
}

// ---------------------------------------------------------------------------
// GET /api/tags/:id/alarms
// ---------------------------------------------------------------------------

// TestAlarmRoute_GetTagAlarms_Unauthenticated verifies that the route returns
// 401 when no Authorization header is present.
func TestAlarmRoute_GetTagAlarms_Unauthenticated(t *testing.T) {
	r := alarmTagRouter()
	code := alarmDo(t, r, http.MethodGet, "/api/tags/1/alarms", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/tags/1/alarms without auth: got %d, want 401", code)
	}
}

// TestAlarmRoute_GetTagAlarms_UserAllowed verifies that a plain (non-admin)
// user can reach the GET /api/tags/:id/alarms handler.
func TestAlarmRoute_GetTagAlarms_UserAllowed(t *testing.T) {
	r := alarmTagRouter()
	token := orgUserToken(t, 1)
	code := alarmDo(t, r, http.MethodGet, "/api/tags/1/alarms", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/tags/1/alarms with user token: got %d, want 200", code)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/tags/:id/alarms — requires admin
// ---------------------------------------------------------------------------

// TestAlarmRoute_SaveTagAlarms_RequiresAdmin verifies that non-admin users
// receive 403 on the write endpoint.
func TestAlarmRoute_SaveTagAlarms_RequiresAdmin(t *testing.T) {
	r := alarmTagRouter()
	token := orgUserToken(t, 1)
	code := alarmDo(t, r, http.MethodPut, "/api/tags/1/alarms", token, `[]`)
	if code != http.StatusForbidden {
		t.Errorf("PUT /api/tags/1/alarms with user token: got %d, want 403", code)
	}
}

// TestAlarmRoute_SaveTagAlarms_AdminAllowed verifies that an admin can reach
// the PUT /api/tags/:id/alarms handler.
func TestAlarmRoute_SaveTagAlarms_AdminAllowed(t *testing.T) {
	r := alarmTagRouter()
	token := adminToken(t, 1)
	code := alarmDo(t, r, http.MethodPut, "/api/tags/1/alarms", token, `[]`)
	if code != http.StatusOK {
		t.Errorf("PUT /api/tags/1/alarms with admin token: got %d, want 200", code)
	}
}

// TestAlarmRoute_SaveTagAlarms_Unauthenticated verifies that 401 is returned
// when no token is provided on the write endpoint.
func TestAlarmRoute_SaveTagAlarms_Unauthenticated(t *testing.T) {
	r := alarmTagRouter()
	code := alarmDo(t, r, http.MethodPut, "/api/tags/1/alarms", "", `[]`)
	if code != http.StatusUnauthorized {
		t.Errorf("PUT /api/tags/1/alarms without auth: got %d, want 401", code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/alarms/active  (mirrors the alarm-events listing endpoint)
// ---------------------------------------------------------------------------

// TestAlarmRoute_GetActiveAlarms_Unauthenticated verifies that the route
// returns 401 without a token.
func TestAlarmRoute_GetActiveAlarms_Unauthenticated(t *testing.T) {
	r := alarmEventRouter()
	code := alarmDo(t, r, http.MethodGet, "/api/alarms/active", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/alarms/active without auth: got %d, want 401", code)
	}
}

// TestAlarmRoute_GetActiveAlarms_UserAllowed verifies that an authenticated
// user can reach the active alarms endpoint.
func TestAlarmRoute_GetActiveAlarms_UserAllowed(t *testing.T) {
	r := alarmEventRouter()
	token := orgUserToken(t, 1)
	code := alarmDo(t, r, http.MethodGet, "/api/alarms/active", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/alarms/active with user token: got %d, want 200", code)
	}
}

// TestAlarmRoute_GetAlarmHistory_Unauthenticated verifies that 401 is returned
// without a token on the history endpoint.
func TestAlarmRoute_GetAlarmHistory_Unauthenticated(t *testing.T) {
	r := alarmEventRouter()
	code := alarmDo(t, r, http.MethodGet, "/api/alarms/history", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/alarms/history without auth: got %d, want 401", code)
	}
}

// TestAlarmRoute_GetAlarmHistory_UserAllowed verifies that an authenticated
// user can reach the history endpoint.
func TestAlarmRoute_GetAlarmHistory_UserAllowed(t *testing.T) {
	r := alarmEventRouter()
	token := orgUserToken(t, 1)
	code := alarmDo(t, r, http.MethodGet, "/api/alarms/history", token, "")
	if code != http.StatusOK {
		t.Errorf("GET /api/alarms/history with user token: got %d, want 200", code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/alarms/:id/ack — requires admin
// ---------------------------------------------------------------------------

// TestAlarmRoute_AckAlarm_Unauthenticated verifies that 401 is returned
// without a token on the ack endpoint.
func TestAlarmRoute_AckAlarm_Unauthenticated(t *testing.T) {
	r := alarmEventRouter()
	code := alarmDo(t, r, http.MethodPost, "/api/alarms/5/ack", "", "")
	if code != http.StatusUnauthorized {
		t.Errorf("POST /api/alarms/5/ack without auth: got %d, want 401", code)
	}
}

// TestAlarmRoute_AckAlarm_UserForbidden verifies that a regular user cannot
// acknowledge alarms (RequireRole(admin) blocks them).
func TestAlarmRoute_AckAlarm_UserForbidden(t *testing.T) {
	r := alarmEventRouter()
	token := orgUserToken(t, 1)
	code := alarmDo(t, r, http.MethodPost, "/api/alarms/5/ack", token, "")
	if code != http.StatusForbidden {
		t.Errorf("POST /api/alarms/5/ack with user token: got %d, want 403", code)
	}
}

// TestAlarmRoute_AckAlarm_AdminAllowed verifies that an admin can reach the
// ack endpoint (stub returns 200).
func TestAlarmRoute_AckAlarm_AdminAllowed(t *testing.T) {
	r := alarmEventRouter()
	token := adminToken(t, 1)
	code := alarmDo(t, r, http.MethodPost, "/api/alarms/5/ack", token, "")
	if code != http.StatusOK {
		t.Errorf("POST /api/alarms/5/ack with admin token: got %d, want 200", code)
	}
}

// TestAlarmRoute_AckAlarm_GlobalAdminAllowed verifies that a global admin
// (no org_id) also reaches the ack endpoint.
func TestAlarmRoute_AckAlarm_GlobalAdminAllowed(t *testing.T) {
	r := alarmEventRouter()
	token := globalAdminToken(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/alarms/5/ack", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	// Global admin: no X-Organization-ID header — OrganizationContext() should
	// let them through without an org scoped context.
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST /api/alarms/5/ack with global admin: got %d, want 200", w.Code)
	}
}

// TestAlarmRoutes_AllEndpointsRequireAuth is a table-driven sweep ensuring
// every alarm route returns 401 without credentials.
func TestAlarmRoutes_AllEndpointsRequireAuth(t *testing.T) {
	tagRouter := alarmTagRouter()
	eventRouter := alarmEventRouter()

	cases := []struct {
		router *gin.Engine
		method string
		path   string
	}{
		{tagRouter, http.MethodGet, "/api/tags/1/alarms"},
		{tagRouter, http.MethodPut, "/api/tags/1/alarms"},
		{eventRouter, http.MethodGet, "/api/alarms/active"},
		{eventRouter, http.MethodGet, "/api/alarms/history"},
		{eventRouter, http.MethodPost, "/api/alarms/5/ack"},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		tc.router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without auth: got %d, want 401", tc.method, tc.path, w.Code)
		}
	}
}
