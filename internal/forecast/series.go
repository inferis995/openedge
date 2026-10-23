// Package forecast predicts where a tag is going.
//
// It knows nothing about devices or industries. What a tag's forecast is, and
// how it is made, depends on the kind of signal (see Kind), which is inferred
// from the data and can be set per organization, site, area, gateway or tag
// (see Settings). Every forecast is checked against the data already known
// (see Backtest) so that one that does not beat a naive guess is never shown
// as a good one.
//
// Engines implement Forecaster: a statistical one in Go is always available,
// a model served over HTTP (Timestral) is optional.
package forecast

import (
	"math"
	"sort"
	"time"
)

// Series is a regular time series: Values[i] is the value at Start + i·Step.
// NaN marks a step with no data.
type Series struct {
	Start  time.Time
	Step   time.Duration
	Values []float64
}

// End is the time just after the last step.
func (s Series) End() time.Time { return s.Start.Add(time.Duration(len(s.Values)) * s.Step) }

// Observed is the number of steps with data.
func (s Series) Observed() int {
	n := 0
	for _, v := range s.Values {
		if !math.IsNaN(v) {
			n++
		}
	}
	return n
}

// Head is the series without its last n steps.
func (s Series) Head(n int) Series {
	if n >= len(s.Values) {
		return Series{Start: s.Start, Step: s.Step}
	}
	return Series{Start: s.Start, Step: s.Step, Values: s.Values[:len(s.Values)-n]}
}

// Sample is one bucket read from the historian.
type Sample struct {
	T time.Time
	V float64
}

// Steps are the resolutions a forecast may use: round numbers, so the forecast
// lines up with the historian's aggregates.
var Steps = []time.Duration{
	time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour,
}

// ChooseStep picks the finest step that is not finer than the typical gap
// between measurements (the median interval between the given times, any
// order). A sensor read every second gets one minute; a data logger that sends
// every eight hours gets a day, unless it sends its history in between.
func ChooseStep(times []time.Time) time.Duration {
	if len(times) < 2 {
		return time.Hour
	}
	ts := append([]time.Time(nil), times...)
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	gaps := make([]time.Duration, 0, len(ts)-1)
	for i := 1; i < len(ts); i++ {
		if d := ts[i].Sub(ts[i-1]); d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) == 0 {
		return time.Hour
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	typical := gaps[len(gaps)/2]
	for _, s := range Steps {
		if s >= typical {
			return s
		}
	}
	return Steps[len(Steps)-1]
}

// Resample places samples on a regular grid of the given step ending at end
// (exclusive), at most maxLen steps long. Samples in the same step are
// averaged; steps without samples are NaN. Leading empty steps are dropped.
func Resample(samples []Sample, step time.Duration, end time.Time, maxLen int) Series {
	endStep := end.Truncate(step)
	if !end.Equal(endStep) {
		endStep = endStep.Add(step)
	}
	start := endStep.Add(-time.Duration(maxLen) * step)
	sum := make([]float64, maxLen)
	cnt := make([]int, maxLen)
	for _, s := range samples {
		if math.IsNaN(s.V) || math.IsInf(s.V, 0) || s.T.Before(start) || !s.T.Before(endStep) {
			continue
		}
		i := int(s.T.Sub(start) / step)
		sum[i] += s.V
		cnt[i]++
	}
	first := -1
	vals := make([]float64, maxLen)
	for i := range vals {
		if cnt[i] == 0 {
			vals[i] = math.NaN()
			continue
		}
		vals[i] = sum[i] / float64(cnt[i])
		if first < 0 {
			first = i
		}
	}
	if first < 0 {
		return Series{Start: endStep, Step: step}
	}
	return Series{Start: start.Add(time.Duration(first) * step), Step: step, Values: vals[first:]}
}

// fillForward replaces NaN with the last value seen (leading NaN with the first
// value). The statistical engine needs a gap-free series; the model does not.
func fillForward(v []float64) []float64 {
	out := make([]float64, len(v))
	last := math.NaN()
	for i, x := range v {
		if !math.IsNaN(x) {
			last = x
		}
		out[i] = last
	}
	first := math.NaN()
	for _, x := range out {
		if !math.IsNaN(x) {
			first = x
			break
		}
	}
	for i := range out {
		if !math.IsNaN(out[i]) {
			break
		}
		out[i] = first
	}
	return out
}

// quantile of a sorted slice, linear interpolation. NaN for an empty slice.
func quantile(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	if n == 1 {
		return sorted[0]
	}
	pos := q * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])
}

func finite(v []float64) []float64 {
	out := make([]float64, 0, len(v))
	for _, x := range v {
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			out = append(out, x)
		}
	}
	return out
}
