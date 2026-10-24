package forecast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// JointRequest is several series of one machine on the same grid, forecast
// together: a model that reads variates jointly (Timestral) sees how they
// move with each other — current, vibration and temperature of one motor.
type JointRequest struct {
	Series  []Series // same Start, Step and length
	Horizon int
	Known   [][]float64
}

// JointForecaster is an engine that can forecast variates jointly.
type JointForecaster interface {
	Name() string
	ForecastJoint(ctx context.Context, reqs []JointRequest) ([][]Forecast, error)
}

type wireJoint struct {
	Values  []nullFloats `json:"values"`
	Horizon int          `json:"horizon"`
	Known   []nullFloats `json:"known_covariates,omitempty"`
}

type wireJointForecast struct {
	Variates []wireForecast `json:"variates"`
}

// ForecastJoint implements JointForecaster: one series per variate in, one
// forecast per variate out.
func (r *Remote) ForecastJoint(ctx context.Context, reqs []JointRequest) ([][]Forecast, error) {
	body := struct {
		Quantiles []float64   `json:"quantiles"`
		Series    []wireJoint `json:"series"`
	}{Quantiles: []float64{0.1, 0.5, 0.9}, Series: make([]wireJoint, len(reqs))}
	for i, q := range reqs {
		w := wireJoint{Horizon: q.Horizon, Known: rows(q.Known)}
		for _, s := range q.Series {
			w.Values = append(w.Values, s.Values)
		}
		body.Series[i] = w
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL+"/v1/forecast", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("forecaster: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("forecaster: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var wr struct {
		Forecasts []wireJointForecast `json:"forecasts"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&wr); err != nil {
		return nil, fmt.Errorf("forecaster: bad response: %w", err)
	}
	if len(wr.Forecasts) != len(reqs) {
		return nil, fmt.Errorf("forecaster: %d forecasts for %d series", len(wr.Forecasts), len(reqs))
	}
	out := make([][]Forecast, len(reqs))
	for i := range reqs {
		q := &reqs[i]
		if len(wr.Forecasts[i].Variates) != len(q.Series) {
			return nil, fmt.Errorf("forecaster: %d variates for %d series", len(wr.Forecasts[i].Variates), len(q.Series))
		}
		for v := range q.Series {
			f := newForecast(&Request{Series: q.Series[v], Horizon: q.Horizon})
			w := wr.Forecasts[i].Variates[v]
			copy(f.Q10, w.Q10)
			copy(f.Q50, w.Q50)
			copy(f.Q90, w.Q90)
			out[i] = append(out[i], f)
		}
	}
	return out, nil
}

// JointEngineSuffix names the result of a joint forecast: "timestral-asset".
const JointEngineSuffix = "-asset"

// jointPlan is one machine's joint forecast: on the whole history, and on
// the history without its last holdout steps to score it.
type jointPlan struct {
	members []int
	full    JointRequest
	test    JointRequest
	holdout int
}

// planJoint lays out a group on one grid — the same step, the last n steps of
// each member, n the shortest — or reports that it cannot be forecast jointly.
// Counters and states are left out: their series are transformed or are
// shares, not values that move with the others.
func planJoint(g []int, jobs []Job, results []Result) (jointPlan, bool) {
	var members []int
	for _, i := range g {
		r := &results[i]
		if (r.Status == StatusOK || r.Status == StatusUnreliable) && r.Kind != KindCounter && r.Kind != KindState {
			members = append(members, i)
		}
	}
	if len(members) < 2 {
		return jointPlan{}, false
	}
	first := &jobs[members[0]].Series
	step, end := first.Step, first.End()
	n, horizon := -1, 0
	for _, i := range members {
		s := &jobs[i].Series
		if s.Step != step || !s.End().Equal(end) {
			return jointPlan{}, false
		}
		if n < 0 || len(s.Values) < n {
			n = len(s.Values)
		}
		if h := len(results[i].Forecast.Q50); h > horizon {
			horizon = h
		}
	}
	if n < MinObserved || horizon == 0 {
		return jointPlan{}, false
	}
	p := jointPlan{members: members}
	series := make([]Series, 0, len(members))
	for _, i := range members {
		v := jobs[i].Series.Values
		series = append(series, Series{Start: end.Add(-step * time.Duration(n)), Step: step, Values: v[len(v)-n:]})
	}
	p.holdout = HoldoutSteps(series[0], horizon)
	if p.holdout <= 0 {
		return jointPlan{}, false
	}
	cal := jobs[members[0]].Calendar
	p.full = JointRequest{Series: series, Horizon: horizon, Known: cal.Known(series[0].Start, step, n+horizon)}
	heads := make([]Series, 0, len(series))
	for k := range series {
		heads = append(heads, series[k].Head(p.holdout))
	}
	p.test = JointRequest{Series: heads, Horizon: p.holdout, Known: cal.Known(series[0].Start, step, n)}
	return p, true
}

// RunJoint forecasts the members of each group (a machine) jointly and keeps,
// per member, the joint forecast when it did better on the known history than
// the member's own. groups are indexes into jobs/results.
func RunJoint(ctx context.Context, e JointForecaster, groups [][]int, jobs []Job, results []Result) {
	var plans []jointPlan
	for _, g := range groups {
		if p, ok := planJoint(g, jobs, results); ok {
			plans = append(plans, p)
		}
	}
	if len(plans) == 0 {
		return
	}
	reqs := make([]JointRequest, 0, 2*len(plans))
	for k := range plans {
		reqs = append(reqs, plans[k].full, plans[k].test)
	}
	fs, err := e.ForecastJoint(ctx, reqs)
	name := e.Name() + JointEngineSuffix
	if err != nil {
		for k := range plans {
			for _, i := range plans[k].members {
				results[i].Errors[name] = err.Error()
			}
		}
		return
	}
	for k := range plans {
		p := &plans[k]
		for v, i := range p.members {
			r := &results[i]
			full, test := fs[2*k][v], fs[2*k+1][v]
			sc := Evaluate(p.full.Series[v], &test, p.holdout)
			r.Engines[name] = sc
			Shape(&full, r.Kind, lastObserved(jobs[i].Series))
			// Trim to the member's own horizon.
			if h := len(r.Forecast.Q50); h > 0 && h < len(full.Q50) {
				full.Q10, full.Q50, full.Q90 = full.Q10[:h], full.Q50[:h], full.Q90[:h]
			}
			if !usable(&full) || !better(sc, r.Score) {
				continue
			}
			c := choice{ok: true, score: sc, name: name, f: full}
			conclude(r, &c)
		}
	}
}
