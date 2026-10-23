package forecast

import (
	"math"
	"time"
)

// Kind is what sort of signal a tag is. It decides what is forecast and how,
// so that one code path serves a pressure transmitter, a motor, an energy
// meter and a battery alike.
type Kind string

const (
	// KindAuto: let Classify decide from the data.
	KindAuto Kind = "auto"
	// KindProcess: an analog value that moves around a level (pressure,
	// temperature, flow, level).
	KindProcess Kind = "process"
	// KindCyclic: repeats with the day or the week (consumption, load).
	KindCyclic Kind = "cyclic"
	// KindDegradation: drifts steadily one way (battery, wear, filter Δp).
	KindDegradation Kind = "degradation"
	// KindCounter: a totalizer that only goes up, resets aside (kWh, pieces).
	KindCounter Kind = "counter"
	// KindState: on/off (running, door open). The forecast is the share of
	// time at 1.
	KindState Kind = "state"
)

// Valid reports whether k is a known kind, auto included.
func (k Kind) Valid() bool {
	switch k {
	case KindAuto, KindProcess, KindCyclic, KindDegradation, KindCounter, KindState:
		return true
	}
	return false
}

// Classify infers the kind of a series. isBool is true for a BOOL tag. period
// is the season length in steps for a cyclic signal (0 otherwise).
//
// The rules are deliberately few and conservative: a signal that fits none
// clearly is a process value, whose forecast makes the fewest assumptions.
func Classify(s Series, isBool bool) (kind Kind, period int) {
	v := s.Values
	obs := finite(v)
	if isBool || isBinary(obs) {
		return KindState, seasonFor(s)
	}
	if len(obs) < 8 {
		return KindProcess, 0
	}
	if isCounter(v) {
		return KindCounter, 0
	}
	if p := seasonFor(s); p > 0 {
		return KindCyclic, p
	}
	if isDrifting(v) {
		return KindDegradation, 0
	}
	return KindProcess, 0
}

func isBinary(obs []float64) bool {
	if len(obs) == 0 {
		return false
	}
	for _, x := range obs {
		if x != 0 && x != 1 {
			return false
		}
	}
	return true
}

// isCounter: almost never goes down (a reset now and then is allowed), goes up
// often enough not to be a constant, and ends above where it started.
func isCounter(v []float64) bool {
	var up, down, steps int
	prev := math.NaN()
	first, last := math.NaN(), math.NaN()
	for _, x := range v {
		if math.IsNaN(x) {
			continue
		}
		if math.IsNaN(first) {
			first = x
		}
		last = x
		if !math.IsNaN(prev) {
			steps++
			switch {
			case x > prev:
				up++
			case x < prev:
				down++
			}
		}
		prev = x
	}
	if steps < 8 {
		return false
	}
	return float64(down) <= 0.02*float64(steps) &&
		float64(up) >= 0.2*float64(steps) &&
		last > first
}

// seasonFor returns the day or the week, in steps, when the series repeats
// with it strongly enough (autocorrelation ≥ 0.5 over at least two seasons);
// 0 otherwise. The day wins when both do.
func seasonFor(s Series) int {
	if s.Step <= 0 {
		return 0
	}
	v := fillForward(s.Values)
	dt := detrend(v)
	// What a straight line leaves of a straight line is rounding error, whose
	// autocorrelation means nothing.
	if variance(dt) <= 1e-9*(variance(v)+1e-300) {
		return 0
	}
	for _, d := range []time.Duration{24 * time.Hour, 7 * 24 * time.Hour} {
		p := int(d / s.Step)
		if p < 2 || len(v) < 2*p {
			continue
		}
		if autocorr(dt, p) >= 0.5 {
			return p
		}
	}
	return 0
}

// isDrifting: a straight line explains most of the variance and the change it
// describes over the series is large next to the noise around it.
func isDrifting(v []float64) bool {
	slope, icept, ok := linearFit(v)
	if !ok {
		return false
	}
	var ssRes, ssTot, mean float64
	n := 0
	for _, x := range v {
		if !math.IsNaN(x) {
			mean += x
			n++
		}
	}
	mean /= float64(n)
	for i, x := range v {
		if math.IsNaN(x) {
			continue
		}
		r := x - (icept + slope*float64(i))
		ssRes += r * r
		ssTot += (x - mean) * (x - mean)
	}
	if ssTot == 0 {
		return false
	}
	r2 := 1 - ssRes/ssTot
	noise := math.Sqrt(ssRes / float64(n))
	change := math.Abs(slope * float64(len(v)))
	return r2 >= 0.6 && change >= 2*noise
}

// linearFit is ordinary least squares of value against step index, NaN
// skipped.
func linearFit(v []float64) (slope, icept float64, ok bool) {
	var sx, sy, sxx, sxy float64
	n := 0.0
	for i, x := range v {
		if math.IsNaN(x) {
			continue
		}
		fi := float64(i)
		sx += fi
		sy += x
		sxx += fi * fi
		sxy += fi * x
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, 0, false
	}
	slope = (n*sxy - sx*sy) / den
	icept = (sy - slope*sx) / n
	return slope, icept, true
}

func detrend(v []float64) []float64 {
	slope, icept, ok := linearFit(v)
	if !ok {
		return v
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x - (icept + slope*float64(i))
	}
	return out
}

func autocorr(v []float64, lag int) float64 {
	n := len(v)
	if lag <= 0 || lag >= n {
		return 0
	}
	var mean float64
	for _, x := range v {
		mean += x
	}
	mean /= float64(n)
	var num, den float64
	for i := 0; i < n; i++ {
		d := v[i] - mean
		den += d * d
		if i+lag < n {
			num += d * (v[i+lag] - mean)
		}
	}
	if den == 0 {
		return 0
	}
	return num / den
}

func variance(v []float64) float64 {
	var mean, n float64
	for _, x := range v {
		if !math.IsNaN(x) {
			mean += x
			n++
		}
	}
	if n == 0 {
		return 0
	}
	mean /= n
	var ss float64
	for _, x := range v {
		if !math.IsNaN(x) {
			ss += (x - mean) * (x - mean)
		}
	}
	return ss / n
}
