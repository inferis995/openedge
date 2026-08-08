package alarms_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/alarms"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// ptr is a helper to get a pointer to a float64 literal.
func ptr(f float64) *float64 { return &f }

// ---------------------------------------------------------------------------
// isConditionViolated
// ---------------------------------------------------------------------------

func TestIsConditionViolated_High_Above(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "high", Threshold: ptr(100.0)}
	if !alarms.IsConditionViolated(def, 101.0) {
		t.Error("high: value above threshold should be violated")
	}
}

func TestIsConditionViolated_High_Equal(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "high", Threshold: ptr(100.0)}
	// val == threshold: NOT a strict violation (val > threshold required)
	if alarms.IsConditionViolated(def, 100.0) {
		t.Error("high: value equal to threshold should NOT be violated (strict >)")
	}
}

func TestIsConditionViolated_High_Below(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "high", Threshold: ptr(100.0)}
	if alarms.IsConditionViolated(def, 99.9) {
		t.Error("high: value below threshold should not be violated")
	}
}

func TestIsConditionViolated_High_NilThreshold(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "high", Threshold: nil}
	if alarms.IsConditionViolated(def, 999.0) {
		t.Error("high: nil threshold should never trigger")
	}
}

func TestIsConditionViolated_HighHigh_Above(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "high_high", Threshold: ptr(200.0)}
	if !alarms.IsConditionViolated(def, 200.1) {
		t.Error("high_high: value above threshold should be violated")
	}
}

func TestIsConditionViolated_Low_Below(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low", Threshold: ptr(20.0)}
	if !alarms.IsConditionViolated(def, 19.9) {
		t.Error("low: value below threshold should be violated")
	}
}

func TestIsConditionViolated_Low_Equal(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low", Threshold: ptr(20.0)}
	if alarms.IsConditionViolated(def, 20.0) {
		t.Error("low: value equal to threshold should NOT be violated (strict <)")
	}
}

func TestIsConditionViolated_Low_Above(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low", Threshold: ptr(20.0)}
	if alarms.IsConditionViolated(def, 20.1) {
		t.Error("low: value above threshold should not be violated")
	}
}

func TestIsConditionViolated_Low_NilThreshold(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low", Threshold: nil}
	if alarms.IsConditionViolated(def, -999.0) {
		t.Error("low: nil threshold should never trigger")
	}
}

func TestIsConditionViolated_LowLow_Below(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low_low", Threshold: ptr(5.0)}
	if !alarms.IsConditionViolated(def, 4.99) {
		t.Error("low_low: value below threshold should be violated")
	}
}

func TestIsConditionViolated_BoolTrue_Nonzero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_true"}
	if !alarms.IsConditionViolated(def, 1.0) {
		t.Error("bool_true: nonzero should be violated")
	}
}

func TestIsConditionViolated_BoolTrue_Zero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_true"}
	if alarms.IsConditionViolated(def, 0.0) {
		t.Error("bool_true: zero should NOT be violated")
	}
}

func TestIsConditionViolated_BoolTrue_NegativeNonzero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_true"}
	if !alarms.IsConditionViolated(def, -1.0) {
		t.Error("bool_true: negative nonzero should be violated")
	}
}

func TestIsConditionViolated_BoolFalse_Zero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_false"}
	if !alarms.IsConditionViolated(def, 0.0) {
		t.Error("bool_false: zero should be violated")
	}
}

func TestIsConditionViolated_BoolFalse_Nonzero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_false"}
	if alarms.IsConditionViolated(def, 1.0) {
		t.Error("bool_false: nonzero should NOT be violated")
	}
}

func TestIsConditionViolated_UnknownType(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "unknown_type", Threshold: ptr(50.0)}
	if alarms.IsConditionViolated(def, 999.0) {
		t.Error("unknown alarm type should never trigger")
	}
}

func TestIsConditionViolated_EmptyType(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: ""}
	if alarms.IsConditionViolated(def, 999.0) {
		t.Error("empty alarm type should never trigger")
	}
}

// ---------------------------------------------------------------------------
// isCleared — hysteresis / deadband logic
// ---------------------------------------------------------------------------

func TestIsCleared_High_ClearedBelowDeadband(t *testing.T) {
	// threshold=100, deadband=2 → cleared when val ≤ 98
	def := models.AlarmDefinition{AlarmType: "high", Threshold: ptr(100.0), Deadband: 2.0}
	if !alarms.IsCleared(def, 98.0) {
		t.Error("high: value at threshold-deadband should be cleared")
	}
	if !alarms.IsCleared(def, 97.9) {
		t.Error("high: value below threshold-deadband should be cleared")
	}
}

func TestIsCleared_High_NotClearedAboveDeadband(t *testing.T) {
	// threshold=100, deadband=2 → NOT cleared when val > 98
	def := models.AlarmDefinition{AlarmType: "high", Threshold: ptr(100.0), Deadband: 2.0}
	if alarms.IsCleared(def, 98.1) {
		t.Error("high: value above threshold-deadband should NOT be cleared yet")
	}
	if alarms.IsCleared(def, 100.0) {
		t.Error("high: value at threshold should NOT be cleared")
	}
}

func TestIsCleared_High_ZeroDeadband(t *testing.T) {
	// deadband=0 → cleared at or below threshold
	def := models.AlarmDefinition{AlarmType: "high", Threshold: ptr(50.0), Deadband: 0}
	if !alarms.IsCleared(def, 50.0) {
		t.Error("high zero-deadband: value at threshold should clear")
	}
	if !alarms.IsCleared(def, 49.9) {
		t.Error("high zero-deadband: value below threshold should clear")
	}
}

func TestIsCleared_High_NilThreshold(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "high", Threshold: nil, Deadband: 5.0}
	if alarms.IsCleared(def, 0.0) {
		t.Error("high: nil threshold should not clear (returns false)")
	}
}

func TestIsCleared_HighHigh_Hysteresis(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "high_high", Threshold: ptr(150.0), Deadband: 10.0}
	// Cleared only at ≤ 140
	if !alarms.IsCleared(def, 140.0) {
		t.Error("high_high: value at threshold-deadband should clear")
	}
	if alarms.IsCleared(def, 141.0) {
		t.Error("high_high: value above threshold-deadband should not clear")
	}
}

func TestIsCleared_Low_ClearedAboveDeadband(t *testing.T) {
	// threshold=20, deadband=3 → cleared when val ≥ 23
	def := models.AlarmDefinition{AlarmType: "low", Threshold: ptr(20.0), Deadband: 3.0}
	if !alarms.IsCleared(def, 23.0) {
		t.Error("low: value at threshold+deadband should be cleared")
	}
	if !alarms.IsCleared(def, 23.1) {
		t.Error("low: value above threshold+deadband should be cleared")
	}
}

func TestIsCleared_Low_NotClearedBelowDeadband(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low", Threshold: ptr(20.0), Deadband: 3.0}
	if alarms.IsCleared(def, 22.9) {
		t.Error("low: value below threshold+deadband should NOT be cleared yet")
	}
	if alarms.IsCleared(def, 20.0) {
		t.Error("low: value at threshold should NOT be cleared")
	}
}

func TestIsCleared_Low_ZeroDeadband(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low", Threshold: ptr(10.0), Deadband: 0}
	if !alarms.IsCleared(def, 10.0) {
		t.Error("low zero-deadband: at threshold should clear")
	}
}

func TestIsCleared_Low_NilThreshold(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low", Threshold: nil, Deadband: 5.0}
	if alarms.IsCleared(def, 100.0) {
		t.Error("low: nil threshold should not clear")
	}
}

func TestIsCleared_LowLow_Hysteresis(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "low_low", Threshold: ptr(5.0), Deadband: 2.0}
	// Cleared only at ≥ 7
	if !alarms.IsCleared(def, 7.0) {
		t.Error("low_low: value at threshold+deadband should clear")
	}
	if alarms.IsCleared(def, 6.9) {
		t.Error("low_low: value below threshold+deadband should not clear")
	}
}

func TestIsCleared_BoolTrue_Zero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_true"}
	if !alarms.IsCleared(def, 0.0) {
		t.Error("bool_true: zero should be cleared")
	}
}

func TestIsCleared_BoolTrue_Nonzero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_true"}
	if alarms.IsCleared(def, 1.0) {
		t.Error("bool_true: nonzero should NOT be cleared")
	}
}

func TestIsCleared_BoolFalse_Nonzero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_false"}
	if !alarms.IsCleared(def, 1.0) {
		t.Error("bool_false: nonzero should be cleared")
	}
}

func TestIsCleared_BoolFalse_Zero(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_false"}
	if alarms.IsCleared(def, 0.0) {
		t.Error("bool_false: zero should NOT be cleared")
	}
}

func TestIsCleared_UnknownType(t *testing.T) {
	// Unknown type → returns true (safe default: consider it cleared)
	def := models.AlarmDefinition{AlarmType: "weird"}
	if !alarms.IsCleared(def, 42.0) {
		t.Error("unknown alarm type should default to cleared=true")
	}
}

// ---------------------------------------------------------------------------
// Alarm type symmetry: violated ↔ cleared are strict inverses
// ---------------------------------------------------------------------------

func TestViolatedAndClearedAreInverse_BoolTrue(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_true"}
	for _, val := range []float64{0.0, 1.0, -1.0, 0.5} {
		v := alarms.IsConditionViolated(def, val)
		c := alarms.IsCleared(def, val)
		// For boolean types, violated XOR cleared should always be true
		if v == c {
			t.Errorf("bool_true val=%v: violated=%v cleared=%v — must be complementary", val, v, c)
		}
	}
}

func TestViolatedAndClearedAreInverse_BoolFalse(t *testing.T) {
	def := models.AlarmDefinition{AlarmType: "bool_false"}
	for _, val := range []float64{0.0, 1.0, -1.0} {
		v := alarms.IsConditionViolated(def, val)
		c := alarms.IsCleared(def, val)
		if v == c {
			t.Errorf("bool_false val=%v: violated=%v cleared=%v — must be complementary", val, v, c)
		}
	}
}

// ---------------------------------------------------------------------------
// toFloat — type coverage
// ---------------------------------------------------------------------------

func TestToFloat_Float64(t *testing.T) {
	if v, ok := alarms.ToFloat(float64(3.14)); !ok || v != 3.14 {
		t.Errorf("float64: got (%v, %v)", v, ok)
	}
}

func TestToFloat_Float32(t *testing.T) {
	v, ok := alarms.ToFloat(float32(1.5))
	if !ok {
		t.Fatal("float32: ok=false")
	}
	if v != float64(float32(1.5)) {
		t.Errorf("float32: got %v", v)
	}
}

func TestToFloat_Int(t *testing.T) {
	if v, ok := alarms.ToFloat(int(42)); !ok || v != 42.0 {
		t.Errorf("int: got (%v, %v)", v, ok)
	}
}

func TestToFloat_Int16(t *testing.T) {
	if v, ok := alarms.ToFloat(int16(100)); !ok || v != 100.0 {
		t.Errorf("int16: got (%v, %v)", v, ok)
	}
}

func TestToFloat_Int32(t *testing.T) {
	if v, ok := alarms.ToFloat(int32(-32768)); !ok || v != -32768.0 {
		t.Errorf("int32: got (%v, %v)", v, ok)
	}
}

func TestToFloat_Int64(t *testing.T) {
	if v, ok := alarms.ToFloat(int64(1000000)); !ok || v != 1000000.0 {
		t.Errorf("int64: got (%v, %v)", v, ok)
	}
}

func TestToFloat_Uint16(t *testing.T) {
	if v, ok := alarms.ToFloat(uint16(65535)); !ok || v != 65535.0 {
		t.Errorf("uint16: got (%v, %v)", v, ok)
	}
}

func TestToFloat_Uint32(t *testing.T) {
	if v, ok := alarms.ToFloat(uint32(4294967295)); !ok || v != 4294967295.0 {
		t.Errorf("uint32: got (%v, %v)", v, ok)
	}
}

func TestToFloat_Uint64(t *testing.T) {
	if v, ok := alarms.ToFloat(uint64(1000)); !ok || v != 1000.0 {
		t.Errorf("uint64: got (%v, %v)", v, ok)
	}
}

func TestToFloat_BoolTrue(t *testing.T) {
	if v, ok := alarms.ToFloat(true); !ok || v != 1.0 {
		t.Errorf("bool true: got (%v, %v)", v, ok)
	}
}

func TestToFloat_BoolFalse(t *testing.T) {
	if v, ok := alarms.ToFloat(false); !ok || v != 0.0 {
		t.Errorf("bool false: got (%v, %v)", v, ok)
	}
}

func TestToFloat_String_NotOK(t *testing.T) {
	if _, ok := alarms.ToFloat("42"); ok {
		t.Error("string should not convert to float")
	}
}

func TestToFloat_Nil_NotOK(t *testing.T) {
	if _, ok := alarms.ToFloat(nil); ok {
		t.Error("nil should not convert to float")
	}
}

func TestToFloat_Struct_NotOK(t *testing.T) {
	type dummy struct{ x int }
	if _, ok := alarms.ToFloat(dummy{x: 1}); ok {
		t.Error("struct should not convert to float")
	}
}

func TestToFloat_Zero(t *testing.T) {
	if v, ok := alarms.ToFloat(float64(0)); !ok || v != 0.0 {
		t.Errorf("zero float64: got (%v, %v)", v, ok)
	}
}

func TestToFloat_NegativeFloat(t *testing.T) {
	if v, ok := alarms.ToFloat(float64(-273.15)); !ok || v != -273.15 {
		t.Errorf("negative float64: got (%v, %v)", v, ok)
	}
}

// ---------------------------------------------------------------------------
// EvaluateTag — bad quality skipped
// ---------------------------------------------------------------------------

func TestEvaluateTag_BadQuality_NoCallback(t *testing.T) {
	// Manager with nil DB, nil mqtt — EvaluateTag with quality != 192 must not panic.
	m := alarms.NewManager(nil, nil, 0)
	fired := false
	m.OnAlarmEvent = func(eventID int, tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
		fired = true
	}
	// quality=0 → skip evaluation entirely
	m.EvaluateTag(1, "tag1", float64(999.0), 0)
	if fired {
		t.Error("OnAlarmEvent must not fire for bad-quality data")
	}
}

func TestEvaluateTag_BadQuality_VariousValues(t *testing.T) {
	m := alarms.NewManager(nil, nil, 0)
	fired := false
	m.OnAlarmEvent = func(_ int, _ int, _ string, _ models.AlarmDefinition, _ float64, _ string) {
		fired = true
	}
	for _, quality := range []int{0, 1, 64, 128, 191, 193, 255} {
		m.EvaluateTag(1, "tag1", float64(1000.0), quality)
	}
	if fired {
		t.Error("OnAlarmEvent must not fire for any non-192 quality")
	}
}

func TestEvaluateTag_UnsupportedType_NoCallback(t *testing.T) {
	// string value is not convertible to float — should be skipped.
	m := alarms.NewManager(nil, nil, 0)
	fired := false
	m.OnAlarmEvent = func(_ int, _ int, _ string, _ models.AlarmDefinition, _ float64, _ string) {
		fired = true
	}
	m.EvaluateTag(1, "tag1", "not-a-number", 192)
	if fired {
		t.Error("OnAlarmEvent must not fire for non-numeric value")
	}
}

func TestEvaluateTag_NoDefinitions_NoCallback(t *testing.T) {
	// No alarm definitions loaded (nil DB means LoadDefinitions is a no-op).
	m := alarms.NewManager(nil, nil, 0)
	fired := false
	m.OnAlarmEvent = func(_ int, _ int, _ string, _ models.AlarmDefinition, _ float64, _ string) {
		fired = true
	}
	m.EvaluateTag(99, "tagX", float64(500.0), 192)
	if fired {
		t.Error("OnAlarmEvent must not fire when no definitions exist for tag")
	}
}

// ---------------------------------------------------------------------------
// NewManager — nil-safe construction
// ---------------------------------------------------------------------------

func TestNewManager_NilDB_NoPanic(t *testing.T) {
	// Should not panic even with nil DB and nil MQTT.
	m := alarms.NewManager(nil, nil, 0)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestNewManager_OnAlarmEventNilByDefault(t *testing.T) {
	m := alarms.NewManager(nil, nil, 0)
	// EvaluateTag with good quality and numeric value must not panic
	// when OnAlarmEvent is nil (nil callback check).
	m.EvaluateTag(1, "t", float64(0.0), 192)
}

// A delay exists to require the alarm condition to PERSIST. A transient spike
// that falls back before the delay elapses must not raise an alarm.
//
// The regression this guards: the clearing test uses the deadband, so a value
// between (threshold - deadband) and threshold was neither "violating" nor
// "cleared". The pending track survived and tickDelays raised a full alarm on a
// process that had been back in spec for almost the entire delay.
func TestPendingAlarmCancelledWhenConditionStops(t *testing.T) {
	threshold := 100.0
	def := models.AlarmDefinition{
		ID:           1,
		AlarmType:    "high",
		Threshold:    &threshold,
		Deadband:     5,
		DelaySeconds: 30,
		Enabled:      true,
	}

	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{def})

	// Spike above the threshold: starts the delay window, fires nothing yet.
	m.EvaluateTag(42, "temp", 101.0, 192)
	if got := m.PendingCount(42); got != 1 {
		t.Fatalf("after spike: pending tracks = %d, want 1", got)
	}

	// Settles at 97: below the threshold but inside the deadband band, so it is
	// NOT "cleared" either. The pending alarm must still be cancelled.
	m.EvaluateTag(42, "temp", 97.0, 192)
	if got := m.PendingCount(42); got != 0 {
		t.Fatalf("after settling back in spec: pending tracks = %d, want 0", got)
	}

	// The delay elapsing must not resurrect it.
	var fired int
	m.OnAlarmEvent = func(int, int, string, models.AlarmDefinition, float64, string) { fired++ }
	m.TickDelays()
	if fired != 0 {
		t.Errorf("alarm fired %d time(s) for a condition that stopped before the delay", fired)
	}
}

// A condition that genuinely persists must still fire once the delay elapses.
func TestPersistingConditionStillFiresAfterDelay(t *testing.T) {
	threshold := 100.0
	def := models.AlarmDefinition{
		ID:           1,
		AlarmType:    "high",
		Threshold:    &threshold,
		Deadband:     5,
		DelaySeconds: 0, // fire immediately, no ticker needed
		Enabled:      true,
	}

	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{def})

	var fired int
	var lastStatus string
	m.OnAlarmEvent = func(_ int, _ int, _ string, _ models.AlarmDefinition, _ float64, status string) {
		fired++
		lastStatus = status
	}

	m.EvaluateTag(42, "temp", 101.0, 192)
	if fired != 1 || lastStatus != "ACTIVE" {
		t.Fatalf("violating value: fired=%d status=%q, want 1 ACTIVE", fired, lastStatus)
	}

	// Back well below the deadband band: the fired alarm clears.
	m.EvaluateTag(42, "temp", 90.0, 192)
	if fired != 2 || lastStatus != "CLEARED" {
		t.Errorf("recovered value: fired=%d status=%q, want 2 CLEARED", fired, lastStatus)
	}
}
