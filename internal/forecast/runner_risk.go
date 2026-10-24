package forecast

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/lib/pq"
)

// RiskNotice is a predicted alarm worth telling the operator about: a rule
// that has become likely or almost certain to trip within the horizon, and
// was not in the previous forecast.
type RiskNotice struct {
	OrgID int
	TagID int
	Alias string
	Risk  Risk
}

type previous struct {
	status string
	f      Forecast
	risks  []Risk
}

// thresholds are the enabled alarm rules of these tags, by tag.
func (r *Runner) thresholds(ctx context.Context, ids []int) map[int][]Threshold {
	out := map[int][]Threshold{}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT id, tag_id, alarm_type, COALESCE(threshold, 0), COALESCE(severity, ''), COALESCE(message, '')
		FROM alarm_definitions
		WHERE enabled = TRUE AND tag_id = ANY($1)
		  AND alarm_type IN ('high', 'high_high', 'low', 'low_low', 'bool_true', 'bool_false')`, pq.Array(ids))
	if err != nil {
		// No alarm table (tests, an old schema): no rules, no risks.
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var th Threshold
		var tag int
		if rows.Scan(&th.RuleID, &tag, &th.Type, &th.Value, &th.Severity, &th.Message) == nil {
			out[tag] = append(out[tag], th)
		}
	}
	return out
}

// previousForecasts are the forecasts these tags had before this run: what
// the new values are checked against, and what a new risk is new against.
func (r *Runner) previousForecasts(ctx context.Context, ids []int) map[int]previous {
	out := map[int]previous{}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT tag_id, status, start_at, step_seconds, quantiles, risks
		FROM tag_forecasts WHERE tag_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			tag         int
			p           previous
			start       sql.NullTime
			step        int
			q, risksRaw []byte
		)
		if rows.Scan(&tag, &p.status, &start, &step, &q, &risksRaw) != nil {
			continue
		}
		var qs Quantiles
		if start.Valid && step > 0 && json.Unmarshal(q, &qs) == nil {
			p.f = Forecast{Start: start.Time, Step: time.Duration(step) * time.Second, Q10: qs.Q10, Q50: qs.Q50, Q90: qs.Q90}
		}
		_ = json.Unmarshal(risksRaw, &p.risks)
		out[tag] = p
	}
	return out
}

func lastObserved(s Series) float64 {
	for i := len(s.Values) - 1; i >= 0; i-- {
		if !math.IsNaN(s.Values[i]) {
			return s.Values[i]
		}
	}
	return math.NaN()
}

// assess adds to a result what the forecast means for the tag: the alarm
// rules it may trip, and whether the values that arrived since the previous
// forecast fell outside it. Only a reliable forecast is assessed — an
// unreliable one must not raise a predicted alarm.
func assess(res *Result, job *Job, rules []Threshold, prev *previous) {
	if res.Status != StatusOK {
		return
	}
	res.Risks = Risks(&res.Forecast, res.Kind, lastObserved(job.Series), rules)
	if prev.status == StatusOK && len(prev.f.Q50) > 0 {
		res.Anomaly = CheckAnomaly(&prev.f, job.Series)
	}
}

// newNotices are the risks worth a notification: likely or almost certain,
// and not already likely or almost certain in the previous forecast.
func newNotices(org, tag int, alias string, risks, before []Risk) []RiskNotice {
	serious := func(l string) bool { return l == Likely || l == AlmostCertain }
	had := map[int]bool{}
	for _, b := range before {
		if serious(b.Likelihood) {
			had[b.RuleID] = true
		}
	}
	var out []RiskNotice
	for _, r := range risks {
		if serious(r.Likelihood) && !had[r.RuleID] {
			out = append(out, RiskNotice{OrgID: org, TagID: tag, Alias: alias, Risk: r})
		}
	}
	return out
}

func (r *Runner) notify(notices []RiskNotice) {
	if r.OnRisk == nil {
		return
	}
	for i := range notices {
		n := &notices[i]
		func() {
			defer func() {
				if p := recover(); p != nil {
					log.Printf("[FORECAST] risk notice: %v", p)
				}
			}()
			r.OnRisk(*n)
		}()
	}
}

// stateTags maps each of these tags to the tag that says its machine is
// running, for tags that belong to a machine that has one.
func (r *Runner) stateTags(ctx context.Context, ids []int) map[int]int {
	out := map[int]int{}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT at.tag_id, a.state_tag_id FROM asset_tags at
		JOIN assets a ON a.id = at.asset_id
		WHERE a.state_tag_id IS NOT NULL AND at.tag_id = ANY($1) AND at.tag_id <> a.state_tag_id
		ORDER BY a.id`, pq.Array(ids))
	if err != nil {
		return out // no machines table yet: nothing to condition on
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var tag, state int
		if rows.Scan(&tag, &state) == nil {
			if _, ok := out[tag]; !ok {
				out[tag] = state
			}
		}
	}
	return out
}

// MaskStopped hides the steps where the machine was not running (its state
// below 0.5) so a forecast learns from running time only: a temperature that
// drops at every stop says nothing about the temperature in operation. A step
// whose state is unknown is kept. It reports how many steps it hid.
func MaskStopped(s *Series, state Series) int {
	if state.Step != s.Step || len(s.Values) == 0 {
		return 0
	}
	hidden := 0
	for i := range s.Values {
		t := s.Start.Add(time.Duration(i) * s.Step)
		j := int(t.Sub(state.Start) / state.Step)
		if j < 0 || j >= len(state.Values) || math.IsNaN(state.Values[j]) || math.IsNaN(s.Values[i]) {
			continue
		}
		if state.Values[j] < 0.5 {
			s.Values[i] = math.NaN()
			hidden++
		}
	}
	return hidden
}

// calendars reads each organization's shifts (its own and the platform's)
// and its maintenance windows around now.
func (r *Runner) calendars(ctx context.Context, orgs []int, now time.Time) map[int]*Calendar {
	out := map[int]*Calendar{}
	for _, o := range orgs {
		out[o] = &Calendar{Loc: time.Local}
	}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT o.id, s.name, s.start_time::text, s.end_time::text, s.weekdays
		FROM shifts s JOIN organizations o ON s.org_id = o.id OR s.org_id IS NULL
		WHERE s.active AND o.id = ANY($1)`, pq.Array(orgs))
	if err == nil {
		for rows.Next() {
			var org int
			var name, st, et string
			var days pq.Int64Array
			if rows.Scan(&org, &name, &st, &et, &days) != nil || out[org] == nil {
				continue
			}
			w := ShiftWindow{Name: name, Start: clock(st), End: clock(et), Weekdays: map[time.Weekday]bool{}}
			for _, d := range days {
				w.Weekdays[time.Weekday(d)] = true
			}
			out[org].Shifts = append(out[org].Shifts, w)
		}
		_ = rows.Close()
	}
	rows, err = r.DB.QueryContext(ctx, `
		SELECT org_id, start_at, end_at FROM maintenance_windows
		WHERE org_id = ANY($1) AND end_at > $2 AND start_at < $3`,
		pq.Array(orgs), now.Add(-MaxContextDays*24*time.Hour), now.Add(MaxHorizonHours*time.Hour))
	if err == nil {
		for rows.Next() {
			var org int
			var a, b time.Time
			if rows.Scan(&org, &a, &b) == nil && out[org] != nil {
				out[org].Maintenance = append(out[org].Maintenance, [2]time.Time{a, b})
			}
		}
		_ = rows.Close()
	}
	return out
}

// clock parses "HH:MM[:SS]" into the time since midnight.
func clock(s string) time.Duration {
	var h, m, sec int
	_, _ = fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec)
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
}

// machineGroups are, among these tags, the members of each machine: indexes
// into tags, for machines with two or more of them in this batch.
func (r *Runner) machineGroups(ctx context.Context, tags []tagRow) [][]int {
	idx := map[int]int{}
	ids := make([]int, len(tags))
	for i, t := range tags {
		idx[t.id] = i
		ids[i] = t.id
	}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT at.asset_id, at.tag_id FROM asset_tags at
		JOIN assets a ON a.id = at.asset_id
		WHERE at.tag_id = ANY($1) AND at.tag_id IS DISTINCT FROM a.state_tag_id
		ORDER BY at.asset_id, at.tag_id`, pq.Array(ids))
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	byAsset := map[int][]int{}
	var order []int
	used := map[int]bool{}
	for rows.Next() {
		var asset, tag int
		if rows.Scan(&asset, &tag) != nil || used[tag] {
			continue // a tag in two machines is forecast with the first
		}
		if _, ok := byAsset[asset]; !ok {
			order = append(order, asset)
		}
		byAsset[asset] = append(byAsset[asset], idx[tag])
		used[tag] = true
	}
	var out [][]int
	for _, a := range order {
		if len(byAsset[a]) >= 2 {
			out = append(out, byAsset[a])
		}
	}
	return out
}
