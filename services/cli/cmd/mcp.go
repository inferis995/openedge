package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
	"github.com/ralph/industrial-edge-middleware/services/cli/internal/config"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server for AI agent integration (stdio, or Streamable HTTP with --http)",
	Long: `Start an MCP (Model Context Protocol) server.

Two transports, same tools.

STDIO (default) — a local process, one identity, taken from the environment.
The server reads JSON-RPC 2.0 messages from stdin (Content-Length framed) and
writes responses to stdout. All logging goes to stderr.

  {
    "mcpServers": {
      "openedge": {
        "command": "openedge",
        "args": ["mcp"],
        "env": {
          "OPENEDGE_URL": "https://app.yourdomain.com",
          "OPENEDGE_TOKEN": "eyJ...",
          "OPENEDGE_ORG_ID": "1"
        }
      }
    }
  }

HTTP (--http) — a shared server, one identity per request, taken from the
caller's bearer token. This is the transport a remote MCP client needs.

  openedge mcp --http 127.0.0.1:9090 --url https://app.yourdomain.com

  claude mcp add --transport http openedge http://127.0.0.1:9090/mcp \
    --header "Authorization: Bearer eyJ..."

Add --auth-server to advertise an OAuth authorization server instead, so
clients sign in rather than carrying a token you pasted for them.`,
	Run: runMCPServer,
}

// ---- JSON-RPC 2.0 types -------------------------------------------------------

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---- MCP protocol types -------------------------------------------------------

type mcpInitResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    mcpCapabilities `json:"capabilities"`
	ServerInfo      mcpServerInfo   `json:"serverInfo"`
}

type mcpCapabilities struct {
	Tools   map[string]interface{} `json:"tools"`
	Prompts map[string]interface{} `json:"prompts,omitempty"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpTool struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	InputSchema interface{}         `json:"inputSchema"`
	Annotations *mcpToolAnnotations `json:"annotations,omitempty"`
}

// mcpToolAnnotations follows the MCP tool-annotations schema: hints a client
// uses to decide whether a call needs confirmation. Everything here touches a
// live plant through the API, so openWorldHint is always true.
type mcpToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// annotateTool derives hints from the tool verb. Reads are safe to run freely;
// deletes, writes to the plant, restarts and downlinks change the world and
// must be confirmed. Creates/updates are world-changing but not destructive.
func annotateTool(name string) *mcpToolAnnotations {
	t := true
	a := &mcpToolAnnotations{OpenWorldHint: &t}
	verb := name
	if j := strings.Index(name, "_"); j > 0 {
		verb = name[:j]
	}
	switch verb {
	case "list", "get", "check", "detect":
		a.ReadOnlyHint = &t
		a.IdempotentHint = &t
	case "delete", "write", "fleet", "send":
		f := false
		a.DestructiveHint = &t
		a.IdempotentHint = &f
	}
	return a
}

type mcpToolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpCallResult struct {
	Content []mcpContent `json:"content"`
}

// ---- Server -------------------------------------------------------------------

type mcpServer struct {
	client      *api.Client
	reader      *bufio.Reader
	writer      *bufio.Writer
	initialized bool

	// Set when the core API refused the caller's token. Over stdio this is
	// nothing but a failed call; over HTTP it is the one failure the client can
	// act on, by refreshing its token, so the transport turns it into a 401.
	upstreamUnauthorized bool
}

func runMCPServer(cmd *cobra.Command, args []string) {
	// MCP server: ALL logging to stderr, stdout is for protocol only
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[openedge-mcp] ")

	if flagMCPHTTP != "" {
		runMCPOverHTTP(flagMCPHTTP)
		return
	}

	client, _ := getMCPClient()

	srv := &mcpServer{
		client: client,
		reader: bufio.NewReader(os.Stdin),
		writer: bufio.NewWriter(os.Stdout),
	}

	log.Println("OpenEdge MCP server started, waiting for messages...")
	srv.serve()
}

// getMCPClient builds the API client for the MCP server, preferring env vars.
// It also returns the configured organization, which the HTTP transport needs
// separately: there it is only a default, applied when the caller names none.
func getMCPClient() (*api.Client, int) {
	cfg, _ := config.Load()

	if v := os.Getenv("OPENEDGE_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("OPENEDGE_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("OPENEDGE_ORG_ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.OrgID = n
		}
	}

	// Allow CLI flags too
	if flagURL != "" {
		cfg.URL = flagURL
	}
	if flagToken != "" {
		cfg.Token = flagToken
	}
	if flagOrg != 0 {
		cfg.OrgID = flagOrg
	}

	if cfg.URL == "" {
		log.Println("Warning: OPENEDGE_URL not set. Tool calls will fail.")
	}

	return api.New(cfg.URL, cfg.Token, cfg.OrgID), cfg.OrgID
}

func (s *mcpServer) serve() {
	for {
		req, err := s.readMessage()
		if err != nil {
			if err == io.EOF {
				log.Println("stdin closed, shutting down")
				return
			}
			log.Printf("read error: %v", err)
			return
		}

		resp := s.handle(req)
		if err := s.writeMessage(resp); err != nil {
			log.Printf("write error: %v", err)
			return
		}
	}
}

// readMessage reads a Content-Length framed JSON-RPC message.
// Falls back to bare newline-delimited JSON if no Content-Length header.
func (s *mcpServer) readMessage() (*jsonRPCRequest, error) {
	// Try to read a line to peek at whether it's Content-Length framed or bare JSON
	line, err := s.reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)

	var body []byte

	if strings.HasPrefix(line, "Content-Length:") {
		// Parse Content-Length
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid Content-Length header: %q", line)
		}
		length, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid Content-Length value: %w", err)
		}

		// Read remaining headers until blank line
		for {
			hdr, err := s.reader.ReadString('\n')
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(hdr) == "" {
				break
			}
		}

		// Read exactly length bytes
		body = make([]byte, length)
		if _, err := io.ReadFull(s.reader, body); err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}
	} else if strings.HasPrefix(line, "{") {
		// Bare JSON (newline delimited)
		body = []byte(line)
	} else if line == "" {
		// Empty line, try again recursively
		return s.readMessage()
	} else {
		return nil, fmt.Errorf("unexpected input: %q", line)
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse JSON-RPC: %w", err)
	}
	return &req, nil
}

// writeMessage writes a Content-Length framed JSON-RPC response.
func (s *mcpServer) writeMessage(resp *jsonRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := s.writer.WriteString(header); err != nil {
		return err
	}
	if _, err := s.writer.Write(data); err != nil {
		return err
	}
	return s.writer.Flush()
}

func (s *mcpServer) errResponse(id interface{}, code int, message string) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
}

func (s *mcpServer) okResponse(id interface{}, result interface{}) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

func (s *mcpServer) handle(req *jsonRPCRequest) *jsonRPCResponse {
	log.Printf("→ method=%s id=%v", req.Method, req.ID)

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized", "notifications/initialized":
		// A notification carries no id and deserves no reply. Over stdio we
		// answer anyway, because a client that framed it as a request would
		// otherwise block; the HTTP transport drops the reply itself, where
		// the rule is enforced by the status code.
		s.initialized = true
		return s.okResponse(req.ID, map[string]interface{}{})
	// The MCP spec spells this method with two l's; it is a wire constant, not
	// our prose, so the linter's preference does not apply.
	case "notifications/cancelled": //nolint:misspell
		// Tool calls run to completion here, so there is nothing to cancel.
		return s.okResponse(req.ID, map[string]interface{}{})
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptsGet(req)
	case "ping":
		return s.okResponse(req.ID, map[string]interface{}{})
	default:
		log.Printf("unknown method: %s", req.Method)
		return s.errResponse(req.ID, -32601, "method not found: "+req.Method)
	}
}

// supportedProtocolVersions lists the MCP revisions this server speaks, newest
// first. The HTTP transport was introduced in 2025-03-26, so a server that only
// ever answered "2024-11-05" would tell a modern client to expect stdio.
// supportedProtocolVersions lists the MCP revisions this server speaks, newest
// first. 2026-07-28 is stateless (no initialize handshake, no sessions) and
// supports full JSON Schema 2020-12 in tool schemas; this server never gated
// calls on the handshake, so it already behaves statelessly.
var supportedProtocolVersions = []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// negotiateProtocol echoes the client's requested revision when we speak it,
// and otherwise names the newest one we do — which is what the client needs in
// order to decide whether it can continue.
func negotiateProtocol(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	for _, v := range supportedProtocolVersions {
		if p.ProtocolVersion == v {
			return v
		}
	}
	return supportedProtocolVersions[0]
}

func (s *mcpServer) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	s.initialized = true
	return s.okResponse(req.ID, mcpInitResult{
		ProtocolVersion: negotiateProtocol(req.Params),
		Capabilities: mcpCapabilities{
			Tools:   map[string]interface{}{},
			Prompts: map[string]interface{}{},
		},
		ServerInfo: mcpServerInfo{
			Name:    "openedge",
			Version: "1.1.0",
		},
	})
}

func (s *mcpServer) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	tools := []mcpTool{
		{
			Name:        "list_organizations",
			Description: "List all organizations in the OpenEdge platform",
			InputSchema: jsonSchema(map[string]interface{}{}),
		},
		{
			Name:        "list_gateways",
			Description: "List all gateways, optionally filtered by organization",
			InputSchema: jsonSchema(map[string]interface{}{
				"org_id": propInt("Filter by organization ID (optional)"),
			}),
		},
		{
			Name:        "list_tags",
			Description: "List all tags, optionally filtered by gateway",
			InputSchema: jsonSchema(map[string]interface{}{
				"gateway_id": propInt("Filter by gateway ID (optional)"),
			}),
		},
		{
			Name:        "get_tag_value",
			Description: "Get the current value, quality, and timestamp for a tag",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id": propInt("The tag ID"),
			}, []string{"tag_id"}),
		},
		{
			Name:        "write_tag_value",
			Description: "Write a value to a tag",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id": propInt("The tag ID"),
				"value":  propAny("The value to write (number, boolean, or string)"),
			}, []string{"tag_id", "value"}),
		},
		{
			Name:        "get_tag_history",
			Description: "Get historical values for a tag",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id": propInt("The tag ID"),
				"from":   propStr("Start time in RFC3339 format (optional)"),
				"to":     propStr("End time in RFC3339 format (optional)"),
				"limit":  propInt("Max number of results (default 100)"),
			}, []string{"tag_id"}),
		},
		{
			Name:        "get_tag_shadows",
			Description: "Get tag shadows (latest known values, live or historic)",
			InputSchema: jsonSchema(map[string]interface{}{
				"gateway_id": propInt("Filter by gateway ID (optional)"),
			}),
		},
		{
			Name:        "list_active_alarms",
			Description: "List active alarms, optionally filtered by severity",
			InputSchema: jsonSchema(map[string]interface{}{
				"severity": propStr("Filter by severity: critical, warning, info (optional)"),
			}),
		},
		{
			Name:        "acknowledge_alarm",
			Description: "Acknowledge an alarm by ID",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"alarm_id": propInt("The alarm ID to acknowledge"),
			}, []string{"alarm_id"}),
		},
		{
			Name:        "get_fleet_status",
			Description: "Get the status of all edge deployments in the fleet",
			InputSchema: jsonSchema(map[string]interface{}{}),
		},
		{
			Name:        "fleet_restart",
			Description: "Restart the edge software for an organization",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"org_id": propInt("The organization ID"),
			}, []string{"org_id"}),
		},
		{
			Name:        "list_lorawan_devices",
			Description: "List LoRaWAN devices for a gateway",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"gateway_id": propInt("The gateway ID"),
			}, []string{"gateway_id"}),
		},
		{
			Name:        "import_lorawan_tags",
			Description: "Import LoRaWAN devices as tags for a gateway",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"gateway_id": propInt("The gateway ID"),
				"devices":    propAny("Array of device objects to import"),
			}, []string{"gateway_id", "devices"}),
		},
		{
			Name:        "send_lorawan_downlink",
			Description: "Send a downlink message to a LoRaWAN device",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"gateway_id":  propInt("The gateway ID"),
				"device_id":   propStr("The device ID"),
				"f_port":      propInt("LoRaWAN fPort (1-223)"),
				"payload_hex": propStr("Payload as hex string"),
				"confirmed":   propBool("Use confirmed downlink (optional, default false)"),
			}, []string{"gateway_id", "device_id", "f_port", "payload_hex"}),
		},
		{
			Name:        "get_aiops_summary",
			Description: "Get AI-powered operations summary",
			InputSchema: jsonSchema(map[string]interface{}{
				"hours": propInt("Number of hours to include (default 24)"),
			}),
		},
		{
			Name:        "detect_anomalies",
			Description: "Detect anomalies for a specific tag",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id":       propInt("The tag ID to analyze"),
				"window_hours": propInt("Analysis window in hours (optional, default 24)"),
			}, []string{"tag_id"}),
		},
		{
			Name:        "get_alarm_digest",
			Description: "Get alarm digest grouped by type and severity",
			InputSchema: jsonSchema(map[string]interface{}{
				"hours": propInt("Number of hours to include (default 24)"),
			}),
		},
		{
			Name:        "check_health",
			Description: "Check the health and readiness of the OpenEdge API",
			InputSchema: jsonSchema(map[string]interface{}{}),
		},

		// ── Forecasts ───────────────────────────────────────────────────────
		//
		// Where tags are going. A forecast is shown only when it beat
		// repeating the last value on the known history (status "ok");
		// "unreliable" ones exist but must not be presented as predictions.

		{
			Name: "get_forecast_overview",
			Description: "Every forecastable tag of the current organization with its forecast status " +
				"(ok, unreliable, learning, disabled, error, pending), signal kind, skill against a naive " +
				"forecast, and the value expected at the end of the horizon with its P10–P90 band",
			InputSchema: jsonSchema(map[string]interface{}{
				"status": propStr("Only this status (optional): ok, unreliable, learning, disabled, error, pending"),
				"site":   propStr("Only tags of this site, by name (optional)"),
			}),
		},
		{
			Name:        "get_tag_forecast",
			Description: "One tag's latest forecast: status, kind, engine and model, skill, coverage, and every step with P10/P50/P90",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id": propInt("The tag ID"),
			}, []string{"tag_id"}),
		},
		{
			Name:        "refresh_tag_forecast",
			Description: "Forecast a tag now instead of waiting for its next scheduled run (admin), and return it",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id": propInt("The tag ID"),
			}, []string{"tag_id"}),
		},
		{
			Name: "get_forecast_settings",
			Description: "Forecast settings of one level of the hierarchy: its own profile, what it inherits " +
				"from the levels above, and the effective result",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"scope":    propStr("org, site, area, gateway or tag"),
				"scope_id": propInt("The ID at that level (the organization's own ID for org)"),
			}, []string{"scope", "scope_id"}),
		},
		{
			Name: "set_forecast_settings",
			Description: "Change the forecast settings of one level (admin). Fields not given keep their value; " +
				"\"inherit\" (or horizon_hours 0) puts a field back to inheriting; clear removes the level's " +
				"profile. The levels below inherit unless they set their own.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"scope":         propStr("org, site, area, gateway or tag"),
				"scope_id":      propInt("The ID at that level"),
				"enabled":       propStr("on, off or inherit"),
				"kind":          propStr("auto, process, cyclic, degradation, counter, state, or inherit"),
				"horizon_hours": propInt("How far ahead, in hours (1–720; 0 = inherit)"),
				"context_days":  propInt("How much history to learn from, in days (1–365; 0 = inherit)"),
				"clear":         propBool("Remove the level's profile: inherit everything"),
			}, []string{"scope", "scope_id"}),
		},

		// ── Provisioning ────────────────────────────────────────────────────
		//
		// Everything below builds a plant rather than reading one. The read
		// tools above answer "what is happening"; these answer "make it exist",
		// which is the half an agent could not reach before.

		{
			Name:        "list_sites",
			Description: "List sites in the current organization",
			InputSchema: jsonSchema(map[string]interface{}{}),
		},
		{
			Name:        "create_site",
			Description: "Create a site (the top level of the plant hierarchy: site -> area -> gateway -> tag)",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"name":   propStr("Site name"),
				"org_id": propInt("Organization ID"),
			}, []string{"name", "org_id"}),
		},
		{
			Name:        "list_areas",
			Description: "List areas, optionally filtered by site",
			InputSchema: jsonSchema(map[string]interface{}{
				"site_id": propInt("Filter by site ID (optional)"),
			}),
		},
		{
			Name: "import_html_widget",
			Description: "Create a synoptic page showing one imported HTML widget full-canvas. " +
				"The page declares custom properties with <meta name=\"oe-props\" content=\"a, b, ts\"> " +
				"(or cwidget.on('...') calls); bind each to a tag id. The ts prop needs no tag: " +
				"the platform feeds the newest sample time automatically.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"name":        propStr("Page name"),
				"html_source": propStr("Full HTML source of the widget page"),
				"bindings": propAny("Array of {prop, tag_id}; tag_id null leaves the prop unbound"),
				"ts_prop":     propStr("Prop fed with the newest sample time (optional)"),
				"width":       propInt("Canvas width (optional, default 1280)"),
				"height":      propInt("Canvas height (optional, default 720)"),
			}, []string{"name", "html_source"}),
		},
		{
			Name:        "create_area",
			Description: "Create an area inside a site",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"name":    propStr("Area name"),
				"site_id": propInt("Parent site ID"),
			}, []string{"name", "site_id"}),
		},
		{
			Name: "create_gateway",
			Description: "Create a gateway (a field device connection). driver_type selects the protocol: " +
				"MODBUS_TCP, S7, OPC_UA, MQTT or LORAWAN. connection_config carries the " +
				"protocol's settings, e.g. {\"ip\":\"192.168.1.10\",\"port\":502} for Modbus TCP, " +
				"{\"ip\":\"192.168.1.20\",\"rack\":0,\"slot\":1} for S7, " +
				"{\"broker_host\":\"…\",\"broker_port\":1883,\"broker_username\":\"…\",\"broker_password\":\"…\"," +
				"\"decoder_preset\":\"mido\",\"stale_after_minutes\":420} for MQTT from an external broker " +
				"with a payload decoder.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"name":              propStr("Gateway name"),
				"area_id":           propInt("Parent area ID"),
				"driver_type":       propStr("MODBUS_TCP, S7, OPC_UA, MQTT or LORAWAN"),
				"connection_config": propAny("Protocol settings as an object"),
				"scan_rate_ms":      propInt("Poll interval in milliseconds (default 1000)"),
			}, []string{"name", "area_id", "driver_type", "connection_config"}),
		},
		{
			Name: "create_tag",
			Description: "Create a single tag on a gateway.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"gateway_id":      propInt("Gateway ID"),
				"code":            propStr("Device address, e.g. 40001 for Modbus or DB10.DBX0.0 for S7"),
				"alias":           propStr("Human-readable name"),
				"data_type":       propStr("INT, REAL, BOOL, DINT or STRING"),
				"historize":       propBool("Record values to the historian (default false)"),
				"scaling_enabled": propBool("Convert raw to engineering units"),
				"scaling_raw_min": propAny("Raw range minimum, e.g. 0"),
				"scaling_raw_max": propAny("Raw range maximum, e.g. 27648"),
				"scaling_eu_min":  propAny("Engineering range minimum, e.g. 0"),
				"scaling_eu_max":  propAny("Engineering range maximum, e.g. 100"),
				"eu_unit":         propStr("Engineering unit, e.g. bar"),
			}, []string{"gateway_id", "code", "alias", "data_type"}),
		},
		{
			Name:        "delete_tag",
			Description: "Delete a tag. This also deletes everything the historian recorded for it.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id": propInt("Tag ID"),
			}, []string{"tag_id"}),
		},
		{
			Name: "set_tag_alarms",
			Description: "Replace a tag's alarm definitions. Send the whole set: this endpoint " +
				"replaces rather than appends.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"tag_id": propInt("Tag ID"),
				"alarms": propAny("Array of {alarm_type: high|low, threshold, severity, message, " +
					"deadband, delay_seconds, enabled}"),
			}, []string{"tag_id", "alarms"}),
		},

		// ── Synoptics (SCADA mimic pages) ───────────────────────────────────

		{
			Name:        "list_synoptics",
			Description: "List SCADA mimic pages in the current organization",
			InputSchema: jsonSchema(map[string]interface{}{}),
		},
		{
			Name:        "get_synoptic",
			Description: "Get one mimic page including its full widget layout",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"synoptic_id": propInt("Synoptic ID"),
			}, []string{"synoptic_id"}),
		},
		{
			Name: "create_synoptic",
			Description: "Create a SCADA mimic page. layout is an array of widgets, each with " +
				"{id, type, x, y, w, h, tagId, config}. Widget types include label, value, gauge, " +
				"bargraph, indicator, button, setpoint, motor, valve, image and shapes.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"name":             propStr("Page name"),
				"description":      propStr("What this page shows"),
				"background_color": propStr("Canvas background, e.g. #0f172a"),
				"canvas_w":         propInt("Canvas width in pixels (default 1280)"),
				"canvas_h":         propInt("Canvas height in pixels (default 720)"),
				"layout":           propAny("Array of widget objects"),
			}, []string{"name"}),
		},
		{
			Name: "update_synoptic",
			Description: "Replace a mimic page's layout. Send the whole layout — it is saved " +
				"atomically. Pass expected_updated_at from get_synoptic and the save is refused " +
				"if somebody else changed the page meanwhile, rather than overwriting their work.",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"synoptic_id":         propInt("Synoptic ID"),
				"name":                propStr("Page name"),
				"description":         propStr("What this page shows"),
				"background_color":    propStr("Canvas background"),
				"canvas_w":            propInt("Canvas width"),
				"canvas_h":            propInt("Canvas height"),
				"layout":              propAny("The complete widget array"),
				"expected_updated_at": propStr("updated_at from get_synoptic, to detect a concurrent edit"),
			}, []string{"synoptic_id", "name", "layout"}),
		},
		{
			Name:        "delete_synoptic",
			Description: "Delete a SCADA mimic page",
			InputSchema: jsonSchemaRequired(map[string]interface{}{
				"synoptic_id": propInt("Synoptic ID"),
			}, []string{"synoptic_id"}),
		},
	}

	for i := range tools {
		tools[i].Annotations = annotateTool(tools[i].Name)
	}
	return s.okResponse(req.ID, mcpToolsListResult{Tools: tools})
}

// ---- Prompts --------------------------------------------------------------

// mcpPrompt describes one guided workflow: the text tells the agent which
// tools to call in which order. Arguments are filled in from prompts/get.
type mcpPrompt struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Arguments   []mcpPromptArg     `json:"arguments,omitempty"`
}

type mcpPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func openedgePrompts() []mcpPrompt {
	return []mcpPrompt{
		{
			Name:        "daily-digest",
			Description: "Riepilogo 24h di un'organizzazione: salute, allarmi attivi, digest",
			Arguments: []mcpPromptArg{
				{Name: "org_id", Description: "Organization ID", Required: true},
			},
		},
		{
			Name:        "offline-triage",
			Description: "Trova le centraline mute e capisci da quando non mandano dati",
			Arguments: []mcpPromptArg{
				{Name: "org_id", Description: "Organization ID", Required: true},
			},
		},
		{
			Name:        "forecast-review",
			Description: "Dove stanno andando i tag: previsioni affidabili, soglie a rischio, tag da sistemare",
			Arguments: []mcpPromptArg{
				{Name: "org_id", Description: "Organization ID", Required: true},
			},
		},
		{
			Name:        "commission-gateway",
			Description: "Checklist di messa in servizio: gateway, tag, allarmi, verifica",
			Arguments: []mcpPromptArg{
				{Name: "gateway_id", Description: "Gateway ID just created", Required: true},
			},
		},
	}
}

func (s *mcpServer) handlePromptsList(req *jsonRPCRequest) *jsonRPCResponse {
	return s.okResponse(req.ID, map[string]interface{}{"prompts": openedgePrompts()})
}

func (s *mcpServer) handlePromptsGet(req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errResponse(req.ID, -32602, "invalid params: "+err.Error())
	}
	arg := func(k string) string {
		if params.Arguments == nil {
			return ""
		}
		v, _ := params.Arguments[k].(string)
		if v == "" {
			if f, ok := params.Arguments[k].(float64); ok {
				return strconv.Itoa(int(f))
			}
		}
		return v
	}
	var text string
	switch params.Name {
	case "daily-digest":
		org := arg("org_id")
		text = "Scrivi il riepilogo delle ultime 24h per l'organization " + org + ": " +
			"1) check_health, 2) list_active_alarms, 3) get_alarm_digest. " +
			"Rispondi in italiano, prima la salute, poi i critici, poi il resto. " +
			"Un valore stale non va mai presentato come buono."
	case "offline-triage":
		org := arg("org_id")
		text = "Trova le centraline mute dell'organization " + org + ": " +
			"1) get_fleet_status per lo stato, 2) per ogni gateway offline get_tag_shadows " +
			"e get_tag_history per datare l'ultimo dato reale. " +
			"Distingui 'mai contattato' da 'muto da una data': il secondo va in campo."
	case "forecast-review":
		org := arg("org_id")
		text = "Rivedi le previsioni dell'organization " + org + ": " +
			"1) get_forecast_overview, 2) per i tag 'ok' confronta il valore previsto a fine orizzonte " +
			"(e la fascia P10–P90) con le soglie di list_active_alarms e get_tag_value, 3) get_tag_forecast " +
			"sui tag a rischio. Rispondi in italiano: prima i tag che usciranno dalle soglie, poi quelli in " +
			"degrado. Una previsione 'unreliable' o 'learning' NON è una previsione: dilla come tale, mai come dato."
	case "commission-gateway":
		gw := arg("gateway_id")
		text = "Metti in servizio il gateway " + gw + " in ordine: " +
			"1) list_tags per verificare i tag, 2) get_tag_value su ognuno per il primo " +
			"dato reale, 3) set_tag_alarms con le soglie del cliente, " +
			"4) check_health finale. Fermati e riporta se un tag non ha mai mandato dati."
	default:
		return s.errResponse(req.ID, -32602, "unknown prompt: "+params.Name)
	}
	return s.okResponse(req.ID, map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "user", "content": map[string]interface{}{"type": "text", "text": text}},
		},
	})
}

func (s *mcpServer) handleToolsCall(req *jsonRPCRequest) *jsonRPCResponse {
	var params mcpCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errResponse(req.ID, -32602, "invalid params: "+err.Error())
	}

	log.Printf("tool call: %s args=%v", params.Name, params.Arguments)

	result, err := s.executeTool(params.Name, params.Arguments)
	if err != nil {
		if api.Unauthorized(err) {
			s.upstreamUnauthorized = true
		}
		log.Printf("tool error: %s: %v", params.Name, err)
		return s.okResponse(req.ID, mcpCallResult{
			Content: []mcpContent{{
				Type: "text",
				Text: fmt.Sprintf("Error: %v", err),
			}},
		})
	}

	return s.okResponse(req.ID, mcpCallResult{
		Content: []mcpContent{{
			Type: "text",
			Text: result,
		}},
	})
}

func (s *mcpServer) executeTool(name string, args map[string]interface{}) (string, error) {
	switch name {
	case "list_organizations":
		return s.toolGet("/api/organizations")

	case "list_gateways":
		path := "/api/gateways"
		if orgID := intArg(args, "org_id"); orgID > 0 {
			path += "?org_id=" + strconv.Itoa(orgID)
		}
		return s.toolGet(path)

	case "list_tags":
		path := "/api/tags"
		if gwID := intArg(args, "gateway_id"); gwID > 0 {
			path += "?gateway_id=" + strconv.Itoa(gwID)
		}
		return s.toolGet(path)

	case "get_tag_value":
		tagID := intArg(args, "tag_id")
		if tagID == 0 {
			return "", fmt.Errorf("tag_id is required")
		}
		return s.toolGet(fmt.Sprintf("/api/tags/%d/current", tagID))

	case "write_tag_value":
		tagID := intArg(args, "tag_id")
		if tagID == 0 {
			return "", fmt.Errorf("tag_id is required")
		}
		value, ok := args["value"]
		if !ok {
			return "", fmt.Errorf("value is required")
		}
		body := map[string]interface{}{"value": value}
		return s.toolPut(fmt.Sprintf("/api/i3x/v1/properties/tag-%d/value", tagID), body)

	case "get_tag_history":
		tagID := intArg(args, "tag_id")
		if tagID == 0 {
			return "", fmt.Errorf("tag_id is required")
		}
		path := fmt.Sprintf("/api/history/stats?tag_id=%d", tagID)
		if from := strArg(args, "from"); from != "" {
			path += "&from=" + from
		}
		if to := strArg(args, "to"); to != "" {
			path += "&to=" + to
		}
		if limit := intArg(args, "limit"); limit > 0 {
			path += "&limit=" + strconv.Itoa(limit)
		}
		return s.toolGet(path)

	case "get_tag_shadows":
		path := "/api/tags/shadows"
		if gwID := intArg(args, "gateway_id"); gwID > 0 {
			path += "?gateway_id=" + strconv.Itoa(gwID)
		}
		return s.toolGet(path)

	case "list_active_alarms":
		path := "/api/alarms/active"
		if sev := strArg(args, "severity"); sev != "" {
			path += "?severity=" + sev
		}
		return s.toolGet(path)

	case "acknowledge_alarm":
		alarmID := intArg(args, "alarm_id")
		if alarmID == 0 {
			return "", fmt.Errorf("alarm_id is required")
		}
		return s.toolPost(fmt.Sprintf("/api/alarms/%d/ack", alarmID), nil)

	case "get_fleet_status":
		return s.toolGet("/api/fleet/status")

	case "fleet_restart":
		orgID := intArg(args, "org_id")
		if orgID == 0 {
			return "", fmt.Errorf("org_id is required")
		}
		return s.toolPost(fmt.Sprintf("/api/organizations/%d/edge-restart", orgID), nil)

	case "list_lorawan_devices":
		gwID := intArg(args, "gateway_id")
		if gwID == 0 {
			return "", fmt.Errorf("gateway_id is required")
		}
		return s.toolGet(fmt.Sprintf("/api/gateways/%d/lorawan/devices", gwID))

	case "import_lorawan_tags":
		gwID := intArg(args, "gateway_id")
		if gwID == 0 {
			return "", fmt.Errorf("gateway_id is required")
		}
		devices, ok := args["devices"]
		if !ok {
			return "", fmt.Errorf("devices is required")
		}
		return s.toolPost(fmt.Sprintf("/api/gateways/%d/lorawan/devices/import", gwID), devices)

	case "send_lorawan_downlink":
		gwID := intArg(args, "gateway_id")
		if gwID == 0 {
			return "", fmt.Errorf("gateway_id is required")
		}
		deviceID := strArg(args, "device_id")
		if deviceID == "" {
			return "", fmt.Errorf("device_id is required")
		}
		fPort := intArg(args, "f_port")
		if fPort == 0 {
			return "", fmt.Errorf("f_port is required")
		}
		payloadHex := strArg(args, "payload_hex")
		if payloadHex == "" {
			return "", fmt.Errorf("payload_hex is required")
		}
		body := map[string]interface{}{
			"device_id":   deviceID,
			"f_port":      fPort,
			"payload_hex": payloadHex,
			"confirmed":   boolArg(args, "confirmed"),
		}
		return s.toolPost(fmt.Sprintf("/api/gateways/%d/lorawan/downlink", gwID), body)

	case "get_aiops_summary":
		hours := intArg(args, "hours")
		if hours == 0 {
			hours = 24
		}
		return s.toolGet(fmt.Sprintf("/api/aiops/summary?hours=%d", hours))

	case "detect_anomalies":
		tagID := intArg(args, "tag_id")
		if tagID == 0 {
			return "", fmt.Errorf("tag_id is required")
		}
		windowHours := intArg(args, "window_hours")
		if windowHours == 0 {
			windowHours = 24
		}
		return s.toolGet(fmt.Sprintf("/api/aiops/anomalies?tag_id=%d&window_hours=%d", tagID, windowHours))

	case "get_alarm_digest":
		hours := intArg(args, "hours")
		if hours == 0 {
			hours = 24
		}
		return s.toolGet(fmt.Sprintf("/api/aiops/alarms/digest?hours=%d", hours))

	case "get_forecast_overview":
		raw, err := s.client.RawGet("/api/forecast/overview")
		if err != nil {
			return "", err
		}
		var ov forecastOverview
		if err := json.Unmarshal(raw, &ov); err != nil {
			return prettyJSON(raw), nil
		}
		ov.Tags = filterForecastOverview(ov.Tags, strArg(args, "status"), strArg(args, "site"))
		data, _ := json.MarshalIndent(ov, "", "  ")
		return string(data), nil

	case "get_tag_forecast", "refresh_tag_forecast":
		tagID := intArg(args, "tag_id")
		if tagID == 0 {
			return "", fmt.Errorf("tag_id is required")
		}
		if name == "refresh_tag_forecast" {
			return s.toolPost(fmt.Sprintf("/api/tags/%d/forecast/refresh", tagID), nil)
		}
		return s.toolGet(fmt.Sprintf("/api/tags/%d/forecast", tagID))

	case "get_forecast_settings", "set_forecast_settings":
		scope, id := strArg(args, "scope"), intArg(args, "scope_id")
		if scope == "" || id == 0 {
			return "", fmt.Errorf("scope and scope_id are required")
		}
		if name == "get_forecast_settings" {
			return s.toolGet(forecastProfilePath(scope, id))
		}
		changes := map[string]interface{}{}
		for _, k := range []string{"enabled", "kind", "horizon_hours", "context_days"} {
			if v, ok := args[k]; ok {
				changes[k] = v
			}
		}
		raw, err := setForecastSettings(s.client.RawGet, func(path string, body interface{}) ([]byte, error) {
			out, err := s.toolPut(path, body)
			return []byte(out), err
		}, scope, id, changes, boolArg(args, "clear"))
		if err != nil {
			return "", err
		}
		return string(raw), nil

	case "check_health":
		var liveResult interface{}
		var readyResult interface{}
		liveErr := s.client.Get("/health", &liveResult)
		readyErr := s.client.Get("/ready", &readyResult)

		combined := map[string]interface{}{
			"health": liveResult,
			"ready":  readyResult,
		}
		if liveErr != nil {
			combined["health_error"] = liveErr.Error()
		}
		if readyErr != nil {
			combined["ready_error"] = readyErr.Error()
		}
		data, _ := json.MarshalIndent(combined, "", "  ")
		return string(data), nil

	// ── Provisioning ────────────────────────────────────────────────────────

	case "list_sites":
		return s.toolGet("/api/sites")

	case "create_site":
		name := strArg(args, "name")
		orgID := intArg(args, "org_id")
		if name == "" || orgID == 0 {
			return "", fmt.Errorf("name and org_id are required")
		}
		return s.toolPost("/api/sites", map[string]interface{}{"name": name, "org_id": orgID})

	case "list_areas":
		path := "/api/areas"
		if siteID := intArg(args, "site_id"); siteID > 0 {
			path += "?site_id=" + strconv.Itoa(siteID)
		}
		return s.toolGet(path)

	case "create_area":
		name := strArg(args, "name")
		siteID := intArg(args, "site_id")
		if name == "" || siteID == 0 {
			return "", fmt.Errorf("name and site_id are required")
		}
		return s.toolPost("/api/areas", map[string]interface{}{"name": name, "site_id": siteID})

	case "import_html_widget":
		name := strArg(args, "name")
		source := strArg(args, "html_source")
		if name == "" || source == "" {
			return "", fmt.Errorf("name and html_source are required")
		}
		width := intArg(args, "width")
		if width <= 0 {
			width = 1280
		}
		height := intArg(args, "height")
		if height <= 0 {
			height = 720
		}
		var bindings interface{}
		if b, ok := args["bindings"]; ok && b != nil {
			bindings = b
		} else {
			bindings = []interface{}{}
		}
		return s.toolPost("/api/synoptics", map[string]interface{}{
			"name":             name,
			"background_color": "#0f172a",
			"canvas_w":         width,
			"canvas_h":         height,
			"layout": []map[string]interface{}{
				{
					"id":    "html-widget",
					"type":  "html",
					"x":     0, "y": 0,
					"w":     width, "h": height,
					"label": name,
					"config": map[string]interface{}{
						"htmlSource": source,
						"bindings":   bindings,
						"tsProp":     strArg(args, "ts_prop"),
					},
				},
			},
		})

	case "create_gateway":
		name := strArg(args, "name")
		areaID := intArg(args, "area_id")
		driver := strArg(args, "driver_type")
		if name == "" || areaID == 0 || driver == "" {
			return "", fmt.Errorf("name, area_id and driver_type are required")
		}
		conn, ok := args["connection_config"]
		if !ok {
			return "", fmt.Errorf("connection_config is required — for MODBUS_TCP it is " +
				`{"ip":"...","port":502}`)
		}
		body := map[string]interface{}{
			"name": name, "area_id": areaID,
			"driver_type": driver, "connection_config": conn,
		}
		if rate := intArg(args, "scan_rate_ms"); rate > 0 {
			body["scan_rate_ms"] = rate
		} else {
			body["scan_rate_ms"] = 1000
		}
		return s.toolPost("/api/gateways", body)

	case "create_tag":
		gwID := intArg(args, "gateway_id")
		if gwID == 0 {
			return "", fmt.Errorf("gateway_id is required")
		}
		body := map[string]interface{}{
			"gateway_id": gwID,
			"code":       strArg(args, "code"),
			"alias":      strArg(args, "alias"),
			"data_type":  strArg(args, "data_type"),
		}
		// Only forward the optional fields that were actually supplied, so an
		// omitted flag keeps the server's default instead of being set false.
		for _, k := range []string{"historize", "scaling_enabled", "scaling_raw_min",
			"scaling_raw_max", "scaling_eu_min", "scaling_eu_max", "eu_unit"} {
			if v, ok := args[k]; ok {
				body[k] = v
			}
		}
		return s.toolPost("/api/tags", body)

	case "delete_tag":
		tagID := intArg(args, "tag_id")
		if tagID == 0 {
			return "", fmt.Errorf("tag_id is required")
		}
		return s.toolDelete(fmt.Sprintf("/api/tags/%d", tagID))

	case "set_tag_alarms":
		tagID := intArg(args, "tag_id")
		alarms, ok := args["alarms"]
		if tagID == 0 || !ok {
			return "", fmt.Errorf("tag_id and alarms are required")
		}
		return s.toolPut(fmt.Sprintf("/api/tags/%d/alarms", tagID), alarms)

	// ── Synoptics ───────────────────────────────────────────────────────────

	case "list_synoptics":
		return s.toolGet("/api/synoptics")

	case "get_synoptic":
		id := intArg(args, "synoptic_id")
		if id == 0 {
			return "", fmt.Errorf("synoptic_id is required")
		}
		return s.toolGet(fmt.Sprintf("/api/synoptics/%d", id))

	case "create_synoptic":
		name := strArg(args, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		return s.toolPost("/api/synoptics", synopticBody(args, name))

	case "update_synoptic":
		id := intArg(args, "synoptic_id")
		name := strArg(args, "name")
		if id == 0 || name == "" {
			return "", fmt.Errorf("synoptic_id and name are required")
		}
		body := synopticBody(args, name)
		if ts := strArg(args, "expected_updated_at"); ts != "" {
			body["expected_updated_at"] = ts
		}
		return s.toolPut(fmt.Sprintf("/api/synoptics/%d", id), body)

	case "delete_synoptic":
		id := intArg(args, "synoptic_id")
		if id == 0 {
			return "", fmt.Errorf("synoptic_id is required")
		}
		return s.toolDelete(fmt.Sprintf("/api/synoptics/%d", id))

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// synopticBody assembles a mimic-page payload, filling the canvas defaults the
// API expects when the caller only cares about the widgets.
func synopticBody(args map[string]interface{}, name string) map[string]interface{} {
	body := map[string]interface{}{
		"name":             name,
		"description":      strArg(args, "description"),
		"background_color": "#0f172a",
		"canvas_w":         1280,
		"canvas_h":         720,
		"layout":           []interface{}{},
	}
	if v := strArg(args, "background_color"); v != "" {
		body["background_color"] = v
	}
	if v := intArg(args, "canvas_w"); v > 0 {
		body["canvas_w"] = v
	}
	if v := intArg(args, "canvas_h"); v > 0 {
		body["canvas_h"] = v
	}
	if v, ok := args["layout"]; ok {
		body["layout"] = v
	}
	return body
}

// toolGet calls GET on path and returns pretty JSON.
func (s *mcpServer) toolGet(path string) (string, error) {
	data, err := s.client.RawGet(path)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

// toolPost calls POST on path with body and returns pretty JSON.
func (s *mcpServer) toolPost(path string, body interface{}) (string, error) {
	data, err := s.client.RawPost(path, body)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

// toolPut calls PUT on path with body and returns pretty JSON.
func (s *mcpServer) toolPut(path string, body interface{}) (string, error) {
	var result interface{}
	if err := s.client.Put(path, body, &result); err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

// prettyJSON tries to pretty-print raw JSON; returns as-is on error.
func prettyJSON(data []byte) string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(data)
	}
	return string(pretty)
}

// ---- argument helpers ---------------------------------------------------------

func intArg(args map[string]interface{}, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func strArg(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func boolArg(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		return strings.ToLower(s) == "true"
	}
	return false
}

// ---- JSON Schema helpers ------------------------------------------------------

func jsonSchema(props map[string]interface{}) interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
}

func jsonSchemaRequired(props map[string]interface{}, required []string) interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func propInt(desc string) interface{} {
	return map[string]interface{}{"type": "integer", "description": desc}
}

func propStr(desc string) interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

func propBool(desc string) interface{} {
	return map[string]interface{}{"type": "boolean", "description": desc}
}

func propAny(desc string) interface{} {
	return map[string]interface{}{"description": desc}
}

// toolDelete calls DELETE on path and returns pretty JSON.
func (s *mcpServer) toolDelete(path string) (string, error) {
	data, err := s.client.RawDelete(path)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}
