//go:build e2e

package e2e

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// The cloud deployment, as a thing that is actually run.
//
// Everything else in this suite runs against docker-compose.yml on its own:
// every service published on the host, the API reached on 8081, the broker on
// 18830. That is the on-prem shape, and it was the only shape any check had
// ever executed. The cloud shape — docker-compose.vps.yml, Traefik in front,
// host ports removed from everything else — had been written, documented and
// recommended, and never once brought up by CI, by a test, or by anyone.
//
// It is the same failure mode this whole suite exists for: two halves that each
// look right and are joined by nothing but an assumption. The application was
// proven, the overlay was proven on paper, and the join between them — routing,
// TLS termination, the nginx behind the proxy, which ports survive — was
// proven nowhere. The rest of the suite now runs THROUGH that overlay; this
// file asserts the properties that only the overlay has.
//
// What this cannot prove: that Let's Encrypt issues a certificate. That needs a
// public domain resolving to the runner and a real CA, neither of which CI has,
// so the ACME directory is pointed at an address nothing answers and Traefik
// serves its own default certificate instead. Everything up to and including
// TLS termination is exercised; the certificate's provenance is not.

func requireProxiedDeployment(t *testing.T) {
	t.Helper()
	if !proxiedDeployment() {
		t.Skip("set E2E_DEPLOYMENT=vps to run the cloud-overlay assertions")
	}
}

// publicURL is the address a customer types, which under this overlay is the
// only address anything is reachable at.
func publicURL(t *testing.T) *url.URL {
	t.Helper()
	u, err := url.Parse(apiBase())
	if err != nil {
		t.Fatalf("E2E_API_URL is not a URL: %v", err)
	}
	return u
}

// ── The proxy ───────────────────────────────────────────────────────────────

// Port 80 exists in this overlay only to move people off it. A deployment that
// serves the application there serves credentials over plaintext to anyone on
// the path, and the HSTS header below never gets a chance to be read.
func TestHTTPIsRedirectedToHTTPS(t *testing.T) {
	requireProxiedDeployment(t)

	u := publicURL(t)
	client := httpClient(15 * time.Second)
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Get("http://" + u.Host + "/")
	if err != nil {
		t.Fatalf("GET http://%s/: %v", u.Host, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		t.Fatalf("plain HTTP answered %d instead of redirecting — the application is being "+
			"served unencrypted on port 80", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || loc.Scheme != "https" {
		t.Fatalf("redirected to %q, which is not https", resp.Header.Get("Location"))
	}
}

// The headers are configured on a Traefik middleware and attached to the router
// by a label. A middleware that is defined and not attached is the default
// outcome of a typo, and it fails silently: the site works, and the protection
// is simply absent.
func TestSecurityHeadersReachTheBrowser(t *testing.T) {
	requireProxiedDeployment(t)

	resp, err := httpClient(15 * time.Second).Get(apiBase() + "/")
	if err != nil {
		t.Fatalf("GET %s/: %v", apiBase(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the public root answered %d — the proxy is not serving the web UI", resp.StatusCode)
	}

	for _, want := range []struct{ header, contains string }{
		{"Strict-Transport-Security", "max-age="},
		{"X-Frame-Options", "DENY"},
		{"X-Content-Type-Options", "nosniff"},
		{"Referrer-Policy", "strict-origin"},
	} {
		got := resp.Header.Get(want.header)
		if !strings.Contains(got, want.contains) {
			t.Errorf("%s is %q, want something containing %q — the middleware is defined in the "+
				"overlay but is not reaching responses", want.header, got, want.contains)
		}
	}
}

// The OAuth issuer is not cosmetic: it is the base every endpoint in the
// discovery document is built from, and a client that reads it goes to those
// addresses and nowhere else. Derived from a request header it happens to work;
// stated wrong in the overlay it sends every connector to a host that does not
// answer, and the failure surfaces in someone else's software.
func TestOAuthIssuerIsThePublicDomain(t *testing.T) {
	requireProxiedDeployment(t)

	resp, err := httpClient(15 * time.Second).Get(apiBase() + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatalf("fetching the discovery document: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var meta struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decoding the discovery document: %v", err)
	}

	want := apiBase()
	for _, f := range []struct{ name, got string }{
		{"issuer", meta.Issuer},
		{"authorization_endpoint", meta.AuthorizationEndpoint},
		{"token_endpoint", meta.TokenEndpoint},
	} {
		if !strings.HasPrefix(f.got, want) {
			t.Errorf("%s is %q; a client reaching this deployment at %s cannot follow that",
				f.name, f.got, want)
		}
	}
}

// ── What must not be reachable ──────────────────────────────────────────────

// Every service except the proxy has its host ports removed by the overlay.
// That is the single property separating "a VPS running OpenEdge" from "a
// Postgres on the public internet with the application beside it", and losing
// it takes one merge that reinstates a ports: entry for local debugging.
func TestInternalPortsAreNotPublished(t *testing.T) {
	requireProxiedDeployment(t)

	// Every host binding docker-compose.yml declares. The overlay replaces each
	// with ports: [] — this is the list that has to stay empty.
	for _, p := range []struct {
		port int
		what string
	}{
		{5432, "PostgreSQL — every tenant's data"},
		{6379, "Redis — live tag values and sessions"},
		{8081, "core-api — the API without the proxy, and /metrics with it"},
		{18830, "Mosquitto — plaintext MQTT, no TLS"},
		{9001, "Mosquitto over WebSocket — plaintext"},
		{3000, "the web UI's nginx, bypassing Traefik and its headers"},
	} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p.port), 3*time.Second)
		if err != nil {
			continue // shut, which is the point
		}
		_ = conn.Close()

		// Something answered. On a shared runner that need not be us, and
		// failing on a stranger's port would be a test that cries wolf — so ask
		// Docker whether this stack is the one publishing it.
		if by := containerPublishing(t, p.port); by != "" {
			t.Errorf("%s publishes port %d to the host: %s. The cloud overlay is supposed to "+
				"remove it — only 80, 443 and 8883 belong on a public server", by, p.port, p.what)
			continue
		}
		t.Logf("port %d is open but no stack container publishes it — something else on this host", p.port)
	}
}

// containerPublishing returns the name of the openedge container publishing the
// given host port, or "" if none does.
func containerPublishing(t *testing.T, port int) string {
	t.Helper()
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Ports}}").Output()
	if err != nil {
		t.Logf("could not ask docker which container publishes %d: %v", port, err)
		return ""
	}
	needle := fmt.Sprintf(":%d->", port)
	for _, line := range strings.Split(string(out), "\n") {
		name, ports, ok := strings.Cut(line, "\t")
		if !ok || !strings.Contains(name, "openedge") {
			continue
		}
		if strings.Contains(ports, needle) {
			return name
		}
	}
	return ""
}

// /metrics is a full description of the deployment: request rates per route,
// tag counts, broker state. nginx proxies /api/, /oauth/, /.well-known/ and
// /health and nothing else, so /metrics falls through to the single-page app —
// and the day someone adds a location block "so Prometheus can scrape it", this
// says what that costs.
func TestMetricsAreNotPublic(t *testing.T) {
	requireProxiedDeployment(t)

	resp, err := httpClient(15 * time.Second).Get(apiBase() + "/metrics")
	if err != nil {
		t.Fatalf("GET %s/metrics: %v", apiBase(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	if resp.StatusCode == http.StatusOK && strings.Contains(body, "openedge_") {
		t.Fatalf("the Prometheus scrape is served on the public domain: %s", truncate([]byte(body)))
	}
}

// ── The broker, as an edge gateway reaches it ───────────────────────────────

// Port 8883 is a Traefik TCP router that terminates TLS and routes on SNI. A
// client that opens it without TLS is a gateway configured with mqtt:// instead
// of mqtts://, and it must not get a working plaintext session: that is the
// whole reason the plaintext 1883 was taken off the host.
func TestBrokerRefusesPlaintextOnTheTLSPort(t *testing.T) {
	requireProxiedDeployment(t)

	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", mqttHost(), mqttPort())).
		SetClientID("e2e-plaintext-" + uniqueSuffix()).
		SetConnectTimeout(5 * time.Second).
		SetCleanSession(true)
	if u := env("E2E_MQTT_USER", ""); u != "" {
		opts.SetUsername(u).SetPassword(env("E2E_MQTT_PASSWORD", ""))
	}

	c := paho.NewClient(opts)
	tok := c.Connect()
	if tok.WaitTimeout(10*time.Second) && tok.Error() == nil {
		c.Disconnect(100)
		t.Fatalf("a plaintext MQTT session was established on %s:%s — TLS is not being enforced "+
			"on the port edge devices connect to", mqttHost(), mqttPort())
	}
}

// And the other half: the same port, with TLS and the public hostname, is the
// one address an edge gateway has. Traefik routes it by SNI, so it works under
// the domain and under no other name — which is worth stating once, plainly,
// beside the negative case above.
func TestBrokerIsReachableOverTLSUnderTheDomain(t *testing.T) {
	requireProxiedDeployment(t)

	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("ssl://%s:%s", mqttHost(), mqttPort())).
		SetClientID("e2e-tls-" + uniqueSuffix()).
		SetConnectTimeout(10 * time.Second).
		SetCleanSession(true).
		SetTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
			// #nosec G402 -- opt-in via E2E_TLS_INSECURE; see insecureTLS.
			InsecureSkipVerify: insecureTLS(),
		})
	if u := env("E2E_MQTT_USER", ""); u != "" {
		opts.SetUsername(u).SetPassword(env("E2E_MQTT_PASSWORD", ""))
	}

	c := paho.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(20 * time.Second) {
		t.Fatalf("MQTT/TLS connect to %s:%s timed out", mqttHost(), mqttPort())
	}
	if err := tok.Error(); err != nil {
		t.Fatalf("MQTT/TLS connect to %s:%s: %v", mqttHost(), mqttPort(), err)
	}
	c.Disconnect(100)
}
