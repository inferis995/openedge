package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

func logoUpload(t *testing.T, orgParam string, own int, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "logo.png")
	_, _ = fw.Write(content)
	_ = mw.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/organizations/"+orgParam+"/logo", &body)
	c.Request.Header.Set("Content-Type", mw.FormDataContentType())
	c.Params = gin.Params{{Key: "id", Value: orgParam}}
	c.Set(middleware.UserKey, jwt.MapClaims{"role": "admin", "org_id": float64(own)})
	c.Set(middleware.ContextKeyOrganizationID, own)
	// No database: every case here must be refused before one is needed.
	(&OrganizationsHandler{}).SetLogo(c)
	return w
}

func TestAnOrganizationsLogoIsItsOwnAdminsToChange(t *testing.T) {
	if w := logoUpload(t, "6", 5, []byte("\x89PNG\r\n\x1a\n")); w.Code != http.StatusForbidden {
		t.Fatalf("another organization's logo: status %d, want 403", w.Code)
	}
}

func TestALogoMustBeARasterImage(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if w := logoUpload(t, "5", 5, svg); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("an SVG: status %d, want 415", w.Code)
	}
	big := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, maxOrgLogoBytes)...)
	if w := logoUpload(t, "5", 5, big); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over 512 KB: status %d, want 413", w.Code)
	}
}
