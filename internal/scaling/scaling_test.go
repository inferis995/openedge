package scaling

import (
	"math"
	"testing"
)

func TestApply_Disabled(t *testing.T) {
	cfg := Config{Enabled: false, RawMin: 4, RawMax: 20, EuMin: 0, EuMax: 100}
	if got := Apply(12.0, cfg); got != 12.0 {
		t.Errorf("disabled scaling changed value: got %v", got)
	}
}

func TestApply_Linear(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 4, RawMax: 20, EuMin: 0, EuMax: 100}
	cases := []struct{ raw, want float64 }{
		{4, 0},
		{20, 100},
		{12, 50},
	}
	for _, c := range cases {
		got := Apply(c.raw, cfg).(float64)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Apply(%v) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestApply_Clamp(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 4, RawMax: 20, EuMin: 0, EuMax: 100, Clamp: true}
	if v := Apply(0.0, cfg).(float64); v != 0 {
		t.Errorf("clamp below: got %v, want 0", v)
	}
	if v := Apply(25.0, cfg).(float64); v != 100 {
		t.Errorf("clamp above: got %v, want 100", v)
	}
}

func TestApply_ZeroSpan(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 10, RawMax: 10, EuMin: 0, EuMax: 100}
	if got := Apply(10.0, cfg); got != 10.0 {
		t.Errorf("zero span should return raw unchanged: got %v", got)
	}
}

func TestApply_BoolInvert(t *testing.T) {
	cfg := Config{Enabled: true, Invert: true}
	if got := Apply(true, cfg).(bool); got != false {
		t.Errorf("bool invert true→false failed")
	}
	if got := Apply(false, cfg).(bool); got != true {
		t.Errorf("bool invert false→true failed")
	}
}

func TestApply_BoolNoInvert(t *testing.T) {
	cfg := Config{Enabled: true, Invert: false}
	if got := Apply(true, cfg).(bool); got != true {
		t.Errorf("bool no-invert changed value")
	}
}

func TestApply_NonNumeric(t *testing.T) {
	cfg := Config{Enabled: true, RawMin: 4, RawMax: 20, EuMin: 0, EuMax: 100}
	// String values should pass through unchanged.
	if got := Apply("hello", cfg); got != "hello" {
		t.Errorf("non-numeric should pass through: got %v", got)
	}
}
