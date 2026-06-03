package modbus

import (
	"testing"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input          string
		zeroBased      bool
		expectedType   string
		expectedOffset uint16
		expectError    bool
	}{
		// 1-based (standard Modbus, zeroBased=false)
		{"40001", false, "holding", 0, false},
		{"40002", false, "holding", 1, false},
		{"49999", false, "holding", 9998, false},
		{"30001", false, "input", 0, false},
		{"30100", false, "input", 99, false},
		{"40000", false, "", 0, true}, // invalid in 1-based mode

		// 0-based (zeroBased=true)
		{"40000", true, "holding", 0, false}, // first register
		{"40001", true, "holding", 1, false},
		{"40002", true, "holding", 2, false},
		{"30000", true, "input", 0, false},
		{"30001", true, "input", 1, false},
		{"10000", true, "discrete", 0, false},
		{"10001", true, "discrete", 1, false},

		// Raw Offsets (treated as holding, zeroBased irrelevant)
		{"0", false, "holding", 0, false},
		{"1", false, "holding", 1, false},
		{"100", false, "holding", 100, false},
		{"25", false, "holding", 25, false},

		// Modicon 0xxxx coil notation (5-digit zero-padded)
		{"00000", true, "coil", 0, false},
		{"00001", true, "coil", 1, false},
		{"00017", true, "coil", 17, false},
		{"00001", false, "coil", 0, false},
		{"00002", false, "coil", 1, false},
		{"00000", false, "", 0, true}, // invalid in 1-based mode

		// Edge cases
		{"invalid", false, "", 0, true},
	}

	for _, tt := range tests {
		name := tt.input
		if tt.zeroBased {
			name += "_zerobased"
		}
		t.Run(name, func(t *testing.T) {
			addr, err := ParseAddress(tt.input, tt.zeroBased)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input %s (zeroBased=%v), but got none", tt.input, tt.zeroBased)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for input %s (zeroBased=%v): %v", tt.input, tt.zeroBased, err)
				return
			}

			if addr.Type != tt.expectedType {
				t.Errorf("input=%s zeroBased=%v: expected type %s, got %s", tt.input, tt.zeroBased, tt.expectedType, addr.Type)
			}

			if addr.Offset != tt.expectedOffset {
				t.Errorf("input=%s zeroBased=%v: expected offset %d, got %d", tt.input, tt.zeroBased, tt.expectedOffset, addr.Offset)
			}
		})
	}
}
