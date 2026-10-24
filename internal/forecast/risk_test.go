package forecast

import (
	"math"
	"testing"
	"time"
)

func fc(q10, q50, q90 []float64) *Forecast {
	return &Forecast{Start: t0, Step: time.Hour, Q10: q10, Q50: q50, Q90: q90}
}

func TestRisksByLikelihood(t *testing.T) {
	high := Threshold{RuleID: 1, Type: "high", Value: 10}
	low := Threshold{RuleID: 2, Type: "low_low", Value: 2}
	for _, tc := range []struct {
		name   string
		f      *Forecast
		rule   Threshold
		want   string
		atStep int
		noRisk bool
	}{
		{"even P10 above: almost certain", fc([]float64{8, 11}, []float64{9, 12}, []float64{10, 13}), high, AlmostCertain, 1, false},
		{"median above: likely", fc([]float64{8, 9}, []float64{9, 11}, []float64{10, 12}), high, Likely, 1, false},
		{"only P90 above: possible", fc([]float64{7, 8}, []float64{8, 9}, []float64{9, 11}), high, Possible, 1, false},
		{"band below the limit: no risk", fc([]float64{7, 8}, []float64{8, 9}, []float64{9, 9.5}), high, "", 0, true},
		{"low limit, median below", fc([]float64{2, 1}, []float64{3, 1.5}, []float64{4, 2.5}), low, Likely, 1, false},
	} {
		got := Risks(tc.f, KindProcess, 5, []Threshold{tc.rule})
		if tc.noRisk {
			if len(got) != 0 {
				t.Errorf("%s: %+v", tc.name, got)
			}
			continue
		}
		if len(got) != 1 || got[0].Likelihood != tc.want || !got[0].At.Equal(t0.Add(time.Duration(tc.atStep)*time.Hour)) {
			t.Errorf("%s: %+v, want %s at step %d", tc.name, got, tc.want, tc.atStep)
		}
	}
}

func TestARuleAlreadyTrippedIsNotAPrediction(t *testing.T) {
	f := fc([]float64{11, 11}, []float64{12, 12}, []float64{13, 13})
	if got := Risks(f, KindProcess, 12, []Threshold{{Type: "high", Value: 10}}); len(got) != 0 {
		t.Errorf("already above the limit: %+v", got)
	}
}

func TestRemainingUsefulLife(t *testing.T) {
	// A battery losing 0.01 V an hour from 3.60 V: 3.3 V is 30 h away, past a
	// 10 h horizon. Extrapolated, and said so.
	q := make([]float64, 10)
	for i := range q {
		q[i] = 3.60 - 0.01*float64(i+1)
	}
	f := fc(q, q, q)
	got := Risks(f, KindDegradation, 3.60, []Threshold{{Type: "low", Value: 3.3}})
	if len(got) != 1 || got[0].Likelihood != Trend || !got[0].Extrapolated {
		t.Fatalf("%+v", got)
	}
	want := t0.Add(29 * time.Hour) // the median is 3.30 at step 29
	if d := got[0].At.Sub(want); math.Abs(d.Hours()) > 0.01 {
		t.Errorf("at %v, want %v", got[0].At, want)
	}
	// Drifting away from the limit: nothing.
	if got := Risks(f, KindDegradation, 3.60, []Threshold{{Type: "high", Value: 4}}); len(got) != 0 {
		t.Errorf("drifting away: %+v", got)
	}
	// Not a degradation signal: no extrapolation.
	if got := Risks(f, KindProcess, 3.60, []Threshold{{Type: "low", Value: 3.3}}); len(got) != 0 {
		t.Errorf("process kind extrapolated: %+v", got)
	}
}

func TestStateRules(t *testing.T) {
	f := fc([]float64{0.4, 0.6}, []float64{0.5, 0.8}, []float64{0.6, 0.9})
	if got := Risks(f, KindState, 0, []Threshold{{Type: "bool_true"}}); len(got) != 1 || got[0].Likelihood != AlmostCertain {
		t.Errorf("bool_true: %+v", got)
	}
	if got := Risks(f, KindProcess, 0, []Threshold{{Type: "bool_true"}}); len(got) != 0 {
		t.Errorf("a bool rule on a non-state forecast: %+v", got)
	}
}

func TestCheckAnomaly(t *testing.T) {
	prev := fc([]float64{9, 9, 9}, []float64{10, 10, 10}, []float64{11, 11, 11})
	actual := Series{Start: t0.Add(-time.Hour), Step: time.Hour, Values: []float64{10, 10.5, 12.5, math.NaN()}}
	a := CheckAnomaly(prev, actual)
	if a == nil || a.Checked != 2 || a.Outside != 1 || a.Value != 12.5 || !a.At.Equal(t0.Add(time.Hour)) {
		t.Fatalf("%+v", a)
	}
	if CheckAnomaly(prev, Series{Start: t0.Add(10 * time.Hour), Step: time.Hour, Values: []float64{1}}) != nil {
		t.Error("no overlap should be nil")
	}
}
