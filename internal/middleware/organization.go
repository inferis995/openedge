package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	// ContextKeyOrganizationID is the key used to store organization ID in the request context
	ContextKeyOrganizationID = "organization_id"
)

// OrganizationContext adds organization context to the request
// It checks for X-Organization-ID header first, then query parameter
func OrganizationContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Try header first
		orgIDStr := c.GetHeader("X-Organization-ID")

		// Fall back to query parameter
		if orgIDStr == "" {
			orgIDStr = c.Query("organization_id")
		}

		// If no organization ID provided, return 400 Bad Request
		if orgIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "X-Organization-ID header or organization_id query parameter is required",
			})
			c.Abort()
			return
		}

		// Parse organization ID
		orgID, err := strconv.Atoi(orgIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid organization ID format",
			})
			c.Abort()
			return
		}

		// Validate organization ID is positive
		if orgID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Organization ID must be a positive integer",
			})
			c.Abort()
			return
		}

		// Store organization ID in context for downstream handlers
		c.Set(ContextKeyOrganizationID, orgID)
		c.Next()
	}
}

// GetOrganizationID retrieves the organization ID from the request context
// Returns ok=false if organization ID is not found in context
func GetOrganizationID(c *gin.Context) (int, bool) {
	orgID, exists := c.Get(ContextKeyOrganizationID)
	if !exists {
		return 0, false
	}
	id, ok := orgID.(int)
	return id, ok
}

// RequireOrganizationContext is a helper that ensures organization context exists
// and returns 403 Forbidden if it doesn't (should not happen if middleware is applied)
func RequireOrganizationContext() gin.HandlerFunc {
	return func(c *gin.Context) {
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
