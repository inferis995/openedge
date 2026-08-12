package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// OAuth 2.1 authorization server.
//
// This exists so a remote client can be given access without being given a
// password or a long-lived token. The user signs in here, on this server, and
// what the client receives is a token scoped to what the user agreed to — which
// is the difference between delegating access and handing over an account.
//
// The access token it issues is the same JWT the rest of the platform already
// validates, with two extra claims: the scope the user consented to, and the
// client that asked. Nothing downstream needed a second notion of identity.

const (
	// The two scopes a token can carry. They are coarse on purpose: the API
	// already decides what a user may touch, per organization and per
	// permission. A finer-grained scheme here could only ever disagree with it.
	ScopeRead  = "openedge:read"
	ScopeWrite = "openedge:write"

	// A code is exchanged by a machine within seconds of being issued; the
	// window only has to cover the redirect back.
	authCodeTTL = 2 * time.Minute
	// Short, because refreshing is cheap and a leaked access token cannot be
	// revoked — only outlived.
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
)

type OAuthHandler struct {
	db     *sql.DB
	auth   *auth.Service
	issuer string
}

// NewOAuthHandler builds the handler. issuer is the public base URL this API is
// reached at; leave it empty to derive it per request from the proxy headers.
func NewOAuthHandler(db *sql.DB, authSvc *auth.Service, issuer string) *OAuthHandler {
	return &OAuthHandler{db: db, auth: authSvc, issuer: strings.TrimRight(issuer, "/")}
}

// baseURL is what goes into metadata and redirects. A configured value is
// always preferred: deriving it from the request means trusting headers a
// caller controls, which is acceptable for building a link back to ourselves
// and not much else.
func (h *OAuthHandler) baseURL(c *gin.Context) string {
	if h.issuer != "" {
		return h.issuer
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = strings.Split(proto, ",")[0]
	}
	host := c.Request.Host
	if fwd := c.GetHeader("X-Forwarded-Host"); fwd != "" {
		host = strings.Split(fwd, ",")[0]
	}
	return scheme + "://" + host
}

// ---- discovery ---------------------------------------------------------------

// Metadata implements RFC 8414. A client reads this to learn where to send the
// user and where to exchange the code, so it never has to hardcode our paths.
func (h *OAuthHandler) Metadata(c *gin.Context) {
	base := h.baseURL(c)
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"revocation_endpoint":                   base + "/oauth/revoke",
		"scopes_supported":                      []string{ScopeRead, ScopeWrite},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"service_documentation":                 base + "/docs",
	})
}

// ---- dynamic client registration ---------------------------------------------

type clientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

// Register implements RFC 7591. It is open by design — that is what lets a
// client the operator has never heard of complete a sign-in — so it creates
// nothing of value on its own: a registration without a user who then approves
// it grants access to nothing. Rate limiting keeps the table from growing
// without bound.
func (h *OAuthHandler) Register(c *gin.Context) {
	var req clientRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "malformed registration request")
		return
	}
	if len(req.RedirectURIs) == 0 {
		oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", "at least one redirect_uri is required")
		return
	}
	if len(req.RedirectURIs) > 10 {
		oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", "too many redirect URIs")
		return
	}
	for _, uri := range req.RedirectURIs {
		if err := validateRedirectURI(uri); err != nil {
			oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}

	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "Unnamed client"
	}
	if len(name) > 120 {
		name = name[:120]
	}

	scope := normalizeScope(req.Scope)

	clientID, err := randomToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// A public client authenticates with PKCE alone. That is the normal shape
	// for anything that runs on the user's machine, where a secret would be
	// readable by whoever holds the machine.
	public := req.TokenEndpointAuthMethod == "none"
	var secret, secretHash string
	if !public {
		if secret, err = randomToken(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		secretHash = string(hash)
	}

	var stored interface{}
	if secretHash != "" {
		stored = secretHash
	}
	if _, err := h.db.ExecContext(c.Request.Context(),
		`INSERT INTO oauth_clients (client_id, client_secret_hash, client_name, redirect_uris, scope)
		 VALUES ($1,$2,$3,$4,$5)`,
		clientID, stored, name, strings.Join(req.RedirectURIs, "\n"), scope); err != nil {
		log.Printf("[OAUTH] register: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	out := gin.H{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      scope,
	}
	if !public {
		out["client_secret"] = secret
		out["token_endpoint_auth_method"] = "client_secret_post"
		// No expiry: rotating it would require a channel to tell the client,
		// and there is none.
		out["client_secret_expires_at"] = 0
	}
	c.JSON(http.StatusCreated, out)
}

// ---- the authorization request ------------------------------------------------

// authzRequest is one attempt to obtain a code, already checked against the
// registered client.
type authzRequest struct {
	ClientID     string
	ClientName   string
	RedirectURI  string
	State        string
	Scope        string
	Challenge    string
	Method       string
	Resource     string
	AllowedScope string
}

// parseAuthzRequest validates in the order the spec requires: anything wrong
// with the client or the redirect URI must NOT be sent to that URI, because we
// have no reason to believe it belongs to the client. Everything after that is
// reported by redirecting, which is how the client learns what it got wrong.
func (h *OAuthHandler) parseAuthzRequest(c *gin.Context) (*authzRequest, error) {
	get := func(k string) string { return strings.TrimSpace(c.Request.FormValue(k)) }

	clientID := get("client_id")
	if clientID == "" {
		return nil, errNoRedirect("client_id is required")
	}

	var name, uris, allowed string
	err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT client_name, redirect_uris, scope FROM oauth_clients WHERE client_id=$1`,
		clientID).Scan(&name, &uris, &allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoRedirect("unknown client")
	}
	if err != nil {
		log.Printf("[OAUTH] authorize lookup: %v", err)
		return nil, errNoRedirect("could not look up the client")
	}

	redirectURI := get("redirect_uri")
	registered := strings.Split(uris, "\n")
	if redirectURI == "" {
		if len(registered) != 1 {
			return nil, errNoRedirect("redirect_uri is required when a client registers more than one")
		}
		redirectURI = registered[0]
	}
	// Exact match. Prefix matching is how redirect URIs turn into open
	// redirects, and an open redirect here hands out authorization codes.
	if !containsExact(registered, redirectURI) {
		return nil, errNoRedirect("redirect_uri does not match the ones registered for this client")
	}

	req := &authzRequest{
		ClientID:     clientID,
		ClientName:   name,
		RedirectURI:  redirectURI,
		State:        get("state"),
		Scope:        get("scope"),
		Challenge:    get("code_challenge"),
		Method:       get("code_challenge_method"),
		Resource:     get("resource"),
		AllowedScope: allowed,
	}

	if rt := get("response_type"); rt != "code" {
		return req, errRedirect("unsupported_response_type", "only response_type=code is supported")
	}
	if req.Challenge == "" {
		return req, errRedirect("invalid_request", "code_challenge is required")
	}
	// S256 only. "plain" puts the verifier on the wire in the authorization
	// request, which defeats the point of having one.
	if req.Method != "S256" {
		return req, errRedirect("invalid_request", "code_challenge_method must be S256")
	}
	if req.Scope == "" {
		req.Scope = allowed
	}
	granted, err := restrictScope(req.Scope, allowed)
	if err != nil {
		return req, errRedirect("invalid_scope", err.Error())
	}
	req.Scope = granted

	return req, nil
}

// Authorize renders the sign-in and consent page.
func (h *OAuthHandler) Authorize(c *gin.Context) {
	req, err := h.parseAuthzRequest(c)
	if err != nil {
		h.failAuthz(c, req, err)
		return
	}
	renderConsent(c, consentView{
		Request: req,
		Action:  h.baseURL(c) + "/oauth/authorize",
	})
}

// Decision handles the submitted form: the user's credentials and their answer.
//
// The form asks for the password every time rather than riding an existing
// session. That costs the user a login, and buys the property that a page on
// another site cannot cause an authorization to be issued — there is no ambient
// credential for it to borrow.
func (h *OAuthHandler) Decision(c *gin.Context) {
	req, err := h.parseAuthzRequest(c)
	if err != nil {
		h.failAuthz(c, req, err)
		return
	}

	if c.Request.FormValue("action") != "allow" {
		h.redirectError(c, req, "access_denied", "the user declined")
		return
	}

	view := consentView{Request: req, Action: h.baseURL(c) + "/oauth/authorize"}
	user, done := h.authenticate(c, &view)
	if !done {
		renderConsent(c, view)
		return
	}

	code, err := h.issueCode(c.Request.Context(), req, &user)
	if err != nil {
		log.Printf("[OAUTH] issue code: %v", err)
		h.redirectError(c, req, "server_error", "could not issue an authorization code")
		return
	}

	target, err := url.Parse(req.RedirectURI)
	if err != nil {
		h.redirectError(c, req, "server_error", "invalid redirect")
		return
	}
	q := target.Query()
	q.Set("code", code)
	if req.State != "" {
		q.Set("state", req.State)
	}
	target.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, target.String())
}

// authenticate runs the login, including the second factor when the account has
// one. It returns done=false when the page has to be shown again — either
// because the credentials were wrong or because a TOTP code is now needed.
func (h *OAuthHandler) authenticate(c *gin.Context, view *consentView) (models.User, bool) {
	ip, ua := c.ClientIP(), c.Request.UserAgent()

	// Second leg: the password was already accepted and we are back for the code.
	if mfaToken := c.Request.FormValue("mfa_token"); mfaToken != "" {
		res, err := h.auth.CompleteMFALogin(c.Request.Context(), mfaToken,
			c.Request.FormValue("mfa_code"), ip, ua)
		if err != nil {
			view.MFAToken = mfaToken
			view.Error = "Codice non valido."
			return models.User{}, false
		}
		return res.User, true
	}

	username := c.Request.FormValue("username")
	res, err := h.auth.LoginWithMeta(c.Request.Context(),
		models.LoginRequest{Username: username, Password: c.Request.FormValue("password")}, ip, ua)
	if err != nil {
		view.Username = username
		// Deliberately the same message whether the user exists or not.
		view.Error = "Credenziali non valide."
		return models.User{}, false
	}
	switch {
	case res.MFASetupRequired:
		view.Error = "Questa organizzazione richiede la verifica in due passaggi. " +
			"Configurala nell'applicazione prima di autorizzare un client."
		return models.User{}, false
	case res.MFARequired:
		view.MFAToken = res.MFAToken
		return models.User{}, false
	}
	return res.User, true
}

func (h *OAuthHandler) issueCode(ctx context.Context, req *authzRequest, user *models.User) (string, error) {
	code, err := randomToken()
	if err != nil {
		return "", err
	}
	var orgID interface{}
	if user.OrgID != nil {
		orgID = *user.OrgID
	}
	_, err = h.db.ExecContext(ctx,
		`INSERT INTO oauth_authorization_codes
		   (code_hash, client_id, user_id, org_id, redirect_uri, scope,
		    code_challenge, code_challenge_method, resource, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		hashToken(code), req.ClientID, user.ID, orgID, req.RedirectURI, req.Scope,
		req.Challenge, req.Method, nullIfEmpty(req.Resource), time.Now().Add(authCodeTTL))
	if err != nil {
		return "", err
	}
	return code, nil
}

// ---- the token endpoint --------------------------------------------------------

func (h *OAuthHandler) Token(c *gin.Context) {
	switch c.Request.FormValue("grant_type") {
	case "authorization_code":
		h.tokenFromCode(c)
	case "refresh_token":
		h.tokenFromRefresh(c)
	default:
		oauthError(c, http.StatusBadRequest, "unsupported_grant_type",
			"supported grants are authorization_code and refresh_token")
	}
}

func (h *OAuthHandler) tokenFromCode(c *gin.Context) {
	ctx := c.Request.Context()
	clientID, ok := h.authenticateClient(c)
	if !ok {
		return
	}

	code := c.Request.FormValue("code")
	if code == "" {
		oauthError(c, http.StatusBadRequest, "invalid_request", "code is required")
		return
	}

	var (
		storedClient, redirectURI, scope, challenge, method string
		userID                                              int
		orgID                                               sql.NullInt64
		expiresAt                                           time.Time
		consumedAt                                          sql.NullTime
	)
	err := h.db.QueryRowContext(ctx,
		`SELECT client_id, user_id, org_id, redirect_uri, scope, code_challenge,
		        code_challenge_method, expires_at, consumed_at
		   FROM oauth_authorization_codes WHERE code_hash=$1`, hashToken(code)).
		Scan(&storedClient, &userID, &orgID, &redirectURI, &scope, &challenge,
			&method, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "unknown or expired code")
		return
	}
	if err != nil {
		log.Printf("[OAUTH] token lookup: %v", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "could not read the code")
		return
	}

	// A code presented twice means it leaked — either the first use or this one
	// is an attacker. We cannot tell which, so we end both: every token issued
	// to this client for this user is revoked and the user has to sign in
	// again. (OAuth 2.1, §4.1.3.)
	if consumedAt.Valid {
		log.Printf("[OAUTH] authorization code replayed for client %s user %d — revoking its tokens",
			storedClient, userID)
		h.revokeFamily(ctx, storedClient, userID)
		oauthError(c, http.StatusBadRequest, "invalid_grant", "this code was already used")
		return
	}
	if storedClient != clientID {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "this code was issued to a different client")
		return
	}
	if time.Now().After(expiresAt) {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "the code has expired")
		return
	}
	if ru := c.Request.FormValue("redirect_uri"); ru != "" && ru != redirectURI {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the request")
		return
	}
	if !verifyPKCE(c.Request.FormValue("code_verifier"), challenge, method) {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}

	// Consume before issuing. If this update matches no row another request
	// beat us to it, and only one of us may proceed.
	res, err := h.db.ExecContext(ctx,
		`UPDATE oauth_authorization_codes SET consumed_at=NOW()
		  WHERE code_hash=$1 AND consumed_at IS NULL`, hashToken(code))
	if err != nil {
		oauthError(c, http.StatusInternalServerError, "server_error", "could not consume the code")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "this code was already used")
		return
	}

	h.issueTokens(c, clientID, userID, scope)
}

func (h *OAuthHandler) tokenFromRefresh(c *gin.Context) {
	ctx := c.Request.Context()
	clientID, ok := h.authenticateClient(c)
	if !ok {
		return
	}

	presented := c.Request.FormValue("refresh_token")
	if presented == "" {
		oauthError(c, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	var (
		storedClient, scope string
		userID              int
		orgID               sql.NullInt64
		expiresAt           time.Time
		revokedAt           sql.NullTime
	)
	err := h.db.QueryRowContext(ctx,
		`SELECT client_id, user_id, org_id, scope, expires_at, revoked_at
		   FROM oauth_refresh_tokens WHERE token_hash=$1`, hashToken(presented)).
		Scan(&storedClient, &userID, &orgID, &scope, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "unknown refresh token")
		return
	}
	if err != nil {
		log.Printf("[OAUTH] refresh lookup: %v", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "could not read the token")
		return
	}

	// Refresh tokens rotate, so a revoked one being presented means the old
	// value is in someone else's hands. Same reasoning as a replayed code.
	if revokedAt.Valid {
		log.Printf("[OAUTH] revoked refresh token replayed for client %s user %d", storedClient, userID)
		h.revokeFamily(ctx, storedClient, userID)
		oauthError(c, http.StatusBadRequest, "invalid_grant", "this refresh token was already used")
		return
	}
	if storedClient != clientID {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "this token was issued to a different client")
		return
	}
	if time.Now().After(expiresAt) {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "the refresh token has expired")
		return
	}

	// A client may ask for less than it holds, never more.
	if asked := strings.TrimSpace(c.Request.FormValue("scope")); asked != "" {
		narrowed, err := restrictScope(asked, scope)
		if err != nil {
			oauthError(c, http.StatusBadRequest, "invalid_scope", err.Error())
			return
		}
		scope = narrowed
	}

	if _, err := h.db.ExecContext(ctx,
		`UPDATE oauth_refresh_tokens SET revoked_at=NOW() WHERE token_hash=$1 AND revoked_at IS NULL`,
		hashToken(presented)); err != nil {
		oauthError(c, http.StatusInternalServerError, "server_error", "could not rotate the token")
		return
	}

	h.issueTokens(c, clientID, userID, scope)
}

// issueTokens mints the access token and a fresh refresh token.
//
// The role and the organization are read from the user NOW, not taken from the
// row that authorized this grant. A refresh token can be a month old; over that
// month somebody may have been moved to another tenant or demoted, and a token
// minted from a stale copy would carry an authority the platform no longer
// believes in.
func (h *OAuthHandler) issueTokens(c *gin.Context, clientID string, userID int, scope string) {
	ctx := c.Request.Context()

	var username, role string
	var tokenVersion int
	var orgID sql.NullInt64
	if err := h.db.QueryRowContext(ctx,
		`SELECT username, role, org_id, COALESCE(token_version,0) FROM users WHERE id=$1`, userID).
		Scan(&username, &role, &orgID, &tokenVersion); err != nil {
		log.Printf("[OAUTH] user lookup: %v", err)
		oauthError(c, http.StatusBadRequest, "invalid_grant", "the user no longer exists")
		return
	}

	claims := jwt.MapClaims{
		"user_id":       userID,
		"username":      username,
		"role":          role,
		"token_version": tokenVersion,
		"scope":         scope,
		"client_id":     clientID,
		"exp":           time.Now().Add(accessTokenTTL).Unix(),
		"iat":           time.Now().Unix(),
		"iss":           h.baseURL(c),
	}
	if orgID.Valid {
		claims["org_id"] = int(orgID.Int64)
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(auth.SecretKey)
	if err != nil {
		log.Printf("[OAUTH] sign access token: %v", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "could not issue a token")
		return
	}

	refresh, err := randomToken()
	if err != nil {
		oauthError(c, http.StatusInternalServerError, "server_error", "could not issue a token")
		return
	}
	var org interface{}
	if orgID.Valid {
		org = orgID.Int64
	}
	if _, err := h.db.ExecContext(ctx,
		`INSERT INTO oauth_refresh_tokens (token_hash, client_id, user_id, org_id, scope, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		hashToken(refresh), clientID, userID, org, scope, time.Now().Add(refreshTokenTTL)); err != nil {
		log.Printf("[OAUTH] store refresh token: %v", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "could not issue a token")
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

// Revoke implements RFC 7009 for refresh tokens. Access tokens are self
// contained and expire on their own; saying so is more honest than accepting
// the request and doing nothing.
func (h *OAuthHandler) Revoke(c *gin.Context) {
	clientID, ok := h.authenticateClient(c)
	if !ok {
		return
	}
	token := c.Request.FormValue("token")
	if token != "" {
		if _, err := h.db.ExecContext(c.Request.Context(),
			`UPDATE oauth_refresh_tokens SET revoked_at=NOW()
			  WHERE token_hash=$1 AND client_id=$2 AND revoked_at IS NULL`,
			hashToken(token), clientID); err != nil {
			log.Printf("[OAUTH] revoke: %v", err)
		}
	}
	// The spec requires 200 even for an unknown token, so that revocation
	// cannot be used to probe which tokens exist.
	c.Status(http.StatusOK)
}

func (h *OAuthHandler) revokeFamily(ctx context.Context, clientID string, userID int) {
	if _, err := h.db.ExecContext(ctx,
		`UPDATE oauth_refresh_tokens SET revoked_at=NOW()
		  WHERE client_id=$1 AND user_id=$2 AND revoked_at IS NULL`, clientID, userID); err != nil {
		log.Printf("[OAUTH] revoke family: %v", err)
	}
}

// authenticateClient identifies the caller at the token endpoint. A public
// client presents only its client_id and is proven by PKCE; a confidential one
// must also present its secret.
func (h *OAuthHandler) authenticateClient(c *gin.Context) (string, bool) {
	clientID := c.Request.FormValue("client_id")
	secret := c.Request.FormValue("client_secret")
	if id, pw, ok := c.Request.BasicAuth(); ok {
		clientID, secret = id, pw
	}
	if clientID == "" {
		oauthError(c, http.StatusUnauthorized, "invalid_client", "client_id is required")
		return "", false
	}

	var hash sql.NullString
	err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT client_secret_hash FROM oauth_clients WHERE client_id=$1`, clientID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		oauthError(c, http.StatusUnauthorized, "invalid_client", "unknown client")
		return "", false
	}
	if err != nil {
		log.Printf("[OAUTH] client lookup: %v", err)
		oauthError(c, http.StatusInternalServerError, "server_error", "could not look up the client")
		return "", false
	}

	if hash.Valid && hash.String != "" {
		if bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(secret)) != nil {
			oauthError(c, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
			return "", false
		}
	}
	return clientID, true
}

// ---- error reporting -----------------------------------------------------------

// redirectableError is one we may report by redirecting back to the client;
// noRedirectError is one we may not, because the redirect target itself is in
// question.
type redirectableError struct{ code, desc string }
type noRedirectError struct{ desc string }

func (e redirectableError) Error() string { return e.code + ": " + e.desc }
func (e noRedirectError) Error() string   { return e.desc }

func errRedirect(code, desc string) error { return redirectableError{code, desc} }
func errNoRedirect(desc string) error     { return noRedirectError{desc} }

func (h *OAuthHandler) failAuthz(c *gin.Context, req *authzRequest, err error) {
	var re redirectableError
	if errors.As(err, &re) && req != nil && req.RedirectURI != "" {
		h.redirectError(c, req, re.code, re.desc)
		return
	}
	renderAuthzError(c, err.Error())
}

func (h *OAuthHandler) redirectError(c *gin.Context, req *authzRequest, code, desc string) {
	target, err := url.Parse(req.RedirectURI)
	if err != nil {
		renderAuthzError(c, desc)
		return
	}
	q := target.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if req.State != "" {
		q.Set("state", req.State)
	}
	target.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, target.String())
}

func oauthError(c *gin.Context, status int, code, desc string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": code, "error_description": desc})
}

// ---- small helpers ---------------------------------------------------------------

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken is what goes in the database. SHA-256 without a salt is right here
// and wrong for passwords: these values are 256 bits of entropy we generated
// ourselves, so there is no dictionary to attack and nothing to slow down.
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func verifyPKCE(verifier, challenge, method string) bool {
	if verifier == "" || method != "S256" {
		return false
	}
	// RFC 7636 fixes the length so a trivially short verifier cannot be used.
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("redirect_uri is not a URL: %s", raw)
	}
	if !u.IsAbs() {
		return fmt.Errorf("redirect_uri must be absolute: %s", raw)
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return fmt.Errorf("redirect_uri must not contain a fragment: %s", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		// Plain HTTP only where the traffic never leaves the machine.
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("http redirect_uri is only allowed for loopback addresses: %s", raw)
	default:
		// A private-use scheme is how a desktop or mobile app receives the
		// redirect. Requiring a dot keeps it from shadowing a real scheme.
		if strings.Contains(u.Scheme, ".") {
			return nil
		}
		return fmt.Errorf("unsupported redirect_uri scheme %q", u.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

func containsExact(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// normalizeScope keeps only scopes this server knows about, so a client cannot
// register itself a scope that nothing enforces and then point at it.
func normalizeScope(requested string) string {
	granted, err := restrictScope(requested, ScopeRead+" "+ScopeWrite)
	if err != nil || granted == "" {
		return ScopeRead + " " + ScopeWrite
	}
	return granted
}

// restrictScope returns the requested scopes, refusing any that the ceiling
// does not contain.
func restrictScope(requested, allowed string) (string, error) {
	allowedSet := map[string]bool{}
	for _, s := range strings.Fields(allowed) {
		allowedSet[s] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range strings.Fields(requested) {
		if !allowedSet[s] {
			return "", fmt.Errorf("scope %q is not available to this client", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return "", errors.New("no valid scope requested")
	}
	return strings.Join(out, " "), nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
