package main

import (
	"math"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Topic parsing
// ---------------------------------------------------------------------------

func TestAWellFormedDataTopicIsSplitIntoItsFiveNames(t *testing.T) {
	dt, err := parseDataTopic("data/acme/stabilimento/reparto-1/plc-1/temperatura")
	if err != nil {
		t.Fatalf("a valid topic was rejected: %v", err)
	}
	if dt.org != "acme" || dt.site != "stabilimento" || dt.area != "reparto-1" ||
		dt.gateway != "plc-1" || dt.alias != "temperatura" {
		t.Fatalf("topic split wrongly: %+v", dt)
	}
}

// The defect this function exists for. A site called "Linea 1/2" used to
// publish on a seven-level topic; the old parser read the first five levels
// and threw the sixth away, so it looked up site "linea-1" and area "2".
// That hierarchy does not exist, the lookup failed on every sample, and
// nothing anywhere said so.
func TestATopicWithAnExtraLevelIsRejectedRatherThanTruncated(t *testing.T) {
	dt, err := parseDataTopic("data/acme/linea-1/2/reparto/plc-1/temperatura")
	if err == nil {
		t.Fatalf("a seven-level topic was accepted and silently truncated to %+v", dt)
	}
}

func TestATopicWithTooFewLevelsIsRejected(t *testing.T) {
	if _, err := parseDataTopic("data/acme/stabilimento/reparto-1/plc-1"); err == nil {
		t.Fatal("a five-level topic was accepted; the alias would be empty")
	}
}

func TestATopicThatIsNotDataIsRejected(t *testing.T) {
	for _, topic := range []string{
		"sys/health/1/2",
		"spBv1.0/acme-site/DDATA/area-gw/plc-1",
		"database/acme/s/a/g/tag", // shares a prefix but is not the "data" level
		"",
	} {
		if _, err := parseDataTopic(topic); err == nil {
			t.Errorf("%q was accepted as a data topic", topic)
		}
	}
}

// An empty level means a name was blank upstream. Accepting it would look up
// the empty string, which matches nothing, once per message forever.
func TestATopicWithAnEmptyLevelIsRejected(t *testing.T) {
	if _, err := parseDataTopic("data/acme//reparto-1/plc-1/temperatura"); err == nil {
		t.Fatal("a topic with an empty site was accepted")
	}
}

// ---------------------------------------------------------------------------
// Value conversion
// ---------------------------------------------------------------------------

// The important half of historianFloat is the refusal. A value that cannot be
// read as a number must not become 0.0 with GOOD quality: on a trend that is a
// pressure of zero, a temperature of zero, a closed valve — a reading an
// operator has no way to recognize as fabricated.
func TestANonNumericValueIsRefusedRatherThanTurnedIntoZero(t *testing.T) {
	for _, v := range []interface{}{
		nil,
		"RUNNING",
		"",
		"  ",
		struct{ A int }{1},
		[]int{1, 2},
		map[string]int{"a": 1},
	} {
		if got, ok := historianFloat(v); ok {
			t.Errorf("historianFloat(%#v) returned %v, ok — a fabricated reading "+
				"would be written with GOOD quality", v, got)
		}
	}
}

// Every numeric shape a driver or a Sparkplug decoder can hand over. A kind
// missing from this switch does not fail loudly: it falls to the string
// fallback, and before that it fell through as zero.
func TestEveryNumericKindConverts(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
	}{
		{float64(20.5), 20.5},
		{float32(1.5), 1.5},
		{int(7), 7},
		{int8(7), 7},
		{int16(7), 7},
		{int32(7), 7},
		{int64(7), 7},
		{uint(7), 7},
		{uint8(7), 7},
		{uint16(7), 7},
		{uint32(7), 7},
		{uint64(7), 7},
		{"20.5", 20.5},
		{" 20.5 ", 20.5},
		{"-3", -3},
	}
	for _, c := range cases {
		got, ok := historianFloat(c.in)
		if !ok {
			t.Errorf("historianFloat(%#v) refused a number", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("historianFloat(%#v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// A digital tag has to land on exactly 1 and 0 or a chart cannot draw it as a
// state, and an alarm threshold of 0.5 stops working.
func TestABooleanBecomesOneOrZero(t *testing.T) {
	if got, ok := historianFloat(true); !ok || got != 1 {
		t.Errorf("historianFloat(true) = %v, %v; want 1, true", got, ok)
	}
	if got, ok := historianFloat(false); !ok || got != 0 {
		t.Errorf("historianFloat(false) = %v, %v; want 0, true", got, ok)
	}
}

// NaN and the infinities parse as numbers, and FLOAT8 stores them without
// complaint — which is exactly why they have to be stopped here. Postgres
// sorts NaN above every other value, so one corrupt float register turns AVG,
// MIN and MAX over its entire window into NaN. The chart for that period does
// not show a bad point; it shows nothing at all, and no error is raised
// anywhere along the way.
func TestNaNAndInfinityAreRefused(t *testing.T) {
	for _, v := range []interface{}{
		math.NaN(), math.Inf(1), math.Inf(-1), float32(math.Inf(1)),
		"NaN", "Inf", "+Inf", "-Inf", "infinity",
	} {
		if got, ok := historianFloat(v); ok {
			t.Errorf("historianFloat(%#v) returned %v, ok — every aggregate over "+
				"the window containing it becomes NaN", v, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Timestamps
// ---------------------------------------------------------------------------

var fixedNow = func() time.Time { return time.UnixMilli(1_700_000_000_000) }

func TestTheFirstUsableTimestampWins(t *testing.T) {
	if got := sampleTimestamp(fixedNow, 111, 222); got != 111 {
		t.Errorf("got %d, want the metric's own timestamp 111", got)
	}
	if got := sampleTimestamp(fixedNow, 0, 222); got != 222 {
		t.Errorf("got %d, want the payload timestamp 222 when the metric has none", got)
	}
}

func TestWithNoTimestampAtAllTheSampleIsStampedNow(t *testing.T) {
	if got := sampleTimestamp(fixedNow, 0, 0); got != 1_700_000_000_000 {
		t.Errorf("got %d, want now; a zero timestamp puts the row in 1970 where "+
			"no trend shows it and the retention worker deletes it", got)
	}
	if got := sampleTimestamp(fixedNow); got != 1_700_000_000_000 {
		t.Errorf("got %d, want now when there are no candidates at all", got)
	}
}

// The Sparkplug path only rejected zero, so a negative timestamp — a device
// with a badly set clock, or a signed overflow — went through and buried the
// row before 1970 just as surely.
func TestANegativeTimestampIsRejectedToo(t *testing.T) {
	if got := sampleTimestamp(fixedNow, -1); got != 1_700_000_000_000 {
		t.Errorf("got %d for a negative timestamp, want now", got)
	}
	if got := sampleTimestamp(fixedNow, -1, 222); got != 222 {
		t.Errorf("got %d, want the next usable candidate 222", got)
	}
}
