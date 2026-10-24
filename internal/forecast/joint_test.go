package forecast

import (
	"context"
	"math/rand"
	"testing"
)

// A joint engine that knows the answer for the test forecast (it copies the
// held-out truth) and is otherwise flat: it must win on the backtest.
type oracleJoint struct {
	truth [][]float64 // per variate, the full series
	calls int
	known int
}

func (o *oracleJoint) Name() string { return "oracle" }
func (o *oracleJoint) ForecastJoint(_ context.Context, reqs []JointRequest) ([][]Forecast, error) {
	o.calls++
	out := make([][]Forecast, len(reqs))
	for i, q := range reqs {
		if len(q.Known) > 0 {
			o.known++
		}
		for v, s := range q.Series {
			f := newForecast(&Request{Series: s, Horizon: q.Horizon})
			for h := 0; h < q.Horizon; h++ {
				x := s.Values[len(s.Values)-1]
				if t := len(s.Values) + h; t < len(o.truth[v]) {
					x = o.truth[v][t]
				}
				f.Q10[h], f.Q50[h], f.Q90[h] = x-0.01, x, x+0.01
			}
			out[i] = append(out[i], f)
		}
	}
	return out, nil
}

func TestRunJointKeepsWhatDidBetter(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	a, b := daily(24*21, 0.5, rng), daily(24*21, 0.5, rng)
	jobs := []Job{job(1, hourly(a), KindProcess, 24), job(2, hourly(b), KindProcess, 24), job(3, hourly(a), KindProcess, 24)}
	jobs[0].Calendar = &Calendar{Loc: t0.Location()}
	results := Run(context.Background(), jobs, []Forecaster{Statistical{}})
	o := &oracleJoint{truth: [][]float64{a, b}}
	RunJoint(context.Background(), o, [][]int{{0, 1}, {2}}, jobs, results)
	if o.calls != 1 {
		t.Fatalf("%d calls: one per batch, and the one-member group is skipped", o.calls)
	}
	if o.known == 0 {
		t.Error("the calendar's known covariates were not sent")
	}
	for _, i := range []int{0, 1} {
		if results[i].Engine != "oracle"+JointEngineSuffix || results[i].Status != StatusOK {
			t.Errorf("member %d: engine %s status %s", i, results[i].Engine, results[i].Status)
		}
		if _, ok := results[i].Engines["oracle"+JointEngineSuffix]; !ok {
			t.Errorf("member %d: joint score not recorded", i)
		}
		if len(results[i].Forecast.Q50) != 24 {
			t.Errorf("member %d: %d steps, want the member's 24", i, len(results[i].Forecast.Q50))
		}
	}
	if results[2].Engine != "statistical" {
		t.Errorf("not in a machine: engine %s", results[2].Engine)
	}
}
