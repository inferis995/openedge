package handlers

import (
	"crypto/rand"
	"crypto/subtle"
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
	"golang.org/x/oauth2"
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

// Short-lived cookies that bind an SSO flow to the browser that started it.
// Both are HttpOnly + SameSite=Lax: Lax is required (and sufficient) because the
// provider sends the user back via a top-level GET navigation, which Lax allows.
const (
	ssoStateCookie    = "oe_sso_state"
	ssoVerifierCookie = "oe_sso_verifier"
	ssoCookiePath     = "/api/auth/sso"
	ssoCookieMaxAge   = 600 // 10 min — long enough for an interactive login, short enough to limit replay
)

// isTLSRequest reports whether the browser reached us over HTTPS, either directly
// or through the reverse proxy (Caddy/Traefik terminate TLS in the TLS deploy modes).
// Marking the cookie Secure on a plain-HTTP on-prem install would make the browser
// drop it and break SSO entirely, so the flag follows the actual scheme.
func isTLSRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// setSSOFlowCookies stores the CSRF nonce and the PKCE verifier for the pending flow.
func setSSOFlowCookies(c *gin.Context, nonce, verifier string) {
	secure := isTLSRequest(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(ssoStateCookie, nonce, ssoCookieMaxAge, ssoCookiePath, "", secure, true)
	c.SetCookie(ssoVerifierCookie, verifier, ssoCookieMaxAge, ssoCookiePath, "", secure, true)
}

// clearSSOFlowCookies expires both cookies so a flow can only ever be completed once.
func clearSSOFlowCookies(c *gin.Context) {
	secure := isTLSRequest(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(ssoStateCookie, "", -1, ssoCookiePath, "", secure, true)
	c.SetCookie(ssoVerifierCookie, "", -1, ssoCookiePath, "", secure, true)
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

	// CSRF: the state is "<orgID>:<nonce>", and the nonce is ALSO kept server-side in
	// an HttpOnly cookie the attacker cannot write. The callback requires the two to
	// match, which stops login-CSRF / session fixation: without it, an attacker can
	// start their own flow, capture their authorization code, and lure a victim to the
	// callback URL — the victim's browser would then hold a JWT for the ATTACKER.
	stateRaw := make([]byte, 32)
	if _, err := rand.Read(stateRaw); err != nil { // crypto/rand — must never be math/rand here
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate SSO state"})
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(stateRaw)
	state := fmt.Sprintf("%d:%s", orgID, nonce)

	// PKCE (RFC 7636): binds the authorization code to this browser. The verifier only
	// ever travels inside the HttpOnly cookie, so a code intercepted at the redirect
	// (referrer leak, shared device, malicious proxy) cannot be redeemed by anyone else.
	verifier := oauth2.GenerateVerifier()
	setSSOFlowCookies(c, nonce, verifier)

	url := oauthCfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
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

	// Recover the flow secrets we planted in SSOLogin. A callback that arrives without
	// them was not started by this browser — reject it rather than guess.
	cookieNonce, nonceErr := c.Cookie(ssoStateCookie)
	verifier, verifierErr := c.Cookie(ssoVerifierCookie)
	if nonceErr != nil || verifierErr != nil || cookieNonce == "" || verifier == "" {
		clearSSOFlowCookies(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or expired SSO state"})
		return
	}

	// Extract orgID from state
	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		clearSSOFlowCookies(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}

	// Constant-time compare so the nonce cannot be recovered byte-by-byte via timing.
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(cookieNonce)) != 1 {
		clearSSOFlowCookies(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	// State is proven; burn the cookies so this flow can never be replayed, and so a
	// failure below cannot leave a reusable nonce behind.
	clearSSOFlowCookies(c)

	// Only now is the org id trustworthy: it rode inside the state we just verified.
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

	// VerifierOption completes PKCE — the provider rejects the code unless its S256
	// challenge was derived from this verifier.
	token, err := oauthCfg.Exchange(c.Request.Context(), code, oauth2.VerifierOption(verifier))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token exchange failed"})
		return
	}

	userInfo, err := auth.FetchUserInfo(c.Request.Context(), provider, token, oauthCfg)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// Org binding policy: the org is whatever the CSRF-verified state said, NEVER
	// something re-derived from the email claim here. Previously an unmatched domain
	// silently left orgID at the unverified state value and provisioned a role='user'
	// account there, so any personal Gmail account could join a target org.
	// When the org's config pins a domain_hint, the authenticated email MUST live in
	// that domain; when it does not, membership is governed solely by the fact that
	// the flow was started against this org's own SSO provider.
	emailDomain := ""
	if idx := strings.LastIndex(userInfo.Email, "@"); idx >= 0 {
		emailDomain = userInfo.Email[idx+1:]
	}
	if ssoProvider.DomainHint != "" && !strings.EqualFold(emailDomain, ssoProvider.DomainHint) {
		c.JSON(http.StatusForbidden, gin.H{"error": "email domain is not allowed for this organization"})
		return
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

	// Hand the token back in the URL *fragment*, not the query string.
	// A fragment is never sent to a server, so the JWT stays out of access
	// logs, proxy logs and the Referer header of any resource the landing
	// page loads. The frontend reads it from location.hash and immediately
	// scrubs it from the address bar.
	c.Redirect(http.StatusFound, fmt.Sprintf("/#sso_token=%s", jwt))
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
	codes, _ := h.service.GenerateRecoveryCodes(c.Request.Context(), userID)
	c.JSON(http.StatusOK, gin.H{"message": "MFA attivato con successo", "recovery_codes": codes})
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

// MFARegenerateCodes handles POST /api/auth/mfa/recovery-codes (authenticated).
func (h *AuthHandler) MFARegenerateCodes(c *gin.Context) {
	raw, _ := c.Get(middleware.UserKey)
	claims := raw.(jwt.MapClaims)
	userID := int(claims["user_id"].(float64))

	var enabled bool
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT totp_enabled FROM users WHERE id=$1`, userID,
	).Scan(&enabled); err != nil || !enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "MFA non attivo"})
		return
	}
	codes, err := h.service.GenerateRecoveryCodes(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate recovery codes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes})
}
