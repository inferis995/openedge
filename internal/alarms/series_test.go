package alarms

import (
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// A logger reporting every six hours: readings every 15 minutes, delivered
// together. These tests run such batches through the alarm rules.

var t0 = time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func series(vals ...float64) []Point {
	out := make([]Point, len(vals))
	for i, v := range vals {
		out[i] = Point{Value: v, At: at(i * 15)}
	}
	return out
}

type notified struct {
	status string
}

func seriesManager(t *testing.T, delay int) (*Manager, *FakeStore, *[]notified) {
	t.Helper()
	m := NewTestManager()
	th := 50.0
	m.SetDefinitions(7, []models.AlarmDefinition{{ID: 1, TagID: 7, AlarmType: "high", Threshold: &th, DelaySeconds: delay, Enabled: true}})
	store := m.UseFakeStore()
	var got []notified
	m.OnAlarmEvent = func(_ int, _ int, _ string, _ models.AlarmDefinition, _ float64, status string) {
		got = append(got, notified{status})
	}
	return m, store, &got
}

// A threshold crossed for half an hour between two reports is in the alarm
// log with its real start and end — and wakes nobody: it ended hours ago.
func TestAnExcursionInsideTheBatchIsRecordedNotNotified(t *testing.T) {
	m, store, got := seriesManager(t, 0)
	m.EvaluateTagSeries(7, "AN1", series(10, 60, 60, 10, 10), 192)

	ev := store.EventLog()
	if len(ev) != 1 {
		t.Fatalf("%d alarm rows, want 1", len(ev))
	}
	if !ev[0].Trigger.Equal(at(15)) || !ev[0].Clear.Equal(at(45)) {
		t.Errorf("alarm %s → %s, want 00:15 → 00:45", ev[0].Trigger.Format("15:04"), ev[0].Clear.Format("15:04"))
	}
	if len(*got) != 0 {
		t.Errorf("%d notifications for an alarm that was already over", len(*got))
	}
}

// Still active at the newest reading: that is news, and is notified once,
// stamped with when it began.
func TestAnAlarmStillActiveAtTheNewestReadingIsNotified(t *testing.T) {
	m, store, got := seriesManager(t, 0)
	m.EvaluateTagSeries(7, "AN1", series(10, 60, 70), 192)

	ev := store.EventLog()
	if len(ev) != 1 || !ev[0].Trigger.Equal(at(15)) || !ev[0].Clear.IsZero() {
		t.Fatalf("rows %+v, want one open alarm from 00:15", ev)
	}
	if len(*got) != 1 || (*got)[0].status != "ACTIVE" {
		t.Errorf("notifications %+v, want one ACTIVE", *got)
	}
}

// The delay is measured between readings, not on the server clock: above the
// threshold for 15 minutes is not enough for a 30-minute delay; for 30 it is,
// and the alarm starts when the delay ran out.
func TestTheDelayIsMeasuredOnTheReadingsTimes(t *testing.T) {
	m, store, _ := seriesManager(t, 1800)
	m.EvaluateTagSeries(7, "AN1", series(10, 60, 60, 10), 192) // 00:15–00:30 above: 15 min
	if n := len(store.EventLog()); n != 0 {
		t.Fatalf("a 15-minute excursion raised %d alarms with a 30-minute delay", n)
	}
	m.EvaluateTagSeries(7, "AN1", []Point{{60.0, at(60)}, {60.0, at(75)}, {60.0, at(90)}, {10.0, at(105)}}, 192)
	ev := store.EventLog()
	if len(ev) != 1 || !ev[0].Trigger.Equal(at(90)) || !ev[0].Clear.Equal(at(105)) {
		t.Fatalf("rows %+v, want one alarm 01:30 → 01:45", ev)
	}
}

// An alarm the operator was told about in an earlier report ends in this
// one: its end is notified, at the time it ended.
func TestTheEndOfAKnownAlarmIsNotified(t *testing.T) {
	m, store, got := seriesManager(t, 0)
	m.EvaluateTagSeries(7, "AN1", series(10, 60), 192) // ACTIVE notified
	m.EvaluateTagSeries(7, "AN1", []Point{{60.0, at(360)}, {20.0, at(375)}}, 192)

	ev := store.EventLog()
	if len(ev) != 1 || !ev[0].Clear.Equal(at(375)) {
		t.Fatalf("rows %+v, want the alarm closed at 06:15", ev)
	}
	if len(*got) != 2 || (*got)[1].status != "CLEARED" {
		t.Errorf("notifications %+v, want ACTIVE then CLEARED", *got)
	}
}

// A batch whose points share one timestamp cannot produce a real occurrence:
// fired and cleared on the same sample is a replay artifact (one point
// re-evaluated after every restart), and writing it stacked the duplicate
// zero-duration CLEARED rows. It is dropped with no row and no notification.
func TestAZeroDurationReplayOccurrenceIsDropped(t *testing.T) {
	m, store, got := seriesManager(t, 0)
	m.EvaluateTagSeries(7, "AN1", []Point{{60.0, at(15)}, {10.0, at(15)}}, 192)
	if n := len(store.EventLog()); n != 0 {
		t.Fatalf("%d alarm rows for a zero-duration replay occurrence, want 0", n)
	}
	if len(*got) != 0 {
		t.Errorf("%d notifications for a dropped replay artifact", len(*got))
	}
}
