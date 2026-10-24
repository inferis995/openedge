package forecast

import (
	"context"
	"database/sql"
	"encoding/json"
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
