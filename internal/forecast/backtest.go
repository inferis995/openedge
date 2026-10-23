package forecast

import "math"

// Score is how a forecast did on data that was already known: the end of the
// series was hidden, forecast from what came before, and compared.
type Score struct {
	// Skill is 1 − MAE(forecast) / MAE(naive), where the naive forecast repeats
	// the last known value. Above 0 the forecast beats it; 1 is perfect.
	Skill float64 `json:"skill"`
	// Coverage is the share of the true values inside the P10–P90 band. A
	// well-calibrated band holds about 0.8.
	Coverage float64 `json:"coverage"`
	// Points is how many values were compared.
	Points int `json:"points"`
	// Flat: neither forecast made any error (a constant signal). Skill is 0
	// then, and the forecast is still trustworthy.
	Flat bool `json:"flat,omitempty"`
}

// Reliable is the bar a forecast must clear to be shown as one: beat the naive
// forecast, or be exact on a signal that does not move.
func (s Score) Reliable() bool { return s.Points > 0 && (s.Skill > 0 || s.Flat) }

// HoldoutSteps is how much of a series to hide for the backtest: as long as
// the horizon, but never more than a quarter of what is known, so that the
// forecast in the test sees most of the history the real one does.
func HoldoutSteps(s Series, horizon int) int {
	h := horizon
	if q := s.Observed() / 4; q < h {
		h = q
	}
	if h < 1 {
		return 0
	}
	return h
}

// Evaluate scores a forecast made from s.Head(h) against the last h values of
// s. Steps without a true value or a forecast are skipped.
func Evaluate(s Series, f *Forecast, h int) Score {
	n := len(s.Values)
	if h <= 0 || h > n {
		return Score{}
	}
	head := fillForward(s.Values[:n-h])
	if len(head) == 0 || math.IsNaN(head[len(head)-1]) {
		return Score{}
	}
	naive := head[len(head)-1]
	var errF, errN float64
	var in, pts int
	for i := 0; i < h && i < len(f.Q50); i++ {
		y := s.Values[n-h+i]
		if math.IsNaN(y) || math.IsNaN(f.Q50[i]) {
			continue
		}
		errF += math.Abs(y - f.Q50[i])
		errN += math.Abs(y - naive)
		if y >= f.Q10[i] && y <= f.Q90[i] {
			in++
		}
		pts++
	}
	if pts == 0 {
		return Score{}
	}
	sc := Score{Points: pts, Coverage: float64(in) / float64(pts)}
	const eps = 1e-12
	switch {
	case errN < eps && errF < eps:
		sc.Flat = true
	case errN < eps:
		sc.Skill = -1
	default:
		sc.Skill = 1 - errF/errN
	}
	return sc
}
