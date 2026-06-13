package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
)

const testSecret = "test-jwt-secret-key-minimum-32-chars-ok!"

func TestMain(m *testing.M) {
	os.Setenv("JWT_SECRET", testSecret)
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func makeToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func newTestRouter() *gin.Engine {
	r := gin.New()
	r.Use(RequireAuth)
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestRequireAuth_MissingToken(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)

	newTestRouter().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer not.a.real.token")

	newTestRouter().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	token := makeToken(t, jwt.MapClaims{
		"user_id":  1,
		"username": "alice",
		"role":     "admin",
		"exp":      time.Now().Add(-1 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	newTestRouter().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_ValidToken(t *testing.T) {
	token := makeToken(t, jwt.MapClaims{
		"user_id":  42,
		"username": "mario",
		"role":     "user",
		"exp":      time.Now().Add(1 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	newTestRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRequireAuth_TokenWithoutBearer(t *testing.T) {
	token := makeToken(t, jwt.MapClaims{
		"user_id": 1,
		"role":    "admin",
		"exp":     time.Now().Add(1 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	// Raw token without "Bearer " prefix — TrimPrefix still works (returns whole string)
	req.Header.Set("Authorization", token)

	newTestRouter().ServeHTTP(w, req)

	// Should still be parsed since TrimPrefix returns the original string if prefix not found
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want %d", w.Code, http.StatusOK)
	}
}
