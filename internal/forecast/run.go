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
}

// Run forecasts every job with every engine, scores each engine on the data
// already known and keeps, per tag, the one that did best. Engines are called
// once each for the whole batch. An engine that fails is skipped; if all do,
// the tag's result is an error.
func Run(ctx context.Context, jobs []Job, engines []Forecaster) []Result {
	results := make([]Result, len(jobs))
	var preps []prepared
	for i, j := range jobs {
		results[i] = Result{TagID: j.TagID, Engines: map[string]Score{}, Errors: map[string]string{}}
		if !j.Settings.Enabled {
			results[i].Status = StatusDisabled
			continue
		}
		obs := j.Series.Observed()
		if obs < MinObserved {
			results[i].Status = StatusLearning
			results[i].Reason = fmt.Sprintf("%d of %d values needed", obs, MinObserved)
			continue
		}
		kind, period := j.Settings.Kind, 0
		if kind == KindAuto {
			kind, period = Classify(j.Series, j.IsBool)
		} else if kind == KindCyclic || kind == KindState {
			period = seasonFor(j.Series)
		}
		p := prepared{idx: i, series: j.Series, kind: kind, period: period}
		p.horizon = HorizonSteps(j.Settings.HorizonHours, j.Series.Step)
		p.holdout = HoldoutSteps(j.Series, p.horizon)
		vals := fillForward(j.Series.Values)
		p.last = vals[len(vals)-1]
		if kind == KindCounter {
			p.series, p.shift = withoutResets(j.Series)
		}
		results[i].Kind, results[i].Period = kind, period
		preps = append(preps, p)
	}
	if len(preps) == 0 {
		return results
	}

	// Two requests per tag: the forecast itself, and the one made from the
	// history without its last part, to score.
	reqs := make([]Request, 0, 2*len(preps))
	for _, p := range preps {
		reqs = append(reqs,
			Request{Series: p.series, Horizon: p.horizon, Kind: p.kind, Period: p.period},
			Request{Series: p.series.Head(p.holdout), Horizon: p.holdout, Kind: p.kind, Period: p.period},
		)
	}
	type best struct {
		ok    bool
		score Score
		name  string
		f     Forecast
	}
	chosen := make([]best, len(preps))
	for _, e := range engines {
		fs, err := e.Forecast(ctx, reqs)
		if err == nil && len(fs) != len(reqs) {
			err = fmt.Errorf("%d forecasts for %d requests", len(fs), len(reqs))
		}
		if err != nil {
			for _, p := range preps {
				results[p.idx].Errors[e.Name()] = err.Error()
			}
			continue
		}
		for k, p := range preps {
			full := Shape(unshift(fs[2*k], p.shift), p.kind, p.last)
			// Scored where the engine worked: a counter without its resets.
			sc := Evaluate(p.series, fs[2*k+1], p.holdout)
			results[p.idx].Engines[e.Name()] = sc
			if !usable(full) {
				results[p.idx].Errors[e.Name()] = "empty forecast"
				continue
			}
			c := &chosen[k]
			if !c.ok || better(sc, c.score) {
				*c = best{ok: true, score: sc, name: e.Name(), f: full}
			}
		}
	}
	for k, p := range preps {
		r := &results[p.idx]
		c := chosen[k]
		if !c.ok {
			r.Status = StatusError
			r.Reason = "no engine produced a forecast"
			continue
		}
		r.Engine, r.Forecast, r.Score = c.name, c.f, c.score
		if c.score.Reliable() {
			r.Status = StatusOK
		} else {
			r.Status = StatusUnreliable
			r.Reason = fmt.Sprintf("does not beat repeating the last value (skill %.2f)", c.score.Skill)
		}
	}
	return results
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

func usable(f Forecast) bool {
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

func unshift(f Forecast, d float64) Forecast {
	if d == 0 {
		return f
	}
	g := Forecast{Start: f.Start, Step: f.Step,
		Q10: make([]float64, len(f.Q10)), Q50: make([]float64, len(f.Q50)), Q90: make([]float64, len(f.Q90))}
	for i := range f.Q50 {
		g.Q10[i], g.Q50[i], g.Q90[i] = f.Q10[i]+d, f.Q50[i]+d, f.Q90[i]+d
	}
	return g
}
