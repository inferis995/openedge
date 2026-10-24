package forecast

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Limits on the size of what is sent to an engine.
const (
	MaxContextSteps = 2048
	MaxHorizonSteps = 512
	// MinObserved is how many steps with data a series needs before it is
	// forecast at all. Below it the tag is "learning".
	MinObserved = 24
)

// Status of a tag's forecast.
const (
	StatusOK         = "ok"         // reliable: beat the naive forecast in the backtest
	StatusUnreliable = "unreliable" // made, but did not beat the naive forecast
	StatusLearning   = "learning"   // not enough history yet
	StatusDisabled   = "disabled"   // switched off by a profile
	StatusError      = "error"      // no engine could forecast it
)

// Job is one tag to forecast: its history on a regular grid and its settings.
type Job struct {
	TagID    int
	Series   Series
	IsBool   bool
	Settings Effective
	// StateTagID: the series was conditioned on this running-state tag.
	StateTagID int
	// Calendar is the organization's known future (shifts, maintenance).
	Calendar *Calendar
}

// Result is the outcome for one Job.
type Result struct {
	TagID    int
	Status   string
	Reason   string
	Kind     Kind
	Period   int
	Engine   string
	Forecast Forecast
	Score    Score
	// Engines is every engine's backtest, so the choice can be explained.
	Engines map[string]Score
	// Errors are the engines that failed, with why.
	Errors map[string]string
	// Risks are the tag's alarm rules this forecast may trip (reliable
	// forecasts only).
	Risks []Risk
	// Anomaly compares the values that arrived with the previous forecast.
	Anomaly *Anomaly
	// StateTagID: forecast from running time only (Job.StateTagID).
	StateTagID int
	// Periods: a counter's or a state's totals by the end of the shift, the
	// day and the month (reliable forecasts only).
	Periods []PeriodTotal
}

// HorizonSteps converts a horizon in hours to steps of the given size, at
// least one and at most MaxHorizonSteps.
func HorizonSteps(hours int, step time.Duration) int {
	if step <= 0 {
		return 1
	}
	n := int(math.Ceil(float64(time.Duration(hours)*time.Hour) / float64(step)))
	if n < 1 {
		n = 1
	}
	if n > MaxHorizonSteps {
		n = MaxHorizonSteps
	}
	return n
}

// ContextSteps is how many steps of history to read for the given settings.
func ContextSteps(days int, step time.Duration) int {
	if step <= 0 {
		return MaxContextSteps
	}
	n := int(time.Duration(days) * 24 * time.Hour / step)
	if n < 1 {
		n = 1
	}
	if n > MaxContextSteps {
		n = MaxContextSteps
	}
	return n
}

type prepared struct {
	idx     int
	series  Series // what the engines see (a counter without its resets)
	shift   float64
	last    float64
	horizon int
	holdout int
	kind    Kind
	period  int
	known   [][]float64 // len(series)+horizon
}

// Run forecasts every job with every engine, scores each engine on the data
// already known and keeps, per tag, the one that did best. Engines are called
// once each for the whole batch. An engine that fails is skipped; if all do,
// the tag's result is an error.
func Run(ctx context.Context, jobs []Job, engines []Forecaster) []Result {
	results := make([]Result, len(jobs))
	var preps []prepared
	for i := range jobs {
		results[i] = Result{TagID: jobs[i].TagID, StateTagID: jobs[i].StateTagID, Engines: map[string]Score{}, Errors: map[string]string{}}
		if p, ok := prepare(&jobs[i], &results[i]); ok {
			p.idx = i
			preps = append(preps, p)
		}
	}
	if len(preps) == 0 {
		return results
	}

	// Two requests per tag: the forecast itself, and the one made from the
	// history without its last part, to score.
	reqs := make([]Request, 0, 2*len(preps))
	for k := range preps {
		p := &preps[k]
		reqs = append(reqs,
			Request{Series: p.series, Horizon: p.horizon, Kind: p.kind, Period: p.period, Known: p.known},
			// The test forecast ends where the history does: its known
			// covariates are the history's.
			Request{Series: p.series.Head(p.holdout), Horizon: p.holdout, Kind: p.kind, Period: p.period,
				Known: cut(p.known, len(p.series.Values))},
		)
	}
	chosen := make([]choice, len(preps))
	for _, e := range engines {
		fs, err := e.Forecast(ctx, reqs)
		if err == nil && len(fs) != len(reqs) {
			err = fmt.Errorf("%d forecasts for %d requests", len(fs), len(reqs))
		}
		if err != nil {
			for k := range preps {
				results[preps[k].idx].Errors[e.Name()] = err.Error()
			}
			continue
		}
		for k := range preps {
			p := &preps[k]
			consider(&chosen[k], &results[p.idx], e.Name(), p, &fs[2*k], &fs[2*k+1])
		}
	}
	for k := range preps {
		conclude(&results[preps[k].idx], &chosen[k])
	}
	return results
}

// prepare decides whether a job is forecast at all and, if so, how. When it is
// not, it says why in the result.
func prepare(j *Job, r *Result) (prepared, bool) {
	if !j.Settings.Enabled {
		r.Status = StatusDisabled
		return prepared{}, false
	}
	if obs := j.Series.Observed(); obs < MinObserved {
		r.Status = StatusLearning
		r.Reason = fmt.Sprintf("%d of %d values needed", obs, MinObserved)
		return prepared{}, false
	}
	kind, period := j.Settings.Kind, 0
	switch kind {
	case KindAuto:
		kind, period = Classify(j.Series, j.IsBool)
	case KindCyclic, KindState:
		period = seasonFor(j.Series)
	}
	p := prepared{series: j.Series, kind: kind, period: period}
	p.horizon = HorizonSteps(j.Settings.HorizonHours, j.Series.Step)
	p.holdout = HoldoutSteps(j.Series, p.horizon)
	vals := fillForward(j.Series.Values)
	p.last = vals[len(vals)-1]
	if kind == KindCounter {
		p.series, p.shift = withoutResets(j.Series)
	}
	p.known = j.Calendar.Known(j.Series.Start, j.Series.Step, len(j.Series.Values)+p.horizon)
	r.Kind, r.Period = kind, period
	return p, true
}

type choice struct {
	ok    bool
	score Score
	name  string
	f     Forecast
}

// consider scores one engine's forecast of one tag and keeps it if it is the
// best so far.
func consider(c *choice, r *Result, engine string, p *prepared, full, test *Forecast) {
	f := unshift(full, p.shift)
	Shape(&f, p.kind, p.last)
	// Scored where the engine worked: a counter without its resets.
	sc := Evaluate(p.series, test, p.holdout)
	r.Engines[engine] = sc
	if !usable(&f) {
		r.Errors[engine] = "empty forecast"
		return
	}
	if !c.ok || better(sc, c.score) {
		*c = choice{ok: true, score: sc, name: engine, f: f}
	}
}

func conclude(r *Result, c *choice) {
	if !c.ok {
		r.Status = StatusError
		r.Reason = "no engine produced a forecast"
		return
	}
	r.Engine, r.Forecast, r.Score = c.name, c.f, c.score
	if c.score.Reliable() {
		r.Status, r.Reason = StatusOK, ""
		return
	}
	r.Status = StatusUnreliable
	r.Reason = fmt.Sprintf("does not beat repeating the last value (skill %.2f)", c.score.Skill)
}

// better: reliability first, then skill, then the calibration of the band.
func better(a, b Score) bool {
	if a.Reliable() != b.Reliable() {
		return a.Reliable()
	}
	if a.Skill != b.Skill {
		return a.Skill > b.Skill
	}
	return math.Abs(a.Coverage-0.8) < math.Abs(b.Coverage-0.8)
}

func usable(f *Forecast) bool {
	for _, x := range f.Q50 {
		if !math.IsNaN(x) {
			return true
		}
	}
	return false
}

// withoutResets returns the counter as if it never reset, and how much to add
// back to a forecast of it so that it continues from the last true value.
func withoutResets(s Series) (Series, float64) {
	out := Series{Start: s.Start, Step: s.Step, Values: make([]float64, len(s.Values))}
	var off float64
	prev := math.NaN()
	lastRaw := math.NaN()
	for i, x := range s.Values {
		if math.IsNaN(x) {
			out.Values[i] = x
			continue
		}
		if !math.IsNaN(prev) && x < prev {
			off += prev - x
		}
		out.Values[i] = x + off
		prev = x
		lastRaw = x
	}
	if math.IsNaN(lastRaw) {
		return out, 0
	}
	return out, -off
}

func unshift(f *Forecast, d float64) Forecast {
	if d == 0 {
		return *f
	}
	g := Forecast{Start: f.Start, Step: f.Step,
		Q10: make([]float64, len(f.Q10)), Q50: make([]float64, len(f.Q50)), Q90: make([]float64, len(f.Q90))}
	for i := range f.Q50 {
		g.Q10[i], g.Q50[i], g.Q90[i] = f.Q10[i]+d, f.Q50[i]+d, f.Q90[i]+d
	}
	return g
}

// cut keeps the first n columns of each row.
func cut(rows [][]float64, n int) [][]float64 {
	if rows == nil {
		return nil
	}
	out := make([][]float64, len(rows))
	for i, r := range rows {
		if n > len(r) {
			n = len(r)
		}
		out[i] = r[:n]
	}
	return out
}
