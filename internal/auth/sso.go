package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// SSOProvider holds the OAuth2 config for one org+provider combination.
type SSOProvider struct {
	OrgID        int
	Provider     string
	ClientID     string
	ClientSecret string
	TenantID     string
	DomainHint   string
}

// GetSSOProvider loads the SSO provider config for an org from DB.
func GetSSOProvider(db *sql.DB, provider string, orgID int) (*SSOProvider, error) {
	var p SSOProvider
	err := db.QueryRowContext(context.Background(), `
		SELECT org_id, provider, client_id, client_secret,
		       COALESCE(tenant_id,''), COALESCE(domain_hint,'')
		FROM sso_providers
		WHERE provider = $1 AND org_id = $2 AND enabled = true
	`, provider, orgID).Scan(&p.OrgID, &p.Provider, &p.ClientID, &p.ClientSecret, &p.TenantID, &p.DomainHint)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetSSOProviderByDomain finds an org's SSO config by email domain.
// Used during callback to assign an org automatically.
func GetSSOProviderByDomain(db *sql.DB, provider, emailDomain string) (*SSOProvider, error) {
	var p SSOProvider
	err := db.QueryRowContext(context.Background(), `
		SELECT org_id, provider, client_id, client_secret,
		       COALESCE(tenant_id,''), COALESCE(domain_hint,'')
		FROM sso_providers
		WHERE provider = $1 AND domain_hint = $2 AND enabled = true
		LIMIT 1
	`, provider, emailDomain).Scan(&p.OrgID, &p.Provider, &p.ClientID, &p.ClientSecret, &p.TenantID, &p.DomainHint)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// OAuth2Config builds the oauth2.Config for the given provider.
func (p *SSOProvider) OAuth2Config(redirectURL string) (*oauth2.Config, error) {
	switch p.Provider {
	case "google":
		return &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		}, nil
	case "azure":
		tenantID := p.TenantID
		if tenantID == "" {
			// SECURITY: "common" is Microsoft's multi-tenant endpoint — it accepts logins
			// from ANY Azure AD tenant, including one an attacker registered themselves.
			// Set tenant_id in sso_providers to pin an org to a single tenant. This matters
			// because Graph's userinfo omits email_verified (see FetchUserInfo), so a
			// self-owned tenant is the one place an arbitrary `mail` value can be minted.
			tenantID = "common"
		}
		return &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile", "offline_access"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", tenantID),
				TokenURL: fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenantID),
			},
		}, nil
	}
	return nil, fmt.Errorf("unsupported SSO provider: %s", p.Provider)
}

// OIDCUserInfo contains the fields we extract from the provider's userinfo endpoint.
type OIDCUserInfo struct {
	Sub       string `json:"sub"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	GivenName string `json:"given_name"`
	// Pointer so we can distinguish "provider said false" from "provider omitted
	// the claim" — the two get different treatment in FetchUserInfo.
	EmailVerified *bool `json:"email_verified"`
}

// FetchUserInfo exchanges the token for user identity from the provider.
func FetchUserInfo(ctx context.Context, provider string, token *oauth2.Token, cfg *oauth2.Config) (*OIDCUserInfo, error) {
	userInfoURL := "https://openidconnect.googleapis.com/v1/userinfo"
	if provider == "azure" {
		userInfoURL = "https://graph.microsoft.com/oidc/userinfo"
	}

	client := cfg.Client(ctx, token)
	client.Timeout = 10 * time.Second
	resp, err := client.Get(userInfoURL)
	if err != nil {
		return nil, fmt.Errorf("userinfo fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("userinfo read: %w", err)
	}

	var info OIDCUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("userinfo parse: %w", err)
	}
	if info.Email == "" {
		return nil, fmt.Errorf("provider did not return an email address")
	}

	// The email IS our identity key — UpsertSSOUser matches existing accounts on it —
	// so an unverified address lets anyone who can set a `mail`/`email` attribute in a
	// tenant they control impersonate a victim. Refuse to trust one.
	//   - Google always emits email_verified, so we require an explicit true.
	//   - Azure/Graph's /oidc/userinfo omits the claim entirely, so there we can only
	//     reject an explicit false; pin tenant_id in sso_providers for real assurance
	//     (see the "common" note in OAuth2Config).
	if provider == "google" {
		if info.EmailVerified == nil || !*info.EmailVerified {
			return nil, fmt.Errorf("provider did not verify the email address")
		}
	} else if info.EmailVerified != nil && !*info.EmailVerified {
		return nil, fmt.Errorf("provider did not verify the email address")
	}

	return &info, nil
}

// UpsertSSOUser finds or creates a user for the SSO login, returning the user_id.
// If a user with the same email exists *in the same org*, we return their id.
// Otherwise a new user is created with role=user in the given org.
func UpsertSSOUser(db *sql.DB, info *OIDCUserInfo, orgID int) (int, string, error) {
	// Match ONLY within the target org (prevents cross-org user takeover).
	// A global admin row (org_id IS NULL) must NOT be reachable here: matching it
	// would return role='admin' with no org, i.e. a full global-admin JWT, to anyone
	// who can get the provider to assert the admin's email address. Global admins
	// therefore log in with password/MFA, never through org-scoped SSO.
	var userID int
	var role, username string
	err := db.QueryRowContext(context.Background(), `
		SELECT id, role, username FROM users
		WHERE email = $1 AND org_id = $2
	`, info.Email, orgID).Scan(&userID, &role, &username)
	if err == nil {
		return userID, role, nil
	}
	if err != sql.ErrNoRows {
		return 0, "", fmt.Errorf("lookup user by email: %w", err)
	}

	// Create new user — derive username from email prefix
	baseUsername := strings.Split(info.Email, "@")[0]
	baseUsername = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, strings.ToLower(baseUsername))

	fullName := info.Name
	if fullName == "" {
		fullName = info.GivenName
	}
	if fullName == "" {
		fullName = baseUsername
	}

	// Use a placeholder non-loginable password hash for SSO users.
	// They cannot log in with password — only via SSO.
	const ssoPasswordHash = "$2a$10$SSONOLOGINXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"

	// ON CONFLICT uses idx_users_org_email (org_id, email) WHERE org_id IS NOT NULL.
	// This lets the same email exist in different orgs (multi-tenant SSO) while still
	// safely handling re-login: just update full_name and return the existing row.
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO users (username, password_hash, role, full_name, org_id, email)
		VALUES ($1, $2, 'user', $3, $4, $5)
		ON CONFLICT (org_id, email) WHERE org_id IS NOT NULL DO UPDATE SET
			full_name = EXCLUDED.full_name
		RETURNING id, role, username
	`, baseUsername, ssoPasswordHash, fullName, orgID, info.Email).Scan(&userID, &role, &username)
	if err != nil {
		return 0, "", fmt.Errorf("insert sso user: %w", err)
	}

	return userID, role, nil
}
