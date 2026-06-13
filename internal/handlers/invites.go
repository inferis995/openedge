package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	redisclient "github.com/ralph/industrial-edge-middleware/internal/redis"
	"golang.org/x/crypto/bcrypt"
)

// InvitesHandler manages user invite creation and acceptance.
type InvitesHandler struct {
	db    *sql.DB
	redis *redisclient.Client
}

// NewInvitesHandler creates a new InvitesHandler.
func NewInvitesHandler(db *sql.DB, redis *redisclient.Client) *InvitesHandler {
	return &InvitesHandler{db: db, redis: redis}
}

// CreateInviteRequest is the body for POST /api/organizations/:id/invites.
type CreateInviteRequest struct {
	Email string          `json:"email" binding:"required,email"`
	Role  models.UserRole `json:"role" binding:"omitempty,oneof=admin user"`
}

// InviteResponse is returned after creating an invite.
type InviteResponse struct {
	ID        int    `json:"id"`
	OrgID     int    `json:"org_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// Create handles POST /api/organizations/:id/invites.
// Only org admins may invite new members to their own organization.
func (h *InvitesHandler) Create(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization id"})
		return
	}

	claims, ok := claimsFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	callerRole, _ := claims["role"].(string)
	callerID := int(claims["user_id"].(float64))

	if callerRole != string(models.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		return
	}

	// Org-scoped admins can only invite into their own org.
	if rawOrgID, hasOrg := claims["org_id"]; hasOrg {
		callerOrgID := int(rawOrgID.(float64))
		if callerOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "cannot invite to a different organization"})
			return
		}
	}

	var req CreateInviteRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Role == "" {
		req.Role = models.RoleUser
	}

	token, err := generateInviteToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	var id int
	var expiresAt time.Time
	insertQ := `
		INSERT INTO user_invites (org_id, email, role, token, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, expires_at`
	err = h.db.QueryRowContext(c.Request.Context(), insertQ, orgID, req.Email, req.Role, token, callerID).
		Scan(&id, &expiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create invite"})
		return
	}

	c.JSON(http.StatusCreated, InviteResponse{
		ID:        id,
		OrgID:     orgID,
		Email:     req.Email,
		Role:      string(req.Role),
		Token:     token,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}

// AcceptInviteRequest is the body for POST /api/auth/accept-invite.
type AcceptInviteRequest struct {
	Token    string `json:"token" binding:"required"`
	Username string `json:"username" binding:"required,min=3"`
	Password string `json:"password" binding:"required,min=6"`
	FullName string `json:"full_name"`
}

// AcceptInvite handles POST /api/auth/accept-invite (public — no auth required).
// Validates the token, creates the user, and marks the invite used.
func (h *InvitesHandler) AcceptInvite(c *gin.Context) {
	var req AcceptInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Load invite — reject if expired or already used.
	var inv struct {
		id    int
		orgID int
		role  string
	}
	lookupQ := `
		SELECT id, org_id, role
		FROM user_invites
		WHERE token = $1 AND accepted_at IS NULL AND expires_at > NOW()`
	err := h.db.QueryRowContext(ctx, lookupQ, req.Token).Scan(&inv.id, &inv.orgID, &inv.role)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invite token is invalid or has expired"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate token"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
		return
	}
	defer tx.Rollback() //nolint:errcheck

	var userID int
	createQ := `
		INSERT INTO users (username, password_hash, role, full_name, org_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	if err = tx.QueryRowContext(ctx, createQ,
		req.Username, string(hash), inv.role, req.FullName, inv.orgID,
	).Scan(&userID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already taken"})
		return
	}

	markQ := `UPDATE user_invites SET accepted_at = NOW(), accepted_by = $1 WHERE id = $2`
	if _, execErr := tx.ExecContext(ctx, markQ, userID, inv.id); execErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark invite as used"})
		return
	}

	if commitErr := tx.Commit(); commitErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction commit failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "account created",
		"user_id":  userID,
		"username": req.Username,
		"org_id":   inv.orgID,
		"role":     inv.role,
	})
}

// EdgeStatus handles GET /api/organizations/:id/edge-status.
// Returns whether the edge manager for the org has sent a heartbeat recently.
func (h *InvitesHandler) EdgeStatus(c *gin.Context) {
	orgID := c.Param("id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing organization id"})
		return
	}

	if h.redis == nil {
		c.JSON(http.StatusOK, gin.H{"online": false, "reason": "redis unavailable"})
		return
	}

	key := "edge:ping:" + orgID
	val, err := h.redis.Get(key)
	if err != nil {
		// Key missing or expired — edge is offline.
		c.JSON(http.StatusOK, gin.H{"online": false, "last_ping": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"online": true, "last_ping": val})
}

// HandleEdgePing records the heartbeat timestamp for an org's edge manager in Redis.
// Called from the MQTT subscription handler in main.go when a ping arrives on
// sys/edge/{org_id}/ping.
func HandleEdgePing(ctx context.Context, orgID string, rc *redisclient.Client) {
	if rc == nil {
		return
	}
	key := "edge:ping:" + orgID
	// TTL 90 s — edge publishes every 30 s, so 3× gives comfortable margin.
	_ = rc.Set(key, time.Now().UTC().Format(time.RFC3339), 90*time.Second)
}

// generateInviteToken returns a cryptographically random 32-byte hex string.
func generateInviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// claimsFromContext extracts JWT MapClaims stored by middleware.RequireAuth.
func claimsFromContext(c *gin.Context) (jwt.MapClaims, bool) {
	raw, exists := c.Get(middleware.UserKey)
	if !exists {
		return nil, false
	}
	claims, ok := raw.(jwt.MapClaims)
	return claims, ok
}
