package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// These cover the parts of the authorization server that decide whether an
// attack works, and that need no database to exercise. The full round trip —
// register, sign in, exchange, call the API — is in test/e2e/oauth_test.go,
// because it only means anything against a real stack.

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// A 43-character verifier is the shortest RFC 7636 permits.
const goodVerifier = "abcdefghijklmnopqrstuvwxyz0123456789-._~ABCD"

func TestPKCEAcceptsTheMatchingVerifier(t *testing.T) {
	if !verifyPKCE(goodVerifier, challengeFor(goodVerifier), "S256") {
		t.Fatal("a correct verifier was rejected")
	}
}

func TestPKCERejectsEverythingElse(t *testing.T) {
	good := challengeFor(goodVerifier)

	cases := []struct {
		name              string
		verifier, chal, m string
	}{
		{"a different verifier", strings.Repeat("z", 43), good, "S256"},
		{"no verifier at all", "", good, "S256"},
		// "plain" would put the verifier in the authorization request, where
		// anything that can read the redirect can read it — which is exactly
		// what PKCE exists to stop.
		{"plain, even when it matches", goodVerifier, goodVerifier, "plain"},
		{"an empty method", goodVerifier, good, ""},
		// A short verifier has too little entropy to be worth guessing against.
		{"a verifier below the minimum length", "short", challengeFor("short"), "S256"},
		{"a verifier above the maximum length", strings.Repeat("a", 129),
			challengeFor(strings.Repeat("a", 129)), "S256"},
		{"an empty challenge", goodVerifier, "", "S256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if verifyPKCE(tc.verifier, tc.chal, tc.m) {
				t.Fatal("accepted")
			}
		})
	}
}

// An open redirect here does not leak a page — it leaks authorization codes.
func TestRedirectURIValidation(t *testing.T) {
	ok := []string{
		"https://claude.ai/api/mcp/auth_callback",
		"https://example.com/cb?tenant=1",
		"http://127.0.0.1:33418/callback",
		"http://localhost:8080/cb",
		"com.example.app:/oauth/callback",
	}
	for _, uri := range ok {
		if err := validateRedirectURI(uri); err != nil {
			t.Errorf("%s should be allowed: %v", uri, err)
		}
	}

	bad := map[string]string{
		"http://evil.example.com/cb": "plain HTTP off the loopback",
		"/relative/cb":               "not absolute",
		"https://ok.example.com/cb#x": "carries a fragment, which the redirect " +
			"would drop and the client could not see",
		"myapp:/cb":  "a private-use scheme with no dot can shadow a real one",
		"javascript": "not a URL at all",
	}
	for uri, why := range bad {
		if err := validateRedirectURI(uri); err == nil {
			t.Errorf("%s should be refused (%s)", uri, why)
		}
	}
}

func TestScopeCannotBeWidened(t *testing.T) {
	granted, err := restrictScope(ScopeRead, ScopeRead+" "+ScopeWrite)
	if err != nil || granted != ScopeRead {
		t.Fatalf("asking for less than allowed should succeed: %q, %v", granted, err)
	}

	if _, err := restrictScope(ScopeRead+" "+ScopeWrite, ScopeRead); err == nil {
		t.Fatal("a client restricted to read obtained write")
	}
	if _, err := restrictScope("openedge:admin", ScopeRead+" "+ScopeWrite); err == nil {
		t.Fatal("a scope this server does not implement was granted")
	}
	if _, err := restrictScope("", ScopeRead); err == nil {
		t.Fatal("an empty scope was treated as valid")
	}
}

// A client registering an unknown scope must not end up holding it: nothing
// would enforce a scope the server does not know, so it would read as a
// restriction while being none.
func TestRegistrationDropsUnknownScopes(t *testing.T) {
	if got := normalizeScope("openedge:root"); strings.Contains(got, "root") {
		t.Fatalf("an unknown scope survived registration: %q", got)
	}
	if got := normalizeScope(ScopeRead); got != ScopeRead {
		t.Fatalf("a known scope was not preserved: %q", got)
	}
}

func TestHashTokenIsNotTheToken(t *testing.T) {
	tok, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	h := hashToken(tok)
	if strings.Contains(h, tok) || h == tok {
		t.Fatal("the stored value contains the credential itself")
	}
	if hashToken(tok) != h {
		t.Fatal("hashing is not stable, so no token could ever be looked up")
	}
	if hashToken(tok+"x") == h {
		t.Fatal("two different tokens hash the same")
	}
}

func TestRandomTokensDoNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		tok, err := randomToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatal("randomToken repeated a value")
		}
		seen[tok] = true
	}
}

// The client name is chosen by whoever registered the client, and it is
// rendered on the page where the user decides whether to trust them.
func TestConsentPageEscapesTheClientName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/oauth/authorize", nil)

	renderConsent(c, consentView{
		Request: &authzRequest{
			ClientName:  `<script>fetch('//evil')</script>`,
			ClientID:    "cid",
			RedirectURI: "https://example.com/cb",
			Scope:       ScopeRead,
			State:       `" onload="alert(1)`,
		},
		Action: "https://app.example.com/oauth/authorize",
	})

	body := rec.Body.String()
	if strings.Contains(body, "<script>fetch") {
		t.Fatal("the client name was rendered as markup")
	}
	if strings.Contains(body, `onload="alert(1)`) {
		t.Fatal("state escaped its attribute")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected the name escaped and shown to the user; got %q", body)
	}
}

// The user is agreeing to something, so the page has to say what.
func TestConsentPageDescribesTheScopes(t *testing.T) {
	view := consentView{Request: &authzRequest{Scope: ScopeRead + " " + ScopeWrite}}
	lines := view.Scopes()
	if len(lines) != 2 {
		t.Fatalf("want one line per scope, got %v", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "openedge:") {
			t.Errorf("the scope is shown as its identifier rather than in words: %q", l)
		}
	}
}

// An error about the redirect URI must be shown here, not sent there.
func TestAuthzErrorPageDoesNotRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/oauth/authorize", nil)

	renderAuthzError(c, "redirect_uri does not match")

	if rec.Code != 400 {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("the error was redirected to %q", loc)
	}
}

func TestBaseURLPrefersTheConfiguredIssuer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Host = "attacker.example.com"

	h := NewOAuthHandler(nil, nil, "https://app.example.com/")
	if got := h.baseURL(c); got != "https://app.example.com" {
		t.Fatalf("a configured issuer was overridden by the Host header: %q", got)
	}

	// With none configured we fall back to the request, which is the only
	// source available — and the reason configuring it is recommended.
	h = NewOAuthHandler(nil, nil, "")
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	if got := h.baseURL(c); got != "https://attacker.example.com" {
		t.Fatalf("unexpected derived base URL: %q", got)
	}
}
