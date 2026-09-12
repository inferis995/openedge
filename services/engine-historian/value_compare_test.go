package main

import (
	"math"
	"testing"
)

// numericValue, hasValueChanged and exceedsDeadband are the comparison this
// service runs on every single reading from every plant.
//
// The comment on numericValue records what happened the last time they were
// wrong: previous values round-trip through Redis as JSON and come back as
// float64, while Sparkplug metrics decode to int32 or int64. The comparisons
// asserted on the concrete Go type, so every Sparkplug reading hit
// "different kinds of value — store it". historize_deadband and
// report-by-exception silently did nothing on that path, and a motionless 1 Hz
// tag with a deadband of 5.0 still wrote about 86,000 rows a day.
//
// It was fixed. It had no test.

func TestEveryNumericAGatewayCanProduceIsRecognised(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want float64
	}{
		{"float64, as Redis returns it", float64(20.5), 20.5},
		{"float32, as some drivers produce", float32(20.5), 20.5},
		{"int", int(20), 20},
		{"int32, as a Sparkplug metric decodes", int32(20), 20},
		{"int64, as a Sparkplug metric decodes", int64(20), 20},
		{"uint32, as a Modbus register reads", uint32(20), 20},
		{"uint64", uint64(20), 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := numericValue(c.in)
			if !ok {
				t.Fatalf("%T was not recognized as a number; every comparison against it "+
					"falls through to \"store it\"", c.in)
			}
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestWhatIsNotANumberSaysSo(t *testing.T) {
	for _, v := range []interface{}{"20", true, nil, []int{1}, struct{}{}} {
		if _, ok := numericValue(v); ok {
			t.Errorf("%#v was treated as a number", v)
		}
	}
}

// The exact case that was broken: the same quantity arriving as two different
// Go types must compare equal, or the deadband never applies to Sparkplug.
func TestTheSameNumberInDifferentGoTypesIsNotAChange(t *testing.T) {
	s := &HistorianService{}

	if s.hasValueChanged(float64(20), int32(20)) {
		t.Error("float64(20) from Redis and int32(20) from Sparkplug were called a change")
	}
	if s.hasValueChanged(float64(20), int64(20)) {
		t.Error("float64(20) and int64(20) were called a change")
	}
	if s.exceedsDeadband(float64(20), int32(20), 5.0) {
		t.Error("an unmoved value crossed a deadband of 5.0 because its Go type differed")
	}
}

// ---------------------------------------------------------------------------
// hasValueChanged
// ---------------------------------------------------------------------------

func TestAChangedNumberIsAChange(t *testing.T) {
	s := &HistorianService{}
	if !s.hasValueChanged(20.0, 20.1) {
		t.Error("20.0 -> 20.1 was not called a change")
	}
	if s.hasValueChanged(20.0, 20.0) {
		t.Error("20.0 -> 20.0 was called a change")
	}
}

func TestBooleansCompareAsBooleans(t *testing.T) {
	s := &HistorianService{}
	if !s.hasValueChanged(false, true) {
		t.Error("false -> true was not called a change")
	}
	if s.hasValueChanged(true, true) {
		t.Error("true -> true was called a change")
	}
}

// A BOOL tag can arrive as a number on one path and as a bool on another. The
// two have to be comparable, or every sample of such a tag is stored.
func TestABooleanAndItsNumberAreComparable(t *testing.T) {
	s := &HistorianService{}

	if s.hasValueChanged(1.0, true) {
		t.Error("previous 1.0 and new true were called a change")
	}
	if !s.hasValueChanged(1.0, false) {
		t.Error("previous 1.0 and new false were not called a change")
	}
	if s.hasValueChanged(true, 1.0) {
		t.Error("previous true and new 1.0 were called a change")
	}
	if !s.hasValueChanged(true, 0.0) {
		t.Error("previous true and new 0.0 were not called a change")
	}
}

// A string tag is compared as text. It is the fallback, and it has to work:
// without it a STRING tag would store every sample forever.
func TestStringsCompareAsText(t *testing.T) {
	s := &HistorianService{}
	if s.hasValueChanged("ciclo-A", "ciclo-A") {
		t.Error("an unchanged string was called a change")
	}
	if !s.hasValueChanged("ciclo-A", "ciclo-B") {
		t.Error("a changed string was not called a change")
	}
}

// ---------------------------------------------------------------------------
// exceedsDeadband
// ---------------------------------------------------------------------------

func TestTheDeadbandIsInclusiveAtItsEdge(t *testing.T) {
	s := &HistorianService{}

	// A movement of exactly the deadband IS stored: the comparison is >=.
	// Pinned because the boundary is the one thing an operator setting this
	// field will reason about, and either answer is defensible — so it must
	// not change by accident.
	if !s.exceedsDeadband(20.0, 25.0, 5.0) {
		t.Error("a movement of exactly the deadband was dropped")
	}
	if s.exceedsDeadband(20.0, 24.999, 5.0) {
		t.Error("a movement just under the deadband was stored")
	}
}

func TestTheDeadbandIgnoresDirection(t *testing.T) {
	s := &HistorianService{}
	if !s.exceedsDeadband(20.0, 14.0, 5.0) {
		t.Error("a drop of 6.0 did not cross a deadband of 5.0; only rises would be recorded")
	}
}

// A deadband of zero means "store every change", not "store nothing".
func TestAZeroDeadbandStoresAnyMovement(t *testing.T) {
	s := &HistorianService{}
	if !s.exceedsDeadband(20.0, 20.000001, 0) {
		t.Error("with a deadband of zero a movement was dropped")
	}
	if !s.exceedsDeadband(20.0, 20.0, 0) {
		t.Error("with a deadband of zero an equal value was dropped; >= 0 is always true " +
			"and this path must not become the place a sample disappears")
	}
}

// When the two sides are genuinely different kinds of thing, the safe answer is
// to store: losing a plant's data is worse than writing a row too many.
func TestGenuinelyDifferentKindsOfValueAreStored(t *testing.T) {
	s := &HistorianService{}
	if !s.exceedsDeadband(20.0, "avviato", 5.0) {
		t.Error("a number followed by a string was dropped")
	}
	if !s.exceedsDeadband(true, "avviato", 5.0) {
		t.Error("a boolean followed by a string was dropped")
	}
	if !s.exceedsDeadband(nil, 20.0, 5.0) {
		t.Error("a reading after an unknown previous value was dropped")
	}
}
