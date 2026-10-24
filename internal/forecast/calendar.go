package forecast

import (
	"math"
	"time"
)

// What is known about the future before it happens: the time of day, the
// weekend, the organization's shifts and its planned maintenance. A model that
// can read known-future covariates (Timestral) is given them, so it knows that
// Sunday the plant is still and that tomorrow 8–12 it is in maintenance; the
// statistical engine ignores them.

// ShiftWindow is one recurring shift, in the plant's local time.
type ShiftWindow struct {
	Name       string
	Start, End time.Duration // since local midnight; End < Start runs past midnight
	Weekdays   map[time.Weekday]bool
}

// Calendar is an organization's known future.
type Calendar struct {
	Loc         *time.Location
	Shifts      []ShiftWindow
	Maintenance [][2]time.Time
}

// on reports whether t falls in one of the shifts.
func (c *Calendar) on(t time.Time) bool {
	lt := t.In(c.Loc)
	midnight := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, c.Loc)
	since := lt.Sub(midnight)
	for _, s := range c.Shifts {
		if s.End > s.Start {
			if s.Weekdays[lt.Weekday()] && since >= s.Start && since < s.End {
				return true
			}
			continue
		}
		// Past midnight: started today, or yesterday and not over yet.
		if s.Weekdays[lt.Weekday()] && since >= s.Start {
			return true
		}
		if s.Weekdays[lt.AddDate(0, 0, -1).Weekday()] && since < s.End {
			return true
		}
	}
	return false
}

func (c *Calendar) inMaintenance(t time.Time) bool {
	for _, w := range c.Maintenance {
		if !t.Before(w[0]) && t.Before(w[1]) {
			return true
		}
	}
	return false
}

// Known returns the known-future covariates for n steps from start, one row
// per covariate: time of day (sine and cosine), weekend, and — when the
// organization has them — shift on and maintenance, as 0/1 indicators.
func (c *Calendar) Known(start time.Time, step time.Duration, n int) [][]float64 {
	if c == nil || c.Loc == nil || n <= 0 || step <= 0 {
		return nil
	}
	rows := 3
	if len(c.Shifts) > 0 {
		rows++
	}
	if len(c.Maintenance) > 0 {
		rows++
	}
	out := make([][]float64, rows)
	for i := range out {
		out[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		t := start.Add(time.Duration(i) * step)
		lt := t.In(c.Loc)
		frac := (float64(lt.Hour()) + float64(lt.Minute())/60) / 24
		out[0][i] = math.Sin(2 * math.Pi * frac)
		out[1][i] = math.Cos(2 * math.Pi * frac)
		if wd := lt.Weekday(); wd == time.Saturday || wd == time.Sunday {
			out[2][i] = 1
		}
		r := 3
		if len(c.Shifts) > 0 {
			if c.on(t) {
				out[r][i] = 1
			}
			r++
		}
		if len(c.Maintenance) > 0 && c.inMaintenance(t) {
			out[r][i] = 1
		}
	}
	return out
}
