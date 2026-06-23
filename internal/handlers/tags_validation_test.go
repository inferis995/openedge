package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

func tagDeleteStubRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api", middleware.RequireAuth, middleware.OrganizationContext())
	// Replicates only the Atoi validation from the real Delete handler.
	g.DELETE("/tags/:id", func(c *gin.Context) {
		if _, err := strconv.Atoi(c.Param("id")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tag id"})
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

func adminToken(t *testing.T, orgID int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 1,
		"role":    "admin",
		"org_id":  float64(orgID),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestTagDelete_InvalidID_Returns400(t *testing.T) {
	r := tagDeleteStubRouter()
	token := adminToken(t, 1)

	tests := []struct {
		id   string
		want int
	}{
		{"abc", http.StatusBadRequest},
		{"1.5", http.StatusBadRequest},
		{"1abc", http.StatusBadRequest},
		{"123", http.StatusOK},
		{"0", http.StatusOK},
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/tags/"+tt.id, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Organization-ID", "1")
		r.ServeHTTP(w, req)
		if w.Code != tt.want {
			t.Errorf("DELETE /api/tags/%s → %d, want %d", tt.id, w.Code, tt.want)
		}
	}
}
