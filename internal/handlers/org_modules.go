package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/modules"
)

// Which pages an organization may use. GET answers the full key list with
// the effective state (no row = enabled); PUT replaces the disabled set.
// Who may change it follows the logo rule: a global admin on any
// organization, an org admin on their own only. Everyone in the organization
// may read it — the menu needs it to know what to show.

// GetModules handles GET /api/organizations/:id/modules.
func (h *OrganizationsHandler) GetModules(c *gin.Context) {
	id, ok := orgLogoTarget(c)
	if !ok {
		return
	}
	disabled := map[string]bool{}
	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT module_key FROM organization_modules WHERE org_id = $1 AND enabled = false`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read modules"})
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		disabled[key] = true
	}
	out := make([]gin.H, 0, len(modules.All))
	for _, key := range modules.All {
		out = append(out, gin.H{"key": key, "enabled": !disabled[key]})
	}
	c.JSON(http.StatusOK, gin.H{"modules": out})
}

// SetModules handles PUT /api/organizations/:id/modules.
// Body: {"disabled": ["oee", "recipes", ...]}. Unknown keys are rejected;
// omitted keys stay enabled.
func (h *OrganizationsHandler) SetModules(c *gin.Context) {
	id, ok := orgLogoTarget(c)
	if !ok {
		return
	}
	if !middleware.IsGlobalAdmin(c) {
		if own, ok := middleware.GetOrganizationID(c); !ok || own != id {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}
		// Note: the route additionally requires the admin role, same as the
		// logo endpoints — changing one's own pages is an admin act.
	}
	var req struct {
		Disabled []string `json:"disabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must be {\"disabled\": [...]}"})
		return
	}
	known := map[string]bool{}
	for _, k := range modules.All {
		known[k] = true
	}
	for _, k := range req.Disabled {
		if !known[k] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown module: " + k})
			return
		}
	}
	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save modules"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(c.Request.Context(),
		`DELETE FROM organization_modules WHERE org_id = $1`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save modules"})
		return
	}
	for _, k := range req.Disabled {
		if _, err := tx.ExecContext(c.Request.Context(),
			`INSERT INTO organization_modules (org_id, module_key, enabled) VALUES ($1, $2, false)`,
			id, k); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save modules"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save modules"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"disabled": req.Disabled})
}
