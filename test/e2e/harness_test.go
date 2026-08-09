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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
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
func login(t *testing.T, username, password string) (*apiClient, loginResponse) {
	t.Helper()
	anon := &apiClient{t: t}
	raw := anon.mustDo(http.MethodPost, "/api/auth/login",
		map[string]string{"username": username, "password": password}, http.StatusOK)

	var lr loginResponse
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("decode login response: %v — body: %s", err, truncate(raw))
	}
	if lr.Token == "" {
		t.Fatalf("login returned no token (mfa_required=%v, mfa_setup_required=%v)", lr.MFARequired, lr.MFASetupRequired)
	}
	return &apiClient{t: t, token: lr.Token}, lr
}

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
	client := &http.Client{Timeout: 3 * time.Second}
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
