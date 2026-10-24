package forecast

import (
	"math"
	"time"
)

// Period totals: what a counter will have counted by the end of the shift,
// the day and the month (kWh, m³, pieces), and how long a state will have been
// on (hours running). The part already counted is measured; the rest is the
// forecast, with its band. Beyond the forecast's horizon the rest follows the
// forecast's own rate, and is marked as extrapolated.

// Period names.
const (
	PeriodShift = "shift"
	PeriodDay   = "day"
	PeriodMonth = "month"
)

// PeriodTotal is one period's total. For a counter, in the tag's unit; for a
// state, in hours on.
type PeriodTotal struct {
	Period string    `json:"period"`
	Name   string    `json:"name,omitempty"` // the shift's name
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	// SoFar is what was counted from Start to now.
	SoFar float64 `json:"so_far"`
	// Q10, Q50, Q90: the total at End.
	Q10 float64 `json:"q10"`
	Q50 float64 `json:"q50"`
	Q90 float64 `json:"q90"`
	// Extrapolated: End is past the forecast's horizon.
	Extrapolated bool `json:"extrapolated,omitempty"`
}

// Window is a period of the calendar.
type Window struct {
	Period, Name string
	Start, End   time.Time
}

// Windows are the periods now falls in: the current shift (when the
// organization has shifts and one is on), the day and the month, in the
// plant's local time.
func (c *Calendar) Windows(now time.Time) []Window {
	loc := time.Local
	if c != nil && c.Loc != nil {
		loc = c.Loc
	}
	lt := now.In(loc)
	day := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
	month := time.Date(lt.Year(), lt.Month(), 1, 0, 0, 0, 0, loc)
	var out []Window
	if c != nil {
		if w, ok := c.currentShift(now); ok {
			out = append(out, w)
		}
	}
	return append(out,
		Window{Period: PeriodDay, Start: day, End: day.AddDate(0, 0, 1)},
		Window{Period: PeriodMonth, Start: month, End: month.AddDate(0, 1, 0)})
}

// currentShift is the shift on at t, with its bounds.
func (c *Calendar) currentShift(t time.Time) (Window, bool) {
	lt := t.In(c.Loc)
	midnight := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, c.Loc)
	since := lt.Sub(midnight)
	at := func(day time.Time, d time.Duration) time.Time {
		// Wall-clock time of day, so a shift keeps its hours across DST.
		return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, c.Loc).Add(d)
	}
	for _, s := range c.Shifts {
		w := Window{Period: PeriodShift, Name: s.Name}
		switch {
		case s.End > s.Start:
			if !s.Weekdays[lt.Weekday()] || since < s.Start || since >= s.End {
				continue
			}
			w.Start, w.End = at(midnight, s.Start), at(midnight, s.End)
		case s.Weekdays[lt.Weekday()] && since >= s.Start:
			// Past midnight, started today.
			w.Start, w.End = at(midnight, s.Start), at(midnight.AddDate(0, 0, 1), s.End)
		case s.Weekdays[lt.AddDate(0, 0, -1).Weekday()] && since < s.End:
			// Past midnight, started yesterday.
			w.Start, w.End = at(midnight.AddDate(0, 0, -1), s.Start), at(midnight, s.End)
		default:
			continue
		}
		return w, true
	}
	return Window{}, false
}

// MaxPeriodExtrapolation is how far past the horizon a total is followed.
const MaxPeriodExtrapolation = 31 * 24 * time.Hour

// SoFar measures a series from start to its end: the reset-free increase of
// a counter, or the hours on of a state. ok is false when the series does not
// reach back to start or has no value in the period.
func SoFar(s *Series, kind Kind, start time.Time) (float64, bool) {
	if len(s.Values) == 0 || s.Start.After(start) {
		return 0, false
	}
	from := int(start.Sub(s.Start) / s.Step)
	if from >= len(s.Values) {
		return 0, false
	}
	vals := s.Values[from:]
	switch kind {
	case KindCounter:
		total, prev, seen := 0.0, math.NaN(), false
		for _, v := range vals {
			if math.IsNaN(v) {
				continue
			}
			if seen {
				if v >= prev {
					total += v - prev
				}
				// A reset: the counter starts again from v, nothing lost.
			}
			prev, seen = v, true
		}
		return total, seen
	case KindState:
		hours, seen := 0.0, false
		for _, v := range vals {
			if !math.IsNaN(v) {
				hours += clamp01(v) * s.Step.Hours()
				seen = true
			}
		}
		return hours, seen
	}
	return 0, false
}

// ProjectTotal is a period's total: soFar plus what the forecast adds from
// its start to the period's end. last is the counter's latest value. ok is
// false for a kind that has no total, a period already over, or one too far
// past the horizon.
func ProjectTotal(f *Forecast, kind Kind, last, soFar float64, w *Window) (PeriodTotal, bool) {
	if (kind != KindCounter && kind != KindState) || len(f.Q50) == 0 || !w.End.After(f.Start) {
		return PeriodTotal{}, false
	}
	horizonEnd := f.Start.Add(time.Duration(len(f.Q50)) * f.Step)
	if w.End.Sub(horizonEnd) > MaxPeriodExtrapolation {
		return PeriodTotal{}, false
	}
	steps := int(math.Ceil(float64(w.End.Sub(f.Start)) / float64(f.Step)))
	rest := [3]float64{}
	for k, q := range [3][]float64{f.Q10, f.Q50, f.Q90} {
		var v float64
		var ok bool
		if kind == KindCounter {
			v, ok = counterRest(q, last, steps, w.End.Sub(f.Start), f.Step)
		} else {
			v, ok = stateRest(q, steps, f.Step)
		}
		if !ok {
			return PeriodTotal{}, false
		}
		rest[k] = v
	}
	return PeriodTotal{
		Period: w.Period, Name: w.Name, Start: w.Start, End: w.End, SoFar: soFar,
		Q10: soFar + rest[0], Q50: soFar + rest[1], Q90: soFar + rest[2],
		Extrapolated: steps > len(f.Q50),
	}, true
}

// counterRest is how much a counter adds over steps of its forecast q; past
// the end of q, at q's average rate over the time d.
func counterRest(q []float64, last float64, steps int, d, step time.Duration) (float64, bool) {
	if math.IsNaN(last) {
		return 0, false
	}
	if steps <= len(q) {
		v := q[steps-1]
		if math.IsNaN(v) {
			return 0, false
		}
		return math.Max(0, v-last), true
	}
	end := q[len(q)-1]
	if math.IsNaN(end) {
		return 0, false
	}
	rate := math.Max(0, end-last) / (time.Duration(len(q)) * step).Hours()
	return rate * d.Hours(), true
}

// stateRest is the hours on over steps of a state's forecast q (the share of
// each step on); past the end of q, at q's average share.
func stateRest(q []float64, steps int, step time.Duration) (float64, bool) {
	sum, n := 0.0, 0
	for i := 0; i < steps && i < len(q); i++ {
		if !math.IsNaN(q[i]) {
			sum += clamp01(q[i])
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	if steps > len(q) {
		sum += sum / float64(n) * float64(steps-len(q))
	}
	return sum * step.Hours(), true
}
