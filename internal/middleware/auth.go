package middleware

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

const UserKey = "user"

// RequireAuth is a Gin middleware that validates JWT tokens
func RequireAuth(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: missing token"})
		return
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return auth.SecretKey, nil
	})

	if err != nil || !token.Valid {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid token"})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid claims"})
		return
	}

	// Reject the MFA step-up challenge token. It is signed with the same key
	// but is issued BEFORE the second factor is verified (auth.generateMFAToken)
	// and carries no role/org claims — accepting it here would let a caller who
	// only knows the password reach every RequireAuth route, including the MFA
	// setup/disable endpoints, collapsing MFA to a password-only check.
	if step, _ := claims["mfa_step"].(bool); step {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: MFA verification required"})
		return
	}

	// Add user info to context
	c.Set(UserKey, claims)
	c.Next()
}

// HasI3xWrite returns true if the caller is allowed to write via the i3X API.
// Any admin (global or org-scoped) always has write access within their visible scope.
// Regular users need the i3x_write JWT claim set to true.
func HasI3xWrite(c *gin.Context) bool {
	claims, exists := c.Get(UserKey)
	if !exists {
		return false
	}
	mapClaims, ok := claims.(jwt.MapClaims)
	if !ok {
		return false
	}
	if role, _ := mapClaims["role"].(string); role == "admin" {
		return true
	}
	v, _ := mapClaims["i3x_write"].(bool)
	return v
}

// RequireRole returns a Gin middleware that checks user role
func RequireRole(role models.UserRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, exists := c.Get(UserKey)
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		mapClaims, ok := claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: invalid claims"})
			return
		}

		// Safe assertion: a validly-signed token may legitimately carry no
		// role claim, and a panic here would be a repeatable 500 reachable
		// by an unprivileged caller.
		roleStr, ok := mapClaims["role"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: missing role claim"})
			return
		}
		userRole := models.UserRole(roleStr)
		if userRole != role && userRole != models.RoleAdmin { // Admin always has access
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden: insufficient permissions"})
			return
		}

		c.Next()
	}
}

// MetricsAuth optionally protects the Prometheus endpoint with a bearer token.
//
// The metrics port is published on the host in the shipped compose files, so
// leaving /metrics fully open exposes runtime internals and per-route traffic
// volumes to anyone who can reach it. When METRICS_TOKEN is set the scraper
// must present it; when unset the endpoint stays open so existing Prometheus
// deployments keep working after an upgrade.
func MetricsAuth() gin.HandlerFunc {
	token := os.Getenv("METRICS_TOKEN")
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}
		presented := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		c.Next()
	}
}

// WebSocketAuth lets a browser authenticate a WebSocket upgrade, which cannot
// carry an Authorization header. The client passes the JWT as the second
// Sec-WebSocket-Protocol value (new WebSocket(url, ["bearer", token])) and this
// middleware promotes it to the header RequireAuth reads. The subprotocol is
// used instead of a query parameter so the token never lands in access logs,
// browser history, or Referer headers.
//
// Chain it BEFORE RequireAuth, which still performs all validation.
func WebSocketAuth(c *gin.Context) {
	if c.GetHeader("Authorization") == "" {
		for _, part := range strings.Split(c.GetHeader("Sec-WebSocket-Protocol"), ",") {
			part = strings.TrimSpace(part)
			if part == "" || strings.EqualFold(part, "bearer") {
				continue
			}
			c.Request.Header.Set("Authorization", "Bearer "+part)
			break
		}
	}
	c.Next()
}

// RequireGlobalAdmin allows only platform-wide administrators (role=admin AND
// no org_id). RequireRole(RoleAdmin) is NOT equivalent: it also passes for
// org-scoped admins, so routes that expose or mutate data across every tenant
// (full DB backup/restore, platform audit trail) must use this instead.
func RequireGlobalAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsGlobalAdmin(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden: global administrator required"})
			return
		}
		c.Next()
	}
}

// OrgIDFromJWT reads the caller's org_id straight from the JWT claims, without
// depending on OrganizationContext having run. Returns ok=false for a global
// admin (no org_id claim) or when the claim is absent/malformed.
func OrgIDFromJWT(c *gin.Context) (int, bool) {
	claims, exists := c.Get(UserKey)
	if !exists {
		return 0, false
	}
	mapClaims, ok := claims.(jwt.MapClaims)
	if !ok {
		return 0, false
	}
	raw, present := mapClaims["org_id"]
	if !present || raw == nil {
		return 0, false
	}
	f, ok := raw.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// RequireOrgParam guards routes shaped like /organizations/:param/... where the
// handler derives the target organization from the URL. Without it, any
// org-scoped admin passes RequireRole(admin) and then operates on an arbitrary
// tenant — minting their API keys, downloading their edge installer, pushing
// container images to their plant. A global admin is allowed through for any
// org; every other caller must match their own org_id.
func RequireOrgParam(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsGlobalAdmin(c) {
			c.Next()
			return
		}

		targetOrgID, err := strconv.Atoi(c.Param(param))
		if err != nil || targetOrgID <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid organization id"})
			return
		}

		callerOrgID, ok := OrgIDFromJWT(c)
		if !ok || callerOrgID != targetOrgID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden: organization access denied"})
			return
		}

		c.Next()
	}
}
