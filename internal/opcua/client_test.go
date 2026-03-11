package opcua

import (
	"testing"
)

// TestConvertToVariantByOpcUaType tests the conversion of values to OPC UA variants
// This is critical for fixing the type mismatch bug when writing via MQTT
func TestConvertToVariantByOpcUaType(t *testing.T) {
	tests := []struct {
		name       string
		value      interface{}
		opcuaType  string
		wantErr    bool
		wantValue  interface{}
	}{
		// Boolean tests
		{
			name:      "bool to Boolean",
			value:     true,
			opcuaType: "Boolean",
			wantErr:   false,
			wantValue: true,
		},
		{
			name:      "float64 1.0 to Boolean",
			value:     float64(1.0),
			opcuaType: "Boolean",
			wantErr:   false,
			wantValue: true,
		},
		{
			name:      "float64 0.0 to Boolean",
			value:     float64(0.0),
			opcuaType: "Boolean",
			wantErr:   false,
			wantValue: false,
		},

		// Int16 tests - JSON numbers come as float64
		{
			name:      "float64 to Int16",
			value:     float64(42),
			opcuaType: "Int16",
			wantErr:   false,
			wantValue: int16(42),
		},
		{
			name:      "int to Int16",
			value:     int(100),
			opcuaType: "Int16",
			wantErr:   false,
			wantValue: int16(100),
		},
		{
			name:      "bool true to Int16",
			value:     true,
			opcuaType: "Int16",
			wantErr:   false,
			wantValue: int16(1),
		},
		{
			name:      "bool false to Int16",
			value:     false,
			opcuaType: "Int16",
			wantErr:   false,
			wantValue: int16(0),
		},

		// UInt16 tests
		{
			name:      "float64 to UInt16",
			value:     float64(100),
			opcuaType: "UInt16",
			wantErr:   false,
			wantValue: uint16(100),
		},
		{
			name:      "float64 max UInt16",
			value:     float64(65535),
			opcuaType: "UInt16",
			wantErr:   false,
			wantValue: uint16(65535),
		},

		// Int32 tests
		{
			name:      "float64 to Int32",
			value:     float64(12345),
			opcuaType: "Int32",
			wantErr:   false,
			wantValue: int32(12345),
		},
		{
			name:      "float64 to Int (alias)",
			value:     float64(999),
			opcuaType: "Int",
			wantErr:   false,
			wantValue: int32(999),
		},

		// UInt32 tests
		{
			name:      "float64 to UInt32",
			value:     float64(500000),
			opcuaType: "UInt32",
			wantErr:   false,
			wantValue: uint32(500000),
		},

		// Int64 tests
		{
			name:      "float64 to Int64",
			value:     float64(1234567890),
			opcuaType: "Int64",
			wantErr:   false,
			wantValue: int64(1234567890),
		},

		// UInt64 tests
		{
			name:      "float64 to UInt64",
			value:     float64(9876543210),
			opcuaType: "UInt64",
			wantErr:   false,
			wantValue: uint64(9876543210),
		},

		// SByte (int8) tests
		{
			name:      "float64 to SByte",
			value:     float64(127),
			opcuaType: "SByte",
			wantErr:   false,
			wantValue: int8(127),
		},
		{
			name:      "float64 negative to SByte",
			value:     float64(-128),
			opcuaType: "SByte",
			wantErr:   false,
			wantValue: int8(-128),
		},

		// Byte (uint8) tests
		{
			name:      "float64 to Byte",
			value:     float64(255),
			opcuaType: "Byte",
			wantErr:   false,
			wantValue: uint8(255),
		},

		// Float tests
		{
			name:      "float64 to Float",
			value:     float64(3.14159),
			opcuaType: "Float",
			wantErr:   false,
			wantValue: float32(3.14159),
		},

		// Double tests
		{
			name:      "float64 to Double",
			value:     float64(3.141592653589793),
			opcuaType: "Double",
			wantErr:   false,
			wantValue: float64(3.141592653589793),
		},

		// String tests
		{
			name:      "string to String",
			value:     "hello",
			opcuaType: "String",
			wantErr:   false,
			wantValue: "hello",
		},
		{
			name:      "int to String",
			value:     42,
			opcuaType: "String",
			wantErr:   false,
			wantValue: "42",
		},

		// Unknown type (fallback)
		{
			name:      "float64 to UnknownType",
			value:     float64(42),
			opcuaType: "UnknownType",
			wantErr:   false,
			wantValue: float64(42), // falls through to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variant, err := convertToVariantByOpcUaType(tt.value, tt.opcuaType)

			if tt.wantErr {
				if err == nil {
					t.Errorf("convertToVariantByOpcUaType() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("convertToVariantByOpcUaType() unexpected error: %v", err)
				return
			}

			if variant == nil {
				t.Errorf("convertToVariantByOpcUaType() returned nil variant")
				return
			}

			gotValue := variant.Value()
			if gotValue != tt.wantValue {
				t.Errorf("convertToVariantByOpcUaType() value = %v (type %T), want %v (type %T)",
					gotValue, gotValue, tt.wantValue, tt.wantValue)
			}
		})
	}
}

// TestConvertToVariant tests the legacy conversion function
func TestConvertToVariant(t *testing.T) {
	tests := []struct {
		name      string
		value     interface{}
		dataType  string
		wantErr   bool
	}{
		{
			name:     "bool to BOOL",
			value:    true,
			dataType: "BOOL",
			wantErr:  false,
		},
		{
			name:     "float64 to INT",
			value:    float64(42),
			dataType: "INT",
			wantErr:  false,
		},
		{
			name:     "float64 to DINT",
			value:    float64(12345),
			dataType: "DINT",
			wantErr:  false,
		},
		{
			name:     "float64 to REAL",
			value:    float64(3.14),
			dataType: "REAL",
			wantErr:  false,
		},
		{
			name:     "string to STRING",
			value:    "test",
			dataType: "STRING",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variant, err := convertToVariant(tt.value, tt.dataType)

			if tt.wantErr {
				if err == nil {
					t.Errorf("convertToVariant() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("convertToVariant() unexpected error: %v", err)
				return
			}

			if variant == nil {
				t.Errorf("convertToVariant() returned nil variant")
			}
		})
	}
}

// TestToBool tests the boolean conversion helper
func TestToBool(t *testing.T) {
	tests := []struct {
		value    interface{}
		want     bool
		wantOk   bool
	}{
		{true, true, true},
		{false, false, true},
		{float64(1), true, true},
		{float64(0), false, true},
		{int(1), true, true},
		{int(0), false, true},
		{"true", false, false}, // string not supported
	}

	for _, tt := range tests {
		got, ok := toBool(tt.value)
		if got != tt.want || ok != tt.wantOk {
			t.Errorf("toBool(%v) = (%v, %v), want (%v, %v)", tt.value, got, ok, tt.want, tt.wantOk)
		}
	}
}
