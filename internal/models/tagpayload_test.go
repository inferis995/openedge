package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// An old consumer must see exactly the payload it saw before the flag existed,
// or a rollout breaks in the direction nobody tests: new publisher, old reader.
func TestAnUnflaggedPayloadIsByteIdenticalToTheOldShape(t *testing.T) {
	b, err := json.Marshal(TagPayload{TagID: 7, OrgID: 3, Value: 20.5, Timestamp: 1700000000000, Quality: 0})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "eu") {
		t.Fatalf("an unscaled payload carries the flag: %s", b)
	}
	want := `{"tag_id":7,"org_id":3,"v":20.5,"ts":1700000000000,"q":0}`
	if string(b) != want {
		t.Fatalf("payload shape changed:\n got %s\nwant %s", b, want)
	}
}

func TestAScaledPayloadSaysSo(t *testing.T) {
	b, _ := json.Marshal(TagPayload{TagID: 7, Value: 100.0, EUScaled: true})
	if !strings.Contains(string(b), `"eu":true`) {
		t.Fatalf("a converted value went out unflagged, so the consumer will "+
			"convert it a second time: %s", b)
	}
}

// The consumer side. A payload from a driver that has not been updated yet has
// no "eu" key at all, and the absence has to read as false — which is what
// makes the receiver convert it, correctly.
func TestAPayloadFromAnOldDriverReadsAsUnscaled(t *testing.T) {
	var p TagPayload
	if err := json.Unmarshal([]byte(`{"tag_id":7,"org_id":3,"v":6912,"ts":1,"q":0}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.EUScaled {
		t.Fatal("a payload with no eu key was read as already converted; its raw " +
			"counts would reach the gauge as if they were engineering units")
	}
}

func TestTheFlagSurvivesARoundTrip(t *testing.T) {
	in := TagPayload{TagID: 7, OrgID: 3, Value: 100.0, Timestamp: 42, Quality: 1, EUScaled: true}
	b, _ := json.Marshal(in)
	var out TagPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.EUScaled {
		t.Fatal("the flag was lost in transit")
	}
}
