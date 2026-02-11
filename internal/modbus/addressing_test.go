package modbus

import (
	"testing"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input          string
		expectedType   string
		expectedOffset uint16
		expectError    bool
	}{
		// Standard Modbus - Holding
		{"40001", "holding", 0, false},
		{"40002", "holding", 1, false},
		{"49999", "holding", 9998, false},

		// Standard Modbus - Input
		{"30001", "input", 0, false},
		{"30100", "input", 99, false},

		// Raw Offsets (The "Tag at 0" fix)
		{"0", "holding", 0, false},
		{"1", "holding", 1, false},
		{"100", "holding", 100, false},
		{"25", "holding", 25, false},

		// Edge cases
		{"invalid", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			addr, err := ParseAddress(tt.input, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input %s, but got none", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for input %s: %v", tt.input, err)
				return
			}

			if addr.Type != tt.expectedType {
				t.Errorf("Expected type %s, got %s", tt.expectedType, addr.Type)
			}

			if addr.Offset != tt.expectedOffset {
				t.Errorf("Expected offset %d, got %d", tt.expectedOffset, addr.Offset)
			}
		})
	}
}
