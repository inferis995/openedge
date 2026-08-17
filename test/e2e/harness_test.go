//go:build e2e

// Package e2e holds acceptance tests that run against a REAL, assembled stack
// (docker compose up), not against mocks.
//
// Why this package exists: the unit suite is large and green, yet the worst
// defects found in this codebase were all of a kind unit tests structurally
// cannot catch — a feature that is fully implemented on both sides but wired to
// nothing. Alarms fired and were persisted, notifications were implemented and
// configurable, and no notification was ever sent because nobody published to
// the topic joining them. Every test here therefore exercises a path END TO END
// across process boundaries, and each one maps to a defect that actually shipped.
//
// Run with:
//
//	docker compose up -d && go test -tags=e2e ./test/e2e/...
//
// The stack address is configurable so the same suite runs in CI and locally.
package e2e

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

// ── Configuration ───────────────────────────────────────────────────────────

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func apiBase() string  { return env("E2E_API_URL", "http://127.0.0.1:8081") }
func mqttHost() string { return env("E2E_MQTT_HOST", "127.0.0.1") }
func mqttPort() string { return env("E2E_MQTT_PORT", "18830") }

// ── The shape of the deployment under test ──────────────────────────────────
//
// The suite runs against two stacks that are the same application and a
// different deployment. The on-prem one (docker-compose.yml alone) publishes
// every service on the host. The cloud one (+ docker-compose.vps.yml) publishes
// nothing but Traefik, and everything reaches the application through the proxy
// and the web-ui nginx behind it — which is how every customer will reach it,
// and which nothing exercised until there was a job for it.

// proxiedDeployment reports whether the cloud overlay is in front of the stack.
func proxiedDeployment() bool { return env("E2E_DEPLOYMENT", "direct") == "vps" }

// requireDirectAccess skips a test that talks to a service the cloud overlay
// deliberately does not publish — Postgres, or core-api's own /metrics.
//
// This is not coverage quietly dropped. Those tests assert properties of the
// database and of core-api, and the direct job proves them against the very
// same images on every push. What the overlay changes is what is REACHABLE,
// and that is asserted head-on in deployment_vps_test.go — including that the
// ports these tests would use are in fact closed.
func requireDirectAccess(t *testing.T, what string) {
	t.Helper()
	if proxiedDeployment() {
		t.Skipf("%s is not published by the cloud overlay — the direct job covers this test, "+
			"and TestInternalPortsAreNotPublished asserts the port is shut", what)
	}
}

// insecureTLS reports whether to accept the certificate the stack presents
// without checking it.
//
// Deliberately a separate switch from E2E_DEPLOYMENT rather than implied by it.
// In CI there is no public domain and no public CA, so Traefik's ACME resolver
// cannot issue anything and serves its own default certificate; refusing it
// would mean the suite never reaches the code under test. Against a real
// deployment the certificate is part of what should be checked, and folding
// this into "is it proxied" would silently stop checking it there.
func insecureTLS() bool { return os.Getenv("E2E_TLS_INSECURE") == "1" }

func httpClient(timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	if insecureTLS() {
		c.Transport = &http.Transport{
			// #nosec G402 -- see insecureTLS: opt-in, for a stack with no public CA.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return c
}

// adminCredentials returns the bootstrap admin login. The password must match
// OPENEDGE_INITIAL_ADMIN_PASSWORD in the compose environment used for the run.
func adminCredentials() (string, string) {
	return env("E2E_ADMIN_USER", "admin"), env("E2E_ADMIN_PASSWORD", "admin123")
}

// ── HTTP helpers ────────────────────────────────────────────────────────────

type apiClient struct {
	t     *testing.T
	token string
	orgID string // sent as X-Organization-ID when non-empty
}

// do issues a request and returns status and raw body. It never fails the test
// itself: callers assert, so a test can legitimately expect a 401 or 403.
func (c *apiClient) do(method, path string, body interface{}) (int, []byte) {
	c.t.Helper()

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal request body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, apiBase()+path, rdr)
	if err != nil {
		c.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.orgID != "" {
		req.Header.Set("X-Organization-ID", c.orgID)
	}

	// The whole suite arrives from one address, and every request counts
	// against GlobalRateLimit — 300 a minute, burst 50, per IP. A suite that
	// creates a fresh organization per test crosses it in bursts, and the 429
	// that comes back has nothing to do with the code under test: the failures
	// read "POST /api/organizations: status 429, want 201" across a dozen
	// unrelated tests at once.
	//
	// Back off rather than loosen the limiter. No test here asserts a 429 from
	// the API, so waiting is always the right move; the OAuth tests do the same
	// for the login limiter, which they hit for the same reason.
	client := httpClient(15 * time.Second)
	deadline := time.Now().Add(45 * time.Second)
	var resp *http.Response
	for {
		var err error
		resp, err = client.Do(req)
		if err != nil {
			c.t.Fatalf("%s %s: %v", method, path, err)
		}
		if resp.StatusCode != http.StatusTooManyRequests || time.Now().After(deadline) {
			break
		}
		_ = resp.Body.Close()
		time.Sleep(2 * time.Second)
		// A request body is consumed by the attempt that sent it.
		if rdr != nil {
			b, _ := json.Marshal(body)
			req.Body = io.NopCloser(bytes.NewReader(b))
		}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read response of %s %s: %v", method, path, err)
	}
	return resp.StatusCode, raw
}

// mustDo asserts an expected status and returns the body.
func (c *apiClient) mustDo(method, path string, body interface{}, wantStatus int) []byte {
	c.t.Helper()
	status, raw := c.do(method, path, body)
	if status != wantStatus {
		c.t.Fatalf("%s %s: status %d, want %d — body: %s", method, path, status, wantStatus, truncate(raw))
	}
	return raw
}

func truncate(b []byte) string {
	const max = 400
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}

// ── Login ───────────────────────────────────────────────────────────────────

type loginResponse struct {
	Token            string `json:"token"`
	MFARequired      bool   `json:"mfa_required"`
	MFAToken         string `json:"mfa_token"`
	MFASetupRequired bool   `json:"mfa_setup_required"`
	User             struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
		OrgID    *int   `json:"org_id"`
	} `json:"user"`
}

// login authenticates and returns a client carrying the session token.
//
// /api/auth/login is rate limited per IP (burst 5, then one every 6 s) and the
// whole suite arrives from a single address, so a 429 here says nothing about
// the code under test. Back off and retry rather than weakening the limiter:
// throttling credential stuffing is exactly what it is for.
func login(t *testing.T, username, password string) (*apiClient, loginResponse) {
	t.Helper()
	anon := &apiClient{t: t}
	body := map[string]string{"username": username, "password": password}

	var status int
	var raw []byte
	deadline := time.Now().Add(60 * time.Second)
	for {
		status, raw = anon.do(http.MethodPost, "/api/auth/login", body)
		if status != http.StatusTooManyRequests || time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if status != http.StatusOK {
		t.Fatalf("login as %q: status %d — body: %s", username, status, truncate(raw))
	}

	var lr loginResponse
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("decode login response: %v — body: %s", err, truncate(raw))
	}
	if lr.Token == "" {
		t.Fatalf("login returned no token (mfa_required=%v, mfa_setup_required=%v)", lr.MFARequired, lr.MFASetupRequired)
	}
	return &apiClient{t: t, token: lr.Token}, lr
}

// adminSession returns the bootstrap admin's session, logging in at most once
// for the whole suite. Every test needs it, and re-authenticating per test
// spent the rate-limit budget on work a real client would do with one token.
func adminSession(t *testing.T) (*apiClient, loginResponse) {
	t.Helper()
	adminOnce.Do(func() {
		user, pass := adminCredentials()
		c, lr := login(t, user, pass)
		adminClient, adminLogin = c, lr
	})
	if adminClient == nil {
		t.Fatal("the bootstrap admin login failed earlier in this run")
	}
	// The cached client carries the token, not the *testing.T of whichever test
	// happened to log in first — failures must be reported against the caller.
	return &apiClient{t: t, token: adminClient.token}, adminLogin
}

var (
	adminOnce   sync.Once
	adminClient *apiClient
	adminLogin  loginResponse
)

// ── Readiness ───────────────────────────────────────────────────────────────

// TestMain waits for the API to answer before running anything, so a slow
// container start reports as "stack not ready" rather than as N failed tests.
func TestMain(m *testing.M) {
	if err := waitForAPI(90 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: stack not ready: %v\n", err)
		fmt.Fprintf(os.Stderr, "e2e: start it with `docker compose up -d` (API expected at %s)\n", apiBase())
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func waitForAPI(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := httpClient(3 * time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		resp, err := client.Get(apiBase() + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("health returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("API did not become ready within %s: %w", timeout, lastErr)
}
