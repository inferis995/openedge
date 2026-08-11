package scaling_test

import (
	"errors"
	"math"
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/scaling"
)

// The defect this file pins down.
//
// Apply runs on the read path and its result replaces the raw value everywhere
// downstream, so a synoptic shows engineering units. Writes traveled the other
// way with no conversion at all: the number an operator typed reached the
// device register verbatim.
//
// The round-trip test below is the one that makes it undeniable. Before Reverse
// existed, writing the value a gauge was displaying produced a completely
// different reading a moment later.

// A pressure transmitter: 0..27648 raw (the S7 analog range) to 0..100 bar.
var pressure = scaling.Config{
	Enabled: true,
	RawMin:  0, RawMax: 27648,
	EuMin: 0, EuMax: 100,
}

func TestReverseIsTheInverseOfApply(t *testing.T) {
	for _, raw := range []float64{0, 1, 6912, 13824, 20736, 27648} {
		eu := scaling.Apply(raw, pressure)

		back, err := scaling.Reverse(eu, pressure)
		if err != nil {
			t.Fatalf("Reverse(%v) returned an error: %v", eu, err)
		}

		got, ok := back.(float64)
		if !ok {
			t.Fatalf("Reverse returned %T, want float64", back)
		}
		if math.Abs(got-raw) > 1e-6 {
			t.Errorf("round trip lost the value: raw %g → eu %v → raw %g", raw, eu, got)
		}
	}
}

// What an operator actually does: read the displayed value, type a new one in
// the same units, press write. The device must receive the raw equivalent.
func TestSetpointInEngineeringUnitsReachesTheDeviceAsRaw(t *testing.T) {
	// The operator asks for 50 bar.
	raw, err := scaling.Reverse(50.0, pressure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := 13824.0 // half of 27648
	got := raw.(float64)
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("50 bar became %g raw, want %g", got, want)
	}

	// And the device reading that register reports 50 bar back.
	if eu := scaling.Apply(got, pressure); math.Abs(eu.(float64)-50) > 1e-6 {
		t.Fatalf("writing %g raw reads back as %v, not 50 bar", got, eu)
	}

	// The bug, stated as a test: sending the engineering value straight through
	// — which is what every write did — lands three orders of magnitude away.
	unconverted := scaling.Apply(50.0, pressure)
	if math.Abs(unconverted.(float64)-50) < 1 {
		t.Fatal("this test no longer demonstrates anything: the unconverted path " +
			"now happens to agree with the converted one, so the scaling config is wrong")
	}
	t.Logf("without Reverse, a 50 bar setpoint would have read back as %.3f bar",
		unconverted.(float64))
}

// A setpoint the range cannot express is refused, not quietly clamped.
func TestOutOfRangeSetpointIsRefused(t *testing.T) {
	for _, v := range []float64{-1, 100.5, 5000} {
		_, err := scaling.Reverse(v, pressure)
		if err == nil {
			t.Errorf("Reverse(%g) was accepted; a value outside 0..100 must be refused", v)
			continue
		}
		var oor scaling.ErrOutOfRange
		if !errors.As(err, &oor) {
			t.Errorf("Reverse(%g) returned %v, want ErrOutOfRange", v, err)
		}
	}
}

// Both ends of the range are valid setpoints.
func TestRangeBoundsAreAccepted(t *testing.T) {
	for _, v := range []float64{0, 100} {
		if _, err := scaling.Reverse(v, pressure); err != nil {
			t.Errorf("Reverse(%g) was refused: %v — the bounds are inside the range", v, err)
		}
	}
}

// An inverted scale — 4..20 mA mapped to 100..0 % for a reverse-acting valve —
// must still round-trip, and must still refuse what falls outside.
func TestInvertedScaleRoundTrips(t *testing.T) {
	reverseActing := scaling.Config{
		Enabled: true,
		RawMin:  4, RawMax: 20,
		EuMin: 100, EuMax: 0,
	}

	raw, err := scaling.Reverse(75.0, reverseActing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eu := scaling.Apply(raw, reverseActing); math.Abs(eu.(float64)-75) > 1e-6 {
		t.Fatalf("75%% became %v raw which reads back as %v", raw, eu)
	}

	if _, err := scaling.Reverse(101.0, reverseActing); err == nil {
		t.Error("101% was accepted on a 0..100 range")
	}
}

// Booleans invert; inversion is its own inverse.
func TestBooleanInversionRoundTrips(t *testing.T) {
	inverted := scaling.Config{Enabled: true, Invert: true}

	back, err := scaling.Reverse(scaling.Apply(true, inverted), inverted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if back != true {
		t.Fatalf("true → %v → %v, want true", scaling.Apply(true, inverted), back)
	}
}

// Scaling off means the value passes through untouched, which is what a plant
// working in raw counts relies on.
func TestDisabledScalingPassesThrough(t *testing.T) {
	v, err := scaling.Reverse(1234.0, scaling.Config{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1234.0 {
		t.Fatalf("got %v, want the value unchanged", v)
	}
}

// A zero engineering span defines no conversion; passing the value through is
// the only honest option, and it must not divide by zero doing it.
func TestZeroSpanDoesNotPanic(t *testing.T) {
	v, err := scaling.Reverse(7.0, scaling.Config{Enabled: true, EuMin: 5, EuMax: 5, RawMin: 0, RawMax: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 7.0 {
		t.Fatalf("got %v, want the value unchanged", v)
	}
}
