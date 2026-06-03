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
	if val >= 40000 && val <= 49999 {
		var offset uint16
		if zeroBased {
			offset = uint16(val - 40000) // 40000→0, 40001→1, ...
		} else {
			if val < 40001 {
				return Address{}, fmt.Errorf("address %d is invalid in 1-based mode (minimum is 40001)", val)
			}
			offset = uint16(val - 40001) // 40001→0, 40002→1, ...
		}
		return Address{Type: "holding", Offset: offset, BitOffset: bitOffset}, nil
	}
	if val >= 30000 && val <= 39999 {
		var offset uint16
		if zeroBased {
			offset = uint16(val - 30000) // 30000→0, 30001→1, ...
		} else {
			if val < 30001 {
				return Address{}, fmt.Errorf("address %d is invalid in 1-based mode (minimum is 30001)", val)
			}
			offset = uint16(val - 30001) // 30001→0, 30002→1, ...
		}
		return Address{Type: "input", Offset: offset, BitOffset: bitOffset}, nil
	}
	if val >= 10000 && val <= 19999 {
		var offset uint16
		if zeroBased {
			offset = uint16(val - 10000) // 10000→0, 10001→1, ...
		} else {
			if val < 10001 {
				return Address{}, fmt.Errorf("address %d is invalid in 1-based mode (minimum is 10001)", val)
			}
			offset = uint16(val - 10001) // 10001→0, 10002→1, ...
		}
		return Address{Type: "discrete", Offset: offset}, nil
	}
	// Modicon 0xxxx coil notation: 5-digit zero-padded strings like "00000"-"09999".
	// Must be checked BEFORE the bare-numeric fallback, because strconv.Atoi("00001")
	// returns 1 which would otherwise match the 0-29999 holding-register catch-all.
	if len(addrPart) == 5 && addrPart[0] == '0' && val >= 0 && val <= 9999 {
		var offset uint16
		if zeroBased {
			offset = uint16(val)
		} else {
			if val < 1 {
				return Address{}, fmt.Errorf("address %s is invalid in 1-based mode (minimum is 00001)", addrPart)
			}
			offset = uint16(val - 1)
		}
		return Address{Type: "coil", Offset: offset, BitOffset: bitOffset}, nil
	}
	// Bare numeric addresses (no Modicon range prefix) are read as HOLDING
	// registers at the raw offset, e.g. "100" -> holding register 100. This is
	// by far the most common case. Coils and discrete inputs are addressed
	// through their Modicon ranges (1xxxx -> discrete) or explicit prefixes,
	// because the 0xxxx coil range is ambiguous with raw offsets.
	if val >= 0 && val <= 29999 {
		return Address{Type: "holding", Offset: uint16(val), BitOffset: bitOffset}, nil
	}

	return Address{}, fmt.Errorf("unsupported address range: %d", val)
}
