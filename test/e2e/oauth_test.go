//go:build e2e

package e2e

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The OAuth 2.1 authorization server, exercised the way a client uses it:
// register, send the user to sign in, exchange the code, call the API with what
// comes back.
//
// This belongs in the e2e suite rather than beside the handler because every
// interesting property crosses a boundary the unit tests do not — the code and
// the refresh token live in Postgres, the access token is validated by the same
// middleware that guards every other route, and the scope is enforced there
// rather than where it is issued. A green unit suite would prove none of that.

// oauthHTTP is a client that does NOT follow redirects: the redirect back to
// the client is the result under test, and following it would throw it away.
func oauthHTTP() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func pkcePair(t *testing.T) (verifier, challenge string) {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

type registeredClient struct {
	ClientID string `json:"client_id"`
	Scope    string `json:"scope"`
}

// registerClient performs RFC 7591 dynamic registration, which is how a client
// nobody configured in advance becomes usable.
func registerClient(t *testing.T, redirectURI, scope string) registeredClient {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"client_name":                "e2e test client",
		"redirect_uris":              []string{redirectURI},
		"token_endpoint_auth_method": "none",
		"scope":                      scope,
	})
	// Registration shares the login limiter with the consent form, and the
	// whole suite arrives from one address. Back off rather than loosen it.
	var resp *http.Response
	deadline := time.Now().Add(60 * time.Second)
	for {
		var err error
		resp, err = oauthHTTP().Post(apiBase()+"/oauth/register", "application/json",
			strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if resp.StatusCode != http.StatusTooManyRequests || time.Now().After(deadline) {
			break
		}
		_ = resp.Body.Close()
		time.Sleep(3 * time.Second)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: status %d — %s", resp.StatusCode, truncate(raw))
	}
	var out registeredClient
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode registration: %v — %s", err, truncate(raw))
	}
	if out.ClientID == "" {
		t.Fatalf("registration returned no client_id: %s", truncate(raw))
	}
	return out
}

// approve submits the consent form and returns the authorization code from the
// redirect. It is the step a human performs in a browser.
func approve(t *testing.T, cl registeredClient, redirectURI, challenge, state, scope string) string {
	t.Helper()
	user, pass := adminCredentials()

	form := url.Values{
		"client_id":             {cl.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {scope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"action":                {"allow"},
		"username":              {user},
		"password":              {pass},
	}

	// The decision endpoint shares the login rate limiter, and the whole suite
	// arrives from one address. Back off rather than weakening the limiter.
	var resp *http.Response
	deadline := time.Now().Add(60 * time.Second)
	for {
		var err error
		resp, err = oauthHTTP().PostForm(apiBase()+"/oauth/authorize", form)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		if resp.StatusCode != http.StatusTooManyRequests || time.Now().After(deadline) {
			break
		}
		_ = resp.Body.Close()
		time.Sleep(3 * time.Second)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize: status %d, want a redirect — %s", resp.StatusCode, truncate(raw))
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if e := loc.Query().Get("error"); e != "" {
		t.Fatalf("authorize returned error=%s (%s)", e, loc.Query().Get("error_description"))
	}
	if got := loc.Query().Get("state"); got != state {
		t.Fatalf("state came back as %q, want %q — a client cannot bind the response without it", got, state)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in the redirect: %s", loc.String())
	}
	return code
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func postToken(t *testing.T, form url.Values) (int, tokenResponse) {
	t.Helper()
	resp, err := oauthHTTP().PostForm(apiBase()+"/oauth/token", form)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out tokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode token response: %v — %s", err, truncate(raw))
	}
	return resp.StatusCode, out
}

func exchange(t *testing.T, cl registeredClient, code, verifier, redirectURI string) tokenResponse {
	t.Helper()
	status, out := postToken(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {cl.ClientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if status != http.StatusOK {
		t.Fatalf("exchange: status %d — %s: %s", status, out.Error, out.Description)
	}
	return out
}

const testRedirect = "http://127.0.0.1:33418/callback"

// ── The round trip ──────────────────────────────────────────────────────────

// The whole point: a client that started with nothing ends up holding a token
// the API accepts, and the user never gave it a password.
func TestOAuthAuthorizationCodeFlow(t *testing.T) {
	verifier, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read openedge:write")
	code := approve(t, cl, testRedirect, challenge, "state-123", "openedge:read openedge:write")
	tok := exchange(t, cl, code, verifier, testRedirect)

	if tok.TokenType != "Bearer" || tok.AccessToken == "" {
		t.Fatalf("unusable token response: %+v", tok)
	}
	if tok.RefreshToken == "" {
		t.Fatal("no refresh token, so the client must send the user back through sign-in every hour")
	}
	if tok.ExpiresIn <= 0 || tok.ExpiresIn > 3600 {
		t.Errorf("expires_in is %d; an access token that cannot be revoked should be short-lived", tok.ExpiresIn)
	}

	// The token has to work on the real API, through the same middleware as
	// every other request.
	client := &apiClient{t: t, token: tok.AccessToken}
	client.mustDo(http.MethodGet, "/api/gateways", nil, http.StatusOK)
}

func TestOAuthDiscoveryDocument(t *testing.T) {
	resp, err := oauthHTTP().Get(apiBase() + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata: status %d", resp.StatusCode)
	}

	var meta struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		RegistrationEndpoint  string   `json:"registration_endpoint"`
		ChallengeMethods      []string `json:"code_challenge_methods_supported"`
		GrantTypes            []string `json:"grant_types_supported"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode metadata: %v — %s", err, truncate(raw))
	}
	for name, value := range map[string]string{
		"issuer":                 meta.Issuer,
		"authorization_endpoint": meta.AuthorizationEndpoint,
		"token_endpoint":         meta.TokenEndpoint,
		"registration_endpoint":  meta.RegistrationEndpoint,
	} {
		if value == "" {
			t.Errorf("%s is missing; a client cannot complete the flow without it", name)
		}
	}
	// Advertising "plain" would invite clients to use it.
	for _, m := range meta.ChallengeMethods {
		if m != "S256" {
			t.Errorf("code_challenge_methods_supported advertises %q", m)
		}
	}
}

// ── What must not work ──────────────────────────────────────────────────────

// Without this check, anyone who intercepts the redirect — a malicious app
// registered for the same URI scheme, a proxy, a shoulder — can redeem the code.
func TestOAuthCodeIsUselessWithoutTheVerifier(t *testing.T) {
	_, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")
	code := approve(t, cl, testRedirect, challenge, "s", "openedge:read")

	wrong, _ := pkcePair(t)
	status, out := postToken(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {cl.ClientID},
		"redirect_uri":  {testRedirect},
		"code_verifier": {wrong},
	})
	if status == http.StatusOK {
		t.Fatal("a code was exchanged with the wrong PKCE verifier")
	}
	if out.Error != "invalid_grant" {
		t.Errorf("want invalid_grant, got %q", out.Error)
	}
}

// A code presented twice means it leaked. We cannot tell which presentation is
// the attacker, so both lose: the tokens already issued are revoked too.
func TestOAuthReplayedCodeRevokesTheTokensItIssued(t *testing.T) {
	verifier, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read openedge:write")
	code := approve(t, cl, testRedirect, challenge, "s", "openedge:read openedge:write")

	first := exchange(t, cl, code, verifier, testRedirect)

	status, out := postToken(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {cl.ClientID},
		"redirect_uri":  {testRedirect},
		"code_verifier": {verifier},
	})
	if status == http.StatusOK {
		t.Fatal("the same authorization code was redeemed twice")
	}
	if out.Error != "invalid_grant" {
		t.Errorf("want invalid_grant on replay, got %q", out.Error)
	}

	// And the refresh token from the first, possibly legitimate, exchange is
	// now dead — the session is ended rather than left to a coin flip.
	status, out = postToken(t, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {first.RefreshToken},
		"client_id":     {cl.ClientID},
	})
	if status == http.StatusOK {
		t.Fatalf("after a code replay the earlier refresh token still works: %+v", out)
	}
}

// Rotation is what makes a stolen refresh token detectable: the moment either
// copy is used, the other stops working.
func TestOAuthRefreshTokensRotate(t *testing.T) {
	verifier, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read openedge:write")
	code := approve(t, cl, testRedirect, challenge, "s", "openedge:read openedge:write")
	first := exchange(t, cl, code, verifier, testRedirect)

	status, second := postToken(t, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {first.RefreshToken},
		"client_id":     {cl.ClientID},
	})
	if status != http.StatusOK {
		t.Fatalf("refresh failed: %d — %s", status, second.Error)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("the refresh token was returned unchanged, so reuse can never be detected")
	}
	if second.AccessToken == "" {
		t.Fatal("refresh returned no access token")
	}

	// The old one is spent.
	status, _ = postToken(t, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {first.RefreshToken},
		"client_id":     {cl.ClientID},
	})
	if status == http.StatusOK {
		t.Fatal("a rotated-away refresh token was accepted a second time")
	}
}

// The consent screen tells the user what they are granting. If the scope were
// not enforced, that sentence would be false.
func TestOAuthReadOnlyTokenCannotWrite(t *testing.T) {
	verifier, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")
	code := approve(t, cl, testRedirect, challenge, "s", "openedge:read")
	tok := exchange(t, cl, code, verifier, testRedirect)

	if strings.Contains(tok.Scope, "write") {
		t.Fatalf("a client registered read-only received %q", tok.Scope)
	}

	client := &apiClient{t: t, token: tok.AccessToken}
	// Reading is what it asked for.
	client.mustDo(http.MethodGet, "/api/gateways", nil, http.StatusOK)

	// Writing is not. The admin bootstrap account is behind this token, so a
	// 403 here can only come from the scope.
	status, body := client.do(http.MethodPost, "/api/sites",
		map[string]interface{}{"name": "should-not-exist", "org_id": 1})
	if status != http.StatusForbidden {
		t.Fatalf("a read-only token created a site: status %d — %s", status, truncate(body))
	}
}

// Prefix matching on redirect URIs is how authorization codes get delivered to
// somebody else.
func TestOAuthRejectsUnregisteredRedirect(t *testing.T) {
	_, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")
	user, pass := adminCredentials()

	resp, err := oauthHTTP().PostForm(apiBase()+"/oauth/authorize", url.Values{
		"client_id":             {cl.ClientID},
		"redirect_uri":          {"http://127.0.0.1:33418/callback.evil.example.com"},
		"response_type":         {"code"},
		"scope":                 {"openedge:read"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"action":                {"allow"},
		"username":              {user},
		"password":              {pass},
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusFound {
		t.Fatalf("an unregistered redirect_uri was honoured: %s", resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// "plain" would carry the verifier in the authorization request itself.
func TestOAuthRefusesPlainPKCE(t *testing.T) {
	verifier, _ := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")

	resp, err := oauthHTTP().Get(apiBase() + "/oauth/authorize?" + url.Values{
		"client_id":             {cl.ClientID},
		"redirect_uri":          {testRedirect},
		"response_type":         {"code"},
		"code_challenge":        {verifier},
		"code_challenge_method": {"plain"},
	}.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The client and its redirect check out, so this error goes back to the
	// client as a redirect rather than to the user as a page.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("want a redirected error, got %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("error") != "invalid_request" {
		t.Fatalf("want error=invalid_request, got %q", loc.Query().Get("error"))
	}
}

// Missing PKCE altogether, not just the wrong method.
func TestOAuthRequiresPKCE(t *testing.T) {
	cl := registerClient(t, testRedirect, "openedge:read")

	resp, err := oauthHTTP().Get(apiBase() + "/oauth/authorize?" + url.Values{
		"client_id":     {cl.ClientID},
		"redirect_uri":  {testRedirect},
		"response_type": {"code"},
	}.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || loc.Query().Get("error") == "" {
		t.Fatalf("a request without code_challenge was not refused: status %d", resp.StatusCode)
	}
}

// The user must see who is asking before deciding.
func TestOAuthConsentPageNamesTheClient(t *testing.T) {
	_, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")

	resp, err := oauthHTTP().Get(apiBase() + "/oauth/authorize?" + url.Values{
		"client_id":             {cl.ClientID},
		"redirect_uri":          {testRedirect},
		"response_type":         {"code"},
		"scope":                 {"openedge:read"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent page: status %d", resp.StatusCode)
	}
	page := string(raw)
	if !strings.Contains(page, "e2e test client") {
		t.Error("the page does not name the client asking for access")
	}
	if !strings.Contains(page, `name="password"`) {
		t.Error("the page does not ask for credentials, so it is riding an ambient session")
	}
	if !strings.Contains(strings.ToLower(page), "leggere") {
		t.Error("the page does not say what is being granted")
	}
}

// Declining must not produce a code.
func TestOAuthDenyReturnsNoCode(t *testing.T) {
	_, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")

	resp, err := oauthHTTP().PostForm(apiBase()+"/oauth/authorize", url.Values{
		"client_id":             {cl.ClientID},
		"redirect_uri":          {testRedirect},
		"response_type":         {"code"},
		"scope":                 {"openedge:read"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"action":                {"deny"},
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("code") != "" {
		t.Fatal("declining produced an authorization code")
	}
	if loc.Query().Get("error") != "access_denied" {
		t.Fatalf("want error=access_denied, got %q", loc.Query().Get("error"))
	}
}

// Wrong credentials must not produce a code either — the form is a login, and
// it has to behave like one.
func TestOAuthWrongPasswordProducesNoCode(t *testing.T) {
	_, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")
	user, _ := adminCredentials()

	resp, err := oauthHTTP().PostForm(apiBase()+"/oauth/authorize", url.Values{
		"client_id":             {cl.ClientID},
		"redirect_uri":          {testRedirect},
		"response_type":         {"code"},
		"scope":                 {"openedge:read"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"action":                {"allow"},
		"username":              {user},
		"password":              {"definitely-not-the-password"},
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusFound {
		t.Fatalf("a wrong password produced a redirect: %s", resp.Header.Get("Location"))
	}
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "Credenziali non valide") {
		t.Errorf("expected the form again with an error; got %s", truncate(raw))
	}
}

// Registration must not accept a redirect URI that would let the code be
// delivered over plain HTTP to another host.
func TestOAuthRegistrationRejectsInsecureRedirect(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"client_name":   "insecure client",
		"redirect_uris": []string{"http://evil.example.com/cb"},
	})
	resp, err := oauthHTTP().Post(apiBase()+"/oauth/register", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusCreated {
		t.Fatal("a client registered a plain-HTTP redirect to another host")
	}
}

// A client cannot obtain more than it registered for, whatever it asks.
func TestOAuthCannotEscalateScopeAtAuthorize(t *testing.T) {
	_, challenge := pkcePair(t)
	cl := registerClient(t, testRedirect, "openedge:read")

	resp, err := oauthHTTP().Get(apiBase() + "/oauth/authorize?" + url.Values{
		"client_id":             {cl.ClientID},
		"redirect_uri":          {testRedirect},
		"response_type":         {"code"},
		"scope":                 {"openedge:read openedge:write"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode())
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("want the request refused by redirect, got %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("error") != "invalid_scope" {
		t.Fatalf("want error=invalid_scope, got %q", loc.Query().Get("error"))
	}
}
