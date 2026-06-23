package s7

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"testing"
)

// ---------------------------------------------------------------------------
// parseAddress — DB area
// ---------------------------------------------------------------------------

func TestParseAddress_DB_Word(t *testing.T) {
	area, dbNum, start, bitOff, size, err := parseAddress("DB1.DBW0", DataTypeINT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if area != areaDB { t.Errorf("area = %#x, want %#x", area, areaDB) }
	if dbNum != 1     { t.Errorf("dbNumber = %d, want 1", dbNum) }
	if start != 0    { t.Errorf("start = %d, want 0", start) }
	if bitOff != 0   { t.Errorf("bitOffset = %d, want 0", bitOff) }
	if size != 2     { t.Errorf("size = %d, want 2", size) }
}

func TestParseAddress_DB_DoubleWord(t *testing.T) {
	_, dbNum, start, _, size, err := parseAddress("DB3.DBD8", DataTypeREAL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dbNum != 3  { t.Errorf("dbNumber = %d, want 3", dbNum) }
	if start != 8  { t.Errorf("start = %d, want 8", start) }
	if size != 4   { t.Errorf("size = %d, want 4", size) }
}

func TestParseAddress_DB_Bit(t *testing.T) {
	_, _, start, bitOff, size, err := parseAddress("DB1.DBX2.3", DataTypeBOOL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 2  { t.Errorf("start = %d, want 2", start) }
	if bitOff != 3 { t.Errorf("bitOffset = %d, want 3", bitOff) }
	if size != 1   { t.Errorf("size = %d, want 1", size) }
}

func TestParseAddress_UnsupportedDataType(t *testing.T) {
	_, _, _, _, _, err := parseAddress("DB1.DBW0", "STRING")
	if err == nil {
		t.Error("expected error for unsupported data type, got nil")
	}
}

func TestParseAddress_InvalidFormat(t *testing.T) {
	_, _, _, _, _, err := parseAddress("INVALID", DataTypeINT)
	if err == nil {
		t.Error("expected error for invalid format, got nil")
	}
}

// ---------------------------------------------------------------------------
// parseAddress — direct areas (M, I, Q)
// ---------------------------------------------------------------------------

func TestParseAddress_Merker_Word(t *testing.T) {
	area, _, start, _, size, err := parseAddress("MW10", DataTypeINT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if area != areaMK { t.Errorf("area = %#x, want %#x (Merker)", area, areaMK) }
	if start != 10    { t.Errorf("start = %d, want 10", start) }
	if size != 2      { t.Errorf("size = %d, want 2", size) }
}

func TestParseAddress_Input_Word(t *testing.T) {
	area, _, _, _, _, err := parseAddress("IW0", DataTypeINT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if area != areaPE { t.Errorf("area = %#x, want %#x (Inputs)", area, areaPE) }
}

func TestParseAddress_Output_DWord(t *testing.T) {
	area, _, start, _, size, err := parseAddress("QD4", DataTypeDINT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if area != areaPA { t.Errorf("area = %#x, want %#x (Outputs)", area, areaPA) }
	if start != 4     { t.Errorf("start = %d, want 4", start) }
	if size != 4      { t.Errorf("size = %d, want 4", size) }
}

// ---------------------------------------------------------------------------
// convertBufferToValue
// ---------------------------------------------------------------------------

func TestConvertBufferToValue_INT(t *testing.T) {
	// big-endian int16: 0x01, 0x00 = 256
	buf := []byte{0x01, 0x00}
	got, err := convertBufferToValue(buf, DataTypeINT, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != int16(256) {
		t.Errorf("convertBufferToValue INT = %v (%T), want int16(256)", got, got)
	}
}

func TestConvertBufferToValue_INT_Negative(t *testing.T) {
	// -1 in big-endian int16: 0xFF, 0xFF
	buf := []byte{0xFF, 0xFF}
	got, err := convertBufferToValue(buf, DataTypeINT, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != int16(-1) {
		t.Errorf("convertBufferToValue INT = %v, want int16(-1)", got)
	}
}

func TestConvertBufferToValue_DINT(t *testing.T) {
	// big-endian int32: 0x00, 0x01, 0x00, 0x00 = 65536
	buf := []byte{0x00, 0x01, 0x00, 0x00}
	got, err := convertBufferToValue(buf, DataTypeDINT, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != int32(65536) {
		t.Errorf("convertBufferToValue DINT = %v, want int32(65536)", got)
	}
}

func TestConvertBufferToValue_REAL(t *testing.T) {
	// IEEE 754 float32: 3.14
	bits := math.Float32bits(3.14)
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, bits)
	got, err := convertBufferToValue(buf, DataTypeREAL, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f, ok := got.(float32)
	if !ok {
		t.Fatalf("want float32, got %T", got)
	}
	if math.Abs(float64(f)-3.14) > 0.001 {
		t.Errorf("REAL = %v, want ~3.14", f)
	}
}

func TestConvertBufferToValue_BOOL_BitZero(t *testing.T) {
	// byte 0x01, bit 0 → true
	got, err := convertBufferToValue([]byte{0x01}, DataTypeBOOL, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != true {
		t.Errorf("BOOL bit0 of 0x01 = %v, want true", got)
	}
}

func TestConvertBufferToValue_BOOL_BitOne_Off(t *testing.T) {
	// byte 0x01, bit 1 → false (bit 1 is 0)
	got, err := convertBufferToValue([]byte{0x01}, DataTypeBOOL, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != false {
		t.Errorf("BOOL bit1 of 0x01 = %v, want false", got)
	}
}

func TestConvertBufferToValue_TooSmall_INT(t *testing.T) {
	_, err := convertBufferToValue([]byte{0x01}, DataTypeINT, 0)
	if err == nil {
		t.Error("expected error for undersized INT buffer, got nil")
	}
}

func TestConvertBufferToValue_TooSmall_BOOL(t *testing.T) {
	_, err := convertBufferToValue([]byte{}, DataTypeBOOL, 0)
	if err == nil {
		t.Error("expected error for empty BOOL buffer, got nil")
	}
}

func TestConvertBufferToValue_UnsupportedType(t *testing.T) {
	_, err := convertBufferToValue([]byte{0x01, 0x02}, "STRING", 0)
	if err == nil {
		t.Error("expected error for unsupported data type, got nil")
	}
}

// ---------------------------------------------------------------------------
// isConnectionError
// ---------------------------------------------------------------------------

func TestIsConnectionError_NetOpError(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: fmt.Errorf("connection refused")}
	if !isConnectionError(err) {
		t.Error("*net.OpError should be a connection error")
	}
}

func TestIsConnectionError_Generic(t *testing.T) {
	if isConnectionError(fmt.Errorf("some other error")) {
		t.Error("generic error should not be a connection error")
	}
}

func TestIsConnectionError_Nil(t *testing.T) {
	if isConnectionError(nil) {
		t.Error("nil error should not be a connection error")
	}
}
