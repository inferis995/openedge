package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Permission flag names — match role_permissions columns.
const (
	PermWriteTags         = "can_write_tags"
	PermAckAlarms         = "can_ack_alarms"
	PermExportData        = "can_export_data"
	PermManageRecipes     = "can_manage_recipes"
	PermManageShifts      = "can_manage_shifts"
	PermViewAudit         = "can_view_audit"
	PermDownloadInstaller = "can_download_installer"
)

// RequirePermission returns a middleware that checks a granular permission.
// Global admins (role=admin, org_id=NULL) always pass.
// Org admins (role=admin with org_id) always pass.
// Regular users need the specific flag set to true in role_permissions.
func RequirePermission(db *sql.DB, perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role == "admin" {
			c.Next()
			return
		}

		userIDRaw, _ := c.Get("user_id")
		userID, ok := userIDRaw.(int)
		if !ok || userID == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			c.Abort()
			return
		}

		var allowed bool
		// Safe: perm is always a compile-time constant from this package.
		query := "SELECT " + perm + " FROM role_permissions WHERE user_id = $1"
		if err := db.QueryRow(query, userID).Scan(&allowed); err != nil {
			// No row → default deny
			c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			c.Abort()
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "permission denied: " + perm})
			c.Abort()
			return
		}
		c.Next()
	}
}
