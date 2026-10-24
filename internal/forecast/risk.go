package forecast

import (
	"math"
	"time"
)

// Threshold is an alarm rule of the tag, as configured (alarm_definitions):
// the forecast is checked against the limits the plant already uses, so a
// predicted alarm needs nothing configured beyond the alarm itself.
type Threshold struct {
	RuleID   int     `json:"rule_id"`
	Type     string  `json:"type"` // high, high_high, low, low_low, bool_true, bool_false
	Value    float64 `json:"threshold"`
	Severity string  `json:"severity,omitempty"`
	Message  string  `json:"message,omitempty"`
}

// Likelihood of a threshold being crossed, read off the forecast's band.
const (
	// AlmostCertain: even the optimistic edge of the band crosses (P10 for a
	// high limit, P90 for a low one): about 90%.
	AlmostCertain = "almost_certain"
	// Likely: the median crosses: more likely than not.
	Likely = "likely"
	// Possible: only the pessimistic edge crosses: about 10–50%.
	Possible = "possible"
	// Trend: a slow drift reaches the limit after the horizon. An
	// extrapolation of the trend, not a forecast: the remaining useful life.
	Trend = "trend"
)

// Risk is one alarm rule the forecast says may trip.
type Risk struct {
	Threshold
	Likelihood string    `json:"likelihood"`
	At         time.Time `json:"at"` // when, first time the band edge of that likelihood crosses
	// Extrapolated: past the horizon, along the trend (Likelihood Trend).
	Extrapolated bool `json:"extrapolated,omitempty"`
}

// MaxExtrapolation is how far a drift is followed to its limit.
const MaxExtrapolation = 365 * 24 * time.Hour

// Risks checks a forecast against the tag's alarm rules. last is the latest
// known value: a rule already tripped now is the alarm system's business, not
// a prediction, and is skipped.
func Risks(f *Forecast, kind Kind, last float64, rules []Threshold) []Risk {
	var out []Risk
	for _, th := range rules {
		if r, ok := risk(f, kind, last, th); ok {
			out = append(out, r)
		}
	}
	return out
}

func risk(f *Forecast, kind Kind, last float64, th Threshold) (Risk, bool) {
	r := Risk{Threshold: th}
	crosses, above, ok := ruleCheck(th, kind, last)
	if !ok {
		return r, false
	}
	// The optimistic edge is the one farthest from the limit.
	optimistic, pessimistic := f.Q90, f.Q10
	if above {
		optimistic, pessimistic = f.Q10, f.Q90
	}
	for _, c := range []struct {
		q    []float64
		like string
	}{{optimistic, AlmostCertain}, {f.Q50, Likely}, {pessimistic, Possible}} {
		if i := firstCrossing(c.q, crosses); i >= 0 {
			r.Likelihood, r.At = c.like, f.Start.Add(time.Duration(i)*f.Step)
			return r, true
		}
	}
	if kind == KindDegradation && !isStateRule(th.Type) {
		if t, ok := extrapolate(f, th.Value); ok {
			r.Likelihood, r.At, r.Extrapolated = Trend, t, true
			return r, true
		}
	}
	return r, false
}

func isStateRule(t string) bool { return t == "bool_true" || t == "bool_false" }

// ruleCheck says what crossing the rule means for a forecast value, which side
// of the limit is the alarm (above), and whether the rule applies at all: not
// to a kind its numbers do not describe, not when it is already tripped now.
func ruleCheck(th Threshold, kind Kind, last float64) (crosses func(float64) bool, above, ok bool) {
	switch th.Type {
	case "high", "high_high":
		crosses, above = func(x float64) bool { return x > th.Value }, true
	case "low", "low_low":
		crosses = func(x float64) bool { return x < th.Value }
	case "bool_true", "bool_false":
		// A state's forecast is the share of time at 1; on any other kind the
		// numbers are not a share and the rule says nothing.
		want1 := th.Type == "bool_true"
		if kind != KindState || (want1 && last != 0) || (!want1 && last == 0) {
			return nil, false, false
		}
		return func(p float64) bool { return (want1 && p > 0.5) || (!want1 && p < 0.5) }, want1, true
	default:
		return nil, false, false
	}
	if !math.IsNaN(last) && crosses(last) {
		return nil, false, false
	}
	return crosses, above, true
}

// firstCrossing is the first step where q crosses, -1 if none.
func firstCrossing(q []float64, crosses func(float64) bool) int {
	for i, x := range q {
		if !math.IsNaN(x) && crosses(x) {
			return i
		}
	}
	return -1
}

// extrapolate follows the median's trend past the horizon to the limit: the
// time a steady drift reaches it, if it is heading there within a year.
func extrapolate(f *Forecast, limit float64) (time.Time, bool) {
	n := len(f.Q50)
	if n < 2 || f.Step <= 0 {
		return time.Time{}, false
	}
	a, b := math.NaN(), math.NaN()
	ia, ib := 0, 0
	for i, x := range f.Q50 {
		if math.IsNaN(x) {
			continue
		}
		if math.IsNaN(a) {
			a, ia = x, i
		}
		b, ib = x, i
	}
	if math.IsNaN(a) || ib <= ia {
		return time.Time{}, false
	}
	slope := (b - a) / float64(ib-ia) // per step
	if slope == 0 || (limit-b)/slope <= 0 {
		return time.Time{}, false // flat, or drifting away from the limit
	}
	d := time.Duration((limit - b) / slope * float64(f.Step))
	if d > MaxExtrapolation {
		return time.Time{}, false
	}
	return f.Start.Add(time.Duration(ib)*f.Step + d), true
}

// Anomaly is how the values that arrived compare with what was forecast for
// them: a value is anomalous when it falls well outside the band (half a band
// width beyond P10 or P90, roughly outside P1–P99).
type Anomaly struct {
	Checked int `json:"checked"`
	Outside int `json:"outside"`
	// The latest anomalous value, when there is one.
	At       *time.Time `json:"at,omitempty"`
	Value    float64    `json:"value,omitempty"`
	Low      float64    `json:"low,omitempty"`
	High     float64    `json:"high,omitempty"`
	Expected float64    `json:"expected,omitempty"`
}

// CheckAnomaly compares the actual series with a previous forecast over the
// steps they share. Nil when they share none.
func CheckAnomaly(prev *Forecast, actual Series) *Anomaly {
	if prev == nil || prev.Step <= 0 || actual.Step <= 0 {
		return nil
	}
	a := &Anomaly{}
	for i := range prev.Q50 {
		t := prev.Start.Add(time.Duration(i) * prev.Step)
		j := int(t.Sub(actual.Start) / actual.Step)
		if j < 0 || j >= len(actual.Values) || !actual.Start.Add(time.Duration(j)*actual.Step).Equal(t) {
			continue
		}
		y := actual.Values[j]
		lo, mid, hi := prev.Q10[i], prev.Q50[i], prev.Q90[i]
		if math.IsNaN(y) || math.IsNaN(lo) || math.IsNaN(hi) {
			continue
		}
		a.Checked++
		w := (hi - lo) / 2
		if y < lo-w || y > hi+w {
			a.Outside++
			tt := t
			a.At, a.Value, a.Low, a.High, a.Expected = &tt, y, lo-w, hi+w, mid
		}
	}
	if a.Checked == 0 {
		return nil
	}
	return a
}
