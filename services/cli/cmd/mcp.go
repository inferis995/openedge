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
	Short: "Start MCP server for AI agent integration (JSON-RPC 2.0 over stdio)",
	Long: `Start an MCP (Model Context Protocol) server over stdio.

The server reads JSON-RPC 2.0 messages from stdin (Content-Length framed)
and writes responses to stdout. All logging goes to stderr.

Configure in Claude Code or Cursor:
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
  }`,
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
	Tools map[string]interface{} `json:"tools"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
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
}

func runMCPServer(cmd *cobra.Command, args []string) {
	// MCP server: ALL logging to stderr, stdout is for protocol only
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[openedge-mcp] ")

	client := getMCPClient()

	srv := &mcpServer{
		client: client,
		reader: bufio.NewReader(os.Stdin),
		writer: bufio.NewWriter(os.Stdout),
	}

	log.Println("OpenEdge MCP server started, waiting for messages...")
	srv.serve()
}

// getMCPClient builds the API client for the MCP server, preferring env vars.
func getMCPClient() *api.Client {
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

	return api.New(cfg.URL, cfg.Token, cfg.OrgID)
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
	case "initialized":
		// Notification, no response needed — but we still return a response to avoid blocking
		s.initialized = true
		return s.okResponse(req.ID, map[string]interface{}{})
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "ping":
		return s.okResponse(req.ID, map[string]interface{}{})
	default:
		log.Printf("unknown method: %s", req.Method)
		return s.errResponse(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *mcpServer) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	s.initialized = true
	return s.okResponse(req.ID, mcpInitResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: mcpCapabilities{
			Tools: map[string]interface{}{},
		},
		ServerInfo: mcpServerInfo{
			Name:    "openedge",
			Version: "1.0.0",
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
	}

	return s.okResponse(req.ID, mcpToolsListResult{Tools: tools})
}

func (s *mcpServer) handleToolsCall(req *jsonRPCRequest) *jsonRPCResponse {
	var params mcpCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errResponse(req.ID, -32602, "invalid params: "+err.Error())
	}

	log.Printf("tool call: %s args=%v", params.Name, params.Arguments)

	result, err := s.executeTool(params.Name, params.Arguments)
	if err != nil {
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

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
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
