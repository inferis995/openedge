package middleware_test

import (
	"database/sql"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// stubDriver is a minimal sql.Driver that returns a single bool column.
type stubRows struct {
	value bool
	done  bool
}

func (r *stubRows) Columns() []string        { return []string{"can_write_tags"} }
func (r *stubRows) Close() error             { return nil }
func (r *stubRows) Next(dest []driver.Value) error {
	if r.done {
		return sql.ErrNoRows
	}
	r.done = true
	dest[0] = r.value
	return nil
}

type stubResult struct{}

func (stubResult) LastInsertId() (int64, error) { return 0, nil }
func (stubResult) RowsAffected() (int64, error)  { return 0, nil }

// permToken creates a JWT with the given role and org_id.
func permToken(t *testing.T, role string, orgID int, userID int) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(userID),
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

func TestRequirePermission_AdminAlwaysPasses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// nil db: admin should never reach the DB query
	r := gin.New()
	r.Use(middleware.RequireAuth, middleware.OrganizationContext())
	r.GET("/test", middleware.RequirePermission(nil, middleware.PermWriteTags), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+permToken(t, "admin", 1, 1))
	req.Header.Set("X-Organization-ID", "1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("admin got %d, want 200", w.Code)
	}
}

func TestRequirePermission_MissingUserIDDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	// No RequireAuth — user_id context key won't be set
	r.GET("/test", middleware.RequirePermission(nil, middleware.PermWriteTags), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("missing user_id got %d, want 403", w.Code)
	}
}

func TestRequirePermission_ConstNames(t *testing.T) {
	// Verify all permission constants are non-empty and unique
	perms := []string{
		middleware.PermWriteTags,
		middleware.PermAckAlarms,
		middleware.PermExportData,
		middleware.PermManageRecipes,
		middleware.PermManageShifts,
		middleware.PermViewAudit,
		middleware.PermDownloadInstaller,
	}
	seen := map[string]bool{}
	for _, p := range perms {
		if p == "" {
			t.Error("permission constant must not be empty")
		}
		if seen[p] {
			t.Errorf("duplicate permission constant: %q", p)
		}
		seen[p] = true
	}
}
