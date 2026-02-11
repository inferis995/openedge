package modbus

import (
	"fmt"
	"strconv"
	"strings"
)

// Address represents a parsed Modbus address
type Address struct {
	Type      string // "holding", "input", "coil", "discrete"
	Offset    uint16 // 0-based offset
	BitOffset *int   // Optional 0-15 bit offset (for BOOL in registers)
}

// ParseAddress parses a raw address string into a standard Modbus Address.
// It supports:
// - Standard Modbus (e.g., 40001 -> Holding, 0)
// - Ranges: 0xxxx (Coil), 1xxxx (Discrete), 3xxxx (Input), 4xxxx (Holding)
// - Bit addressing: ADDRESS.BIT (e.g., 40001.0)
func ParseAddress(c string, zeroBased bool) (Address, error) {
	c = strings.TrimSpace(c)
	if c == "" {
		return Address{}, fmt.Errorf("empty address")
	}

	var bitOffset *int
	addrPart := c

	// Check for bit offset (e.g., 40001.0)
	if strings.Contains(c, ".") {
		parts := strings.Split(c, ".")
		if len(parts) != 2 {
			return Address{}, fmt.Errorf("invalid bit address format: %s", c)
		}
		addrPart = parts[0]
		bit, err := strconv.Atoi(parts[1])
		if err != nil || bit < 0 || bit > 15 {
			return Address{}, fmt.Errorf("invalid bit offset (must be 0-15): %s", parts[1])
		}
		bitOffset = &bit
	}

	// Try to parse address part as integer
	val, err := strconv.Atoi(addrPart)
	if err != nil {
		return Address{}, fmt.Errorf("invalid address format: %s", addrPart)
	}

	// Standard Modbus mapping
	if val >= 40001 && val <= 49999 {
		offset := uint16(val - 40001)
		if zeroBased {
			offset = uint16(val - 40000)
		}
		return Address{Type: "holding", Offset: offset, BitOffset: bitOffset}, nil
	}
	if val >= 30001 && val <= 39999 {
		offset := uint16(val - 30001)
		if zeroBased {
			offset = uint16(val - 30000)
		}
		return Address{Type: "input", Offset: offset, BitOffset: bitOffset}, nil
	}
	if val >= 10001 && val <= 19999 {
		offset := uint16(val - 10001)
		if zeroBased {
			offset = uint16(val - 10000)
		}
		return Address{Type: "discrete", Offset: offset}, nil
	}
	if val >= 1 && val <= 9999 {
		offset := uint16(val - 1)
		if zeroBased {
			offset = uint16(val)
		}
		return Address{Type: "coil", Offset: offset}, nil
	}

	// Fallback for raw offsets (treated as holding for backward compatibility)
	if val < 30000 {
		return Address{Type: "holding", Offset: uint16(val), BitOffset: bitOffset}, nil
	}

	return Address{}, fmt.Errorf("unsupported address range: %d", val)
}
