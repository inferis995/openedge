package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	// ContextKeyOrganizationID is the key used to store organization ID in the request context
	ContextKeyOrganizationID = "organization_id"
)

// OrganizationContext adds organization context to the request
// It checks for X-Organization-ID header first, then query parameter, then user's org from JWT
// For global admin (org_id=NULL or role=admin), the header is optional and can be any value
func OrganizationContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user from JWT context (set by RequireAuth middleware)
		claims, exists := c.Get(UserKey)
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "User not authenticated - missing JWT context",
			})
			c.Abort()
			return
		}

		mapClaims, ok := claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid JWT claims format",
			})
			c.Abort()
			return
		}

		// Extract user role and org_id from JWT
		role, _ := mapClaims["role"].(string)
		userOrgIDInterface, userHasOrg := mapClaims["org_id"]

		// Global admin = admin role AND no org_id assigned.
		// Admin WITH an org_id is org-scoped: treated like a regular user for data filtering.
		isGlobalAdmin := role == "admin" && (!userHasOrg || userOrgIDInterface == nil)

		// Try header first
		orgIDStr := c.GetHeader("X-Organization-ID")
		if orgIDStr == "" {
			// Fall back to query parameter
			orgIDStr = c.Query("organization_id")
		}

		var orgID int
		var orgIDProvided bool

		// Parse organization ID from header/parameter if provided
		if orgIDStr != "" {
			parsedID, err := strconv.Atoi(orgIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "Invalid organization ID format",
				})
				c.Abort()
				return
			}
			orgID = parsedID
			orgIDProvided = true
		}

		// Logic for organization ID handling
		if isGlobalAdmin {
			// Global admin can access any organization
			// If org_id is provided, use it; otherwise, don't set a filter (access all)
			if orgIDProvided {
				if orgID <= 0 {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": "Organization ID must be a positive integer",
					})
					c.Abort()
					return
				}
				c.Set(ContextKeyOrganizationID, orgID)
			}
			// For global admin without specified org, we don't set ContextKeyOrganizationID
			// Downstream handlers should check: if org_id not set and user is admin, allow all
		} else {
			// Regular user: must have org_id in JWT
			if !userHasOrg || userOrgIDInterface == nil {
				c.JSON(http.StatusForbidden, gin.H{
					"error": "User is not assigned to any organization",
				})
				c.Abort()
				return
			}

			// Get user's org_id from JWT
			userOrgIDFloat, ok := userOrgIDInterface.(float64)
			if !ok {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Invalid org_id format in JWT",
				})
				c.Abort()
				return
			}
			userOrgID := int(userOrgIDFloat)

			// If header/parameter provided, validate it matches user's org
			if orgIDProvided {
				if orgID != userOrgID {
					c.JSON(http.StatusForbidden, gin.H{
						"error": "Organization access denied - user belongs to a different organization",
					})
					c.Abort()
					return
				}
				c.Set(ContextKeyOrganizationID, orgID)
			} else {
				// No header provided - use user's org_id from JWT
				c.Set(ContextKeyOrganizationID, userOrgID)
			}
		}

		c.Next()
	}
}

// GetOrganizationID retrieves the organization ID from the request context
// Returns ok=false if organization ID is not found in context (which is valid for global admin)
func GetOrganizationID(c *gin.Context) (int, bool) {
	orgID, exists := c.Get(ContextKeyOrganizationID)
	if !exists {
		return 0, false
	}
	id, ok := orgID.(int)
	return id, ok
}

// GetUserID retrieves the user ID from the request context
func GetUserID(c *gin.Context) (int, bool) {
	claims, exists := c.Get(UserKey)
	if !exists {
		return 0, false
	}

	mapClaims, ok := claims.(jwt.MapClaims)
	if !ok {
		return 0, false
	}

	userIDFloat, ok := mapClaims["user_id"].(float64)
	if !ok {
		return 0, false
	}

	return int(userIDFloat), true
}

// IsGlobalAdmin returns true only for admins with no org_id assigned.
// An admin with an org_id is org-scoped and does NOT qualify as global admin.
func IsGlobalAdmin(c *gin.Context) bool {
	claims, exists := c.Get(UserKey)
	if !exists {
		return false
	}

	mapClaims, ok := claims.(jwt.MapClaims)
	if !ok {
		return false
	}

	role, _ := mapClaims["role"].(string)
	if role != "admin" {
		return false
	}

	userOrgIDInterface, userHasOrg := mapClaims["org_id"]
	return !userHasOrg || userOrgIDInterface == nil
}

// RequireOrganizationContext is a helper that ensures organization context exists
// For global admin, this is optional (they can access all organizations)
func RequireOrganizationContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		// If user is global admin, organization context is optional
		if IsGlobalAdmin(c) {
			c.Next()
			return
		}

		// For regular users, organization context must exist
		_, exists := GetOrganizationID(c)
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Organization context not found - unauthorized access",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// GetOrgFilterForQuery returns the organization ID for filtering
// Returns nil for global admin (no filter - access all organizations)
// Returns the org ID pointer for regular users (filter by their org)
// This is useful for handlers that need to build organization-aware queries
func GetOrgFilterForQuery(c *gin.Context) *int {
	if IsGlobalAdmin(c) {
		return nil // Global admin - no filter (access all)
	}

	orgID, ok := GetOrganizationID(c)
	if !ok {
		// This shouldn't happen if RequireOrganizationContext is used
		// Return -1 to ensure no rows match (safe fallback)
		negOne := -1
		return &negOne
	}

	return &orgID
}
