package cmd

import (
	"testing"
	"time"
)

var historyNow = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// /api/history answers 400 without start and end: the last 24 hours are sent.
func TestHistoryToolDefaultsToTheLastDay(t *testing.T) {
	path, q, err := historyToolRequest(map[string]interface{}{"tag_id": float64(42)}, historyNow)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/history" {
		t.Errorf("path %q", path)
	}
	if q.Get("tag_id") != "42" || q.Get("start") != "2026-11-13T10:00:00Z" || q.Get("end") != "2026-11-14T10:00:00Z" {
		t.Errorf("query %v", q)
	}
	if q.Has("limit") || q.Has("from") || q.Has("to") || q.Has("interval") || q.Has("agg") {
		t.Errorf("unexpected parameters: %v", q)
	}
}

func TestHistoryToolPassesPeriodIntervalAndAgg(t *testing.T) {
	_, q, err := historyToolRequest(map[string]interface{}{
		"tag_id": float64(7), "from": "2026-11-10T00:00:00+01:00", "to": "2026-11-11T00:00:00+01:00",
		"interval": "1h", "agg": "max",
	}, historyNow)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("start") != "2026-11-09T23:00:00Z" || q.Get("end") != "2026-11-10T23:00:00Z" {
		t.Errorf("period %s – %s", q.Get("start"), q.Get("end"))
	}
	if q.Get("interval") != "1h" || q.Get("agg") != "max" {
		t.Errorf("interval/agg %v", q)
	}
	// Only the end: 24 h before it.
	_, q, _ = historyToolRequest(map[string]interface{}{"tag_id": float64(7), "to": "2026-11-11T00:00:00Z"}, historyNow)
	if q.Get("start") != "2026-11-10T00:00:00Z" {
		t.Errorf("end only: start %s", q.Get("start"))
	}
}

func TestHistoryToolRejectsBadInput(t *testing.T) {
	for name, args := range map[string]map[string]interface{}{
		"no tag":       {"from": "2026-11-10T00:00:00Z"},
		"bad from":     {"tag_id": float64(1), "from": "ieri"},
		"bare date":    {"tag_id": float64(1), "to": "2026-11-10"},
		"from >= to":   {"tag_id": float64(1), "from": "2026-11-10T00:00:00Z", "to": "2026-11-10T00:00:00Z"},
		"bad interval": {"tag_id": float64(1), "interval": "15m"},
		"bad agg":      {"tag_id": float64(1), "agg": "sum"},
	} {
		if _, _, err := historyToolRequest(args, historyNow); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
