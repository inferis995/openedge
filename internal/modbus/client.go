package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simonvetter/modbus"
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

// Client represents a Modbus TCP client wrapper around simonvetter/modbus
type Client struct {
	config Config
	client *modbus.ModbusClient
	mu     sync.Mutex // Protects connection state
}

// NewClient creates a new Modbus TCP client
func NewClient(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Client{
		config: cfg,
	}
}

// NewClientFromConfig creates a Modbus client from a connection config map
func NewClientFromConfig(connConfig map[string]interface{}) (*Client, error) {
	host, _ := connConfig["ip"].(string)
	if host == "" {
		host, _ = connConfig["ip_address"].(string)
	}
	if host == "" {
		return nil, errors.New("missing 'ip' or 'ip_address' in connection config")
	}
	host = strings.TrimSpace(host)

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
	c.mu.Lock()
	defer c.mu.Unlock()

	// simonvetter/modbus manages connection state internally, but Open() re-opens or errors?
	// The library doesn't expose IsConnected. We should just try to Open() or handle errors.
	// For this wrapper, let's just proceed to Open().

	url := fmt.Sprintf("tcp://%s:%d", c.config.Host, c.config.Port)
	client, err := modbus.NewClient(&modbus.ClientConfiguration{
		URL:      url,
		Timeout:  c.config.Timeout,
		Speed:    19200, // Default for RTU, ignored for TCP but good to set
		DataBits: 8,     // Default
		Parity:   modbus.PARITY_NONE,
		StopBits: 1,
	})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	if err := client.Open(); err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionTimeout, err)
	}

	client.SetUnitId(uint8(c.config.SlaveID))

	c.client = client
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		if err := c.client.Close(); err != nil {
			return err
		}
		c.client = nil
	}
	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client != nil
	// Note: simonvetter doesn't expose IsConnected() on the interface easily without locking issues or internal checks?
	// Actually we should trust our own state or simple check.
	// But let's just check if client is not nil.
	// The library manages connection state internally.
}

// ReadCoils reads coils (function code 0x01)
func (c *Client) ReadCoils(address uint16, quantity uint16) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil, ErrNotConnected
	}

	// ReadCoils returns []byte where each byte = 1 coil if we want raw bytes?
	// simonvetter: ReadCoils(addr, quantity) returns ([]bool, error)
	// We need to convert []bool to []byte (packed) to match existing interface?
	// Existing interface used to return []byte.
	// Let's check how driver-modbus uses it.
	// driver-modbus calls: data, err := c.ReadCoils(...).
	// Then it passes 'data' to parseValue.
	// parseValue expects []byte.
	// Internal logic was: return response[1:] (byte count + data).
	// Actually response[1] is byte count.
	// Whatever, let's keep it simple. If usage expects []byte packed, lets pack it.

	bits, err := c.client.ReadCoils(address, quantity)
	if err != nil {
		return nil, err
	}

	return boolsToBytes(bits), nil
}

// ReadDiscreteInputs reads discrete inputs (function code 0x02)
func (c *Client) ReadDiscreteInputs(address uint16, quantity uint16) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil, ErrNotConnected
	}

	bits, err := c.client.ReadDiscreteInputs(address, quantity)
	if err != nil {
		return nil, err
	}

	return boolsToBytes(bits), nil
}

// ReadHoldingRegister reads a single holding register (function code 0x03)
func (c *Client) ReadHoldingRegister(address uint16) (uint16, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return 0, ErrNotConnected
	}

	return c.client.ReadRegister(address, modbus.HOLDING_REGISTER)
}

// ReadHoldingRegisters reads multiple holding registers (function code 0x03)
func (c *Client) ReadHoldingRegisters(address uint16, quantity uint16) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil, ErrNotConnected
	}

	// Returns []uint16
	regs, err := c.client.ReadRegisters(address, quantity, modbus.HOLDING_REGISTER)
	if err != nil {
		return nil, err
	}

	// Convert []uint16 to []byte (Big Endian)
	bytes := make([]byte, len(regs)*2)
	for i, reg := range regs {
		binary.BigEndian.PutUint16(bytes[i*2:], reg)
	}
	return bytes, nil
}

// ReadInputRegister reads a single input register (function code 0x04)
func (c *Client) ReadInputRegister(address uint16) (uint16, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return 0, ErrNotConnected
	}

	return c.client.ReadRegister(address, modbus.INPUT_REGISTER)
}

// ReadInputRegisters reads multiple input registers (function code 0x04)
func (c *Client) ReadInputRegisters(address uint16, quantity uint16) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil, ErrNotConnected
	}

	regs, err := c.client.ReadRegisters(address, quantity, modbus.INPUT_REGISTER)
	if err != nil {
		return nil, err
	}

	bytes := make([]byte, len(regs)*2)
	for i, reg := range regs {
		binary.BigEndian.PutUint16(bytes[i*2:], reg)
	}
	return bytes, nil
}

// Helper to pack bools to bytes
func boolsToBytes(bits []bool) []byte {
	numBytes := (len(bits) + 7) / 8
	bytes := make([]byte, numBytes)
	for i, b := range bits {
		if b {
			byteIdx := i / 8
			bitIdx := uint(i % 8)
			bytes[byteIdx] |= (1 << bitIdx)
		}
	}
	return bytes
}

// ReadTag reads a tag value based on address and data type
func (c *Client) ReadTag(address string, dataType string) (interface{}, error) {
	addrType, addrOffset, bitOffset, err := parseAddress(address)
	if err != nil {
		return nil, err
	}

	switch dataType {
	case "BOOL":
		// For Coils/Discrete Inputs, bitOffset is ignored (or effectively 0)
		if addrType == "coil" || addrType == "discrete" {
			return c.readBoolDirect(addrType, addrOffset)
		}
		// For Registers, we need a bit offset
		if bitOffset < 0 {
			return nil, fmt.Errorf("%w: BOOL register address must include bit offset (e.g., 40001.0)", ErrInvalidAddressFormat)
		}
		return c.readBoolFromRegister(addrType, addrOffset, bitOffset)
	case "INT":
		return c.readInt(addrType, addrOffset)
	case "REAL":
		return c.readReal(addrType, addrOffset)
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidDataType, dataType)
	}
}

// parseAddress parses the address string and returns type, offset, and bit offset
func parseAddress(address string) (string, uint16, int8, error) {
	c := strings.TrimSpace(address)
	if c == "" {
		return "", 0, -1, ErrInvalidAddressFormat
	}

	var bitOffset int = -1
	addrPart := c

	// Check for bit offset (e.g., 40001.0)
	if strings.Contains(c, ".") {
		parts := strings.Split(c, ".")
		if len(parts) != 2 {
			return "", 0, -1, ErrInvalidAddressFormat
		}
		addrPart = parts[0]
		bit, err := strconv.Atoi(parts[1])
		if err != nil || bit < 0 || bit > 15 {
			return "", 0, -1, ErrInvalidAddressFormat
		}
		bitOffset = bit
	}

	// Try to parse address part as integer
	val, err := strconv.Atoi(addrPart)
	if err != nil {
		// Legacy/Prefix format (HR1, C1, etc.)
		upperC := strings.ToUpper(addrPart)
		var prefix string
		var suffix string
		if strings.HasPrefix(upperC, "HR") {
			prefix = "holding"
			suffix = addrPart[2:]
		} else if strings.HasPrefix(upperC, "IR") {
			prefix = "input"
			suffix = addrPart[2:]
		} else if strings.HasPrefix(upperC, "DI") {
			prefix = "discrete"
			suffix = addrPart[2:]
		} else if strings.HasPrefix(upperC, "C") {
			prefix = "coil"
			suffix = addrPart[1:]
		}

		if prefix != "" {
			off, err := strconv.Atoi(suffix)
			if err == nil {
				return prefix, uint16(off), int8(bitOffset), nil
			}
		}
		return "", 0, -1, ErrInvalidAddressFormat
	}

	// Standard Modbus mapping
	// Standard Modbus mapping
	if val >= 40001 && val <= 49999 {
		return "holding", uint16(val - 40001), int8(bitOffset), nil
	}
	if val >= 30001 && val <= 39999 {
		return "input", uint16(val - 30001), int8(bitOffset), nil
	}
	if val >= 10001 && val <= 19999 {
		return "discrete", uint16(val - 10001), int8(bitOffset), nil
	}
	if val >= 1 && val <= 9999 {
		return "coil", uint16(val - 1), int8(bitOffset), nil
	}

	// Fallback for raw offsets (< 30000)
	if val < 30000 {
		return "holding", uint16(val), int8(bitOffset), nil
	}

	// Extended addressing (6-digit)
	if val >= 400001 && val <= 499999 {
		return "holding", uint16(val - 400001), int8(bitOffset), nil
	}
	if val >= 300001 && val <= 399999 {
		return "input", uint16(val - 300001), int8(bitOffset), nil
	}

	return "", 0, -1, ErrInvalidAddressFormat
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
		return 0, fmt.Errorf("%w: INT valid only for holding/input registers", ErrInvalidAddressFormat)
	}

	if err != nil {
		return 0, err
	}

	return int16(value), nil
}

// readBoolDirect reads a boolean from Coil or Discrete Input
func (c *Client) readBoolDirect(addrType string, address uint16) (bool, error) {
	var data []byte
	var err error

	switch addrType {
	case "coil":
		data, err = c.ReadCoils(address, 1)
	case "discrete":
		data, err = c.ReadDiscreteInputs(address, 1)
	default:
		return false, ErrInvalidAddressFormat
	}

	if err != nil {
		return false, err
	}

	if len(data) == 0 {
		return false, ErrInvalidResponse
	}

	// First bit of first byte
	return (data[0] & 0x01) == 1, nil
}

// readBoolFromRegister reads a boolean value from a single bit in a register
func (c *Client) readBoolFromRegister(addrType string, regAddr uint16, bitOffset int8) (bool, error) {
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
	// Read 2 registers (4 bytes)
	var regs []byte
	var err error

	switch addrType {
	case "holding":
		regs, err = c.ReadHoldingRegisters(address, 2)
	case "input":
		regs, err = c.ReadInputRegisters(address, 2)
	default:
		return 0, fmt.Errorf("%w: REAL valid only for holding/input registers", ErrInvalidAddressFormat)
	}

	if err != nil {
		return 0, err
	}

	if len(regs) < 4 {
		return 0, fmt.Errorf("%w: insufficient data", ErrInvalidResponse)
	}

	bits := binary.BigEndian.Uint32(regs)
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

// WriteSingleCoil writes a single coil (function code 0x05)
func (c *Client) WriteSingleCoil(address uint16, value uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return ErrNotConnected
	}

	// value is 0xFF00 for ON, 0x0000 for OFF
	boolVal := value == 0xFF00
	return c.client.WriteCoil(address, boolVal)
}

// WriteSingleRegister writes a single holding register (function code 0x06)
func (c *Client) WriteSingleRegister(address uint16, value uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return ErrNotConnected
	}

	return c.client.WriteRegister(address, value)
}

// WriteMultipleRegisters writes multiple holding registers (function code 0x10)
func (c *Client) WriteMultipleRegisters(address uint16, quantity uint16, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return ErrNotConnected
	}

	// Convert []byte to []uint16
	if len(value)%2 != 0 {
		return fmt.Errorf("value length must be even")
	}

	numRegs := len(value) / 2
	if uint16(numRegs) != quantity {
		return fmt.Errorf("quantity mismatch: expected %d, got %d from bytes", quantity, numRegs)
	}

	regs := make([]uint16, quantity)
	for i := 0; i < int(quantity); i++ {
		regs[i] = binary.BigEndian.Uint16(value[i*2:])
	}

	return c.client.WriteRegisters(address, regs)
}
