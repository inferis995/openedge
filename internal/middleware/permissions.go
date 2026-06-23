package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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
		claimsRaw, exists := c.Get(UserKey)
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}
		claims, ok := claimsRaw.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}

		// Admins always pass (global admin: org_id=nil, org admin: org_id set)
		if role, _ := claims["role"].(string); role == "admin" {
			c.Next()
			return
		}

		userIDFloat, _ := claims["user_id"].(float64)
		userID := int(userIDFloat)
		if userID == 0 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}

		var allowed bool
		// Safe: perm is always a compile-time constant from this package.
		query := "SELECT " + perm + " FROM role_permissions WHERE user_id = $1"
		if err := db.QueryRowContext(c.Request.Context(), query, userID).Scan(&allowed); err != nil {
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
