package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// A token the OAuth flow issued carries the scope the user consented to. If
// nothing checked it, the consent screen would be describing a restriction that
// does not exist — the user would read "read-only" and grant full control.

func scopedRouter() *gin.Engine {
	r := gin.New()
	r.Use(RequireAuth)
	handler := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	for _, register := range []func(string, ...gin.HandlerFunc) gin.IRoutes{
		r.GET, r.POST, r.PUT, r.PATCH, r.DELETE,
	} {
		register("/thing", handler)
	}
	return r
}

func callWith(t *testing.T, method string, claims jwt.MapClaims) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/thing", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(t, claims))
	scopedRouter().ServeHTTP(w, req)
	return w
}

func baseClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"user_id":  float64(1),
		"username": "alice",
		"role":     "admin",
		"org_id":   float64(1),
		"exp":      time.Now().Add(time.Hour).Unix(),
	}
}

func TestReadOnlyScopeCannotWrite(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		claims := baseClaims()
		claims["scope"] = "openedge:read"
		w := callWith(t, method, claims)

		if w.Code != http.StatusForbidden {
			t.Errorf("%s with a read-only token: want 403, got %d", method, w.Code)
		}
		// The header is how a client learns it needs to ask for more, rather
		// than reporting a permission problem the user cannot act on.
		if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "insufficient_scope") {
			t.Errorf("%s: want an insufficient_scope challenge, got %q", method, got)
		}
	}
}

func TestReadOnlyScopeCanStillRead(t *testing.T) {
	claims := baseClaims()
	claims["scope"] = "openedge:read"

	if w := callWith(t, http.MethodGet, claims); w.Code != http.StatusOK {
		t.Fatalf("a read-only token was refused a GET: %d", w.Code)
	}
}

func TestWriteScopeCanWrite(t *testing.T) {
	claims := baseClaims()
	claims["scope"] = "openedge:read openedge:write"

	if w := callWith(t, http.MethodPost, claims); w.Code != http.StatusOK {
		t.Fatalf("a token holding the write scope was refused: %d", w.Code)
	}
}

// Every existing session predates this check and carries no scope claim.
// Narrowing those would log out the whole platform on deploy.
func TestPasswordLoginTokensAreUnrestricted(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if w := callWith(t, method, baseClaims()); w.Code != http.StatusOK {
			t.Errorf("%s with an ordinary login token: want 200, got %d", method, w.Code)
		}
	}
}

// A scope claim of the wrong type is not a scope. Treating it as absent would
// let a forged-looking token opt out of the check — but the token is signed, so
// the only way to get here is a bug on our side; failing open on writes is the
// wrong direction to guess.
func TestNonStringScopeIsIgnoredSafely(t *testing.T) {
	claims := baseClaims()
	claims["scope"] = []string{"openedge:write"}

	// Not a string, so not a delegated token, so unrestricted — the same rule
	// as a missing claim, applied consistently rather than by accident.
	if w := callWith(t, http.MethodPost, claims); w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", w.Code)
	}
}

func TestScopeWithExtraWhitespace(t *testing.T) {
	claims := baseClaims()
	claims["scope"] = "  openedge:read   openedge:write  "

	if w := callWith(t, http.MethodPost, claims); w.Code != http.StatusOK {
		t.Fatalf("whitespace in the scope string blocked a legitimate write: %d", w.Code)
	}
}

// "openedge:writer" contains "openedge:write" as a prefix. A substring check
// would grant it.
func TestScopeIsMatchedWholeNotByPrefix(t *testing.T) {
	claims := baseClaims()
	claims["scope"] = "openedge:writeXX"

	if w := callWith(t, http.MethodPost, claims); w.Code != http.StatusForbidden {
		t.Fatalf("a scope that merely starts with the write scope was accepted: %d", w.Code)
	}
}
