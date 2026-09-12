package main

import (
	"math"
	"testing"
)

// parseValue turns bytes off the wire into a reading. It is the one piece of
// this driver that is pure arithmetic, it had no tests, and every way of
// getting it wrong produces a number rather than an error: a sign bit read as
// magnitude, a word read one register along, a bit counted from the wrong end.
// All of those look like process data.

func bit(n int) *int { return &n }

// ---------------------------------------------------------------------------
// INT — 16-bit signed, big-endian
// ---------------------------------------------------------------------------

func TestINTReadsTwosComplement(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want int16
	}{
		{"zero", []byte{0x00, 0x00}, 0},
		{"one", []byte{0x00, 0x01}, 1},
		{"largest positive", []byte{0x7F, 0xFF}, 32767},
		{"minus one", []byte{0xFF, 0xFF}, -1},
		{"most negative", []byte{0x80, 0x00}, -32768},
		{"a temperature in tenths", []byte{0xFF, 0x9C}, -100}, // -10.0 °C
	}
	for _, c := range cases {
		got, err := parseValue(c.data, 0, "INT", nil, "holding")
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %v (%T), want %d — a sign read as magnitude turns "+
				"a reading below zero into a large positive one", c.name, got, got, c.want)
		}
	}
}

// The offset is in REGISTERS and the buffer is in BYTES. Getting the factor of
// two wrong reads the neighboring tag: still a number, still in range, and
// belonging to something else entirely.
func TestTheOffsetIsCountedInRegisters(t *testing.T) {
	// four registers: 1, 2, 3, 4
	data := []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x03, 0x00, 0x04}
	for reg, want := range []int16{1, 2, 3, 4} {
		got, err := parseValue(data, uint16(reg), "INT", nil, "holding")
		if err != nil {
			t.Fatalf("register %d: %v", reg, err)
		}
		if got != want {
			t.Errorf("register %d read %v, want %d — the tag is reading its "+
				"neighbor", reg, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// DINT and REAL — two registers, high word first
// ---------------------------------------------------------------------------

func TestDINTSpansTwoRegistersHighWordFirst(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want int32
	}{
		{"one", []byte{0x00, 0x00, 0x00, 0x01}, 1},
		{"65536 — the boundary between the words", []byte{0x00, 0x01, 0x00, 0x00}, 65536},
		{"minus one", []byte{0xFF, 0xFF, 0xFF, 0xFF}, -1},
		{"a counter", []byte{0x00, 0x0F, 0x42, 0x40}, 1000000},
	}
	for _, c := range cases {
		got, err := parseValue(c.data, 0, "DINT", nil, "holding")
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %v, want %d — with the words swapped this stays a "+
				"plausible number and is wrong by a factor of 65536", c.name, got, c.want)
		}
	}
}

func TestREALDecodesIEEE754(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want float32
	}{
		{"zero", []byte{0x00, 0x00, 0x00, 0x00}, 0},
		{"one", []byte{0x3F, 0x80, 0x00, 0x00}, 1},
		{"a pressure", []byte{0x42, 0xC8, 0x00, 0x00}, 100},
		{"negative", []byte{0xC2, 0xC8, 0x00, 0x00}, -100},
		{"a fraction", []byte{0x41, 0xA4, 0x00, 0x00}, 20.5},
	}
	for _, c := range cases {
		got, err := parseValue(c.data, 0, "REAL", nil, "holding")
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		f, ok := got.(float32)
		if !ok {
			t.Errorf("%s: got %T, want float32", c.name, got)
			continue
		}
		if f != c.want {
			t.Errorf("%s: got %v, want %v", c.name, f, c.want)
		}
	}
}

// A float register a PLC has never written holds whatever was in that memory,
// and 0x7FC00000 is a real pattern to find there. It must not reach the
// historian: Postgres sorts NaN above every value, so one of them turns every
// average over its window into NaN.
func TestAnUninitialisedFloatRegisterIsRecognisable(t *testing.T) {
	got, err := parseValue([]byte{0x7F, 0xC0, 0x00, 0x00}, 0, "REAL", nil, "holding")
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(float64(got.(float32))) {
		t.Fatalf("got %v, want NaN — this pattern is a NaN and has to be "+
			"recognizable as one downstream", got)
	}
}

// ---------------------------------------------------------------------------
// Bits
// ---------------------------------------------------------------------------

// A BOOL inside a holding register is addressed by bit, counted from the least
// significant end of the 16-bit word. Counting from the other end silently
// reads a different signal from the same register.
func TestABoolInARegisterIsCountedFromTheLeastSignificantBit(t *testing.T) {
	// 0x0001 = only bit 0 set
	data := []byte{0x00, 0x01}
	if got, _ := parseValue(data, 0, "BOOL", bit(0), "holding"); got != true {
		t.Error("bit 0 of 0x0001 read as false")
	}
	for b := 1; b <= 15; b++ {
		if got, _ := parseValue(data, 0, "BOOL", bit(b), "holding"); got != false {
			t.Errorf("bit %d of 0x0001 read as true", b)
		}
	}
	// 0x8000 = only bit 15 set
	data = []byte{0x80, 0x00}
	if got, _ := parseValue(data, 0, "BOOL", bit(15), "holding"); got != true {
		t.Error("bit 15 of 0x8000 read as false")
	}
	if got, _ := parseValue(data, 0, "BOOL", bit(0), "holding"); got != false {
		t.Error("bit 0 of 0x8000 read as true")
	}
}

// Coil responses pack one bit per coil, eight to a byte, least significant
// first. Reading them the other way round turns a machine that is running into
// one that is stopped.
func TestCoilsArePackedLeastSignificantBitFirst(t *testing.T) {
	// 0x01 = coil 0 on, coils 1..7 off
	data := []byte{0x01, 0x00}
	if got, _ := parseValue(data, 0, "BOOL", nil, "coil"); got != true {
		t.Error("coil 0 of 0x01 read as false")
	}
	for c := uint16(1); c < 8; c++ {
		if got, _ := parseValue(data, c, "BOOL", nil, "coil"); got != false {
			t.Errorf("coil %d of 0x01 read as true", c)
		}
	}
	// coil 8 lives in the second byte
	data = []byte{0x00, 0x01}
	if got, _ := parseValue(data, 8, "BOOL", nil, "coil"); got != true {
		t.Error("coil 8 was not found in the second byte")
	}
}

func TestDiscreteInputsArePackedLikeCoils(t *testing.T) {
	if got, _ := parseValue([]byte{0x80}, 7, "BOOL", nil, "discrete"); got != true {
		t.Error("discrete input 7 of 0x80 read as false")
	}
}

// ---------------------------------------------------------------------------
// Refusals
// ---------------------------------------------------------------------------

// A short reply must produce an error, not a value read out of whatever
// happened to follow in memory.
func TestAReplyTooShortIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		data  []byte
		off   uint16
		dtype string
	}{
		{"INT past the end", []byte{0x00, 0x01}, 1, "INT"},
		{"DINT with only one register left", []byte{0x00, 0x01, 0x00, 0x02}, 1, "DINT"},
		{"REAL with only one register left", []byte{0x00, 0x01, 0x00, 0x02}, 1, "REAL"},
		{"empty buffer", []byte{}, 0, "INT"},
	}
	for _, c := range cases {
		if _, err := parseValue(c.data, c.off, c.dtype, nil, "holding"); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if _, err := parseValue([]byte{}, 0, "BOOL", nil, "coil"); err == nil {
		t.Error("a coil past the end of an empty buffer was accepted")
	}
}

// A BOOL in a register with no bit given is not a reading, and answering
// "false" would look exactly like a signal that is off.
func TestABoolWithNoBitOffsetIsRefused(t *testing.T) {
	if _, err := parseValue([]byte{0xFF, 0xFF}, 0, "BOOL", nil, "holding"); err == nil {
		t.Fatal("a BOOL in a holding register with no bit offset was accepted")
	}
}

// STRING is a data type the API accepts (validateDataType in
// internal/handlers/tags.go) and this driver cannot decode. It must say so
// rather than return something.
func TestAnUndecodableTypeIsRefused(t *testing.T) {
	for _, dt := range []string{"STRING", "UINT", "", "real"} {
		if _, err := parseValue([]byte{0x00, 0x01, 0x00, 0x02}, 0, dt, nil, "holding"); err == nil {
			t.Errorf("%q was decoded", dt)
		}
	}
}
