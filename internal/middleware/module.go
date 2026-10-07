package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireModule blocks an API group when the caller's organization disabled
// that module. Global admins (no organization) always pass: the setting
// describes what an organization's own users may see, not what the platform
// operator may do. No row in organization_modules means enabled.
func RequireModule(db *sql.DB, moduleKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsGlobalAdmin(c) {
			c.Next()
			return
		}
		orgID, _ := c.Get(ContextKeyOrganizationID)
		oid, ok := orgID.(int)
		if !ok || oid <= 0 {
			// No organization in context: nothing to gate against.
			c.Next()
			return
		}
		var enabled bool
		err := db.QueryRowContext(c.Request.Context(),
			`SELECT enabled FROM organization_modules WHERE org_id = $1 AND module_key = $2`,
			oid, moduleKey).Scan(&enabled)
		if err == nil && !enabled {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "module '" + moduleKey + "' is disabled for this organization",
			})
			c.Abort()
			return
		}
		// No row, or enabled: pass. A missing table (migration not yet run)
		// must not lock anyone out either — fail open on query error.
		c.Next()
	}
}
