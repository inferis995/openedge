package opcua

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/ua"
)

// Config holds the OPC UA client configuration
type Config struct {
	Endpoint       string // e.g. "opc.tcp://192.168.1.10:4840"
	SecurityMode   string // "None" (future: "Sign", "SignAndEncrypt")
	SecurityPolicy string // "None" (future: "Basic256Sha256")
	Timeout        time.Duration
	AuthMode       string // "Anonymous", "Username", "Certificate"
	Username       string
	Password       string
	CertFile       string
	KeyFile        string
}

// NodeInfo represents a browsed OPC UA node
type NodeInfo struct {
	NodeID        string `json:"node_id"`
	BrowseName    string `json:"name"`
	DisplayName   string `json:"display_name"`
	NodeClass     string `json:"node_class"` // "Object", "Variable", "Method"
	DataType      string `json:"data_type"`  // For Variable nodes: "Int32", "Float", "Boolean", "Double", "String"
	ChildrenCount int    `json:"children_count"`
}

// Client wraps the gopcua client with helper methods
type Client struct {
	config    Config
	client    *opcua.Client
	mu        sync.Mutex
	connected bool
}

// NewClient creates a new OPC UA client with the given configuration
func NewClient(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.SecurityMode == "" {
		cfg.SecurityMode = "None"
	}
	if cfg.SecurityPolicy == "" {
		cfg.SecurityPolicy = "None"
	}
	return &Client{
		config:    cfg,
		connected: false,
	}
}

// Connect establishes connection to the OPC UA server
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	log.Printf("[OPC-UA] Connecting to %s (AuthMode: %s)...", c.config.Endpoint, c.config.AuthMode)

	// Start with basic options
	opts := []opcua.Option{
		opcua.RequestTimeout(c.config.Timeout),
	}

	// Determine security mode and policy from config (defaults to None for compatibility)
	securityMode := ua.MessageSecurityModeNone
	securityPolicy := "None"

	if c.config.SecurityMode == "Sign" {
		securityMode = ua.MessageSecurityModeSign
	} else if c.config.SecurityMode == "SignAndEncrypt" {
		securityMode = ua.MessageSecurityModeSignAndEncrypt
	}

	if c.config.SecurityPolicy != "" {
		securityPolicy = c.config.SecurityPolicy
	}

	opts = append(opts, opcua.SecurityMode(securityMode))
	opts = append(opts, opcua.SecurityPolicy(securityPolicy))
	log.Printf("[OPC-UA] Security: Mode=%v, Policy=%s", securityMode, securityPolicy)

	// Add authentication options
	switch c.config.AuthMode {
	case "Username":
		opts = append(opts, opcua.AuthUsername(c.config.Username, c.config.Password))
		log.Printf("[OPC-UA] Using Username authentication (user: %s)", c.config.Username)
	case "Certificate":
		opts = append(opts, opcua.CertificateFile(c.config.CertFile))
		opts = append(opts, opcua.PrivateKeyFile(c.config.KeyFile))
		log.Printf("[OPC-UA] Using Certificate authentication (cert: %s)", c.config.CertFile)
	default:
		// Default is Anonymous
		opts = append(opts, opcua.AuthAnonymous())
		log.Printf("[OPC-UA] Using Anonymous authentication")
	}

	// Try to discover server endpoints for better compatibility
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	log.Printf("[OPC-UA] Discovering server endpoints...")
	endpoints, err := opcua.GetEndpoints(ctx, c.config.Endpoint)
	if err != nil {
		log.Printf("[OPC-UA] Warning: Endpoint discovery failed: %v (continuing with configured settings)", err)
	} else {
		// Find a compatible endpoint
		for _, ep := range endpoints {
			log.Printf("[OPC-UA] Discovered endpoint: %s, SecurityMode=%v, SecurityPolicy=%s",
				ep.EndpointURL, ep.SecurityMode, ep.SecurityPolicyURI)

			// If using default None/None, try to find the best matching endpoint
			if securityMode == ua.MessageSecurityModeNone && securityPolicy == "None" {
				// Look for a None/None endpoint
				if ep.SecurityMode == ua.MessageSecurityModeNone {
					log.Printf("[OPC-UA] Using discovered None/None endpoint")
					break
				}
			}
		}
	}

	client, err := opcua.NewClient(c.config.Endpoint, opts...)
	if err != nil {
		return fmt.Errorf("failed to create OPC UA client: %w", err)
	}

	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to OPC UA server at %s: %w", c.config.Endpoint, err)
	}

	c.client = client
	c.connected = true
	log.Printf("[OPC-UA] Successfully connected to %s", c.config.Endpoint)
	return nil
}

// ConnectWithRetry attempts to connect with retry logic
func (c *Client) ConnectWithRetry(attempts int, interval time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := c.Connect(); err != nil {
			lastErr = err
			log.Printf("[OPC-UA] Connection attempt %d/%d failed: %v", i+1, attempts, err)
			if i < attempts-1 {
				time.Sleep(interval)
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to connect after %d attempts: %w", attempts, lastErr)
}

// Disconnect closes the connection to the OPC UA server
func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c.client.Close(ctx)
	c.connected = false
	log.Printf("[OPC-UA] Disconnected from %s", c.config.Endpoint)
}

// IsConnected returns whether the client is currently connected
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Browse navigates the OPC UA address space starting from the given node ID
// Returns the list of child nodes. Pass "i=84" for the root Objects folder.
func (c *Client) Browse(parentNodeID string) ([]NodeInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return nil, fmt.Errorf("not connected to OPC UA server")
	}

	nodeID, err := ua.ParseNodeID(parentNodeID)
	if err != nil {
		return nil, fmt.Errorf("invalid node ID '%s': %w", parentNodeID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	// Browse request — get hierarchical references (children)
	browseReq := &ua.BrowseRequest{
		RequestHeader: &ua.RequestHeader{},
		NodesToBrowse: []*ua.BrowseDescription{
			{
				NodeID:          nodeID,
				BrowseDirection: ua.BrowseDirectionForward,
				ReferenceTypeID: ua.NewNumericNodeID(0, id.HierarchicalReferences),
				IncludeSubtypes: true,
				NodeClassMask:   uint32(ua.NodeClassObject | ua.NodeClassVariable | ua.NodeClassMethod),
				ResultMask:      uint32(ua.BrowseResultMaskAll),
			},
		},
	}

	browseResp, err := c.client.Browse(ctx, browseReq)
	if err != nil {
		c.handleConnectionError(err)
		return nil, fmt.Errorf("browse failed: %w", err)
	}

	if len(browseResp.Results) == 0 {
		return []NodeInfo{}, nil
	}

	result := browseResp.Results[0]
	if result.StatusCode != ua.StatusOK {
		return nil, fmt.Errorf("browse returned status: %v", result.StatusCode)
	}

	nodes := make([]NodeInfo, 0, len(result.References))
	for _, ref := range result.References {
		nodeInfo := NodeInfo{
			NodeID:      ref.NodeID.NodeID.String(),
			BrowseName:  ref.BrowseName.Name,
			DisplayName: ref.DisplayName.Text,
			NodeClass:   nodeClassToString(ref.NodeClass),
		}

		// For Variable nodes, try to read the data type
		if ref.NodeClass == ua.NodeClassVariable {
			nodeInfo.DataType = c.readDataType(ctx, ref.NodeID.NodeID)
		}

		// Count children for expandable UI
		nodeInfo.ChildrenCount = c.countChildren(ctx, ref.NodeID.NodeID)

		nodes = append(nodes, nodeInfo)
	}

	return nodes, nil
}

// ReadValue reads the current value of a node
func (c *Client) ReadValue(nodeID string) (interface{}, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return nil, 1, fmt.Errorf("not connected to OPC UA server")
	}

	nid, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return nil, 1, fmt.Errorf("invalid node ID '%s': %w", nodeID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	req := &ua.ReadRequest{
		MaxAge: 2000,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      nid,
				AttributeID: ua.AttributeIDValue,
			},
		},
	}

	resp, err := c.client.Read(ctx, req)
	if err != nil {
		c.handleConnectionError(err)
		return nil, 1, fmt.Errorf("read failed: %w", err)
	}

	if len(resp.Results) == 0 {
		return nil, 1, fmt.Errorf("no results returned")
	}

	result := resp.Results[0]
	if result.Status != ua.StatusOK {
		return nil, 1, fmt.Errorf("read returned status: %v", result.Status)
	}

	// Quality: 0 = good, 1 = bad
	quality := 0
	if result.Status != ua.StatusOK {
		quality = 1
	}

	return result.Value.Value(), quality, nil
}

// WriteValue writes a value to a node
func (c *Client) WriteValue(nodeID string, value interface{}, dataType string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return fmt.Errorf("not connected to OPC UA server")
	}

	nid, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return fmt.Errorf("invalid node ID '%s': %w", nodeID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	log.Printf("[OPC-UA WRITE] NodeID: %s, DatabaseType: %s, Value: %v (%T)", nodeID, dataType, value, value)

	// STRATEGY: Try multiple approaches to write the value, starting with most compatible

	// Convert the incoming value to multiple possible representations
	boolVal, isBool := toBool(value)

	// Build a list of variants to try, in order of preference
	variantsToTry := []struct {
		name string
		variant *ua.Variant
		desc string
	}{}

	// First, read current value from server to understand its type
	readReq := &ua.ReadRequest{
		MaxAge: 2000,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      nid,
				AttributeID: ua.AttributeIDValue,
			},
		},
	}

	var serverType string
	var currentVariant *ua.Variant
	readResp, err := c.client.Read(ctx, readReq)
	if err == nil && len(readResp.Results) > 0 {
		result := readResp.Results[0]
		if result.Status == ua.StatusOK && result.Value != nil {
			currentVariant = result.Value
			serverType = result.Value.Type().String()
			log.Printf("[OPC-UA WRITE] Current server value: Type=%s, Value=%v", serverType, result.Value.Value())
		}
	}

	// Read the DataType attribute
	opcuaType := c.readDataType(ctx, nid)
	log.Printf("[OPC-UA WRITE] Server DataType attribute: %s", opcuaType)

	if isBool {
		// For boolean values, try multiple numeric encodings
		var int8Val int8
		if boolVal {
			int8Val = 1
		}

		var int16Val int16
		if boolVal {
			int16Val = 1
		}

		var int32Val int32
		if boolVal {
			int32Val = 1
		}

		var uint8Val uint8
		if boolVal {
			uint8Val = 1
		}

		var uint16Val uint16
		if boolVal {
			uint16Val = 1
		}

		var uint32Val uint32
		if boolVal {
			uint32Val = 1
		}

		// Try in order of most likely to work
		variantsToTry = append(variantsToTry,
			struct{ name string; variant *ua.Variant; desc string }{
				name: "Boolean", variant: mustVariant(boolVal), desc: "native Go bool",
			},
			struct{ name string; variant *ua.Variant; desc string }{
				name: "Int16", variant: mustVariant(int16Val), desc: "signed 16-bit (0/1)",
			},
			struct{ name string; variant *ua.Variant; desc string }{
				name: "Int32", variant: mustVariant(int32Val), desc: "signed 32-bit (0/1)",
			},
			struct{ name string; variant *ua.Variant; desc string }{
				name: "SByte", variant: mustVariant(int8Val), desc: "signed 8-bit (0/1)",
			},
			struct{ name string; variant *ua.Variant; desc string }{
				name: "Byte", variant: mustVariant(uint8Val), desc: "unsigned 8-bit (0/1)",
			},
			struct{ name string; variant *ua.Variant; desc string }{
				name: "UInt16", variant: mustVariant(uint16Val), desc: "unsigned 16-bit (0/1)",
			},
			struct{ name string; variant *ua.Variant; desc string }{
				name: "UInt32", variant: mustVariant(uint32Val), desc: "unsigned 32-bit (0/1)",
			},
		)
	} else {
		// For non-boolean values, use standard conversion
		v, err := convertToVariant(value, dataType)
		if err != nil {
			return fmt.Errorf("failed to convert value: %w", err)
		}
		variantsToTry = append(variantsToTry,
			struct{ name string; variant *ua.Variant; desc string }{
				name: dataType, variant: v, desc: "standard conversion",
			},
		)
	}

	// Try each variant until one succeeds
	var lastErr error
	for i, vt := range variantsToTry {
		log.Printf("[OPC-UA WRITE] Attempt %d/%d: Trying %s (%s)",
			i+1, len(variantsToTry), vt.name, vt.desc)

		req := &ua.WriteRequest{
			NodesToWrite: []*ua.WriteValue{
				{
					NodeID:      nid,
					AttributeID: ua.AttributeIDValue,
					Value: &ua.DataValue{
						Value: vt.variant,
					},
				},
			},
		}

		resp, err := c.client.Write(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("write error with %s: %w", vt.name, err)
			log.Printf("[OPC-UA WRITE] Attempt %d failed: %v", i+1, err)
			continue
		}

		if len(resp.Results) > 0 {
			status := resp.Results[0]
			if status == ua.StatusOK {
				log.Printf("[OPC-UA WRITE] SUCCESS with %s type!", vt.name)
				return nil
			}
			lastErr = fmt.Errorf("write with %s returned status: %v", vt.name, status)
			log.Printf("[OPC-UA WRITE] Attempt %d failed with status: %v", i+1, status)
			continue
		}
	}

	// All attempts failed
	log.Printf("[OPC-UA WRITE] All %d attempts failed", len(variantsToTry))
	return lastErr
}

// mustVariant creates a variant, panicking on error (used when input is guaranteed valid)
func mustVariant(value interface{}) *ua.Variant {
	v, err := ua.NewVariant(value)
	if err != nil {
		// For our simple types, this should never fail
		return ua.MustVariant(value)
	}
	return v
}

// readDataType reads the DataType attribute of a variable node and returns a human-readable string
func (c *Client) readDataType(ctx context.Context, nodeID *ua.NodeID) string {
	req := &ua.ReadRequest{
		MaxAge: 2000,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      nodeID,
				AttributeID: ua.AttributeIDDataType,
			},
		},
	}

	resp, err := c.client.Read(ctx, req)
	if err != nil || len(resp.Results) == 0 {
		log.Printf("[OPC-UA DEBUG] Failed to read DataType for node %s: %v", nodeID.String(), err)
		return "Unknown"
	}

	result := resp.Results[0]
	if result.Status != ua.StatusOK || result.Value == nil {
		log.Printf("[OPC-UA DEBUG] DataType read status for node %s: %v", nodeID.String(), result.Status)
		return "Unknown"
	}

	dtNodeID, ok := result.Value.Value().(*ua.NodeID)
	if !ok {
		log.Printf("[OPC-UA DEBUG] DataType value for node %s is not a NodeID", nodeID.String())
		return "Unknown"
	}

	dataType := dataTypeNodeIDToString(dtNodeID)
	log.Printf("[OPC-UA DEBUG] Node %s has DataType: %s (NodeID: %s)", nodeID.String(), dataType, dtNodeID.String())
	return dataType
}

// countChildren performs a quick browse to count child references
func (c *Client) countChildren(ctx context.Context, nodeID *ua.NodeID) int {
	browseReq := &ua.BrowseRequest{
		RequestHeader: &ua.RequestHeader{},
		NodesToBrowse: []*ua.BrowseDescription{
			{
				NodeID:          nodeID,
				BrowseDirection: ua.BrowseDirectionForward,
				ReferenceTypeID: ua.NewNumericNodeID(0, id.HierarchicalReferences),
				IncludeSubtypes: true,
				NodeClassMask:   uint32(ua.NodeClassObject | ua.NodeClassVariable),
				ResultMask:      uint32(ua.BrowseResultMaskNodeClass),
			},
		},
	}

	browseResp, err := c.client.Browse(ctx, browseReq)
	if err != nil || len(browseResp.Results) == 0 {
		return 0
	}

	return len(browseResp.Results[0].References)
}

// handleConnectionError marks the client as disconnected on connection errors
func (c *Client) handleConnectionError(err error) {
	if err != nil {
		log.Printf("[OPC-UA] Connection error: %v, marking as disconnected", err)
		c.connected = false
	}
}

// nodeClassToString converts a NodeClass enum to a human-readable string
func nodeClassToString(nc ua.NodeClass) string {
	switch nc {
	case ua.NodeClassObject:
		return "Object"
	case ua.NodeClassVariable:
		return "Variable"
	case ua.NodeClassMethod:
		return "Method"
	case ua.NodeClassObjectType:
		return "ObjectType"
	case ua.NodeClassVariableType:
		return "VariableType"
	case ua.NodeClassReferenceType:
		return "ReferenceType"
	case ua.NodeClassDataType:
		return "DataType"
	case ua.NodeClassView:
		return "View"
	default:
		return "Unknown"
	}
}

// dataTypeNodeIDToString maps well-known OPC UA data type node IDs to strings
func dataTypeNodeIDToString(nodeID *ua.NodeID) string {
	if nodeID.Namespace() != 0 {
		return "Custom"
	}
	switch nodeID.IntID() {
	case id.Boolean:
		return "Boolean"
	case id.SByte:
		return "SByte"
	case id.Byte:
		return "Byte"
	case id.Int16:
		return "Int16"
	case id.UInt16:
		return "UInt16"
	case id.Int32:
		return "Int32"
	case id.UInt32:
		return "UInt32"
	case id.Int64:
		return "Int64"
	case id.UInt64:
		return "UInt64"
	case id.Float:
		return "Float"
	case id.Double:
		return "Double"
	case id.String:
		return "String"
	case id.DateTime:
		return "DateTime"
	case id.ByteString:
		return "ByteString"
	default:
		return fmt.Sprintf("TypeID_%d", nodeID.IntID())
	}
}

// convertToVariant converts a Go value to an OPC UA Variant based on data type
func convertToVariant(value interface{}, dataType string) (*ua.Variant, error) {
	switch dataType {
	case "BOOL":
		b, ok := toBool(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %v to bool", value)
		}
		return ua.NewVariant(b)
	case "INT":
		n, ok := toInt16(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %v to int16", value)
		}
		return ua.NewVariant(n)
	case "DINT":
		n, ok := toInt32(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %v to int32", value)
		}
		return ua.NewVariant(n)
	case "REAL":
		f, ok := toFloat32(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %v to float32", value)
		}
		return ua.NewVariant(f)
	case "STRING":
		s := fmt.Sprintf("%v", value)
		return ua.NewVariant(s)
	default:
		return ua.NewVariant(value)
	}
}

// convertToVariantByOpcUaType converts a value to an OPC UA Variant based on the exact OPC UA data type
// This is useful when the server has specific type requirements (e.g., OpenPLC)
func convertToVariantByOpcUaType(value interface{}, opcuaType string) (*ua.Variant, error) {
	// Convert boolean true/false to various integer types for compatibility
	boolVal, isBool := toBool(value)

	switch opcuaType {
	case "Boolean":
		if isBool {
			return ua.NewVariant(boolVal)
		}
		return ua.NewVariant(value)
	case "SByte": // Signed 8-bit integer
		if isBool {
			var val int8
			if boolVal {
				val = 1
			}
			return ua.NewVariant(val)
		}
		return ua.NewVariant(value)
	case "Byte": // Unsigned 8-bit integer
		if isBool {
			var val uint8
			if boolVal {
				val = 1
			}
			return ua.NewVariant(val)
		}
		return ua.NewVariant(value)
	case "Int16", "Int16[]":
		if isBool {
			var val int16
			if boolVal {
				val = 1
			}
			return ua.NewVariant(val)
		}
		return ua.NewVariant(value)
	case "UInt16", "UInt16[]":
		if isBool {
			var val uint16
			if boolVal {
				val = 1
			}
			return ua.NewVariant(val)
		}
		return ua.NewVariant(value)
	case "Int32", "Int32[]", "Int":
		if isBool {
			var val int32
			if boolVal {
				val = 1
			}
			return ua.NewVariant(val)
		}
		return ua.NewVariant(value)
	case "UInt32", "UInt32[]":
		if isBool {
			var val uint32
			if boolVal {
				val = 1
			}
			return ua.NewVariant(val)
		}
		return ua.NewVariant(value)
	case "Float", "Float[]":
		f, ok := toFloat32(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %v to float32", value)
		}
		return ua.NewVariant(f)
	case "Double", "Double[]":
		f, ok := toFloat64(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %v to float64", value)
		}
		return ua.NewVariant(f)
	case "String":
		s := fmt.Sprintf("%v", value)
		return ua.NewVariant(s)
	default:
		// Fallback to standard conversion
		return ua.NewVariant(value)
	}
}

// MapOpcUaDataTypeToTagDataType converts OPC UA data type names to our system data types
func MapOpcUaDataTypeToTagDataType(opcuaType string) string {
	switch opcuaType {
	case "Boolean":
		return "BOOL"
	case "Int16", "UInt16", "SByte", "Byte":
		return "INT"
	case "Int32", "UInt32":
		return "DINT"
	case "Float":
		return "REAL"
	case "Double":
		return "REAL"
	case "String", "ByteString", "DateTime":
		return "STRING"
	default:
		return "REAL" // safe default
	}
}

// Helper type conversion functions
func toBool(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case float64:
		return val != 0, true
	case int:
		return val != 0, true
	default:
		return false, false
	}
}

func toInt16(v interface{}) (int16, bool) {
	switch val := v.(type) {
	case float64:
		return int16(val), true
	case int:
		return int16(val), true
	case int16:
		return val, true
	default:
		return 0, false
	}
}

func toInt32(v interface{}) (int32, bool) {
	switch val := v.(type) {
	case float64:
		return int32(val), true
	case int:
		return int32(val), true
	case int32:
		return val, true
	default:
		return 0, false
	}
}

func toFloat32(v interface{}) (float32, bool) {
	switch val := v.(type) {
	case float64:
		return float32(val), true
	case float32:
		return val, true
	case int:
		return float32(val), true
	default:
		return 0, false
	}
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int32:
		return float64(val), true
	case int16:
		return float64(val), true
	default:
		return 0, false
	}
}
