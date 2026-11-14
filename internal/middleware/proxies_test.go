package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func clientIP(t *testing.T, r *gin.Engine, remote, xff string) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ip", nil)
	req.RemoteAddr = remote + ":40000"
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Body.String()
}

func proxyRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := ConfigureTrustedProxies(r, []string{"127.0.0.1", "::1", "172.16.0.0/12", "10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	r.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	return r
}

// A client talking to core-api directly cannot pick its IP with the header —
// which is what the login rate limit keys on.
func TestUntrustedClientCannotSpoofItsIP(t *testing.T) {
	r := proxyRouter(t)
	if got := clientIP(t, r, "203.0.113.5", "1.2.3.4"); got != "203.0.113.5" {
		t.Errorf("spoofed X-Forwarded-For was believed: %s", got)
	}
}

// Behind Traefik → nginx: Traefik sets X-Forwarded-For to the client, nginx
// appends Traefik's address and connects from its own.
func TestClientIPBehindTraefikAndNginx(t *testing.T) {
	r := proxyRouter(t)
	if got := clientIP(t, r, "172.18.0.4", "198.51.100.7, 172.18.0.2"); got != "198.51.100.7" {
		t.Errorf("want the real client, got %s", got)
	}
	// With only nginx in front, whatever the client sent sits left of its
	// real address, and is ignored.
	if got := clientIP(t, r, "172.18.0.4", "6.6.6.6, 198.51.100.7"); got != "198.51.100.7" {
		t.Errorf("client-sent header went past nginx: %s", got)
	}
}

func TestTrustedProxiesFromEnv(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	if got := TrustedProxies(); len(got) != 4 || got[2] != "172.16.0.0/12" {
		t.Errorf("default %v", got)
	}
	t.Setenv("TRUSTED_PROXIES", " 192.168.5.0/24 , 127.0.0.1,")
	if got := TrustedProxies(); len(got) != 2 || got[0] != "192.168.5.0/24" {
		t.Errorf("from env %v", got)
	}
	if err := ConfigureTrustedProxies(gin.New(), []string{"not-an-ip"}); err == nil {
		t.Error("an invalid proxy was accepted")
	}
}
