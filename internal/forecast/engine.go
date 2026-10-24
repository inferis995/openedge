package forecast

import (
	"context"
	"math"
	"sort"
	"time"
)

// Request is one series to forecast.
type Request struct {
	Series  Series
	Horizon int // steps
	Kind    Kind
	Period  int // season length in steps, cyclic signals
	// Known are covariates known over the history and the horizon (one row
	// each, len(Series.Values)+Horizon long): see Calendar. Engines that
	// cannot use them ignore them.
	Known [][]float64
}

// Forecast is the result for one Request: three quantiles per step after the
// end of the series. A step the engine could not forecast is NaN.
type Forecast struct {
	Start         time.Time
	Step          time.Duration
	Q10, Q50, Q90 []float64
}

// Forecaster is a forecasting engine. It takes a batch so that a model served
// over the network is called once per run, not once per tag.
type Forecaster interface {
	Name() string
	Forecast(ctx context.Context, reqs []Request) ([]Forecast, error)
}

// Statistical is the engine that is always there: simple, explainable methods
// chosen by kind. It is the fallback when no model is configured or the model
// is down, and the yardstick a model has to beat.
type Statistical struct{}

// Name implements Forecaster.
func (Statistical) Name() string { return "statistical" }

// Forecast implements Forecaster.
func (Statistical) Forecast(_ context.Context, reqs []Request) ([]Forecast, error) {
	out := make([]Forecast, len(reqs))
	for i := range reqs {
		out[i] = statistical(&reqs[i])
	}
	return out, nil
}

func statistical(r *Request) Forecast {
	f := newForecast(r)
	v := fillForward(r.Series.Values)
	if len(v) == 0 || math.IsNaN(v[len(v)-1]) || r.Horizon <= 0 {
		return f
	}
	switch r.Kind {
	case KindCyclic, KindState:
		if r.Period > 0 && len(v) >= 2*r.Period {
			seasonal(v, r.Period, &f)
		} else {
			changes(v, &f)
		}
	case KindDegradation:
		trend(v, &f)
	case KindCounter:
		changes(resetFree(v), &f)
		last := v[len(v)-1]
		base := resetFree(v)
		shift := last - base[len(base)-1]
		for i := range f.Q50 {
			f.Q10[i] += shift
			f.Q50[i] += shift
			f.Q90[i] += shift
		}
	default:
		changes(v, &f)
	}
	return f
}

func newForecast(r *Request) Forecast {
	nan := func() []float64 {
		s := make([]float64, r.Horizon)
		for i := range s {
			s[i] = math.NaN()
		}
		return s
	}
	return Forecast{Start: r.Series.End(), Step: r.Series.Step, Q10: nan(), Q50: nan(), Q90: nan()}
}

// changes: the last value plus the quantiles of how much the series moved
// over h steps in the past, for each h. Makes no assumption about the shape
// of the signal: a value that wanders gets a band that widens, one that
// returns to its level gets a band that stays put.
//
// The quantiles are computed at a few horizons (every step up to 8, then
// about 24 more) and interpolated in between: the band changes slowly with h,
// and computing every h costs O(n·H·log n) per series.
func changes(v []float64, f *Forecast) {
	n := len(v)
	last := v[n-1]
	H := len(f.Q50)
	stride := 1
	if H > 32 {
		stride = (H + 23) / 24
	}
	type point struct {
		h           int
		lo, mid, hi float64
	}
	var grid []point
	buf := make([]float64, 0, n)
	for h := 1; h <= H && h < n; h++ {
		if h > 8 && h%stride != 0 && h != H && h != n-1 {
			continue
		}
		buf = buf[:0]
		for t := 0; t+h < n; t++ {
			buf = append(buf, v[t+h]-v[t])
		}
		if len(buf) < 8 {
			break
		}
		sort.Float64s(buf)
		grid = append(grid, point{h, quantile(buf, 0.1), quantile(buf, 0.5), quantile(buf, 0.9)})
	}
	if len(grid) == 0 {
		return
	}
	g := 0
	for h := 1; h <= H; h++ {
		for g+1 < len(grid) && grid[g+1].h <= h {
			g++
		}
		p := grid[g]
		if g+1 < len(grid) && h > p.h {
			q := grid[g+1]
			w := float64(h-p.h) / float64(q.h-p.h)
			p = point{h, p.lo + w*(q.lo-p.lo), p.mid + w*(q.mid-p.mid), p.hi + w*(q.hi-p.hi)}
		}
		// Past the longest horizon the history covers, the band stays as wide
		// as the widest one measured.
		f.Q10[h-1] = last + p.lo
		f.Q50[h-1] = last + p.mid
		f.Q90[h-1] = last + p.hi
	}
}

// seasonal: the median of the same moment in the last (up to four) seasons,
// with the band of how far a value has strayed from that in the past.
func seasonal(v []float64, p int, f *Forecast) {
	n := len(v)
	const seasons = 4
	at := func(t int) float64 { // median of t-p, t-2p, … known before t
		vals := make([]float64, 0, seasons)
		for k := 1; k <= seasons; k++ {
			if j := t - k*p; j >= 0 && j < n {
				vals = append(vals, v[j])
			}
		}
		if len(vals) == 0 {
			return math.NaN()
		}
		sort.Float64s(vals)
		return quantile(vals, 0.5)
	}
	resid := make([]float64, 0, n)
	for t := p; t < n; t++ {
		if m := at(t); !math.IsNaN(m) {
			resid = append(resid, v[t]-m)
		}
	}
	if len(resid) < 8 {
		changes(v, f)
		return
	}
	sort.Float64s(resid)
	lo, mid, hi := quantile(resid, 0.1), quantile(resid, 0.5), quantile(resid, 0.9)
	ext := append([]float64(nil), v...)
	for h := 0; h < len(f.Q50); h++ {
		m := at(n + h)
		if math.IsNaN(m) {
			// Past the last known season: use the forecast itself.
			m = ext[len(ext)-p]
		}
		ext = append(ext, m)
		f.Q10[h], f.Q50[h], f.Q90[h] = m+lo, m+mid, m+hi
	}
}

// trend: the straight line through the recent past, with the band of the
// residuals around it.
func trend(v []float64, f *Forecast) {
	n := len(v)
	slope, icept, ok := linearFit(v)
	if !ok {
		changes(v, f)
		return
	}
	resid := make([]float64, 0, n)
	for i, x := range v {
		resid = append(resid, x-(icept+slope*float64(i)))
	}
	sort.Float64s(resid)
	lo, hi := quantile(resid, 0.1), quantile(resid, 0.9)
	// Anchor on the last value, not the line: the forecast starts where the
	// signal is.
	offset := v[n-1] - (icept + slope*float64(n-1))
	for h := 1; h <= len(f.Q50); h++ {
		m := icept + slope*float64(n-1+h) + offset
		f.Q10[h-1], f.Q50[h-1], f.Q90[h-1] = m+lo, m, m+hi
	}
}

// resetFree turns a counter with resets into one that only goes up: a drop is
// a reset, and what follows continues from where it was.
func resetFree(v []float64) []float64 {
	out := make([]float64, len(v))
	var off float64
	for i, x := range v {
		if i > 0 && x < v[i-1] {
			off += v[i-1] - x
		}
		out[i] = x + off
	}
	return out
}

// Shape makes a forecast, in place, respect what the kind implies, whatever the engine:
// quantiles in order, a state's share of time within [0, 1], a counter that
// does not go below its last value or down from one step to the next.
func Shape(f *Forecast, kind Kind, last float64) {
	for i := range f.Q50 {
		lo, mid, hi := f.Q10[i], f.Q50[i], f.Q90[i]
		if lo > mid {
			lo, mid = mid, lo
		}
		if mid > hi {
			mid, hi = hi, mid
		}
		if lo > mid {
			lo, mid = mid, lo
		}
		f.Q10[i], f.Q50[i], f.Q90[i] = lo, mid, hi
	}
	switch kind {
	case KindState:
		for i := range f.Q50 {
			f.Q10[i], f.Q50[i], f.Q90[i] = clamp01(f.Q10[i]), clamp01(f.Q50[i]), clamp01(f.Q90[i])
		}
	case KindCounter:
		floor := [3]float64{last, last, last}
		for i := range f.Q50 {
			for j, q := range []*float64{&f.Q10[i], &f.Q50[i], &f.Q90[i]} {
				if math.IsNaN(*q) {
					continue
				}
				if *q < floor[j] {
					*q = floor[j]
				}
				floor[j] = *q
			}
		}
	}
}

func clamp01(x float64) float64 {
	if math.IsNaN(x) {
		return x
	}
	return math.Max(0, math.Min(1, x))
}
