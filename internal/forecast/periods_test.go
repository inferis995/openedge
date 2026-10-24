package forecast

import (
	"math"
	"testing"
	"time"
)

func TestWindows(t *testing.T) {
	loc := time.FixedZone("plant", 3600)
	night := ShiftWindow{Name: "Notte", Start: 22 * time.Hour, End: 6 * time.Hour,
		Weekdays: map[time.Weekday]bool{time.Monday: true}}
	cal := &Calendar{Loc: loc, Shifts: []ShiftWindow{night}}

	// Tuesday 02:00 local: in Monday's night shift.
	now := time.Date(2026, 10, 20, 2, 0, 0, 0, loc)
	ws := cal.Windows(now)
	if len(ws) != 3 || ws[0].Period != PeriodShift || ws[0].Name != "Notte" {
		t.Fatalf("%+v", ws)
	}
	if !ws[0].Start.Equal(time.Date(2026, 10, 19, 22, 0, 0, 0, loc)) || !ws[0].End.Equal(time.Date(2026, 10, 20, 6, 0, 0, 0, loc)) {
		t.Errorf("shift %v – %v", ws[0].Start, ws[0].End)
	}
	if !ws[1].Start.Equal(time.Date(2026, 10, 20, 0, 0, 0, 0, loc)) || !ws[2].End.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, loc)) {
		t.Errorf("day %+v month %+v", ws[1], ws[2])
	}
	// Tuesday noon: no shift on, the day and the month still.
	if ws := cal.Windows(time.Date(2026, 10, 20, 12, 0, 0, 0, loc)); len(ws) != 2 || ws[0].Period != PeriodDay {
		t.Errorf("%+v", ws)
	}
	// No calendar at all.
	if ws := (*Calendar)(nil).Windows(now); len(ws) != 2 {
		t.Errorf("%+v", ws)
	}
}

func TestSoFar(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nan := math.NaN()
	// A counter with a reset at the fourth step and a missing value.
	c := Series{Start: start, Step: time.Hour, Values: []float64{100, 110, nan, 130, 5, 15}}
	if v, ok := SoFar(&c, KindCounter, start.Add(time.Hour)); !ok || v != 30 {
		t.Errorf("counter: %v %v, want 110→130 then 5→15 = 30", v, ok)
	}
	if _, ok := SoFar(&c, KindCounter, start.Add(-time.Hour)); ok {
		t.Error("a series that starts after the period's start cannot say what was counted")
	}
	s := Series{Start: start, Step: 30 * time.Minute, Values: []float64{1, 1, 0, nan, 0.5}}
	if v, ok := SoFar(&s, KindState, start); !ok || v != 1.25 {
		t.Errorf("state: %v hours on, want 1.25", v)
	}
	if _, ok := SoFar(&s, KindProcess, start); ok {
		t.Error("a process value has no total")
	}
}

func TestProjectTotal(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	// Counter at 1000, +10 an hour, band ±2 an hour, 6 hourly steps.
	f := Forecast{Start: now, Step: time.Hour}
	for i := 1; i <= 6; i++ {
		f.Q10 = append(f.Q10, 1000+8*float64(i))
		f.Q50 = append(f.Q50, 1000+10*float64(i))
		f.Q90 = append(f.Q90, 1000+12*float64(i))
	}
	w := Window{Period: PeriodShift, Start: now.Add(-4 * time.Hour), End: now.Add(4 * time.Hour)}
	p, ok := ProjectTotal(&f, KindCounter, 1000, 40, &w)
	if !ok || p.Q50 != 80 || p.Q10 != 72 || p.Q90 != 88 || p.Extrapolated {
		t.Errorf("shift: %+v", p)
	}
	// The day ends 12 h from now, past the 6 h horizon: at the forecast's rate.
	w = Window{Period: PeriodDay, Start: now.Add(-12 * time.Hour), End: now.Add(12 * time.Hour)}
	p, ok = ProjectTotal(&f, KindCounter, 1000, 120, &w)
	if !ok || p.Q50 != 240 || p.Q10 != 216 || !p.Extrapolated {
		t.Errorf("day: %+v", p)
	}
	// A period over, or too far, has no total.
	if _, bad := ProjectTotal(&f, KindCounter, 1000, 0, &Window{End: now}); bad {
		t.Error("a period already over")
	}
	if _, bad := ProjectTotal(&f, KindCounter, 1000, 0, &Window{End: now.Add(40 * 24 * time.Hour)}); bad {
		t.Error("40 days past the horizon")
	}
	if _, bad := ProjectTotal(&f, KindProcess, 1000, 0, &w); bad {
		t.Error("a process value")
	}

	// A state on half of each of 4 half-hour steps, then extrapolated.
	st := Forecast{Start: now, Step: 30 * time.Minute, Q10: []float64{0, 0, 0, 0}, Q50: []float64{.5, .5, .5, .5}, Q90: []float64{1, 1, 1, 1}}
	p, ok = ProjectTotal(&st, KindState, 1, 3, &Window{End: now.Add(3 * time.Hour)})
	if !ok || p.Q50 != 4.5 || p.Q10 != 3 || p.Q90 != 6 || !p.Extrapolated {
		t.Errorf("state: %+v", p)
	}
}
