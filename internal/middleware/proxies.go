package middleware

import (
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// DefaultTrustedProxies are the networks the stack's own proxies live on:
// loopback and the private ranges docker hands its networks (Traefik, Caddy
// and web-ui's nginx in front of core-api).
const DefaultTrustedProxies = "127.0.0.1,::1,172.16.0.0/12,10.0.0.0/8"

// TrustedProxies is the list from TRUSTED_PROXIES (comma separated IPs or
// CIDRs), or the default.
func TrustedProxies() []string {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		raw = DefaultTrustedProxies
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ConfigureTrustedProxies makes c.ClientIP() read X-Forwarded-For only when
// the request comes from one of the given proxies. Gin trusts every proxy by
// default, so a client could set the header itself and get a new IP — and a
// fresh login rate limit — on every request.
func ConfigureTrustedProxies(r *gin.Engine, proxies []string) error {
	return r.SetTrustedProxies(proxies)
}
