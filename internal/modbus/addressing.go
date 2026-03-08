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
	// Standard Modbus mapping
	if val >= 40000 && val <= 49999 {
		var offset uint16
		if val == 40000 {
			offset = 0
		} else if zeroBased {
			offset = uint16(val - 40000)
		} else {
			offset = uint16(val - 40001)
		}
		return Address{Type: "holding", Offset: offset, BitOffset: bitOffset}, nil
	}
	if val >= 30000 && val <= 39999 {
		var offset uint16
		if val == 30000 {
			offset = 0
		} else if zeroBased {
			offset = uint16(val - 30000)
		} else {
			offset = uint16(val - 30001)
		}
		return Address{Type: "input", Offset: offset, BitOffset: bitOffset}, nil
	}
	if val >= 10000 && val <= 19999 {
		var offset uint16
		if val == 10000 {
			offset = 0
		} else if zeroBased {
			offset = uint16(val - 10000)
		} else {
			offset = uint16(val - 10001)
		}
		return Address{Type: "discrete", Offset: offset}, nil
	}
	if val >= 0 && val <= 9999 {
		offset := uint16(val)
		// Standard Modbus starts at 1. If not zero-based, 1 -> 0.
		// If user explicitly types 0, we treat it as offset 0 regardless of zero-based setting.
		if val > 0 && !zeroBased {
			offset = uint16(val - 1)
		}
		return Address{Type: "coil", Offset: offset}, nil
	}

	// Fallback for raw offsets (treated as holding for backward compatibility)
	if val < 30000 {
		return Address{Type: "holding", Offset: uint16(val), BitOffset: bitOffset}, nil
	}

	return Address{}, fmt.Errorf("unsupported address range: %d", val)
}
