package forecast

import (
	"context"
	"log"
	"math"
	"time"
)

// periods works out a reliable counter's or state's totals by the end of the
// shift, the day and the month. What was counted so far comes from the
// series the forecast learned from when it reaches back to the period's
// start, and otherwise from the hourly history.
func (r *Runner) periods(ctx context.Context, res *Result, job *Job, now time.Time) []PeriodTotal {
	if res.Status != StatusOK || (res.Kind != KindCounter && res.Kind != KindState) {
		return nil
	}
	last := lastObserved(job.Series)
	if res.Kind == KindCounter && math.IsNaN(last) {
		return nil
	}
	var hourly *Series
	var out []PeriodTotal
	windows := job.Calendar.Windows(now)
	for i := range windows {
		w := &windows[i]
		so, ok := SoFar(&job.Series, res.Kind, w.Start)
		if !ok {
			if hourly == nil {
				// From the month's start, the earliest: one read covers every window.
				n := int(math.Ceil(now.Sub(windows[len(windows)-1].Start).Hours())) + 1
				s, err := r.seriesAt(ctx, res.TagID, time.Hour, n, now)
				if err != nil {
					log.Printf("[FORECAST] tag %d: period history: %v", res.TagID, err)
					s = Series{Step: time.Hour, Start: now}
				}
				hourly = &s
			}
			// Read from the month's start: a series that begins later had no
			// data before, and is counted from its first value.
			from := w.Start
			if hourly.Start.After(from) {
				from = hourly.Start
			}
			if so, ok = SoFar(hourly, res.Kind, from); !ok {
				continue
			}
		}
		if t, ok := ProjectTotal(&res.Forecast, res.Kind, last, so, &windows[i]); ok {
			out = append(out, t)
		}
	}
	return out
}
