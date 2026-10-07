package handlers

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// An organization's logo: its users see it at the top of the menu instead of
// the product's. PNG, JPEG or WebP, by content rather than by name — not SVG,
// which can carry script and would run if the file were opened on its own.

const maxOrgLogoBytes = 512 << 10

var orgLogoTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

// orgLogoTarget is the organization in the path, if the caller may act on it:
// a global admin on any, anyone else on their own only.
func orgLogoTarget(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization id"})
		return 0, false
	}
	if !middleware.IsGlobalAdmin(c) {
		if own, ok := middleware.GetOrganizationID(c); !ok || own != id {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return 0, false
		}
	}
	return id, true
}

// GetLogo handles GET /api/organizations/:id/logo.
func (h *OrganizationsHandler) GetLogo(c *gin.Context) {
	id, ok := orgLogoTarget(c)
	if !ok {
		return
	}
	var logo []byte
	var ctype sql.NullString
	var updated sql.NullTime
	err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT logo, logo_type, logo_updated_at FROM organizations WHERE id = $1`, id).Scan(&logo, &ctype, &updated)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && len(logo) == 0) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the logo"})
		return
	}
	etag := `"` + strconv.FormatInt(updated.Time.UnixNano(), 36) + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, ctype.String, logo)
}

// SetLogo handles PUT /api/organizations/:id/logo: the image as the
// multipart field "file".
func (h *OrganizationsHandler) SetLogo(c *gin.Context) {
	id, ok := orgLogoTarget(c)
	if !ok {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "send the image as the form field \"file\""})
		return
	}
	if fh.Size > maxOrgLogoBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "the logo must be at most 512 KB"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read the file"})
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxOrgLogoBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxOrgLogoBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the logo must be an image of at most 512 KB"})
		return
	}
	ctype := http.DetectContentType(data)
	if !orgLogoTypes[ctype] {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "the logo must be PNG, JPEG or WebP"})
		return
	}
	res, err := h.db.ExecContext(c.Request.Context(),
		`UPDATE organizations SET logo = $1, logo_type = $2, logo_updated_at = $3 WHERE id = $4`,
		data, ctype, time.Now(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the logo"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteLogo handles DELETE /api/organizations/:id/logo.
func (h *OrganizationsHandler) DeleteLogo(c *gin.Context) {
	id, ok := orgLogoTarget(c)
	if !ok {
		return
	}
	if _, err := h.db.ExecContext(c.Request.Context(),
		`UPDATE organizations SET logo = NULL, logo_type = NULL, logo_updated_at = NULL WHERE id = $1`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove the logo"})
		return
	}
	c.Status(http.StatusNoContent)
}
