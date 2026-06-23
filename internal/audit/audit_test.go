package audit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/audit"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newCtx builds a minimal gin.Context backed by a real *httptest.ResponseRecorder.
func newCtx(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	return c, w
}

// ---------------------------------------------------------------------------
// Log() — nil DB must not panic
// ---------------------------------------------------------------------------

func TestLog_NilDB_NoPanic(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/1/write")
	audit.Log(c, nil, audit.Entry{
		Action:  "tag.write",
		Success: true,
		Details: map[string]interface{}{"tag_id": 1, "value": 42},
	})
	// If we reach here without panic, the nil-guard works.
}

func TestLog_NilDB_EmptyEntry_NoPanic(t *testing.T) {
	c, _ := newCtx(http.MethodGet, "/api/tags")
	audit.Log(c, nil, audit.Entry{})
}

// ---------------------------------------------------------------------------
// Log() — various Entry configurations with nil DB
// ---------------------------------------------------------------------------

func TestLog_SuccessTrue(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/recipes/load")
	e := audit.Entry{
		Action:  "recipe.load",
		Success: true,
		Details: map[string]interface{}{"recipe_id": 7, "operator": "alice"},
	}
	// Must not panic. DB is nil so no actual insert happens.
	audit.Log(c, nil, e)
}

func TestLog_SuccessFalse(t *testing.T) {
	c, _ := newCtx(http.MethodDelete, "/api/tags/5")
	e := audit.Entry{
		Action:  "tag.delete",
		Success: false,
	}
	audit.Log(c, nil, e)
}

func TestLog_NilDetails_NoPanic(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/auth/logout")
	audit.Log(c, nil, audit.Entry{
		Action:  "auth.logout",
		Success: true,
		Details: nil,
	})
}

func TestLog_EmptyDetails_NoPanic(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/auth/login")
	audit.Log(c, nil, audit.Entry{
		Action:  "auth.login",
		Success: true,
		Details: map[string]interface{}{},
	})
}

func TestLog_LargeDetails_NoPanic(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/batch")
	details := make(map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		details[string(rune('a'+i%26))] = i
	}
	audit.Log(c, nil, audit.Entry{
		Action:  "batch.op",
		Success: true,
		Details: details,
	})
}

// ---------------------------------------------------------------------------
// Log() — context with JWT claims (actor extraction)
// ---------------------------------------------------------------------------

func TestLog_WithJWTClaims_Float64UserID(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/1/write")
	claims := jwt.MapClaims{
		"user_id":  float64(42),
		"username": "bob",
		"org_id":   float64(1),
	}
	c.Set(middleware.UserKey, claims)

	// Must not panic with a valid JWT user in context.
	audit.Log(c, nil, audit.Entry{
		Action:  "tag.write",
		Success: true,
	})
}

func TestLog_WithJWTClaims_IntUserID(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/2/write")
	claims := jwt.MapClaims{
		"user_id":  int(7),
		"username": "carol",
	}
	c.Set(middleware.UserKey, claims)
	audit.Log(c, nil, audit.Entry{Action: "tag.write", Success: true})
}

func TestLog_WithJWTClaims_Int64UserID(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/3/write")
	claims := jwt.MapClaims{
		"user_id":  int64(999),
		"username": "dave",
	}
	c.Set(middleware.UserKey, claims)
	audit.Log(c, nil, audit.Entry{Action: "tag.write", Success: true})
}

func TestLog_WithJWTClaims_MissingUsername(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/4/write")
	claims := jwt.MapClaims{
		"user_id": float64(5),
		// username absent — actor() should return ""
	}
	c.Set(middleware.UserKey, claims)
	audit.Log(c, nil, audit.Entry{Action: "tag.write", Success: false})
}

func TestLog_WithJWTClaims_ZeroUserID(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/5/write")
	claims := jwt.MapClaims{
		"user_id":  float64(0),
		"username": "system",
	}
	c.Set(middleware.UserKey, claims)
	// userIDArg should be nil (uid == 0)
	audit.Log(c, nil, audit.Entry{Action: "system.op", Success: true})
}

func TestLog_NoJWT_NoPanic(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/6/write")
	// No JWT in context — actor() returns (0, "")
	audit.Log(c, nil, audit.Entry{Action: "tag.write", Success: true})
}

func TestLog_WrongContextType_NoPanic(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/7/write")
	// Stash a non-MapClaims value under UserKey.
	c.Set(middleware.UserKey, "not-claims")
	audit.Log(c, nil, audit.Entry{Action: "tag.write", Success: true})
}

// ---------------------------------------------------------------------------
// Entry struct — field values are preserved
// ---------------------------------------------------------------------------

func TestEntry_Fields(t *testing.T) {
	e := audit.Entry{
		Action:  "alarm.ack",
		Success: false,
		Details: map[string]interface{}{"alarm_id": 99},
	}
	if e.Action != "alarm.ack" {
		t.Errorf("Action = %q, want %q", e.Action, "alarm.ack")
	}
	if e.Success {
		t.Error("Success should be false")
	}
	if e.Details["alarm_id"] != 99 {
		t.Errorf("Details alarm_id = %v, want 99", e.Details["alarm_id"])
	}
}

// ---------------------------------------------------------------------------
// Log() — IP and User-Agent from request
// ---------------------------------------------------------------------------

func TestLog_WithIPAndUserAgent(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/api/tags/1/write")
	c.Request.Header.Set("User-Agent", "TestAgent/1.0")
	c.Request.Header.Set("X-Real-IP", "10.0.0.1")
	audit.Log(c, nil, audit.Entry{Action: "tag.write", Success: true})
}

func TestLog_ActionVariants(t *testing.T) {
	actions := []string{
		"tag.write",
		"recipe.load",
		"alarm.ack",
		"user.create",
		"user.delete",
		"settings.update",
		"", // empty action — should not panic
	}
	for _, action := range actions {
		c, _ := newCtx(http.MethodPost, "/test")
		audit.Log(c, nil, audit.Entry{Action: action, Success: true})
	}
}
