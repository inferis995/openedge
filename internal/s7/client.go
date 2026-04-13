package s7

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sync"
	"time"
)

// DataType represents the supported S7 data types
type DataType string

const (
	DataTypeINT  DataType = "INT"
	DataTypeREAL DataType = "REAL"
	DataTypeBOOL DataType = "BOOL"
	DataTypeDINT DataType = "DINT"
)

// S7 Protocol Constants
const (
	// S7 Areas
	areaDB = 0x84 // Data blocks
	areaMK = 0x83 // Merker (Memory)
	areaPE = 0x81 // Inputs
	areaPA = 0x82 // Outputs

	// TPKT Header
	tpktVersion  = 0x03
	tpktReserved = 0x00

	// COTP Connect
	cotpConnectRequest = 0xE0
	cotpConnectConfirm = 0xD0
	cotpDataTransfer   = 0xF0

	// S7 PDU Types
	s7PDUJobRequest = 0x01
	s7PDUAck        = 0x02
	s7PDUAckData    = 0x03
	s7PDUUserData   = 0x07

	// S7 Functions
	s7FunctionSetupComm = 0xF0
	s7FunctionReadVar   = 0x04
	s7FunctionWriteVar  = 0x05

	// Transport sizes
	s7TransportBit   = 0x01
	s7TransportByte  = 0x02
	s7TransportWord  = 0x04
	s7TransportDWord = 0x06
	s7TransportReal  = 0x08
)

// Config holds the S7 client configuration
type Config struct {
	IP            string
	Rack          int
	Slot          int
	Port          int
	ConnectRetry  int
	RetryInterval time.Duration
	Timeout       time.Duration
}

// TagValue represents a read value from the PLC
type TagValue struct {
	Value   interface{}
	Quality int // 0 = good, 1 = bad
	Error   error
}

// Client manages the S7 PLC connection
type Client struct {
	config      Config
	conn        net.Conn
	mu          sync.Mutex
	connected   bool
	pduLength   int
	sequenceNum uint16
}

// NewClient creates a new S7 client with the given configuration
func NewClient(cfg Config) *Client {
	if cfg.Port == 0 {
		cfg.Port = 102 // Default S7 port
	}
	if cfg.ConnectRetry <= 0 {
		cfg.ConnectRetry = 3
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 5 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	return &Client{
		config:    cfg,
		connected: false,
		pduLength: 480, // Default PDU size
	}
}

// NewClientFromConfig creates a new S7 client from a connection config map
func NewClientFromConfig(connConfig map[string]interface{}) (*Client, error) {
	ip, ok := connConfig["ip"].(string)
	if !ok || ip == "" {
		// fallback: UI may send ip_address instead of ip
		ip, ok = connConfig["ip_address"].(string)
		if !ok || ip == "" {
			// fallback: some configs use host
			ip, ok = connConfig["host"].(string)
			if !ok || ip == "" {
				return nil, fmt.Errorf("missing or invalid 'ip' in connection config")
			}
		}
	}

	rack := 0
	if r, ok := connConfig["rack"].(float64); ok {
		rack = int(r)
	} else if r, ok := connConfig["rack"].(int); ok {
		rack = r
	}

	slot := 1
	if s, ok := connConfig["slot"].(float64); ok {
		slot = int(s)
	} else if s, ok := connConfig["slot"].(int); ok {
		slot = s
	}

	port := 102
	if p, ok := connConfig["port"].(float64); ok {
		port = int(p)
	} else if p, ok := connConfig["port"].(int); ok {
		port = p
	}

	cfg := Config{
		IP:            ip,
		Rack:          rack,
		Slot:          slot,
		Port:          port,
		ConnectRetry:  3,
		RetryInterval: 5 * time.Second,
		Timeout:       10 * time.Second,
	}

	return NewClient(cfg), nil
}

// Connect establishes connection to the S7 PLC
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	log.Printf("Connecting to S7 PLC at %s:%d (rack=%d, slot=%d)...",
		c.config.IP, c.config.Port, c.config.Rack, c.config.Slot)

	// Establish TCP connection
	addr := fmt.Sprintf("%s:%d", c.config.IP, c.config.Port)
	conn, err := net.DialTimeout("tcp", addr, c.config.Timeout)
	if err != nil {
		return fmt.Errorf("failed to connect to S7 PLC at %s: %w", addr, err)
	}
	c.conn = conn

	// Set connection timeouts
	c.conn.SetDeadline(time.Now().Add(c.config.Timeout))

	// COTP Connection Request
	if err := c.cotpConnect(); err != nil {
		c.conn.Close()
		return fmt.Errorf("COTP connect failed: %w", err)
	}

	// S7 Communication Setup
	if err := c.s7SetupCommunication(); err != nil {
		c.conn.Close()
		return fmt.Errorf("S7 setup communication failed: %w", err)
	}

	c.connected = true
	c.conn.SetDeadline(time.Time{}) // Remove deadline for normal operations

	log.Printf("Connected to S7 PLC at %s", addr)
	return nil
}

// cotpConnect sends COTP connection request
func (c *Client) cotpConnect() error {
	// Calculate TSAP values based on rack and slot
	// Local TSAP: 0x0100 (typically for programmer)
	// Remote TSAP: 0x01 + (rack * 0x20) + slot
	remoteTSAP := byte(c.config.Rack*0x20 + c.config.Slot)

	// COTP Connection Request PDU
	cotpCR := []byte{
		// TPKT Header
		tpktVersion, tpktReserved, 0x00, 0x16, // Length = 22 bytes total
		// COTP Header
		0x11,               // Length indicator
		cotpConnectRequest, // COTP type: Connect Request
		0x00, 0x00,         // Destination reference
		0x00, 0x01, // Source reference
		0x00, // Class and options
		// Parameters
		0xC0, 0x01, 0x0A, // TPDU size (1024 bytes)
		0xC1, 0x02, 0x01, 0x00, // Source TSAP
		0xC2, 0x02, 0x01, remoteTSAP, // Destination TSAP
	}

	if _, err := c.conn.Write(cotpCR); err != nil {
		return fmt.Errorf("failed to send COTP connect request: %w", err)
	}

	// Read COTP Connect Confirm
	response := make([]byte, 256)
	n, err := c.conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read COTP connect confirm: %w", err)
	}

	if n < 6 {
		return fmt.Errorf("COTP response too short: %d bytes", n)
	}

	// Verify COTP Connect Confirm (byte 5 should be 0xD0)
	if response[5] != cotpConnectConfirm {
		return fmt.Errorf("unexpected COTP response type: 0x%02X", response[5])
	}

	return nil
}

// s7SetupCommunication negotiates the PDU size with the PLC
func (c *Client) s7SetupCommunication() error {
	// S7 Setup Communication PDU
	setupPDU := []byte{
		// TPKT Header
		tpktVersion, tpktReserved, 0x00, 0x19, // Length = 25 bytes
		// COTP Header
		0x02,             // Length indicator
		cotpDataTransfer, // COTP type: Data
		0x80,             // Last data unit
		// S7 Header
		0x32,            // Protocol ID
		s7PDUJobRequest, // PDU type: Job Request
		0x00, 0x00,      // Reserved
		0x00, 0x00, // PDU reference
		0x00, 0x08, // Parameter length
		0x00, 0x00, // Data length
		// S7 Parameters (Setup Communication)
		s7FunctionSetupComm, // Function
		0x00,                // Reserved
		0x00, 0x01,          // Max AmQ calling
		0x00, 0x01, // Max AmQ called
		0x01, 0xE0, // PDU length (480 bytes)
	}

	if _, err := c.conn.Write(setupPDU); err != nil {
		return fmt.Errorf("failed to send S7 setup: %w", err)
	}

	// Read response
	response := make([]byte, 512)
	n, err := c.conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read S7 setup response: %w", err)
	}

	if n < 19 {
		return fmt.Errorf("S7 setup response too short: %d bytes", n)
	}

	// Extract negotiated PDU length (last 2 bytes of parameters)
	c.pduLength = int(binary.BigEndian.Uint16(response[n-2:]))
	if c.pduLength < 200 {
		c.pduLength = 200 // Minimum PDU size
	}

	log.Printf("S7 PDU size negotiated: %d bytes", c.pduLength)
	return nil
}

// ConnectWithRetry attempts to connect with retry logic
func (c *Client) ConnectWithRetry() error {
	var lastErr error
	for i := 0; i < c.config.ConnectRetry; i++ {
		if err := c.Connect(); err != nil {
			lastErr = err
			log.Printf("Connection attempt %d/%d failed: %v", i+1, c.config.ConnectRetry, err)
			if i < c.config.ConnectRetry-1 {
				time.Sleep(c.config.RetryInterval)
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to connect after %d attempts: %w", c.config.ConnectRetry, lastErr)
}

// Disconnect closes the connection to the S7 PLC
func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return
	}

	if c.conn != nil {
		c.conn.Close()
	}
	c.connected = false
	log.Printf("Disconnected from S7 PLC at %s", c.config.IP)
}

// IsConnected returns whether the client is currently connected
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// ReadTag reads a value from the PLC based on address and data type
// Address format: DB<number>.DB<type><offset> (e.g., "DB1.DBW0", "DB1.DBD4", "DB1.DBX0.0")
// Also supports: MW<address>, IW<address>, QW<address> for direct memory access
func (c *Client) ReadTag(address string, dataType DataType) TagValue {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return TagValue{Value: nil, Quality: 1, Error: fmt.Errorf("not connected to PLC")}
	}

	// Parse the address
	area, dbNumber, start, bitOffset, size, err := parseAddress(address, dataType)
	if err != nil {
		return TagValue{Value: nil, Quality: 1, Error: err}
	}

	// Set timeout for read operation
	c.conn.SetDeadline(time.Now().Add(c.config.Timeout))
	defer c.conn.SetDeadline(time.Time{})

	// Read from PLC
	buffer, err := c.readArea(area, dbNumber, start, size, dataType)
	if err != nil {
		c.handleConnectionError(err)
		return TagValue{Value: nil, Quality: 1, Error: fmt.Errorf("read error: %w", err)}
	}

	// Convert buffer to value
	value, err := convertBufferToValue(buffer, dataType, bitOffset)
	if err != nil {
		return TagValue{Value: nil, Quality: 1, Error: err}
	}

	return TagValue{Value: value, Quality: 0, Error: nil}
}

// readArea reads data from a specific memory area
func (c *Client) readArea(area int, dbNumber int, start int, size int, dataType DataType) ([]byte, error) {
	c.sequenceNum++

	// Determine transport size for request
	transportSize := byte(s7TransportByte)
	switch dataType {
	case DataTypeBOOL:
		transportSize = s7TransportBit
	case DataTypeINT:
		transportSize = s7TransportWord
	case DataTypeDINT:
		transportSize = s7TransportDWord
	case DataTypeREAL:
		transportSize = s7TransportReal
	}

	// Calculate bit address for S7 protocol
	// For non-bit access, multiply byte offset by 8
	bitAddress := start * 8
	if dataType == DataTypeBOOL {
		// For BOOL, start is already byte offset, need to parse bit offset separately
		// This is handled in the caller
	}

	// Build S7 Read Request
	paramLength := 14 // Read item parameter length
	dataLength := 0

	// Calculate total TPKT length
	tpktLength := 4 + 3 + 10 + paramLength + dataLength // TPKT + COTP + S7 Header + Params + Data

	request := make([]byte, tpktLength)

	// TPKT Header
	request[0] = tpktVersion
	request[1] = tpktReserved
	binary.BigEndian.PutUint16(request[2:], uint16(tpktLength))

	// COTP Header
	request[4] = 0x02
	request[5] = cotpDataTransfer
	request[6] = 0x80

	// S7 Header
	request[7] = 0x32                                             // Protocol ID
	request[8] = s7PDUJobRequest                                  // PDU type
	request[9] = 0x00                                             // Reserved
	request[10] = 0x00                                            // Reserved
	binary.BigEndian.PutUint16(request[11:], c.sequenceNum)       // PDU reference
	binary.BigEndian.PutUint16(request[13:], uint16(paramLength)) // Parameter length
	binary.BigEndian.PutUint16(request[15:], 0)                   // Data length

	// S7 Read Parameters
	request[17] = s7FunctionReadVar // Function: Read Var
	request[18] = 0x01              // Item count

	// Read Item
	request[19] = 0x12                                         // Specification type
	request[20] = 0x0A                                         // Length of this item
	request[21] = 0x10                                         // Syntax ID: S7ANY
	request[22] = transportSize                                // Transport size
	binary.BigEndian.PutUint16(request[23:], uint16(size))     // Length
	binary.BigEndian.PutUint16(request[25:], uint16(dbNumber)) // DB Number
	request[27] = byte(area)                                   // Area
	// Address (3 bytes, big endian, bit address)
	request[28] = byte(bitAddress >> 16)
	request[29] = byte(bitAddress >> 8)
	request[30] = byte(bitAddress)

	// Send request
	if _, err := c.conn.Write(request); err != nil {
		return nil, fmt.Errorf("failed to send read request: %w", err)
	}

	// Read response
	response := make([]byte, 512)
	n, err := c.conn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if n < 21 {
		return nil, fmt.Errorf("response too short: %d bytes", n)
	}

	// Check S7 response
	pduType := response[8]
	if pduType != s7PDUAckData {
		return nil, fmt.Errorf("unexpected PDU type: 0x%02X", pduType)
	}

	// Check error class/code
	errorClass := response[17]
	errorCode := response[18]
	if errorClass != 0 || errorCode != 0 {
		return nil, fmt.Errorf("S7 error: class=0x%02X code=0x%02X", errorClass, errorCode)
	}

	// Parse data response
	// Data starts after S7 header + parameter header
	paramLen := binary.BigEndian.Uint16(response[13:])
	dataStart := 19 + int(paramLen)

	if dataStart+4 > n {
		return nil, fmt.Errorf("data response too short")
	}

	// Check return code in data
	returnCode := response[dataStart]
	if returnCode != 0xFF { // 0xFF = success
		return nil, fmt.Errorf("read failed with return code: 0x%02X", returnCode)
	}

	// Get transport size and length
	// transportSizeResp := response[dataStart+1]
	dataLen := binary.BigEndian.Uint16(response[dataStart+2:])

	// S7 response length is always in bits — convert to bytes
	dataLen = (dataLen + 7) / 8

	// Extract data
	dataOffset := dataStart + 4
	if dataOffset+int(dataLen) > n {
		return nil, fmt.Errorf("insufficient data in response")
	}

	return response[dataOffset : dataOffset+int(dataLen)], nil
}

// ReadMultipleTags reads multiple tags (one at a time for simplicity)
func (c *Client) ReadMultipleTags(addresses []string, dataTypes []DataType) []TagValue {
	if len(addresses) != len(dataTypes) {
		result := make([]TagValue, len(addresses))
		for i := range result {
			result[i] = TagValue{Value: nil, Quality: 1, Error: fmt.Errorf("address/dataType count mismatch")}
		}
		return result
	}

	results := make([]TagValue, len(addresses))
	for i, addr := range addresses {
		results[i] = c.ReadTag(addr, dataTypes[i])
	}
	return results
}

// handleConnectionError handles connection errors and marks client as disconnected
func (c *Client) handleConnectionError(err error) {
	if err == io.EOF || isConnectionError(err) {
		log.Printf("S7 connection error: %v, marking as disconnected", err)
		c.connected = false
		if c.conn != nil {
			c.conn.Close()
		}
	}
}

// isConnectionError checks if the error indicates a connection problem
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*net.OpError); ok {
		return true
	}
	return false
}

// parseAddress parses an S7 address string and returns area, db number, start offset, bit offset, and size
func parseAddress(address string, dataType DataType) (area int, dbNumber int, start int, bitOffset int, size int, err error) {
	// Determine size based on data type
	switch dataType {
	case DataTypeBOOL:
		size = 1
	case DataTypeINT:
		size = 2
	case DataTypeDINT, DataTypeREAL:
		size = 4
	default:
		err = fmt.Errorf("unsupported data type: %s", dataType)
		return
	}

	// Parse DB address format: DB<n>.DB<type><offset>[.bit]
	var n int
	if _, scanErr := fmt.Sscanf(address, "DB%d.DB", &n); scanErr == nil {
		area = areaDB
		dbNumber = n

		// Find the type and offset part
		dotIndex := -1
		for i := 0; i < len(address); i++ {
			if address[i] == '.' {
				dotIndex = i
				break
			}
		}
		if dotIndex == -1 || dotIndex+4 > len(address) {
			err = fmt.Errorf("invalid DB address format: %s", address)
			return
		}

		typeChar := address[dotIndex+3]
		offsetStr := address[dotIndex+4:]

		switch typeChar {
		case 'X': // Bit address: DB1.DBX0.0
			var byteOff, bitOff int
			if _, scanErr := fmt.Sscanf(offsetStr, "%d.%d", &byteOff, &bitOff); scanErr != nil {
				err = fmt.Errorf("invalid bit address format: %s", address)
				return
			}
			start = byteOff
			bitOffset = bitOff
			size = 1
		case 'B': // Byte: DB1.DBB0
			if _, scanErr := fmt.Sscanf(offsetStr, "%d", &start); scanErr != nil {
				err = fmt.Errorf("invalid byte address format: %s", address)
				return
			}
			size = 1
		case 'W': // Word (INT): DB1.DBW0
			if _, scanErr := fmt.Sscanf(offsetStr, "%d", &start); scanErr != nil {
				err = fmt.Errorf("invalid word address format: %s", address)
				return
			}
			size = 2
		case 'D': // Double word (DINT/REAL): DB1.DBD0
			if _, scanErr := fmt.Sscanf(offsetStr, "%d", &start); scanErr != nil {
				err = fmt.Errorf("invalid double word address format: %s", address)
				return
			}
			size = 4
		default:
			err = fmt.Errorf("unknown DB type specifier: %c", typeChar)
			return
		}
		return
	}

	// Parse M (Merker/Memory) address: MW<offset>, MD<offset>, M<offset>.<bit>
	if len(address) >= 2 && address[0] == 'M' {
		area = areaMK
		dbNumber = 0
		start, bitOffset, size, err = parseDirectAddress(address[1:])
		return
	}

	// Parse I (Input) address: IW<offset>, ID<offset>, I<offset>.<bit>
	if len(address) >= 2 && address[0] == 'I' {
		area = areaPE
		dbNumber = 0
		start, bitOffset, size, err = parseDirectAddress(address[1:])
		return
	}

	// Parse Q (Output) address: QW<offset>, QD<offset>, Q<offset>.<bit>
	if len(address) >= 2 && address[0] == 'Q' {
		area = areaPA
		dbNumber = 0
		start, bitOffset, size, err = parseDirectAddress(address[1:])
		return
	}

	err = fmt.Errorf("unsupported address format: %s", address)
	return
}

// parseDirectAddress parses the offset part of direct memory addresses (M, I, Q)
func parseDirectAddress(addr string) (start int, bitOffset int, size int, err error) {
	if len(addr) == 0 {
		err = fmt.Errorf("empty address offset")
		return
	}

	switch addr[0] {
	case 'W': // Word
		if _, scanErr := fmt.Sscanf(addr[1:], "%d", &start); scanErr != nil {
			err = fmt.Errorf("invalid word address: %s", addr)
			return
		}
		size = 2
	case 'D': // Double word
		if _, scanErr := fmt.Sscanf(addr[1:], "%d", &start); scanErr != nil {
			err = fmt.Errorf("invalid dword address: %s", addr)
			return
		}
		size = 4
	case 'B': // Byte
		if _, scanErr := fmt.Sscanf(addr[1:], "%d", &start); scanErr != nil {
			err = fmt.Errorf("invalid byte address: %s", addr)
			return
		}
		size = 1
	default:
		// Bit address: <byte>.<bit>
		var byteOff, bitOff int
		if _, scanErr := fmt.Sscanf(addr, "%d.%d", &byteOff, &bitOff); scanErr != nil {
			// Try just byte offset
			if _, scanErr := fmt.Sscanf(addr, "%d", &byteOff); scanErr != nil {
				err = fmt.Errorf("invalid address offset: %s", addr)
				return
			}
		} else {
			bitOffset = bitOff
		}
		start = byteOff
		size = 1
	}
	return
}

// convertBufferToValue converts raw bytes to the appropriate Go type
func convertBufferToValue(buffer []byte, dataType DataType, bitOffset int) (interface{}, error) {
	switch dataType {
	case DataTypeBOOL:
		if len(buffer) < 1 {
			return nil, fmt.Errorf("buffer too small for BOOL")
		}
		// Extract bit at specified offset
		return (buffer[0] & (1 << bitOffset)) != 0, nil

	case DataTypeINT:
		if len(buffer) < 2 {
			return nil, fmt.Errorf("buffer too small for INT")
		}
		// S7 uses big-endian
		return int16(binary.BigEndian.Uint16(buffer)), nil

	case DataTypeDINT:
		if len(buffer) < 4 {
			return nil, fmt.Errorf("buffer too small for DINT")
		}
		return int32(binary.BigEndian.Uint32(buffer)), nil

	case DataTypeREAL:
		if len(buffer) < 4 {
			return nil, fmt.Errorf("buffer too small for REAL")
		}
		bits := binary.BigEndian.Uint32(buffer)
		return math.Float32frombits(bits), nil

	default:
		return nil, fmt.Errorf("unsupported data type: %s", dataType)
	}
}

// WriteTag writes a value to the PLC based on address and data type
func (c *Client) WriteTag(address string, dataType DataType, value interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected to PLC")
	}

	// Parse the address
	area, dbNumber, start, bitOffset, size, err := parseAddress(address, dataType)
	if err != nil {
		return err
	}

	// Convert value to buffer
	buffer, err := convertValueToBuffer(value, dataType, size)
	if err != nil {
		return err
	}

	// Perform write
	c.conn.SetDeadline(time.Now().Add(c.config.Timeout))
	defer c.conn.SetDeadline(time.Time{})

	if err := c.writeArea(area, dbNumber, start, bitOffset, buffer, dataType); err != nil {
		c.handleConnectionError(err)
		return fmt.Errorf("write error: %w", err)
	}

	return nil
}

// writeArea writes data to a specific memory area
func (c *Client) writeArea(area int, dbNumber int, start int, bitOffset int, data []byte, dataType DataType) error {
	c.sequenceNum++

	// Determine transport size for request
	transportSize := byte(s7TransportByte)
	if dataType == DataTypeBOOL {
		transportSize = s7TransportBit
	} else if dataType == DataTypeINT {
		transportSize = s7TransportWord
	} else if dataType == DataTypeDINT {
		transportSize = s7TransportDWord
	} else if dataType == DataTypeREAL {
		transportSize = s7TransportReal
	}

	// Calculate bit address
	bitAddress := start * 8
	if dataType == DataTypeBOOL {
		bitAddress += bitOffset
	}

	// Build S7 Write Request
	paramLength := 14
	dataLength := 4 + len(data) // Data header (4) + data

	// TPKT + COTP + S7 Header + Params + Data
	tpktLength := 4 + 3 + 12 + paramLength + dataLength

	request := make([]byte, tpktLength)

	// TPKT Header
	request[0] = tpktVersion
	request[1] = tpktReserved
	binary.BigEndian.PutUint16(request[2:], uint16(tpktLength))

	// COTP Header
	request[4] = 0x02
	request[5] = cotpDataTransfer
	request[6] = 0x80

	// S7 Header
	request[7] = 0x32
	request[8] = s7PDUJobRequest
	request[9] = 0x00
	request[10] = 0x00
	binary.BigEndian.PutUint16(request[11:], c.sequenceNum)
	binary.BigEndian.PutUint16(request[13:], uint16(paramLength))
	binary.BigEndian.PutUint16(request[15:], uint16(dataLength))

	// S7 Write Parameters
	request[17] = s7FunctionWriteVar
	request[18] = 0x01 // Item count

	// Write Item Specification
	request[19] = 0x12
	request[20] = 0x0A
	request[21] = 0x10
	request[22] = transportSize
	// Length in bits or bytes depending on transport size?
	// For Write Var, length is typically 1 (item count) for basic types, or byte count
	// Standard S7: Length is always number of items (bits/bytes/words)
	// For specific types (REAL, INT, DINT), we are writing 1 unit
	writeLen := 1
	if dataType == DataTypeBOOL {
		writeLen = 1 // 1 bit
	} else if dataType == DataTypeINT {
		writeLen = 1 // 1 word
	} else if dataType == DataTypeDINT || dataType == DataTypeREAL {
		writeLen = 1 // 1 dword
	}
	binary.BigEndian.PutUint16(request[23:], uint16(writeLen))

	binary.BigEndian.PutUint16(request[25:], uint16(dbNumber))
	request[27] = byte(area)
	request[28] = byte(bitAddress >> 16)
	request[29] = byte(bitAddress >> 8)
	request[30] = byte(bitAddress)

	// Data Section
	dataOffset := 31           // End of params
	request[dataOffset] = 0xFF // Return code (0xFF for request)

	// Transport Size in Data Header
	// For bit access: 0x03, byte/word/dword: 0x04
	if dataType == DataTypeBOOL {
		request[dataOffset+1] = 0x03
	} else {
		request[dataOffset+1] = 0x04
	}

	// Length in bits for data section
	bitsLen := len(data) * 8
	binary.BigEndian.PutUint16(request[dataOffset+2:], uint16(bitsLen))

	// Payload
	copy(request[dataOffset+4:], data)

	// Send request
	if _, err := c.conn.Write(request); err != nil {
		return fmt.Errorf("failed to send write request: %w", err)
	}

	// Read response
	response := make([]byte, 512)
	n, err := c.conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if n < 10 { // Minimal response check
		return fmt.Errorf("response too short")
	}

	// Check S7 response
	if response[8] != s7PDUAckData {
		return fmt.Errorf("unexpected PDU type: 0x%02X", response[8])
	}

	// Check error class
	if response[17] != 0 || response[18] != 0 {
		return fmt.Errorf("S7 error: class=0x%02X code=0x%02X", response[17], response[18])
	}

	// Check item return code
	// Param length
	paramLen := binary.BigEndian.Uint16(response[13:])
	// Data starts after S7 header (19 bytes) + params
	itemReturnCodeOffset := 19 + int(paramLen)
	if itemReturnCodeOffset >= n {
		return fmt.Errorf("invalid response format")
	}

	if response[itemReturnCodeOffset] != 0xFF {
		return fmt.Errorf("write item failed with code: 0x%02X", response[itemReturnCodeOffset])
	}

	return nil
}

// convertValueToBuffer converts Go value to byte buffer for S7
func convertValueToBuffer(value interface{}, dataType DataType, size int) ([]byte, error) {
	buffer := make([]byte, size)

	switch dataType {
	case DataTypeBOOL:
		val := false
		if v, ok := value.(bool); ok {
			val = v
		} else if v, ok := value.(float64); ok { // JSON float64
			val = v != 0
		} else if v, ok := value.(int); ok {
			val = v != 0
		} else {
			return nil, fmt.Errorf("value is not boolean")
		}

		if val {
			buffer[0] = 1
		} else {
			buffer[0] = 0
		}
	case DataTypeINT:
		var val int16
		if v, ok := value.(float64); ok {
			val = int16(v)
		} else if v, ok := value.(int); ok {
			val = int16(v)
		} else {
			return nil, fmt.Errorf("value is not int")
		}
		binary.BigEndian.PutUint16(buffer, uint16(val))
	case DataTypeDINT:
		var val int32
		if v, ok := value.(float64); ok {
			val = int32(v)
		} else if v, ok := value.(int); ok {
			val = int32(v)
		} else {
			return nil, fmt.Errorf("value is not dint")
		}
		binary.BigEndian.PutUint32(buffer, uint32(val))
	case DataTypeREAL:
		var val float32
		if v, ok := value.(float64); ok {
			val = float32(v)
		} else if v, ok := value.(float32); ok {
			val = v
		} else {
			return nil, fmt.Errorf("value is not real")
		}
		bits := math.Float32bits(val)
		binary.BigEndian.PutUint32(buffer, bits)
	default:
		return nil, fmt.Errorf("unsupported write data type")
	}
	return buffer, nil
}
