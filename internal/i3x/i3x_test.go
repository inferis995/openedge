package i3x_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/i3x"
)

// ---------------------------------------------------------------------------
// MapDataType
// ---------------------------------------------------------------------------

func TestMapDataType(t *testing.T) {
	cases := []struct {
		internal string
		want     i3x.DataType
	}{
		{"BOOL", i3x.DataTypeBoolean},
		{"INT", i3x.DataTypeInt32},
		{"DINT", i3x.DataTypeInt32},
		{"REAL", i3x.DataTypeFloat},
		{"STRING", i3x.DataTypeString},
		{"", i3x.DataTypeString},      // unknown → String
		{"WORD", i3x.DataTypeString},  // unknown → String
		{"FLOAT", i3x.DataTypeString}, // "FLOAT" is not "REAL" — maps to String
	}
	for _, c := range cases {
		if got := i3x.MapDataType(c.internal); got != c.want {
			t.Errorf("MapDataType(%q) = %q, want %q", c.internal, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// MapQuality
// ---------------------------------------------------------------------------

func TestMapQuality_GoodWhenQuality0AndHasValue(t *testing.T) {
	if got := i3x.MapQuality(0, true); got != i3x.QualityGood {
		t.Errorf("MapQuality(0, true) = %v, want QualityGood (%v)", got, i3x.QualityGood)
	}
}

func TestMapQuality_UncertainWhenQuality1AndHasValue(t *testing.T) {
	if got := i3x.MapQuality(1, true); got != i3x.QualityUncertain {
		t.Errorf("MapQuality(1, true) = %v, want QualityUncertain (%v)", got, i3x.QualityUncertain)
	}
}

func TestMapQuality_BadWhenQuality2AndHasValue(t *testing.T) {
	if got := i3x.MapQuality(2, true); got != i3x.QualityBad {
		t.Errorf("MapQuality(2, true) = %v, want QualityBad (%v)", got, i3x.QualityBad)
	}
}

func TestMapQuality_BadWhenNoValue(t *testing.T) {
	// Missing value always reports Bad, regardless of stored quality code.
	for _, q := range []int{0, 1, 2, 99} {
		if got := i3x.MapQuality(q, false); got != i3x.QualityBad {
			t.Errorf("MapQuality(%d, false) = %v, want QualityBad", q, got)
		}
	}
}

func TestMapQuality_BadForUnknownQualityCode(t *testing.T) {
	// Any unexpected quality code (not 0 or 1) is treated as Bad.
	for _, q := range []int{3, 10, 99, -1} {
		if got := i3x.MapQuality(q, true); got != i3x.QualityBad {
			t.Errorf("MapQuality(%d, true) = %v, want QualityBad", q, got)
		}
	}
}

// ---------------------------------------------------------------------------
// MapSeverity
// ---------------------------------------------------------------------------

func TestMapSeverity(t *testing.T) {
	cases := []struct {
		s    string
		want i3x.AlarmSeverity
	}{
		{"info", i3x.AlarmSeverityInfo},
		{"warning", i3x.AlarmSeverityWarning},
		{"critical", i3x.AlarmSeverityCritical},
		{"", i3x.AlarmSeverityInfo},        // default
		{"unknown", i3x.AlarmSeverityInfo}, // default
		{"INFO", i3x.AlarmSeverityInfo},    // case-sensitive — falls through to default
	}
	for _, c := range cases {
		if got := i3x.MapSeverity(c.s); got != c.want {
			t.Errorf("MapSeverity(%q) = %q, want %q", c.s, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// MapStatus
// ---------------------------------------------------------------------------

func TestMapStatus(t *testing.T) {
	cases := []struct {
		s    string
		want i3x.AlarmStatus
	}{
		{"ACTIVE", i3x.AlarmStatusActive},
		{"ACKNOWLEDGED", i3x.AlarmStatusAcknowledged},
		{"CLEARED", i3x.AlarmStatusCleared},
		{"", i3x.AlarmStatusActive},        // default
		{"unknown", i3x.AlarmStatusActive}, // default
		{"active", i3x.AlarmStatusActive},  // case-sensitive — falls through to default
	}
	for _, c := range cases {
		if got := i3x.MapStatus(c.s); got != c.want {
			t.Errorf("MapStatus(%q) = %q, want %q", c.s, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Quality constant values (OPC-UA standard)
// ---------------------------------------------------------------------------

func TestQuality_Constants(t *testing.T) {
	if i3x.QualityBad != 0 {
		t.Errorf("QualityBad = %d, want 0", i3x.QualityBad)
	}
	if i3x.QualityUncertain != 64 {
		t.Errorf("QualityUncertain = %d, want 64", i3x.QualityUncertain)
	}
	if i3x.QualityGood != 192 {
		t.Errorf("QualityGood = %d, want 192", i3x.QualityGood)
	}
}
