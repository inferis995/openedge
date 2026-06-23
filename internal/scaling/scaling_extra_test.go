package scaling

// Additional edge-case tests for scaling.Apply and scaling.toFloat64.
// Uses the internal package (white-box) so we can call toFloat64 directly.

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Apply — inverted EU range (EuMin > EuMax)
// ---------------------------------------------------------------------------

func TestApply_InvertedEURange(t *testing.T) {
	// EuMin=100, EuMax=0 — physically reversed scale (e.g. pressure that reads
	// 100 at raw=4 and 0 at raw=20).
	cfg := Config{Enabled: true, RawMin: 4, RawMax: 20, EuMin: 100, EuMax: 0}
	cases := []struct{ raw, want float64 }{
		{4, 100},
		{20, 0},
		{12, 50},
	}
	for _, c := range cases {
		got := Apply(c.raw, cfg).(float64)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("inverted range Apply(%v) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestApply_ClampWithInvertedEURange(t *testing.T) {
	// EuMin=100, EuMax=0, Clamp=true
	// lo = min(100,0)=0, hi = max(100,0)=100
	cfg := Config{Enabled: true, RawMin: 4, RawMax: 20, EuMin: 100, EuMax: 0, Clamp: true}
	// raw=0 → eu would be (0-4)/(20-4)*(-100)+100 = 125 → clamped to 100
	got := Apply(0.0, cfg).(float64)
	if got != 100 {
		t.Errorf("clamp below in inverted range: got %v, want 100", got)
	}
	// raw=25 → eu would be (25-4)/(20-4)*(-100)+100 = -31.25 → clamped to 0
	got = Apply(25.0, cfg).(float64)
	if got != 0 {
		t.Errorf("clamp above in inverted range: got %v, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Apply — numeric types other than float64
// ---------------------------------------------------------------------------

func TestApply_Int32(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 100, EuMin: 0, EuMax: 10}
	got := Apply(int32(50), cfg).(float64)
	if math.Abs(got-5.0) > 1e-9 {
		t.Errorf("int32 Apply(50) = %v, want 5.0", got)
	}
}

func TestApply_Int64(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 1000, EuMin: 0, EuMax: 1}
	got := Apply(int64(500), cfg).(float64)
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("int64 Apply(500) = %v, want 0.5", got)
	}
}

func TestApply_Uint32(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 100, EuMin: 0, EuMax: 100}
	got := Apply(uint32(75), cfg).(float64)
	if math.Abs(got-75.0) > 1e-9 {
		t.Errorf("uint32 Apply(75) = %v, want 75.0", got)
	}
}

func TestApply_Float32(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 10, EuMin: 0, EuMax: 100}
	got := Apply(float32(5), cfg).(float64)
	if math.Abs(got-50.0) > 1e-9 {
		t.Errorf("float32 Apply(5) = %v, want 50.0", got)
	}
}

// ---------------------------------------------------------------------------
// Apply — edge: RawMin == 0 and negative raw values (no clamp)
// ---------------------------------------------------------------------------

func TestApply_NegativeRawNoClamp(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 100, EuMin: 0, EuMax: 100}
	got := Apply(-10.0, cfg).(float64)
	// eu = (-10 - 0) / 100 * 100 + 0 = -10
	if math.Abs(got-(-10.0)) > 1e-9 {
		t.Errorf("negative raw without clamp: got %v, want -10.0", got)
	}
}

func TestApply_NegativeRawWithClamp(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 100, EuMin: 0, EuMax: 100, Clamp: true}
	got := Apply(-10.0, cfg).(float64)
	if got != 0 {
		t.Errorf("negative raw with clamp: got %v, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Apply — nil input (not a supported type)
// ---------------------------------------------------------------------------

func TestApply_NilInput_Enabled(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 10, EuMin: 0, EuMax: 100}
	got := Apply(nil, cfg)
	if got != nil {
		t.Errorf("nil input should pass through: got %v", got)
	}
}

func TestApply_NilInput_Disabled(t *testing.T) {
	cfg := Config{Enabled: false}
	got := Apply(nil, cfg)
	if got != nil {
		t.Errorf("nil input disabled: got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Apply — EuMin == EuMax (zero output span)
// ---------------------------------------------------------------------------

func TestApply_ZeroOutputSpan(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 10, EuMin: 50, EuMax: 50}
	// eu = (raw-0)/10 * 0 + 50 = 50 for any raw
	got := Apply(7.0, cfg).(float64)
	if got != 50.0 {
		t.Errorf("zero output span: got %v, want 50.0", got)
	}
}

// ---------------------------------------------------------------------------
// Apply — large values (no overflow in float64)
// ---------------------------------------------------------------------------

func TestApply_LargeValues(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 0, RawMax: 1e12, EuMin: 0, EuMax: 1e6}
	got := Apply(5e11, cfg).(float64)
	if math.Abs(got-5e5) > 1e-3 {
		t.Errorf("large values: got %v, want 5e5", got)
	}
}

// ---------------------------------------------------------------------------
// toFloat64 — internal helper (white-box, same package)
// ---------------------------------------------------------------------------

func TestToFloat64_Int(t *testing.T) {
	r := toFloat64(int(10))
	if r == nil || *r != 10.0 {
		t.Errorf("toFloat64(int) = %v", r)
	}
}

func TestToFloat64_Uint(t *testing.T) {
	r := toFloat64(uint(255))
	if r == nil || *r != 255.0 {
		t.Errorf("toFloat64(uint) = %v", r)
	}
}

func TestToFloat64_Uint64(t *testing.T) {
	r := toFloat64(uint64(1024))
	if r == nil || *r != 1024.0 {
		t.Errorf("toFloat64(uint64) = %v", r)
	}
}

func TestToFloat64_String_ReturnsNil(t *testing.T) {
	if r := toFloat64("42"); r != nil {
		t.Errorf("toFloat64(string) should be nil, got %v", r)
	}
}

func TestToFloat64_Bool_ReturnsNil(t *testing.T) {
	// bool is handled by Apply before toFloat64 is called, but toFloat64
	// itself should return nil for bool.
	if r := toFloat64(true); r != nil {
		t.Errorf("toFloat64(bool) should be nil, got %v", r)
	}
}

func TestToFloat64_Nil_ReturnsNil(t *testing.T) {
	if r := toFloat64(nil); r != nil {
		t.Errorf("toFloat64(nil) should be nil, got %v", r)
	}
}

// ---------------------------------------------------------------------------
// Apply — bool with Enabled=false (no inversion)
// ---------------------------------------------------------------------------

func TestApply_Bool_Disabled(t *testing.T) {
	cfg := Config{Enabled: false, Invert: true}
	if got := Apply(true, cfg); got != true {
		t.Errorf("disabled: bool true should pass through, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Apply — midpoint precision
// ---------------------------------------------------------------------------

func TestApply_MidpointPrecision(t *testing.T) {
	// 4–20 mA → 0–100: midpoint raw=12 must map to exactly 50.
	cfg := Config{Enabled: true, RawMin: 4, RawMax: 20, EuMin: 0, EuMax: 100}
	got := Apply(12.0, cfg).(float64)
	if math.Abs(got-50.0) > 1e-12 {
		t.Errorf("midpoint precision: got %.15f, want 50.0", got)
	}
}
