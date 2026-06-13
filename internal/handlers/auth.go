package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	service *auth.Service
	db      *sql.DB
}

func NewAuthHandler(service *auth.Service, db *sql.DB) *AuthHandler {
	return &AuthHandler{service: service, db: db}
}

// Me handles GET /api/auth/me — returns the logged-in user's profile.
func (h *AuthHandler) Me(c *gin.Context) {
	raw, _ := c.Get(middleware.UserKey)
	claims, ok := raw.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := int(claims["user_id"].(float64))

	var profile struct {
		ID       int     `json:"id"`
		Username string  `json:"username"`
		FullName *string `json:"full_name"`
		Email    *string `json:"email"`
		Role     string  `json:"role"`
		OrgID    *int    `json:"org_id"`
	}
	err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT id, username, full_name, email, role, org_id FROM users WHERE id = $1`, userID,
	).Scan(&profile.ID, &profile.Username, &profile.FullName, &profile.Email, &profile.Role, &profile.OrgID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch profile"})
		return
	}
	c.JSON(http.StatusOK, profile)
}

// ChangePassword handles PUT /api/auth/me/password.
// Requires old password for verification.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	raw, _ := c.Get(middleware.UserKey)
	claims, ok := raw.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID := int(claims["user_id"].(float64))

	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var currentHash string
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT password_hash FROM users WHERE id = $1`, userID,
	).Scan(&currentHash); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	if _, err = h.db.ExecContext(c.Request.Context(),
		`UPDATE users SET password_hash = $1 WHERE id = $2`, string(newHash), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Password updated successfully."})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Get IP address and user agent for audit logging
	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")

	resp, err := h.service.LoginWithMeta(c.Request.Context(), req, ipAddress, userAgent)
	if err != nil {
		slog.Warn("login failed", "username", req.Username, "ip", ipAddress)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	slog.Info("login success", "username", req.Username, "ip", ipAddress)
	c.JSON(http.StatusOK, resp)
}

// ForgotPassword handles POST /api/auth/forgot-password (public).
// Always returns 200 to prevent email enumeration.
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}
	// Non-blocking — user lookup + token insert + email send all happen in background.
	go h.service.RequestPasswordReset(c.Request.Context(), req.Email)
	c.JSON(http.StatusOK, gin.H{"message": "If the email is registered you will receive a reset link shortly."})
}

// ResetPassword handles POST /api/auth/reset-password (public).
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Token    string `json:"token"    binding:"required"`
		Password string `json:"password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.ResetPassword(c.Request.Context(), req.Token, req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Password updated successfully."})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	// Get user info from context (set by auth middleware)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	username, _ := c.Get("username")

	// Get IP address and user agent for audit logging
	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")

	// Log logout
	uid := userID.(int)
	h.service.Logout(uid, username.(string), ipAddress, userAgent)

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}
