package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
)

// The MCP server over Streamable HTTP.
//
// The stdio transport runs as a local process, so it borrows the identity of
// whoever started it: one token, set once, in the environment. Over HTTP the
// server is shared, and the identity has to arrive with each request instead —
// every call builds its own API client from the caller's bearer token, so the
// multi-tenancy the core API already enforces keeps working unchanged.
//
// Nothing here re-implements a tool. The protocol handling in mcp.go is the
// same code; this file only changes how the messages arrive.

const (
	// A JSON-RPC message large enough for a synoptic layout, small enough that
	// an unauthenticated caller cannot make us allocate at will.
	maxMCPRequestBytes = 4 << 20

	// The core API client allows 30s per call; a tool that fans out to several
	// of them still has to finish inside the write deadline.
	mcpWriteTimeout = 2 * time.Minute

	// The two scopes an OAuth token can carry. They are coarse on purpose: the
	// core API already decides what a user may touch, per organization and per
	// permission, and a second, finer-grained scheme here would only be able to
	// disagree with it.
	mcpScopeRead  = "openedge:read"
	mcpScopeWrite = "openedge:write"
)

var (
	flagMCPHTTP          string
	flagMCPAuthServer    string
	flagMCPPublicURL     string
	flagMCPAllowedOrigin []string
)

func init() {
	mcpCmd.Flags().StringVar(&flagMCPHTTP, "http", "",
		"Serve MCP over Streamable HTTP on this address instead of stdio (e.g. 127.0.0.1:9090)")
	mcpCmd.Flags().StringVar(&flagMCPAuthServer, "auth-server", "",
		"OAuth authorization server that issues tokens for this MCP server "+
			"(enables RFC 9728 discovery; omit to accept a static bearer token)")
	mcpCmd.Flags().StringVar(&flagMCPPublicURL, "public-url", "",
		"Public base URL this server is reached at, used in OAuth metadata (e.g. https://mcp.example.com)")
	mcpCmd.Flags().StringSliceVar(&flagMCPAllowedOrigin, "allow-origin", nil,
		"Browser Origin allowed to call this server; repeatable. Any Origin is rejected by default.")
}

// mcpHTTPServer holds what is shared between requests. The per-request part —
// the caller's identity — deliberately is not here.
type mcpHTTPServer struct {
	base           *api.Client
	authServer     string
	publicURL      string
	allowedOrigins map[string]bool
}

func runMCPOverHTTP(addr string) {
	origins := make(map[string]bool, len(flagMCPAllowedOrigin))
	for _, o := range flagMCPAllowedOrigin {
		origins[strings.TrimRight(o, "/")] = true
	}

	h := &mcpHTTPServer{
		base:           getMCPClient(),
		authServer:     strings.TrimRight(flagMCPAuthServer, "/"),
		publicURL:      strings.TrimRight(flagMCPPublicURL, "/"),
		allowedOrigins: origins,
	}
	if h.publicURL == "" {
		h.publicURL = "http://" + addr
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           h.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      mcpWriteTimeout,
		IdleTimeout:       2 * time.Minute,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("MCP over HTTP on %s (endpoint POST /mcp)", addr)
		if h.authServer == "" {
			log.Printf("no --auth-server: callers must present a static OpenEdge token as a bearer")
		} else {
			log.Printf("OAuth discovery points at %s", h.authServer)
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("listen: %v", err)
			stop <- syscall.SIGTERM
		}
	}()

	<-stop
	log.Println("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func (h *mcpHTTPServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", h.handleMCP)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// RFC 9728 defines both the bare path and the form with the resource path
	// appended; clients probe one or the other, so answer on both.
	mux.HandleFunc("/.well-known/oauth-protected-resource", h.handleResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", h.handleResourceMetadata)
	return mux
}

// handleResourceMetadata tells a client which authorization server issues
// tokens for this resource. Without one configured there is nothing truthful to
// publish, so the endpoint reports that it does not exist and the client falls
// back to the static credential it was given.
func (h *mcpHTTPServer) handleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if h.authServer == "" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"resource":                 h.publicURL + "/mcp",
		"authorization_servers":    []string{h.authServer},
		"scopes_supported":         []string{mcpScopeRead, mcpScopeWrite},
		"bearer_methods_supported": []string{"header"},
	})
}

func (h *mcpHTTPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// handled below
	case http.MethodGet:
		// A server-initiated stream. This server only answers what it is asked,
		// so saying so is more useful than holding an idle connection open.
		http.Error(w, "this server does not open server-initiated streams", http.StatusMethodNotAllowed)
		return
	case http.MethodDelete:
		// Sessions are not kept between requests, so there is nothing to end.
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.originAllowed(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	token := bearerToken(r)
	if token == "" {
		h.unauthorized(w, "a bearer token is required")
		return
	}

	body, err := readLimited(r, maxMCPRequestBytes)
	if err != nil {
		writeRPCError(w, http.StatusRequestEntityTooLarge, nil, -32600, err.Error())
		return
	}

	// The caller's identity, not the process's.
	srv := &mcpServer{
		client:      h.base.WithToken(token, orgFromRequest(r)),
		initialized: true,
	}

	batch, isBatch, err := decodeRPC(body)
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, -32700, "parse error: "+err.Error())
		return
	}

	responses := make([]*jsonRPCResponse, 0, len(batch))
	for _, req := range batch {
		resp := srv.handle(req)
		// A JSON-RPC notification has no id, and MUST NOT be answered. The
		// stdio path replies anyway to keep naive clients unblocked; here the
		// status code carries the acknowledgement instead.
		if req.ID == nil {
			continue
		}
		responses = append(responses, resp)
	}

	// An access token expires — after an hour, on the OAuth path. Reporting
	// that as a tool error would leave the client retrying a call that can
	// never succeed; reporting it as a 401 with the challenge is what makes it
	// refresh and carry on.
	if srv.upstreamUnauthorized {
		h.unauthorized(w, "the token was refused by the OpenEdge API")
		return
	}

	if len(responses) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if isBatch {
		writeJSON(w, http.StatusOK, responses)
		return
	}
	writeJSON(w, http.StatusOK, responses[0])
}

// unauthorized answers a request that carried no usable credential. The
// WWW-Authenticate header is what turns a refusal into a sign-in: a client that
// understands RFC 9728 follows resource_metadata to find out where to
// authenticate, instead of simply reporting a failure to the user.
func (h *mcpHTTPServer) unauthorized(w http.ResponseWriter, detail string) {
	challenge := `Bearer realm="OpenEdge"`
	if h.authServer != "" {
		challenge += fmt.Sprintf(`, resource_metadata="%s/.well-known/oauth-protected-resource"`, h.publicURL)
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": detail})
}

// originAllowed guards against a web page in the user's browser driving this
// server through the credentials the browser already holds. Non-browser
// clients — the CLI, a connector calling from a backend — send no Origin at
// all, so the default of "no Origin allowed" costs them nothing.
func (h *mcpHTTPServer) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return h.allowedOrigins[strings.TrimRight(origin, "/")]
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

// orgFromRequest reads the optional organization header. The core API decides
// what the token is allowed to see regardless; this only lets a global admin
// say which tenant they mean.
func orgFromRequest(r *http.Request) int {
	n, err := strconv.Atoi(r.Header.Get("X-Organization-ID"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func readLimited(r *http.Request, limit int64) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	data, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, limit))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, fmt.Errorf("request body exceeds %d bytes", limit)
		}
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

// decodeRPC accepts either a single JSON-RPC message or a batch, and reports
// which one it was so the reply keeps the same shape.
func decodeRPC(body []byte) (msgs []*jsonRPCRequest, isBatch bool, err error) {
	trimmed := strings.TrimLeft(string(body), " \t\r\n")
	if trimmed == "" {
		return nil, false, errors.New("empty body")
	}
	if trimmed[0] == '[' {
		var batch []*jsonRPCRequest
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, true, err
		}
		if len(batch) == 0 {
			return nil, true, errors.New("empty batch")
		}
		return batch, true, nil
	}
	var single jsonRPCRequest
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, false, err
	}
	return []*jsonRPCRequest{&single}, false, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeRPCError(w http.ResponseWriter, status int, id interface{}, code int, message string) {
	writeJSON(w, status, &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}
