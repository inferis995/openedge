// internal_test.go uses package settings (white-box) to test helpers exposed
// via export_test.go.
package settings_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/settings"
)

// ---------------------------------------------------------------------------
// valuesEqual — nil handling
// ---------------------------------------------------------------------------

func TestValuesEqual_BothNil(t *testing.T) {
	if !settings.ValuesEqual(nil, nil, 0) {
		t.Error("nil == nil should be true")
	}
}

func TestValuesEqual_OneNil(t *testing.T) {
	if settings.ValuesEqual(nil, 1.0, 0) {
		t.Error("nil != 1.0 should be false")
	}
	if settings.ValuesEqual(1.0, nil, 0) {
		t.Error("1.0 != nil should be false")
	}
}

// ---------------------------------------------------------------------------
// valuesEqual — float64
// ---------------------------------------------------------------------------

func TestValuesEqual_Float64_Equal_NoDeadband(t *testing.T) {
	if !settings.ValuesEqual(float64(3.14), float64(3.14), 0) {
		t.Error("identical float64 with 0 deadband should be equal")
	}
}

func TestValuesEqual_Float64_NotEqual_NoDeadband(t *testing.T) {
	if settings.ValuesEqual(float64(1.0), float64(2.0), 0) {
		t.Error("different float64 with 0 deadband should not be equal")
	}
}

func TestValuesEqual_Float64_WithinDeadband(t *testing.T) {
	// 1% deadband of 100.0 = 1.0 tolerance. |100.5 - 100| = 0.5 ≤ 1.0
	if !settings.ValuesEqual(float64(100.0), float64(100.5), 1.0) {
		t.Error("float64 within deadband should be equal")
	}
}

func TestValuesEqual_Float64_OutsideDeadband(t *testing.T) {
	// 0.5% deadband of 100.0 = 0.5. |102 - 100| = 2 > 0.5
	if settings.ValuesEqual(float64(100.0), float64(102.0), 0.5) {
		t.Error("float64 outside deadband should not be equal")
	}
}

func TestValuesEqual_Float64_ZeroBase_DeadbandIgnored(t *testing.T) {
	// When av == 0, deadband tolerance is 0 and only exact equality counts.
	if !settings.ValuesEqual(float64(0), float64(0), 10.0) {
		t.Error("0 == 0 should be equal even with large deadband")
	}
	if settings.ValuesEqual(float64(0), float64(0.001), 10.0) {
		t.Error("0 != 0.001 with av==0 (deadband disabled) should not be equal")
	}
}

func TestValuesEqual_Float64_NegativeValue_Deadband(t *testing.T) {
	// av = -100, deadband 1% → tolerance = |-100| * 1/100 = 1.0
	// |-100 - (-101)| = 1.0 ≤ 1.0 → equal
	if !settings.ValuesEqual(float64(-100.0), float64(-101.0), 1.0) {
		t.Error("negative float64 within deadband should be equal")
	}
}

// ---------------------------------------------------------------------------
// valuesEqual — float32
// ---------------------------------------------------------------------------

func TestValuesEqual_Float32_Equal(t *testing.T) {
	if !settings.ValuesEqual(float32(1.5), float32(1.5), 0) {
		t.Error("identical float32 should be equal")
	}
}

func TestValuesEqual_Float32_WithinDeadband(t *testing.T) {
	// 1% of 100.0 = 1.0; |100.5 - 100| = 0.5 ≤ 1.0
	if !settings.ValuesEqual(float32(100.0), float32(100.5), 1.0) {
		t.Error("float32 within deadband should be equal")
	}
}

func TestValuesEqual_Float32_TypeMismatch(t *testing.T) {
	// float32 vs float64 must not be equal
	if settings.ValuesEqual(float32(1.0), float64(1.0), 0) {
		t.Error("float32 vs float64 type mismatch should not be equal")
	}
}

// ---------------------------------------------------------------------------
// valuesEqual — integer types
// ---------------------------------------------------------------------------

func TestValuesEqual_Int(t *testing.T) {
	if !settings.ValuesEqual(int(42), int(42), 5.0) {
		t.Error("int equal should be true")
	}
	if settings.ValuesEqual(int(42), int(43), 5.0) {
		t.Error("int 42 != 43 should be false (int deadband not applied)")
	}
}

func TestValuesEqual_Int16(t *testing.T) {
	if !settings.ValuesEqual(int16(10), int16(10), 0) {
		t.Error("int16 equal should be true")
	}
	if settings.ValuesEqual(int16(10), int16(11), 0) {
		t.Error("int16 10 != 11 should be false")
	}
}

func TestValuesEqual_Int32(t *testing.T) {
	if !settings.ValuesEqual(int32(1000), int32(1000), 0) {
		t.Error("int32 equal should be true")
	}
}

func TestValuesEqual_Uint16(t *testing.T) {
	if !settings.ValuesEqual(uint16(65535), uint16(65535), 0) {
		t.Error("uint16 max equal should be true")
	}
}

func TestValuesEqual_Uint32(t *testing.T) {
	if !settings.ValuesEqual(uint32(0), uint32(0), 0) {
		t.Error("uint32 zero equal should be true")
	}
	if settings.ValuesEqual(uint32(1), uint32(2), 0) {
		t.Error("uint32 1 != 2 should be false")
	}
}

func TestValuesEqual_Int_TypeMismatch(t *testing.T) {
	// int vs int32 must not be equal
	if settings.ValuesEqual(int(1), int32(1), 0) {
		t.Error("int vs int32 should not be equal (type mismatch)")
	}
}

// ---------------------------------------------------------------------------
// valuesEqual — bool
// ---------------------------------------------------------------------------

func TestValuesEqual_Bool_Same(t *testing.T) {
	if !settings.ValuesEqual(true, true, 0) {
		t.Error("true == true should be equal")
	}
	if !settings.ValuesEqual(false, false, 0) {
		t.Error("false == false should be equal")
	}
}

func TestValuesEqual_Bool_Different(t *testing.T) {
	if settings.ValuesEqual(true, false, 0) {
		t.Error("true != false should not be equal")
	}
}

// ---------------------------------------------------------------------------
// valuesEqual — string (default case)
// ---------------------------------------------------------------------------

func TestValuesEqual_String_Equal(t *testing.T) {
	if !settings.ValuesEqual("hello", "hello", 0) {
		t.Error("identical strings should be equal")
	}
}

func TestValuesEqual_String_NotEqual(t *testing.T) {
	if settings.ValuesEqual("hello", "world", 0) {
		t.Error("different strings should not be equal")
	}
}

// ---------------------------------------------------------------------------
// parseInt helper
// ---------------------------------------------------------------------------

func TestParseInt_Valid(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"0", 0},
		{"1", 1},
		{"60", 60},
		{"-1", -1},
		{"2147483647", 2147483647},
	}
	for _, c := range cases {
		if got := settings.ParseInt(c.input, 99); got != c.want {
			t.Errorf("ParseInt(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestParseInt_Invalid_UsesDefault(t *testing.T) {
	cases := []string{"", "abc", "1.5", " 1", "1 "}
	for _, s := range cases {
		if got := settings.ParseInt(s, 42); got != 42 {
			t.Errorf("ParseInt(%q) = %d, want default 42", s, got)
		}
	}
}

// ---------------------------------------------------------------------------
// parseFloat helper
// ---------------------------------------------------------------------------

func TestParseFloat_Valid(t *testing.T) {
	cases := []struct {
		input string
		want  float64
	}{
		{"0", 0},
		{"0.5", 0.5},
		{"1.0", 1.0},
		{"-3.14", -3.14},
		{"100", 100},
	}
	for _, c := range cases {
		if got := settings.ParseFloat(c.input, 99.9); got != c.want {
			t.Errorf("ParseFloat(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestParseFloat_Invalid_UsesDefault(t *testing.T) {
	cases := []string{"", "abc", "one", " 1.0"}
	for _, s := range cases {
		if got := settings.ParseFloat(s, 3.14); got != 3.14 {
			t.Errorf("ParseFloat(%q) = %v, want default 3.14", s, got)
		}
	}
}
