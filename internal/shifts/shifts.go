// Package shifts answers the two questions every part of the platform asks
// about work shifts: which shift is running at a given moment, and how many
// minutes of a window fall inside a shift.
//
// Both used to be answered four times — the shifts page, the dashboard, OEE's
// planned production time and the hourly OEE snapshot — each with its own copy
// of the midnight-wrap logic, and all four in UTC. A shift is written the way
// a plant speaks, "06:00–14:00" on its own clock; read in UTC, an Italian
// morning shift started at 08:00 in summer and 07:00 in winter, and OEE
// counted the wrong hours as planned. Times are now read in the server's
// local zone, which the deployment sets with TZ (Europe/Rome by default).
//
// And all four read every shift in the database, of every organization.
package shifts

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Shift is an active shift definition.
type Shift struct {
	ID       int
	Name     string
	StartMin int // minutes after local midnight
	EndMin   int
	Weekdays []int // 0 = Sunday … 6 = Saturday, of the day the shift STARTS
	Wraps    bool  // ends the next day (22:00–06:00)
}

// Scope says whose shifts to read. A shift with no organization is the
// platform's and applies to everybody.
type Scope struct {
	OrgID int
	All   bool // the global administrator with no organization selected
}

// Load reads the active shifts visible in scope.
func Load(ctx context.Context, db *sql.DB, s Scope) ([]Shift, error) {
	q := `SELECT id, name, start_time::text, end_time::text, weekdays FROM shifts WHERE active = true`
	var args []interface{}
	if !s.All {
		// The organization's own shifts; the platform's only while it has
		// none of its own — otherwise a company that defined two shifts would
		// still be planned on the default three.
		q += ` AND (org_id = $1 OR (org_id IS NULL AND NOT EXISTS (
			SELECT 1 FROM shifts o WHERE o.org_id = $1 AND o.active = true)))`
		args = append(args, s.OrgID)
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY start_time, name`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Shift
	for rows.Next() {
		var sh Shift
		var start, end string
		var wd pq.Int64Array
		if err := rows.Scan(&sh.ID, &sh.Name, &start, &end, &wd); err != nil {
			return nil, err
		}
		sh.StartMin, sh.EndMin = Minutes(start), Minutes(end)
		sh.Wraps = sh.StartMin > sh.EndMin
		for _, d := range wd {
			sh.Weekdays = append(sh.Weekdays, int(d))
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// Minutes turns "HH:MM" or "HH:MM:SS" into minutes after midnight.
func Minutes(hhmm string) int {
	parts := strings.SplitN(hhmm, ":", 3)
	if len(parts) < 2 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	return h*60 + m
}

func has(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

// span is one occurrence of a shift, starting on day (local midnight).
func span(sh *Shift, day time.Time) (time.Time, time.Time) {
	start := time.Date(day.Year(), day.Month(), day.Day(), sh.StartMin/60, sh.StartMin%60, 0, 0, day.Location())
	end := time.Date(day.Year(), day.Month(), day.Day(), sh.EndMin/60, sh.EndMin%60, 0, 0, day.Location())
	if sh.Wraps {
		end = end.AddDate(0, 0, 1)
	}
	return start, end
}

// ActiveAt returns the shift running at `at` and its start and end, read on
// the server's local clock. Nil when no shift is running.
func ActiveAt(list []Shift, at time.Time) (*Shift, time.Time, time.Time) {
	return ActiveAtIn(list, at, time.Local)
}

// ActiveAtIn is ActiveAt on the clock of loc.
func ActiveAtIn(list []Shift, at time.Time, loc *time.Location) (*Shift, time.Time, time.Time) {
	local := at.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	// A night shift that started yesterday is still running this morning.
	for _, day := range []time.Time{today, today.AddDate(0, 0, -1)} {
		for i := range list {
			sh := &list[i]
			if !has(sh.Weekdays, int(day.Weekday())) {
				continue
			}
			start, end := span(sh, day)
			if !local.Before(start) && local.Before(end) {
				return sh, start, end
			}
		}
	}
	return nil, time.Time{}, time.Time{}
}

// PlannedMinutes is how many minutes of [start, end) fall inside a shift.
// Overlapping shifts are counted once per shift, as before.
func PlannedMinutes(list []Shift, start, end time.Time) float64 {
	return PlannedMinutesIn(list, start, end, time.Local)
}

// PlannedMinutesIn is PlannedMinutes on the clock of loc.
func PlannedMinutesIn(list []Shift, start, end time.Time, loc *time.Location) float64 {
	if !end.After(start) || len(list) == 0 {
		return 0
	}
	s := start.In(loc)
	day := time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1) // yesterday's night shift
	total := 0.0
	for !day.After(end) {
		for i := range list {
			sh := &list[i]
			if !has(sh.Weekdays, int(day.Weekday())) {
				continue
			}
			a, b := span(sh, day)
			if a.Before(start) {
				a = start
			}
			if b.After(end) {
				b = end
			}
			if b.After(a) {
				total += b.Sub(a).Minutes()
			}
		}
		// AddDate, not +24h: a day is 23 or 25 hours when the clocks change.
		day = day.AddDate(0, 0, 1)
	}
	return total
}
