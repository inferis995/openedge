package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"time"
)

var (
	ErrNotConnected         = errors.New("not connected to Modbus device")
	ErrInvalidResponse      = errors.New("invalid Modbus response")
	ErrModbusException      = errors.New("Modbus exception response")
	ErrInvalidDataType      = errors.New("invalid data type")
	ErrInvalidAddressFormat = errors.New("invalid address format")
	ErrConnectionTimeout    = errors.New("connection timeout")
	ErrReadTimeout          = errors.New("read timeout")
)

// Config holds the Modbus TCP connection configuration
type Config struct {
	Host    string
	Port    int
	SlaveID byte
	Timeout time.Duration
}

// Client represents a Modbus TCP client
type Client struct {
	config     Config
	conn       net.Conn
	connected  bool
	connMu     sync.RWMutex
	transactionID uint16
}

// NewClient creates a new Modbus TCP client
func NewClient(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Client{
		config:        cfg,
		transactionID: 1,
	}
}

// NewClientFromConfig creates a Modbus client from a connection config map
func NewClientFromConfig(connConfig map[string]interface{}) (*Client, error) {
	host, _ := connConfig["ip"].(string)
	if host == "" {
		return nil, errors.New("missing 'ip' in connection config")
	}

	var port int
	if p, ok := connConfig["port"].(float64); ok {
		port = int(p)
	} else {
		port = 502 // Default Modbus TCP port
	}

	var slaveID byte
	if s, ok := connConfig["slave_id"].(float64); ok {
		slaveID = byte(s)
	} else {
		slaveID = 1 // Default slave ID
	}

	return NewClient(Config{
		Host:    host,
		Port:    port,
		SlaveID: slaveID,
		Timeout: 5 * time.Second,
	}), nil
}

// Connect establishes a TCP connection to the Modbus device
func (c *Client) Connect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	address := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	conn, err := net.DialTimeout("tcp", address, c.config.Timeout)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionTimeout, err)
	}

	// Set read/write deadlines
	if err := conn.SetDeadline(time.Now().Add(c.config.Timeout)); err != nil {
		conn.Close()
		return err
	}

	c.conn = conn
	c.connected = true
	return nil
}

// ConnectWithRetry attempts to connect with retry logic
func (c *Client) ConnectWithRetry(maxRetries int, retryInterval time.Duration) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := c.Connect(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxRetries-1 {
			time.Sleep(retryInterval)
		}
	}
	return lastErr
}

// Disconnect closes the connection to the Modbus device
func (c *Client) Disconnect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		c.connected = false
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.connected && c.conn != nil
}

// nextTransactionID returns the next transaction ID
func (c *Client) nextTransactionID() uint16 {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	id := c.transactionID
	c.transactionID++
	if c.transactionID == 0 {
		c.transactionID = 1 // Skip 0 as it's not valid
	}
	return id
}

// sendRequest sends a Modbus request and receives the response
func (c *Client) sendRequest(functionCode byte, data []byte) ([]byte, error) {
	c.connMu.RLock()
	if !c.connected || c.conn == nil {
		c.connMu.RUnlock()
		return nil, ErrNotConnected
	}
	conn := c.conn
	c.connMu.RUnlock()

	// Update deadline for this transaction
	if err := conn.SetDeadline(time.Now().Add(c.config.Timeout)); err != nil {
		return nil, err
	}

	transactionID := c.nextTransactionID()

	// Build Modbus TCP MBAP header (7 bytes)
	// Transaction ID (2 bytes) + Protocol ID (2 bytes, always 0) + Length (2 bytes) + Unit ID (1 byte)
	length := byte(len(data) + 1) // +1 for unit ID
	mbap := make([]byte, 7)
	binary.BigEndian.PutUint16(mbap[0:2], transactionID)
	binary.BigEndian.PutUint16(mbap[2:4], 0) // Protocol ID
	binary.BigEndian.PutUint16(mbap[4:6], uint16(length))
	mbap[6] = c.config.SlaveID

	// Build complete request: MBAP + Function Code + Data
	request := make([]byte, 0, len(mbap)+1+len(data))
	request = append(request, mbap...)
	request = append(request, functionCode)
	request = append(request, data...)

	// Send request
	if _, err := conn.Write(request); err != nil {
		c.connected = false
		return nil, fmt.Errorf("write error: %w", err)
	}

	// Read response header (MBAP - 7 bytes)
	header := make([]byte, 7)
	if _, err := conn.Read(header); err != nil {
		c.connected = false
		return nil, fmt.Errorf("read header error: %w", err)
	}

	// Verify transaction ID
	respTransactionID := binary.BigEndian.Uint16(header[0:2])
	if respTransactionID != transactionID {
		return nil, fmt.Errorf("%w: transaction ID mismatch", ErrInvalidResponse)
	}

	// Get response length
	respLength := binary.BigEndian.Uint16(header[4:6])
	if respLength < 2 {
		return nil, fmt.Errorf("%w: invalid length", ErrInvalidResponse)
	}

	// Read rest of response (function code + data)
	remaining := int(respLength) - 1 // -1 for unit ID which is already in header
	response := make([]byte, remaining)
	if _, err := conn.Read(response); err != nil {
		c.connected = false
		return nil, fmt.Errorf("read response error: %w", err)
	}

	// Check for exception response
	if response[0]&0x80 != 0 {
		if len(response) < 2 {
			return nil, ErrModbusException
		}
		return nil, fmt.Errorf("%w: exception code %d", ErrModbusException, response[1])
	}

	// Verify function code matches
	if response[0] != functionCode {
		return nil, fmt.Errorf("%w: function code mismatch", ErrInvalidResponse)
	}

	return response[1:], nil // Return data part (skip function code)
}

// ReadHoldingRegister reads a single holding register (function code 0x03)
func (c *Client) ReadHoldingRegister(address uint16) (uint16, error) {
	data, err := c.readRegisters(0x03, address, 1)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

// ReadInputRegister reads a single input register (function code 0x04)
func (c *Client) ReadInputRegister(address uint16) (uint16, error) {
	data, err := c.readRegisters(0x04, address, 1)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

// readRegisters reads multiple registers using function code 0x03 or 0x04
func (c *Client) readRegisters(functionCode byte, address uint16, quantity uint16) ([]byte, error) {
	// Build request: starting address (2 bytes) + quantity (2 bytes)
	request := make([]byte, 4)
	binary.BigEndian.PutUint16(request[0:2], address)
	binary.BigEndian.PutUint16(request[2:4], quantity)

	response, err := c.sendRequest(functionCode, request)
	if err != nil {
		return nil, err
	}

	// Response format: byte count (1 byte) + register data
	if len(response) < 1 {
		return nil, fmt.Errorf("%w: no byte count", ErrInvalidResponse)
	}

	byteCount := int(response[0])
	if len(response) < 1+byteCount {
		return nil, fmt.Errorf("%w: incomplete data", ErrInvalidResponse)
	}

	return response[1:], nil
}

// ReadTag reads a tag value based on address and data type
// Address format for holding registers: HR{address} (e.g., HR100) or HR{address}.{bit} for BOOL (e.g., HR100.0)
// Address format for input registers: IR{address} (e.g., IR100) or IR{address}.{bit} for BOOL (e.g., IR100.0)
// Data types supported: INT, BOOL, REAL
func (c *Client) ReadTag(address string, dataType string) (interface{}, error) {
	addrType, addrOffset, bitOffset, err := parseAddress(address)
	if err != nil {
		return nil, err
	}

	switch dataType {
	case "INT":
		return c.readInt(addrType, addrOffset)
	case "BOOL":
		if bitOffset < 0 {
			return nil, fmt.Errorf("%w: BOOL address must include bit offset (e.g., HR100.0)", ErrInvalidAddressFormat)
		}
		return c.readBool(addrType, addrOffset, bitOffset)
	case "REAL":
		return c.readReal(addrType, addrOffset)
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidDataType, dataType)
	}
}

// parseAddress parses the address string and returns type, offset, and bit offset
func parseAddress(address string) (string, uint16, int8, error) {
	if len(address) < 3 {
		return "", 0, -1, ErrInvalidAddressFormat
	}

	prefix := address[:2]
	offsetStr := address[2:]

	var addrOffset uint16
	var bitOffset int8 = -1

	// Check for bit offset in BOOL addresses (e.g., HR100.0)
	dotIdx := -1
	for i, ch := range offsetStr {
		if ch == '.' {
			dotIdx = i
			break
		}
	}

	if dotIdx >= 0 {
		// Parse bit offset
		if _, err := fmt.Sscanf(offsetStr[dotIdx+1:], "%d", &bitOffset); err != nil || bitOffset < 0 || bitOffset > 15 {
			return "", 0, -1, ErrInvalidAddressFormat
		}
		offsetStr = offsetStr[:dotIdx]
	}

	if _, err := fmt.Sscanf(offsetStr, "%d", &addrOffset); err != nil {
		return "", 0, -1, ErrInvalidAddressFormat
	}

	switch prefix {
	case "HR", "hr":
		return "holding", addrOffset, bitOffset, nil
	case "IR", "ir":
		return "input", addrOffset, bitOffset, nil
	default:
		return "", 0, -1, ErrInvalidAddressFormat
	}
}

// readInt reads a 16-bit signed integer
func (c *Client) readInt(addrType string, address uint16) (int16, error) {
	var value uint16
	var err error

	switch addrType {
	case "holding":
		value, err = c.ReadHoldingRegister(address)
	case "input":
		value, err = c.ReadInputRegister(address)
	default:
		return 0, ErrInvalidAddressFormat
	}

	if err != nil {
		return 0, err
	}

	return int16(value), nil
}

// readBool reads a boolean value from a single bit in a register
// Format: HR100.0 = bit 0 of holding register 100, HR100.15 = bit 15
func (c *Client) readBool(addrType string, regAddr uint16, bitOffset int8) (bool, error) {
	var value uint16
	var err error

	switch addrType {
	case "holding":
		value, err = c.ReadHoldingRegister(regAddr)
	case "input":
		value, err = c.ReadInputRegister(regAddr)
	default:
		return false, ErrInvalidAddressFormat
	}

	if err != nil {
		return false, err
	}

	// Extract the bit
	bitValue := (value >> bitOffset) & 1
	return bitValue == 1, nil
}

// readReal reads a 32-bit float (REAL) from two consecutive registers
func (c *Client) readReal(addrType string, address uint16) (float32, error) {
	var data []byte
	var err error

	// Read 2 registers (4 bytes)
	switch addrType {
	case "holding":
		data, err = c.readRegisters(0x03, address, 2)
	case "input":
		data, err = c.readRegisters(0x04, address, 2)
	default:
		return 0, ErrInvalidAddressFormat
	}

	if err != nil {
		return 0, err
	}

	if len(data) < 4 {
		return 0, fmt.Errorf("%w: insufficient data for REAL", ErrInvalidResponse)
	}

	// Convert big-endian uint32 to float32
	bits := binary.BigEndian.Uint32(data[0:4])
	value := math.Float32frombits(bits)

	return value, nil
}

// ReadMultipleTags reads multiple tags for batch operations
func (c *Client) ReadMultipleTags(addresses []string, dataTypes []string) ([]interface{}, error) {
	if len(addresses) != len(dataTypes) {
		return nil, errors.New("addresses and dataTypes must have same length")
	}

	results := make([]interface{}, len(addresses))
	for i, addr := range addresses {
		value, err := c.ReadTag(addr, dataTypes[i])
		if err != nil {
			return nil, fmt.Errorf("error reading tag %s: %w", addr, err)
		}
		results[i] = value
	}

	return results, nil
}
