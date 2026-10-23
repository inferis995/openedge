package forecast

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func hourly(vals []float64) Series { return Series{Start: t0, Step: time.Hour, Values: vals} }

func gen(n int, f func(i int) float64) []float64 {
	v := make([]float64, n)
	for i := range v {
		v[i] = f(i)
	}
	return v
}

func TestChooseStep(t *testing.T) {
	every := func(d time.Duration, n int) []time.Time {
		ts := make([]time.Time, n)
		for i := range ts {
			ts[i] = t0.Add(time.Duration(i) * d)
		}
		return ts
	}
	for _, tc := range []struct {
		gap  time.Duration
		want time.Duration
	}{
		{time.Second, time.Minute},
		{15 * time.Minute, 15 * time.Minute},
		{20 * time.Minute, time.Hour},
		{8 * time.Hour, 24 * time.Hour},
	} {
		if got := ChooseStep(every(tc.gap, 50)); got != tc.want {
			t.Errorf("gap %v: step %v, want %v", tc.gap, got, tc.want)
		}
	}
}

func TestResample(t *testing.T) {
	end := t0.Add(5 * time.Hour)
	s := Resample([]Sample{
		{t0.Add(1 * time.Hour), 1}, {t0.Add(1*time.Hour + 30*time.Minute), 3}, // averaged
		{t0.Add(3 * time.Hour), 5},
		{t0.Add(9 * time.Hour), 9}, // after the end: dropped
	}, time.Hour, end, 5)
	if !s.Start.Equal(t0.Add(time.Hour)) {
		t.Fatalf("start %v: leading empty steps should be dropped", s.Start)
	}
	want := []float64{2, math.NaN(), 5, math.NaN()}
	if len(s.Values) != len(want) {
		t.Fatalf("values %v, want %v", s.Values, want)
	}
	for i := range want {
		if math.IsNaN(want[i]) != math.IsNaN(s.Values[i]) || (!math.IsNaN(want[i]) && want[i] != s.Values[i]) {
			t.Fatalf("values %v, want %v", s.Values, want)
		}
	}
}

func daily(n int, noise float64, rng *rand.Rand) []float64 {
	return gen(n, func(i int) float64 {
		return 10 + 3*math.Sin(2*math.Pi*float64(i)/24) + noise*rng.NormFloat64()
	})
}

func TestClassify(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	counter := gen(200, func(i int) float64 { return float64(i % 120 * 3) }) // one reset
	walk := make([]float64, 300)
	for i := 1; i < len(walk); i++ {
		walk[i] = walk[i-1] + rng.NormFloat64()*0.1
		walk[i] -= 0.2 * walk[i-1] // pulled back to its level
	}
	for _, tc := range []struct {
		name   string
		s      Series
		isBool bool
		want   Kind
	}{
		{"daily cycle", hourly(daily(24*14, 0.3, rng)), false, KindCyclic},
		{"steady drift", hourly(gen(300, func(i int) float64 { return 3.6 - 0.001*float64(i) + 0.002*rng.NormFloat64() })), false, KindDegradation},
		{"totalizer with a reset", hourly(counter), false, KindCounter},
		{"bool tag", hourly(gen(100, func(i int) float64 { return float64(i % 2) })), true, KindState},
		{"0/1 values", hourly(gen(100, func(i int) float64 { return float64((i / 3) % 2) })), false, KindState},
		{"noise around a level", hourly(walk), false, KindProcess},
	} {
		if got, _ := Classify(tc.s, tc.isBool); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	if _, p := Classify(hourly(daily(24*14, 0.3, rng)), false); p != 24 {
		t.Errorf("daily cycle: period %d, want 24", p)
	}
}

func job(id int, s Series, kind Kind, horizonH int) Job {
	e := Defaults
	e.Kind = kind
	e.HorizonHours = horizonH
	return Job{TagID: id, Series: s, Settings: e}
}

func TestRunCyclicIsReliable(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	r := Run(context.Background(), []Job{job(1, hourly(daily(24*21, 0.2, rng)), KindAuto, 24)}, []Forecaster{Statistical{}})[0]
	if r.Status != StatusOK || r.Kind != KindCyclic {
		t.Fatalf("status %s kind %s (%s), want ok cyclic", r.Status, r.Kind, r.Reason)
	}
	if r.Score.Skill < 0.3 {
		t.Errorf("skill %.2f: a clean daily cycle should beat repeating the last value easily", r.Score.Skill)
	}
	if len(r.Forecast.Q50) != 24 || !r.Forecast.Start.Equal(t0.Add(24*21*time.Hour)) {
		t.Errorf("forecast of %d steps from %v", len(r.Forecast.Q50), r.Forecast.Start)
	}
	for i := range r.Forecast.Q50 {
		if !(r.Forecast.Q10[i] <= r.Forecast.Q50[i] && r.Forecast.Q50[i] <= r.Forecast.Q90[i]) {
			t.Fatalf("step %d: quantiles out of order", i)
		}
	}
}

func TestRunDegradationContinuesTheSlope(t *testing.T) {
	s := hourly(gen(400, func(i int) float64 { return 3.6 - 0.001*float64(i) }))
	r := Run(context.Background(), []Job{job(1, s, KindAuto, 100)}, []Forecaster{Statistical{}})[0]
	if r.Kind != KindDegradation {
		t.Fatalf("kind %s", r.Kind)
	}
	if got, want := r.Forecast.Q50[99], 3.6-0.001*499; math.Abs(got-want) > 1e-6 {
		t.Errorf("100 steps ahead: %.4f, want %.4f", got, want)
	}
}

func TestRunCounterNeverGoesDown(t *testing.T) {
	v := gen(300, func(i int) float64 { return float64(i%200) * 2 }) // reset at 200
	r := Run(context.Background(), []Job{job(1, hourly(v), KindAuto, 48)}, []Forecaster{Statistical{}})[0]
	if r.Kind != KindCounter {
		t.Fatalf("kind %s", r.Kind)
	}
	last := v[len(v)-1]
	prev := last
	for i, x := range r.Forecast.Q50 {
		if x < prev {
			t.Fatalf("step %d: %.1f after %.1f, a counter does not go down", i, x, prev)
		}
		prev = x
	}
	if got := r.Forecast.Q50[0]; math.Abs(got-(last+2)) > 1e-9 {
		t.Errorf("first step %.1f, want %.1f: it continues from the last value, not from before the reset", got, last+2)
	}
}

func TestRunStateIsAShareOfTime(t *testing.T) {
	v := gen(24*14, func(i int) float64 {
		if h := i % 24; h >= 8 && h < 18 {
			return 1
		}
		return 0
	})
	r := Run(context.Background(), []Job{job(1, hourly(v), KindAuto, 24)}, []Forecaster{Statistical{}})[0]
	if r.Kind != KindState {
		t.Fatalf("kind %s", r.Kind)
	}
	for i := range r.Forecast.Q50 {
		for _, q := range []float64{r.Forecast.Q10[i], r.Forecast.Q50[i], r.Forecast.Q90[i]} {
			if q < 0 || q > 1 {
				t.Fatalf("step %d: %v outside [0, 1]", i, q)
			}
		}
	}
}

func TestRunLearningAndDisabled(t *testing.T) {
	few := job(1, hourly(gen(10, func(i int) float64 { return float64(i) })), KindAuto, 24)
	off := job(2, hourly(gen(100, func(i int) float64 { return float64(i) })), KindAuto, 24)
	off.Settings.Enabled = false
	rs := Run(context.Background(), []Job{few, off}, []Forecaster{Statistical{}})
	if rs[0].Status != StatusLearning || !strings.Contains(rs[0].Reason, "10 of 24") {
		t.Errorf("few points: %s %q", rs[0].Status, rs[0].Reason)
	}
	if rs[1].Status != StatusDisabled {
		t.Errorf("disabled: %s", rs[1].Status)
	}
}

func TestRunNoiseIsUnreliable(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	// Independent noise: nothing beats repeating the last value by much, and
	// the forecast must not be passed off as a good one when it does not.
	v := gen(500, func(int) float64 { return rng.NormFloat64() })
	r := Run(context.Background(), []Job{job(1, hourly(v), KindDegradation, 24)}, []Forecaster{Statistical{}})[0]
	if r.Status == StatusOK && r.Score.Skill <= 0 {
		t.Fatalf("ok with skill %.2f", r.Score.Skill)
	}
}

type fakeEngine struct {
	name  string
	err   error
	value float64
}

func (f fakeEngine) Name() string { return f.name }
func (f fakeEngine) Forecast(_ context.Context, reqs []Request) ([]Forecast, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]Forecast, len(reqs))
	for i := range reqs {
		fc := newForecast(&reqs[i])
		for h := range fc.Q50 {
			fc.Q10[h], fc.Q50[h], fc.Q90[h] = f.value-1, f.value, f.value+1
		}
		out[i] = fc
	}
	return out, nil
}

func TestRunKeepsTheBestEngine(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	s := hourly(daily(24*21, 0.2, rng))
	bad := fakeEngine{name: "bad", value: 1000}
	down := fakeEngine{name: "down", err: errors.New("connection refused")}
	r := Run(context.Background(), []Job{job(1, s, KindAuto, 24)}, []Forecaster{bad, down, Statistical{}})[0]
	if r.Engine != "statistical" {
		t.Errorf("engine %s, want statistical (the other is far off)", r.Engine)
	}
	if _, ok := r.Engines["bad"]; !ok {
		t.Error("the losing engine's score should be kept")
	}
	if !strings.Contains(r.Errors["down"], "connection refused") {
		t.Errorf("errors %v", r.Errors)
	}
}

func TestRunAllEnginesDown(t *testing.T) {
	s := hourly(gen(100, func(i int) float64 { return float64(i) }))
	r := Run(context.Background(), []Job{job(1, s, KindAuto, 24)}, []Forecaster{fakeEngine{name: "x", err: errors.New("boom")}})[0]
	if r.Status != StatusError {
		t.Fatalf("status %s", r.Status)
	}
}

func TestMergeAndValidate(t *testing.T) {
	off, cyc, h := false, KindCyclic, 72
	e := Merge(Settings{Enabled: &off}, Settings{Kind: &cyc}, Settings{HorizonHours: &h})
	if e.Enabled || e.Kind != KindCyclic || e.HorizonHours != 72 || e.ContextDays != Defaults.ContextDays {
		t.Fatalf("merged %+v", e)
	}
	on := true
	if e := Merge(Settings{Enabled: &off}, Settings{Enabled: &on}); !e.Enabled {
		t.Error("the more specific level should win")
	}
	bad, zero := Kind("magic"), 0
	if err := (Settings{Kind: &bad, HorizonHours: &zero}).Validate(); err == nil ||
		!strings.Contains(err.Error(), "kind") || !strings.Contains(err.Error(), "horizon_hours") {
		t.Errorf("validate: %v", err)
	}
	if err := (Settings{Kind: &cyc, HorizonHours: &h}).Validate(); err != nil {
		t.Errorf("valid settings rejected: %v", err)
	}
}

func TestRemote(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/forecast" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Series []struct {
				Values  []*float64 `json:"values"`
				Horizon int        `json:"horizon"`
			} `json:"series"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		out := struct {
			Model     string                  `json:"model"`
			Forecasts []map[string][]*float64 `json:"forecasts"`
		}{Model: "test-model"}
		for _, s := range req.Series {
			if s.Values[1] != nil {
				http.Error(w, "NaN should travel as null", 400)
				return
			}
			last := *s.Values[len(s.Values)-1]
			q := make([]*float64, s.Horizon)
			for i := range q {
				v := last
				q[i] = &v
			}
			q[0] = nil // a step the model could not forecast
			out.Forecasts = append(out.Forecasts, map[string][]*float64{"q10": q, "q50": q, "q90": q})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	rm := NewRemote(srv.URL+"/", "", 5*time.Second)
	rm.Batch = 2
	reqs := make([]Request, 3)
	for i := range reqs {
		reqs[i] = Request{Series: hourly([]float64{1, math.NaN(), float64(i)}), Horizon: 4}
	}
	fs, err := rm.Forecast(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("%d calls for 3 series in batches of 2", calls)
	}
	if rm.Name() != "timestral" || rm.Model != "test-model" {
		t.Errorf("name %q model %q", rm.Name(), rm.Model)
	}
	if !math.IsNaN(fs[0].Q50[0]) || fs[2].Q50[3] != 2 {
		t.Errorf("forecasts %v / %v", fs[0].Q50, fs[2].Q50)
	}
	if !fs[1].Start.Equal(t0.Add(3 * time.Hour)) {
		t.Errorf("start %v", fs[1].Start)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	}))
	defer broken.Close()
	if _, err := NewRemote(broken.URL, "", time.Second).Forecast(context.Background(), reqs); err == nil ||
		!strings.Contains(err.Error(), "503") {
		t.Errorf("error %v", err)
	}
}
