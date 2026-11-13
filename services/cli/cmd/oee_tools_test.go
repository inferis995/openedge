package cmd

import (
	"testing"
	"time"
)

var oeeNow = time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)

// Without dates the API answers 400: the last 24 hours are always sent.
func TestOEEToolsAlwaysSendThePeriod(t *testing.T) {
	_, q, err := oeeToolRequest("get_oee_history", map[string]interface{}{}, oeeNow)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("from") != "2026-11-12T09:30:00Z" || q.Get("to") != "2026-11-13T09:30:00Z" {
		t.Errorf("default period %s – %s", q.Get("from"), q.Get("to"))
	}
	// Only the end given: 24 h before it.
	_, q, _ = oeeToolRequest("get_oee_by_shift", map[string]interface{}{"to": "2026-11-10"}, oeeNow)
	if q.Get("from") != "2026-11-09T00:00:00Z" || q.Get("to") != "2026-11-10T00:00:00Z" {
		t.Errorf("end only: %s – %s", q.Get("from"), q.Get("to"))
	}
	if _, _, err := oeeToolRequest("get_oee_history", map[string]interface{}{"from": "ieri"}, oeeNow); err == nil {
		t.Error("a date that does not parse was accepted")
	}
	if _, _, err := oeeToolRequest("get_oee_history", map[string]interface{}{"from": "2026-11-13", "to": "2026-11-12"}, oeeNow); err == nil {
		t.Error("from after to was accepted")
	}
}

// history and by-shift: no profile is the whole plant.
func TestOEEToolsPlantWideWithoutProfile(t *testing.T) {
	for _, name := range []string{"get_oee_history", "get_oee_by_shift"} {
		path, q, err := oeeToolRequest(name, map[string]interface{}{}, oeeNow)
		if err != nil || q.Has("profile_id") {
			t.Errorf("%s: %s %v %v", name, path, q, err)
		}
	}
	path, q, _ := oeeToolRequest("get_oee_history", map[string]interface{}{"profile_id": float64(3), "bucket": "day"}, oeeNow)
	if path != "/api/oee/history-v2" || q.Get("profile_id") != "3" || q.Get("bucket") != "day" {
		t.Errorf("%s %v", path, q)
	}
}

// loss tree and reliability need a profile, and reliability carries it in the path.
func TestOEEToolsLossAndReliabilityNeedAProfile(t *testing.T) {
	for _, name := range []string{"get_loss_tree", "get_oee_reliability"} {
		if _, _, err := oeeToolRequest(name, map[string]interface{}{}, oeeNow); err == nil {
			t.Errorf("%s without profile_id accepted", name)
		}
	}
	path, q, err := oeeToolRequest("get_oee_reliability", map[string]interface{}{"profile_id": float64(7)}, oeeNow)
	if err != nil || path != "/api/oee/profiles/7/reliability" || q.Has("profile_id") || q.Get("from") == "" {
		t.Errorf("%s %v %v", path, q, err)
	}
	path, q, _ = oeeToolRequest("get_loss_tree", map[string]interface{}{"profile_id": "7"}, oeeNow)
	if path != "/api/oee/loss-tree" || q.Get("profile_id") != "7" {
		t.Errorf("%s %v", path, q)
	}
}
