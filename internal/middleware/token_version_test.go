package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// A token is retired when the user's token_version has moved past it, or the
// user is gone; a failure to read the version does not sign everybody out.
func TestRequireAuth_RetiredTokensAreRefused(t *testing.T) {
	versions := map[int]int{1: 0, 2: 3}
	var lookupErr error
	TokenVersionLookup = func(_ context.Context, id int) (int, error) {
		if lookupErr != nil {
			return 0, lookupErr
		}
		v, ok := versions[id]
		if !ok {
			return 0, ErrUserGone
		}
		return v, nil
	}
	t.Cleanup(func() { TokenVersionLookup = nil })

	r := newTestRouter()
	call := func(claims jwt.MapClaims) int {
		claims["exp"] = time.Now().Add(time.Hour).Unix()
		claims["role"] = "admin"
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ping", nil)
		req.Header.Set("Authorization", "Bearer "+makeToken(t, claims))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	cases := []struct {
		name   string
		claims jwt.MapClaims
		want   int
	}{
		{"current version", jwt.MapClaims{"user_id": 2, "token_version": 3}, http.StatusOK},
		{"older version", jwt.MapClaims{"user_id": 2, "token_version": 2}, http.StatusUnauthorized},
		{"no claim, version 0", jwt.MapClaims{"user_id": 1}, http.StatusOK},
		{"no claim, version moved", jwt.MapClaims{"user_id": 2}, http.StatusUnauthorized},
		{"deleted user", jwt.MapClaims{"user_id": 9, "token_version": 0}, http.StatusUnauthorized},
		{"no user id", jwt.MapClaims{"token_version": 0}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		if got := call(tc.claims); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}

	lookupErr = errors.New("database unavailable")
	if got := call(jwt.MapClaims{"user_id": 2, "token_version": 0}); got != http.StatusOK {
		t.Errorf("a failed lookup refused the request: %d", got)
	}
}
