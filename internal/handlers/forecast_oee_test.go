package handlers

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/forecast"
)

func TestProjectOEE(t *testing.T) {
	start := time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)
	shift := func(q10, q50, q90 float64) []forecast.PeriodTotal {
		return []forecast.PeriodTotal{
			{Period: forecast.PeriodShift, Name: "Mattino", Start: start, End: end, Q10: q10, Q50: q50, Q90: q90},
			{Period: forecast.PeriodDay, Start: start.Add(-6 * time.Hour), End: start.Add(18 * time.Hour), Q10: 1, Q50: 1, Q90: 1},
		}
	}
	p := oeeProfileRow{id: 1, name: "Linea 1", run: 10, produced: 11, good: 12, targetPPH: 100, targetOEE: 85}
	periods := map[int][]forecast.PeriodTotal{
		10: shift(6, 7, 8),       // hours running of 8
		11: shift(600, 700, 750), // pieces, of 800 expected
		12: shift(600, 665, 700), // good: 95% of 700
	}
	got := projectOEE(&p, periods)
	if got.Period != forecast.PeriodShift || got.PeriodName != "Mattino" || len(got.Missing) != 0 {
		t.Fatalf("%+v", got)
	}
	near := func(x, want float64) bool { return math.Abs(x-want) < 1e-9 }
	if !near(got.Availability.Q50, 87.5) || !near(got.Performance.Q50, 87.5) || !near(got.Quality.Q50, 95) {
		t.Errorf("A %v P %v Q %v", got.Availability, got.Performance, got.Quality)
	}
	if !near(got.OEE.Q50, 0.875*0.875*95) || got.OEE.Q10 >= got.OEE.Q50 || got.OEE.Q90 <= got.OEE.Q50 {
		t.Errorf("OEE %+v", got.OEE)
	}

	// No forecast for run time or good pieces: 100%, as on the OEE page, and said.
	got = projectOEE(&p, map[int][]forecast.PeriodTotal{11: periods[11]})
	if !reflect.DeepEqual(got.Missing, []string{"run_time", "good"}) || !near(got.OEE.Q50, 87.5) {
		t.Errorf("%+v %+v", got.Missing, got.OEE)
	}
	// No pieces: no forecast.
	if got = projectOEE(&p, map[int][]forecast.PeriodTotal{10: periods[10]}); got.OEE != nil || got.Missing[0] != "produced" {
		t.Errorf("%+v", got)
	}
	// Performance and availability never above 100%.
	periods[11] = shift(900, 1000, 1100)
	if got = projectOEE(&p, periods); got.Performance.Q90 != 100 {
		t.Errorf("%+v", got.Performance)
	}
}
