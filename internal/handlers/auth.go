package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	totpLib "github.com/pquerna/otp/totp"
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

// SSOLogin handles GET /api/auth/sso/:provider/login?org_id=N (or ?email=user@company.com)
// Redirects the browser to the OAuth2 provider's authorization page.
func (h *AuthHandler) SSOLogin(c *gin.Context) {
	provider := c.Param("provider")

	orgIDStr := c.Query("org_id")
	orgID, _ := strconv.Atoi(orgIDStr)

	var ssoProvider *auth.SSOProvider
	var err error

	if orgID > 0 {
		ssoProvider, err = auth.GetSSOProvider(h.db, provider, orgID)
	} else {
		// Accept email or domain for automatic org resolution.
		email := c.Query("email")
		if email == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "org_id or email required"})
			return
		}
		domain := email
		if idx := strings.LastIndex(email, "@"); idx >= 0 {
			domain = email[idx+1:]
		}
		ssoProvider, err = auth.GetSSOProviderByDomain(h.db, provider, domain)
		if err == nil {
			orgID = ssoProvider.OrgID
		}
	}

	if err != nil || ssoProvider == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSO provider not configured for this org"})
		return
	}

	redirectURL := fmt.Sprintf("https://%s/api/auth/sso/%s/callback", publicHost(), provider)
	oauthCfg, err := ssoProvider.OAuth2Config(redirectURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// PKCE-like state: base64(orgID:randomBytes) — verified in callback
	stateRaw := make([]byte, 16)
	_, _ = rand.Read(stateRaw)
	state := fmt.Sprintf("%d:%s", orgID, base64.RawURLEncoding.EncodeToString(stateRaw))

	url := oauthCfg.AuthCodeURL(state)
	c.Redirect(http.StatusFound, url)
}

// SSOCallback handles GET /api/auth/sso/:provider/callback
// Exchanges the code for a token, fetches user info, upserts the user, and returns a JWT.
func (h *AuthHandler) SSOCallback(c *gin.Context) {
	provider := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code or state"})
		return
	}

	// Extract orgID from state
	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	orgID, err := strconv.Atoi(parts[0])
	if err != nil || orgID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state org_id"})
		return
	}

	ssoProvider, err := auth.GetSSOProvider(h.db, provider, orgID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSO provider not configured"})
		return
	}

	redirectURL := fmt.Sprintf("https://%s/api/auth/sso/%s/callback", publicHost(), provider)
	oauthCfg, err := ssoProvider.OAuth2Config(redirectURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	token, err := oauthCfg.Exchange(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token exchange failed"})
		return
	}

	userInfo, err := auth.FetchUserInfo(c.Request.Context(), provider, token, oauthCfg)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// If email domain matches another org's SSO config, use that org instead
	emailDomain := ""
	if idx := strings.LastIndex(userInfo.Email, "@"); idx >= 0 {
		emailDomain = userInfo.Email[idx+1:]
	}
	if domainProvider, domainErr := auth.GetSSOProviderByDomain(h.db, provider, emailDomain); domainErr == nil {
		orgID = domainProvider.OrgID
	}

	userID, role, err := auth.UpsertSSOUser(h.db, userInfo, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user provisioning failed"})
		return
	}

	jwt, err := h.service.GenerateTokenForUser(userID, userInfo.Email, role, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	// Redirect to frontend with token in query (frontend stores it in localStorage)
	c.Redirect(http.StatusFound, fmt.Sprintf("/?sso_token=%s", jwt))
}

func publicHost() string {
	if h := os.Getenv("PUBLIC_HOST"); h != "" {
		return h
	}
	return "localhost:8081"
}

// MFAVerify handles POST /api/auth/mfa/verify (public).
// Accepts the 5-min mfa_token from Login + a 6-digit TOTP code.
func (h *AuthHandler) MFAVerify(c *gin.Context) {
	var req struct {
		MFAToken string `json:"mfa_token" binding:"required"`
		Code     string `json:"code"      binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.service.CompleteMFALogin(c.Request.Context(), req.MFAToken, req.Code, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// MFASetup handles POST /api/auth/mfa/setup (authenticated).
// Generates a new TOTP secret, stores it (disabled until confirmed), returns QR URL.
func (h *AuthHandler) MFASetup(c *gin.Context) {
	raw, _ := c.Get(middleware.UserKey)
	claims := raw.(jwt.MapClaims)
	userID := int(claims["user_id"].(float64))
	username := claims["username"].(string)

	key, err := totpLib.Generate(totpLib.GenerateOpts{
		Issuer:      "OpenEdge",
		AccountName: username,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate TOTP secret"})
		return
	}

	// Store secret (still disabled — user must confirm with a valid code first)
	if _, err := h.db.ExecContext(c.Request.Context(),
		`UPDATE users SET totp_secret=$1, totp_enabled=false WHERE id=$2`, key.Secret(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save TOTP secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secret":   key.Secret(),
		"qr_url":   key.URL(),
		"issuer":   "OpenEdge",
		"username": username,
	})
}

// MFAEnable handles POST /api/auth/mfa/enable (authenticated).
// Verifies the first TOTP code and activates MFA for the user.
func (h *AuthHandler) MFAEnable(c *gin.Context) {
	raw, _ := c.Get(middleware.UserKey)
	claims := raw.(jwt.MapClaims)
	userID := int(claims["user_id"].(float64))

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var secret string
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT totp_secret FROM users WHERE id=$1 AND totp_secret IS NOT NULL`, userID,
	).Scan(&secret); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run /mfa/setup first"})
		return
	}

	if !totpLib.Validate(req.Code, secret) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid code — try again"})
		return
	}

	if _, err := h.db.ExecContext(c.Request.Context(),
		`UPDATE users SET totp_enabled=true WHERE id=$1`, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enable MFA"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "MFA attivato con successo"})
}

// MFADisable handles DELETE /api/auth/mfa/disable (authenticated).
// Requires current password confirmation before disabling MFA.
func (h *AuthHandler) MFADisable(c *gin.Context) {
	raw, _ := c.Get(middleware.UserKey)
	claims := raw.(jwt.MapClaims)
	userID := int(claims["user_id"].(float64))

	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var hash string
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT password_hash FROM users WHERE id=$1`, userID,
	).Scan(&hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user not found"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "password errata"})
		return
	}

	if _, err := h.db.ExecContext(c.Request.Context(),
		`UPDATE users SET totp_enabled=false, totp_secret=NULL WHERE id=$1`, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to disable MFA"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "MFA disattivato"})
}

// MFAStatus handles GET /api/auth/mfa/status (authenticated).
func (h *AuthHandler) MFAStatus(c *gin.Context) {
	raw, _ := c.Get(middleware.UserKey)
	claims := raw.(jwt.MapClaims)
	userID := int(claims["user_id"].(float64))

	var enabled bool
	_ = h.db.QueryRowContext(c.Request.Context(),
		`SELECT totp_enabled FROM users WHERE id=$1`, userID,
	).Scan(&enabled)
	c.JSON(http.StatusOK, gin.H{"mfa_enabled": enabled})
}
