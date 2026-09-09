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

// ExceptionError wraps a Modbus exception response (e.g. ILLEGAL DATA ADDRESS).
// These are deterministic protocol-level replies from the device: the TCP
// session is still healthy, so callers should NOT disconnect on them.
type ExceptionError struct {
	Err error
}

func (e *ExceptionError) Error() string {
	return fmt.Sprintf("%v: %v", ErrModbusException, e.Err)
}

func (e *ExceptionError) Unwrap() error { return e.Err }

// IsExceptionError reports whether err (or any error it wraps) is a Modbus
// exception response rather than a transport-level failure (timeout, broken
// pipe, protocol corruption).
func IsExceptionError(err error) bool {
	var ee *ExceptionError
	return errors.As(err, &ee)
}

// wrapModbusError wraps Modbus exception responses in ExceptionError so
// callers can distinguish them from transport errors. Transport errors
// (timeouts, net errors, protocol errors) are returned unchanged.
func wrapModbusError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, modbus.ErrIllegalFunction),
		errors.Is(err, modbus.ErrIllegalDataAddress),
		errors.Is(err, modbus.ErrIllegalDataValue),
		errors.Is(err, modbus.ErrServerDeviceFailure),
		errors.Is(err, modbus.ErrAcknowledge),
		errors.Is(err, modbus.ErrMemoryParityError),
		errors.Is(err, modbus.ErrServerDeviceBusy),
		errors.Is(err, modbus.ErrGWPathUnavailable),
		errors.Is(err, modbus.ErrGWTargetFailedToRespond):
		return &ExceptionError{Err: err}
	}
	return err
}

// Config holds the Modbus TCP connection configuration
// Transport picks the wire a Modbus device is reached over.
//
// TCP was the only one for a long time, and on a plant built this decade it is
// usually right. It is not right on the plants this platform is meant to reach:
// an Italian factory floor is full of inverters, energy meters and older
// instrumentation that speak Modbus RTU over RS-485 and have no Ethernet port
// at all. Without RTU every one of those jobs needs a serial-to-Ethernet
// gateway bought and wired for it.
const (
	// TransportTCP is Modbus TCP — the default, and what every existing
	// gateway configuration means when it says nothing.
	TransportTCP = "tcp"
	// TransportRTU is Modbus RTU on a serial port: /dev/ttyUSB0, COM3.
	TransportRTU = "rtu"
	// TransportRTUOverTCP is RTU framing carried over a TCP socket, which is
	// what a serial gateway in transparent mode gives you. It looks like TCP
	// on the wire and like RTU inside, and getting the two confused produces a
	// connection that opens and then answers nothing.
	TransportRTUOverTCP = "rtuovertcp"
)

type Config struct {
	// Transport is one of the Transport* constants. Empty means TCP, which is
	// what makes every configuration written before this field existed keep
	// working unchanged.
	Transport string

	// TCP and RTU-over-TCP.
	Host string
	Port int

	// RTU only.
	Device   string // serial port: /dev/ttyUSB0, COM3
	BaudRate int
	DataBits int
	Parity   string // "N", "E", "O" — see parseParity
	StopBits int

	SlaveID byte
	Timeout time.Duration
}

// transport returns the configured transport, defaulting to TCP.
func (c *Config) transport() string {
	if c.Transport == "" {
		return TransportTCP
	}
	return strings.ToLower(strings.TrimSpace(c.Transport))
}

// url builds the connection string the library expects, and refuses a
// configuration that cannot produce one.
//
// It is separate from Connect so it can be tested without a serial port or a
// device on the other end — the two things a test machine never has.
func (c *Config) url() (string, error) {
	switch t := c.transport(); t {
	case TransportTCP:
		if c.Host == "" {
			return "", errors.New("modbus tcp: no host configured")
		}
		port := c.Port
		if port == 0 {
			port = 502
		}
		return fmt.Sprintf("tcp://%s:%d", c.Host, port), nil

	case TransportRTUOverTCP:
		if c.Host == "" {
			return "", errors.New("modbus rtuovertcp: no host configured")
		}
		port := c.Port
		if port == 0 {
			port = 502
		}
		return fmt.Sprintf("rtuovertcp://%s:%d", c.Host, port), nil

	case TransportRTU:
		if c.Device == "" {
			return "", errors.New("modbus rtu: no serial device configured (e.g. /dev/ttyUSB0 or COM3)")
		}
		// The library splits on "://" and takes the remainder as the device
		// path, so an absolute path lands as rtu:///dev/ttyUSB0 — three
		// slashes, which looks like a typo and is not one.
		return "rtu://" + c.Device, nil

	default:
		return "", fmt.Errorf("modbus: unknown transport %q (want %q, %q or %q)",
			t, TransportTCP, TransportRTU, TransportRTUOverTCP)
	}
}

// parseParity maps what an operator writes into what the library wants.
//
// Accepts the single letters used on every device datasheet and the words used
// in every configuration UI, because both will be typed. An empty value is
// none, which is what almost every industrial serial device uses.
func parseParity(p string) (uint, error) {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "", "N", "NONE":
		return modbus.PARITY_NONE, nil
	case "E", "EVEN":
		return modbus.PARITY_EVEN, nil
	case "O", "ODD":
		return modbus.PARITY_ODD, nil
	default:
		return 0, fmt.Errorf("modbus: unknown parity %q (want N, E or O)", p)
	}
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
	// A configuration that names no transport is TCP. Every gateway configured
	// before this field existed says nothing, and must keep meaning what it
	// meant.
	transport := TransportTCP
	if t, ok := firstString(connConfig, "transport", "mode"); ok {
		transport = strings.ToLower(strings.TrimSpace(t))
	}

	slaveID := byte(1)
	if s, ok := firstNumber(connConfig, "slave_id", "unit_id"); ok {
		slaveID = byte(s)
	}

	cfg := Config{
		Transport: transport,
		SlaveID:   slaveID,
		// TCP over a plant network tolerates a long wait. A serial line does
		// not: a slave that is switched off would hold the poll loop for the
		// whole timeout, once per scan, and the other devices on the bus would
		// go stale because of it.
		Timeout: 5 * time.Second,
	}

	switch transport {
	case TransportTCP, TransportRTUOverTCP:
		host, ok := firstString(connConfig, "ip", "ip_address", "host")
		if !ok || strings.TrimSpace(host) == "" {
			return nil, errors.New("missing 'ip' or 'ip_address' in connection config")
		}
		cfg.Host = strings.TrimSpace(host)

		cfg.Port = 502
		if p, ok := firstNumber(connConfig, "port"); ok {
			cfg.Port = int(p)
		}

	case TransportRTU:
		device, ok := firstString(connConfig, "device", "serial_port", "port_name")
		if !ok || strings.TrimSpace(device) == "" {
			return nil, errors.New("modbus rtu: missing 'device' in connection config (e.g. /dev/ttyUSB0 or COM3)")
		}
		cfg.Device = strings.TrimSpace(device)

		if b, ok := firstNumber(connConfig, "baud_rate", "baudrate", "speed"); ok {
			cfg.BaudRate = int(b)
		}
		if d, ok := firstNumber(connConfig, "data_bits", "databits"); ok {
			cfg.DataBits = int(d)
		}
		if sb, ok := firstNumber(connConfig, "stop_bits", "stopbits"); ok {
			cfg.StopBits = int(sb)
		}
		if par, ok := firstString(connConfig, "parity"); ok {
			cfg.Parity = par
		}
		// Reject a bad parity here rather than at Connect: the operator is
		// looking at the form now, and will be looking at a red gateway in an
		// hour.
		if _, err := parseParity(cfg.Parity); err != nil {
			return nil, err
		}
		cfg.Timeout = time.Second

	default:
		return nil, fmt.Errorf("modbus: unknown transport %q (want %q, %q or %q)",
			transport, TransportTCP, TransportRTU, TransportRTUOverTCP)
	}

	// Fail here rather than on the first poll, so a wrong configuration is a
	// refused save instead of a gateway that looks configured and never reads.
	if _, err := cfg.url(); err != nil {
		return nil, err
	}

	return NewClient(cfg), nil
}

// firstString returns the first key present as a non-empty string.
//
// The connection config is JSON written by several generations of the UI and
// by hand, so the same thing is spelled more than one way. Accepting the
// aliases costs a line each and saves an operator from a form that silently
// ignores what they typed.
func firstString(m map[string]interface{}, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v, true
		}
	}
	return "", false
}

// firstNumber returns the first key present as a number.
//
// JSON numbers decode to float64, but a value that came back from Postgres or
// was built in Go can be an int, and a form can send it as a string.
func firstNumber(m map[string]interface{}, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case string:
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// Connect opens the link to the Modbus device over the configured transport.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// simonvetter/modbus manages connection state internally, but Open() re-opens or errors?
	// The library doesn't expose IsConnected. We should just try to Open() or handle errors.
	// For this wrapper, let's just proceed to Open().

	url, err := c.config.url()
	if err != nil {
		return err
	}

	parity, err := parseParity(c.config.Parity)
	if err != nil {
		return err
	}

	// Zero means "let the library choose", and its choices are the ones from
	// the Modbus-over-serial specification: 19200 baud, 8 data bits, and two
	// stop bits when there is no parity. Overriding them with our own guesses
	// would be a second set of defaults to keep in step with a document we do
	// not own.
	client, err := modbus.NewClient(&modbus.ClientConfiguration{
		URL:      url,
		Timeout:  c.config.Timeout,
		Speed:    uint(c.config.BaudRate),
		DataBits: uint(c.config.DataBits),
		Parity:   parity,
		StopBits: uint(c.config.StopBits),
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
		return nil, wrapModbusError(err)
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
		return nil, wrapModbusError(err)
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
		return nil, wrapModbusError(err)
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
		return nil, wrapModbusError(err)
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
	// Bare numeric addresses are read as HOLDING registers at the raw offset
	// (e.g. "100" -> holding 100). Coils are addressed via the "C" prefix and
	// discrete inputs via the 1xxxx range, so bare numbers stay holding.
	if val >= 0 && val < 30000 {
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
