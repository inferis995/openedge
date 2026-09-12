package main

import (
	"testing"
)

// parseIncomingValue is where a message from somebody else's broker becomes a
// reading in this product. It had no tests, and its failure mode is the usual
// one: not an error, a value. A digital signal read as off, a field read from
// the wrong place, a number that is really a string.

// ---------------------------------------------------------------------------
// Two-state signals
// ---------------------------------------------------------------------------

// The defect: the same word meant different things depending on whether the
// device wrapped it in an object. "on" inside {"v":"on"} read as false, while
// a bare on read as true.
func TestTheSameWordMeansTheSameThingWrappedOrNot(t *testing.T) {
	for _, word := range []string{"true", "TRUE", "on", "ON", "yes", "1", "t"} {
		bare, errBare := parseIncomingValue([]byte(word), "BOOL", "")
		wrapped, errWrapped := parseIncomingValue([]byte(`{"v":"`+word+`"}`), "BOOL", "")
		if errBare != nil || errWrapped != nil {
			t.Errorf("%q: bare err=%v, wrapped err=%v", word, errBare, errWrapped)
			continue
		}
		if bare != wrapped {
			t.Errorf("%q read as %v bare and %v inside an object; the same device "+
				"reports a different state depending on how it packages it",
				word, bare, wrapped)
		}
		if bare != true {
			t.Errorf("%q read as %v, want true", word, bare)
		}
	}
}

func TestTheWordsForFalse(t *testing.T) {
	for _, word := range []string{"false", "FALSE", "off", "no", "0", "f"} {
		got, err := parseIncomingValue([]byte(word), "BOOL", "")
		if err != nil {
			t.Errorf("%q: %v", word, err)
			continue
		}
		if got != false {
			t.Errorf("%q read as %v, want false", word, got)
		}
	}
}

// A word outside the vocabulary must be refused. Answering false reports a
// valve as shut and a pump as stopped, and neither looks like an error.
func TestAnUnrecognizedWordIsRefusedRatherThanReadAsOff(t *testing.T) {
	for _, word := range []string{"OPEN", "RUNNING", "aperto", "enabled", "-"} {
		if got, err := parseIncomingValue([]byte(word), "BOOL", ""); err == nil {
			t.Errorf("%q was read as %v; an operator sees a state the device "+
				"never reported", word, got)
		}
	}
}

// null, an object and an array are not two-state signals.
func TestAShapeThatIsNotASignalIsRefused(t *testing.T) {
	for _, payload := range []string{`{"v":null}`, `{"v":{"a":1}}`, `{"v":[1,2]}`} {
		if got, err := parseIncomingValue([]byte(payload), "BOOL", ""); err == nil {
			t.Errorf("%s was read as %v instead of refused", payload, got)
		}
	}
}

// Devices that send a number for a digital signal are common enough to accept.
func TestANumberIsADigitalSignalToo(t *testing.T) {
	cases := map[string]bool{"0": false, "1": true, "2": true, "-1": true, "0.0": false}
	for payload, want := range cases {
		got, err := parseIncomingValue([]byte(payload), "BOOL", "")
		if err != nil {
			t.Errorf("%q: %v", payload, err)
			continue
		}
		if got != want {
			t.Errorf("%q read as %v, want %v", payload, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Where the value is found
// ---------------------------------------------------------------------------

func TestAConfiguredPathIsFollowed(t *testing.T) {
	payload := []byte(`{"sensors":{"line1":[{"temp":22.5},{"temp":30.0}]}}`)
	got, err := parseIncomingValue(payload, "REAL", "sensors.line1.1.temp")
	if err != nil {
		t.Fatal(err)
	}
	if got != 30.0 {
		t.Fatalf("got %v, want 30.0 — the wrong element of the array is a real "+
			"reading belonging to another sensor", got)
	}
}

func TestBothPathNotationsAreAccepted(t *testing.T) {
	payload := []byte(`{"a":{"b":7}}`)
	dotted, err1 := parseIncomingValue(payload, "REAL", "a.b")
	jsonpath, err2 := parseIncomingValue(payload, "REAL", "$.a.b")
	if err1 != nil || err2 != nil {
		t.Fatalf("dotted: %v, $-prefixed: %v", err1, err2)
	}
	if dotted != jsonpath {
		t.Fatalf("a.b gave %v and $.a.b gave %v", dotted, jsonpath)
	}
}

// A typo in the path must be an error. Falling back to "value" or to the whole
// payload would publish a number from somewhere else in the message.
func TestAPathThatDoesNotResolveIsAnError(t *testing.T) {
	payload := []byte(`{"temp":22.5,"value":999,"probes":[1,2]}`)
	for _, path := range []string{"tmp", "temp.inner", "sensors.0", "temp.0"} {
		if got, err := parseIncomingValue(payload, "REAL", path); err == nil {
			t.Errorf("path %q resolved to %v; with a typo in the path this "+
				"publishes a number from elsewhere in the message", path, got)
		}
	}

	// An index past the end of an array that does exist. Clamping it to the
	// last element — a tempting way to be forgiving — silently publishes
	// probe 2's reading for probe 5, and both are real numbers.
	for _, path := range []string{"probes.2", "probes.9", "probes.-1"} {
		if got, err := parseIncomingValue(payload, "REAL", path); err == nil {
			t.Errorf("path %q resolved to %v, but the array holds two elements; "+
				"a reading belonging to another probe would be published under "+
				"this tag's name", path, got)
		}
	}
}

// With no path configured the well-known field names are tried, in order.
func TestWithNoPathTheUsualFieldNamesAreTried(t *testing.T) {
	cases := map[string]float64{
		`{"value":1}`: 1,
		`{"v":2}`:     2,
		`{"val":3}`:   3,
		`22.5`:        22.5,
	}
	for payload, want := range cases {
		got, err := parseIncomingValue([]byte(payload), "REAL", "")
		if err != nil {
			t.Errorf("%s: %v", payload, err)
			continue
		}
		if got != want {
			t.Errorf("%s gave %v, want %v", payload, got, want)
		}
	}
}

// An object with none of those fields is not a reading. Publishing something
// from it would be a guess.
func TestAnObjectWithNoKnownFieldIsRefused(t *testing.T) {
	if got, err := parseIncomingValue([]byte(`{"temperature":22.5}`), "REAL", ""); err == nil {
		t.Fatalf("got %v; the field name is not one this driver knows and no "+
			"json_path was configured", got)
	}
}

// A retained topic cleared with an empty message, or a device that publishes
// nothing on start-up, must not become a reading.
//
// The type matters here: for a numeric tag an empty payload is refused by the
// number parser anyway, so testing REAL proves nothing about the guard. For a
// STRING tag there is nothing further along to object, and without the guard
// the empty string is published as the tag's value.
func TestAnEmptyPayloadIsRefused(t *testing.T) {
	for _, dataType := range []string{"REAL", "INT", "BOOL", "STRING"} {
		for _, payload := range []string{"", "   ", "\n"} {
			if got, err := parseIncomingValue([]byte(payload), dataType, ""); err == nil {
				t.Errorf("%s: an empty payload (%q) was read as %#v", dataType, payload, got)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Numbers
// ---------------------------------------------------------------------------

func TestANumberSentAsTextIsStillANumber(t *testing.T) {
	got, err := parseIncomingValue([]byte(`{"v":"22.5"}`), "REAL", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != 22.5 {
		t.Fatalf("got %v (%T), want 22.5 — a quoted number stored as a string "+
			"is not comparable against an alarm threshold", got, got)
	}
}

func TestTextThatIsNotANumberIsRefusedForANumericTag(t *testing.T) {
	for _, payload := range []string{"ERROR", "n/a", "--"} {
		if got, err := parseIncomingValue([]byte(payload), "REAL", ""); err == nil {
			t.Errorf("%q was read as %v for a REAL tag", payload, got)
		}
	}
}
