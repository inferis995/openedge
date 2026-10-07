package cmd

import (
	"encoding/json"
	"testing"
)

func TestMergeGatewayConfig(t *testing.T) {
	var cur map[string]interface{}
	// As the server returns it: numbers decode as float64.
	_ = json.Unmarshal([]byte(`{"broker_host":"b","stale_after_minutes":420,"x":1}`), &cur)

	next, diff := mergeGatewayConfig(cur, map[string]interface{}{"stale_after_minutes": 1440, "alarms_on_history": true}, []string{"x", "absent"})
	if len(diff) != 3 {
		t.Fatalf("diff = %v", diff)
	}
	if next["broker_host"] != "b" || next["stale_after_minutes"] != 1440 || next["alarms_on_history"] != true {
		t.Fatalf("next = %v", next)
	}
	if _, ok := next["x"]; ok {
		t.Fatal("x should be removed")
	}
	if cur["stale_after_minutes"] != float64(420) {
		t.Fatal("the current config must not be modified")
	}

	// Already as asked: nothing to write.
	if _, diff := mergeGatewayConfig(cur, map[string]interface{}{"stale_after_minutes": 420}, nil); len(diff) != 0 {
		t.Fatalf("diff = %v", diff)
	}
}
