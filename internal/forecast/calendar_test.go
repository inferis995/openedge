package forecast

import (
	"testing"
	"time"
)

func TestCalendarKnown(t *testing.T) {
	rome, _ := time.LoadLocation("Europe/Rome")
	weekdays := map[time.Weekday]bool{time.Monday: true, time.Tuesday: true, time.Wednesday: true, time.Thursday: true, time.Friday: true}
	c := &Calendar{
		Loc: rome,
		Shifts: []ShiftWindow{
			{Start: 6 * time.Hour, End: 14 * time.Hour, Weekdays: weekdays},
			{Start: 22 * time.Hour, End: 6 * time.Hour, Weekdays: weekdays}, // the night shift
		},
		Maintenance: [][2]time.Time{{
			time.Date(2026, 10, 20, 8, 0, 0, 0, rome), time.Date(2026, 10, 20, 12, 0, 0, 0, rome),
		}},
	}
	// Monday 19 Oct 2026 00:00 Rome, hourly for a week.
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, rome)
	k := c.Known(start, time.Hour, 7*24)
	if len(k) != 5 {
		t.Fatalf("%d rows, want 5 (sin, cos, weekend, shift, maintenance)", len(k))
	}
	at := func(day, hour int) int { return day*24 + hour }
	for _, tc := range []struct {
		name   string
		i, row int
		want   float64
	}{
		{"Monday 07:00 in the morning shift", at(0, 7), 3, 1},
		{"Monday 15:00 between shifts", at(0, 15), 3, 0},
		{"Monday 23:00 night shift", at(0, 23), 3, 1},
		{"Tuesday 03:00 still Monday's night shift", at(1, 3), 3, 1},
		{"Saturday 03:00 after Friday's night shift", at(5, 3), 3, 1},
		{"Sunday 03:00 no shift", at(6, 3), 3, 0},
		{"Saturday is weekend", at(5, 12), 2, 1},
		{"Monday is not", at(0, 12), 2, 0},
		{"Tuesday 09:00 in maintenance", at(1, 9), 4, 1},
		{"Tuesday 13:00 not any more", at(1, 13), 4, 0},
		{"noon: sine 0", at(0, 12), 0, 0},
		{"noon: cosine -1", at(0, 12), 1, -1},
	} {
		if got := k[tc.row][tc.i]; got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := (&Calendar{Loc: rome}).Known(start, time.Hour, 3); len(got) != 3 {
		t.Errorf("no shifts or maintenance: %d rows, want 3", len(got))
	}
}
