package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
)

// A stand-in for the core API. It records what the MCP server sent it, which is
// how the tests check the property that matters most on this transport: the
// caller's own credential is what reaches the backend, not the server's.
type fakeCore struct {
	*httptest.Server
	lastAuth string
	lastOrg  string
	lastPath string
}

func newFakeCore(t *testing.T) *fakeCore {
	t.Helper()
	f := &fakeCore{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth = r.Header.Get("Authorization")
		f.lastOrg = r.Header.Get("X-Organization-ID")
		f.lastPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func newTestMCP(t *testing.T, core *fakeCore, authServer string) *mcpHTTPServer {
	t.Helper()
	return &mcpHTTPServer{
		base:           api.New(core.URL, "", 0),
		authServer:     authServer,
		publicURL:      "https://mcp.example.com",
		allowedOrigins: map[string]bool{"https://console.example.com": true},
	}
}

// post sends one JSON-RPC message and returns the raw recorder.
func post(t *testing.T, h *mcpHTTPServer, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.routes().ServeHTTP(rec, req)
	return rec
}

func withToken(tok string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + tok}
}

func decodeOne(t *testing.T, rec *httptest.ResponseRecorder) jsonRPCResponse {
	t.Helper()
	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %q)", err, rec.Body.String())
	}
	return resp
}

func TestMCPHTTPRequiresBearer(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without a token, got %d", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer ") {
		t.Fatalf("want a Bearer challenge so the client knows how to authenticate, got %q", got)
	}
}

// The challenge is what makes an OAuth client discover where to sign in. With
// no authorization server configured there is nowhere to point, and the header
// must not claim otherwise.
func TestMCPHTTPChallengePointsAtAuthServerOnlyWhenConfigured(t *testing.T) {
	bare := post(t, newTestMCP(t, newFakeCore(t), ""), `{"jsonrpc":"2.0","id":1,"method":"ping"}`, nil)
	if strings.Contains(bare.Header().Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatalf("no auth server configured, yet the challenge advertises discovery: %q",
			bare.Header().Get("WWW-Authenticate"))
	}

	withAS := post(t, newTestMCP(t, newFakeCore(t), "https://app.example.com"),
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`, nil)
	got := withAS.Header().Get("WWW-Authenticate")
	if !strings.Contains(got, `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`) {
		t.Fatalf("challenge does not point at the resource metadata: %q", got)
	}
}

func TestMCPHTTPInitializeNegotiatesProtocol(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	cases := []struct{ asked, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"2024-11-05", "2024-11-05"},
		// An unknown revision gets the newest one we speak, so the client can
		// decide for itself whether to continue.
		{"1999-01-01", "2025-06-18"},
	}
	for _, tc := range cases {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, tc.asked)
		rec := post(t, h, body, withToken("tok"))
		if rec.Code != http.StatusOK {
			t.Fatalf("initialize %s: status %d", tc.asked, rec.Code)
		}
		var out struct {
			Result mcpInitResult `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.Result.ProtocolVersion != tc.want {
			t.Errorf("client asked %s: want %s, got %s", tc.asked, tc.want, out.Result.ProtocolVersion)
		}
	}
}

func TestMCPHTTPToolsListIsNotEmpty(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, withToken("tok"))

	var out struct {
		Result mcpToolsListResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Result.Tools) < 30 {
		t.Fatalf("the HTTP transport exposes %d tools; it should expose the same set as stdio",
			len(out.Result.Tools))
	}
	seen := map[string]bool{}
	for _, tool := range out.Result.Tools {
		if seen[tool.Name] {
			t.Errorf("duplicate tool %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %q has no description, so a model cannot tell when to use it", tool.Name)
		}
	}
}

// The point of the transport: two callers, two identities, no shared state.
func TestMCPHTTPCallUsesTheCallersToken(t *testing.T) {
	core := newFakeCore(t)
	h := newTestMCP(t, core, "")

	call := `{"jsonrpc":"2.0","id":3,"method":"tools/call",
	          "params":{"name":"list_gateways","arguments":{}}}`

	post(t, h, call, withToken("alice-token"))
	if core.lastAuth != "Bearer alice-token" {
		t.Fatalf("core API saw %q, want alice's token", core.lastAuth)
	}

	post(t, h, call, withToken("bob-token"))
	if core.lastAuth != "Bearer bob-token" {
		t.Fatalf("second caller reused the first one's identity: %q", core.lastAuth)
	}
}

func TestMCPHTTPForwardsOrganizationHeader(t *testing.T) {
	core := newFakeCore(t)
	h := newTestMCP(t, core, "")

	post(t, h, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_gateways","arguments":{}}}`,
		map[string]string{"Authorization": "Bearer tok", "X-Organization-ID": "7"})

	if core.lastOrg != "7" {
		t.Fatalf("core API saw org %q, want 7", core.lastOrg)
	}
}

// A JSON-RPC notification carries no id and must not be answered. Returning a
// body here would be a protocol violation the client reports as an error.
func TestMCPHTTPNotificationGetsNoBody(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, withToken("tok"))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202 for a notification, got %d", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "" {
		t.Fatalf("a notification was answered with %q", body)
	}
}

func TestMCPHTTPBatchKeepsItsShape(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `[{"jsonrpc":"2.0","id":1,"method":"ping"},
	                    {"jsonrpc":"2.0","method":"notifications/initialized"},
	                    {"jsonrpc":"2.0","id":2,"method":"ping"}]`, withToken("tok"))

	var out []jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("a batch must be answered with an array: %v (%q)", err, rec.Body.String())
	}
	// Two requests and one notification: the notification contributes nothing.
	if len(out) != 2 {
		t.Fatalf("want 2 replies for 2 requests and 1 notification, got %d", len(out))
	}
}

func TestMCPHTTPSingleMessageIsNotWrappedInAnArray(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `{"jsonrpc":"2.0","id":9,"method":"ping"}`, withToken("tok"))

	if strings.HasPrefix(strings.TrimSpace(rec.Body.String()), "[") {
		t.Fatalf("a single message came back as a batch: %q", rec.Body.String())
	}
	if resp := decodeOne(t, rec); fmt.Sprint(resp.ID) != "9" {
		t.Fatalf("reply id %v does not match the request", resp.ID)
	}
}

// A page in the user's browser must not be able to drive this server using
// credentials the browser already holds.
func TestMCPHTTPRejectsUnknownOrigin(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		map[string]string{"Authorization": "Bearer tok", "Origin": "https://evil.example.com"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for an unlisted Origin, got %d", rec.Code)
	}

	rec = post(t, h, `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		map[string]string{"Authorization": "Bearer tok", "Origin": "https://console.example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("an allowed Origin was rejected: %d", rec.Code)
	}
}

// Non-browser clients send no Origin at all; refusing them would break the CLI
// and every connector calling from a backend.
func TestMCPHTTPAllowsAbsentOrigin(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, withToken("tok"))

	if rec.Code != http.StatusOK {
		t.Fatalf("a request without an Origin was rejected: %d", rec.Code)
	}
}

func TestMCPHTTPMethodsOtherThanPost(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	for method, want := range map[string]int{
		http.MethodGet:    http.StatusMethodNotAllowed,
		http.MethodDelete: http.StatusNoContent,
		http.MethodPut:    http.StatusMethodNotAllowed,
	} {
		rec := httptest.NewRecorder()
		h.routes().ServeHTTP(rec, httptest.NewRequest(method, "/mcp", nil))
		if rec.Code != want {
			t.Errorf("%s /mcp: want %d, got %d", method, want, rec.Code)
		}
	}
}

func TestMCPHTTPResourceMetadata(t *testing.T) {
	// Without an authorization server there is nothing truthful to publish.
	rec := httptest.NewRecorder()
	newTestMCP(t, newFakeCore(t), "").routes().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 when no auth server is configured, got %d", rec.Code)
	}

	h := newTestMCP(t, newFakeCore(t), "https://app.example.com")
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		rec := httptest.NewRecorder()
		h.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		var meta struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			ScopesSupported      []string `json:"scopes_supported"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		if meta.Resource != "https://mcp.example.com/mcp" {
			t.Errorf("%s: resource is %q", path, meta.Resource)
		}
		if len(meta.AuthorizationServers) != 1 || meta.AuthorizationServers[0] != "https://app.example.com" {
			t.Errorf("%s: authorization_servers is %v", path, meta.AuthorizationServers)
		}
		if len(meta.ScopesSupported) == 0 {
			t.Errorf("%s: no scopes advertised", path)
		}
	}
}

func TestMCPHTTPRejectsOversizeBody(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` +
		strings.Repeat("x", maxMCPRequestBytes+1) + `"}}`
	rec := post(t, h, big, withToken("tok"))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413 for an oversize body, got %d", rec.Code)
	}
}

func TestMCPHTTPMalformedJSON(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h, `{"jsonrpc":"2.0",`, withToken("tok"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for malformed JSON, got %d", rec.Code)
	}
	if resp := decodeOne(t, rec); resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("want a JSON-RPC parse error (-32700), got %+v", resp.Error)
	}
}

func TestMCPHTTPUnknownToolIsAnErrorNotACrash(t *testing.T) {
	h := newTestMCP(t, newFakeCore(t), "")

	rec := post(t, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		withToken("tok"))

	if rec.Code != http.StatusOK {
		t.Fatalf("a tool error is reported inside the response, not as a status: got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown tool") {
		t.Fatalf("expected the reply to name the problem, got %q", rec.Body.String())
	}
}
