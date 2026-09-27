package shifts

import (
	"testing"
	"time"
)

func rome(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	return loc
}

var weekdays = []int{1, 2, 3, 4, 5}

func TestAMorningShiftStartsOnThePlantClock(t *testing.T) {
	loc := rome(t)
	list := []Shift{{ID: 1, Name: "Mattina", StartMin: 6 * 60, EndMin: 14 * 60, Weekdays: weekdays}}

	// Monday 22 June 2026, 06:30 in Italy = 04:30 UTC.
	at := time.Date(2026, 6, 22, 4, 30, 0, 0, time.UTC)
	sh, start, end := ActiveAtIn(list, at, loc)
	if sh == nil {
		t.Fatal("at 06:30 local the morning shift is not running (it was read in UTC: 04:30)")
	}
	if got := start.In(loc).Format("15:04"); got != "06:00" {
		t.Errorf("starts %s, want 06:00", got)
	}
	if got := end.In(loc).Format("15:04"); got != "14:00" {
		t.Errorf("ends %s, want 14:00", got)
	}
	// 14:30 local = 12:30 UTC: over, although it would still run in UTC.
	if sh, _, _ := ActiveAtIn(list, time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC), loc); sh != nil {
		t.Error("at 14:30 local the morning shift is still running")
	}
}

func TestANightShiftRunsIntoTheNextDay(t *testing.T) {
	loc := rome(t)
	list := []Shift{{ID: 3, Name: "Notte", StartMin: 22 * 60, EndMin: 6 * 60, Weekdays: []int{5}, Wraps: true}}
	// Saturday 04:00 local: Friday's night shift.
	at := time.Date(2026, 6, 27, 4, 0, 0, 0, loc)
	sh, start, _ := ActiveAtIn(list, at, loc)
	if sh == nil || start.Weekday() != time.Friday {
		t.Fatalf("Friday's night shift not found on Saturday morning: %v %v", sh, start)
	}
	// Saturday 23:00: no Saturday night shift.
	if sh, _, _ := ActiveAtIn(list, time.Date(2026, 6, 27, 23, 0, 0, 0, loc), loc); sh != nil {
		t.Error("a Friday-only night shift runs on Saturday night")
	}
}

func TestPlannedMinutesCountShiftTimeOnly(t *testing.T) {
	loc := rome(t)
	list := []Shift{
		{ID: 1, StartMin: 6 * 60, EndMin: 14 * 60, Weekdays: weekdays},
		{ID: 3, StartMin: 22 * 60, EndMin: 6 * 60, Weekdays: weekdays, Wraps: true},
	}
	// Monday 00:00 → Tuesday 00:00 local.
	start := time.Date(2026, 6, 22, 0, 0, 0, 0, loc)
	got := PlannedMinutesIn(list, start, start.AddDate(0, 0, 1), loc)
	// 8h morning + 2h of Monday's night shift (22–24); Sunday has no night shift.
	if want := 10.0 * 60; got != want {
		t.Errorf("planned minutes = %v, want %v", got, want)
	}
	// Tuesday: 6h of Monday's night shift, 8h morning, 2h of Tuesday's.
	tue := start.AddDate(0, 0, 1)
	if got, want := PlannedMinutesIn(list, tue, tue.AddDate(0, 0, 1), loc), 16.0*60; got != want {
		t.Errorf("Tuesday planned minutes = %v, want %v", got, want)
	}
}

func TestTheClockChangeDoesNotMoveAShift(t *testing.T) {
	loc := rome(t)
	list := []Shift{{ID: 1, StartMin: 6 * 60, EndMin: 14 * 60, Weekdays: []int{1}}}
	// Monday 30 March 2026, the day after summer time starts.
	day := time.Date(2026, 3, 30, 0, 0, 0, 0, loc)
	if got := PlannedMinutesIn(list, day.AddDate(0, 0, -2), day.AddDate(0, 0, 1), loc); got != 480 {
		t.Errorf("planned minutes across the clock change = %v, want 480", got)
	}
	if sh, _, _ := ActiveAtIn(list, time.Date(2026, 3, 30, 6, 5, 0, 0, loc), loc); sh == nil {
		t.Error("the shift did not start at 06:00 local after the clock change")
	}
}
