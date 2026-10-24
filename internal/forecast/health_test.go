package forecast

import (
	"math"
	"testing"
	"time"
)

func TestAssetHealth(t *testing.T) {
	now := t0
	ok := func(id int, risks []Risk, a *Anomaly) Member {
		return Member{TagID: id, Alias: "t", Status: StatusOK, Risks: risks, Anomaly: a}
	}
	t.Run("nothing known is not healthy", func(t *testing.T) {
		h := AssetHealth([]Member{{TagID: 1, Status: StatusLearning}}, now)
		if h.Score != nil || h.Level != "unknown" || h.Known != 0 || h.Members != 1 {
			t.Errorf("%+v", h)
		}
	})
	t.Run("all fine", func(t *testing.T) {
		h := AssetHealth([]Member{ok(1, nil, &Anomaly{Checked: 4})}, now)
		if h.Score == nil || *h.Score != 100 || h.Level != "good" || len(h.Issues) != 0 {
			t.Errorf("%+v", h)
		}
	})
	t.Run("the weakest part decides", func(t *testing.T) {
		h := AssetHealth([]Member{
			ok(1, []Risk{{Likelihood: Possible}}, nil),
			ok(2, []Risk{{Likelihood: Likely}}, &Anomaly{Checked: 4, Outside: 4}),
		}, now)
		// Tag 2: likely (30) + every value off (35) = 65 → 35.
		if h.Score == nil || *h.Score != 35 || h.Level != "critical" {
			t.Fatalf("%+v", h)
		}
		if len(h.Issues) != 3 || h.Issues[0].Kind != "anomaly" || h.Issues[0].TagID != 2 {
			t.Errorf("issues worst first: %+v", h.Issues)
		}
	})
	t.Run("a drift counts less the further it is", func(t *testing.T) {
		near := AssetHealth([]Member{ok(1, []Risk{{Likelihood: Trend, At: now.Add(3 * 24 * time.Hour)}}, nil)}, now)
		far := AssetHealth([]Member{ok(1, []Risk{{Likelihood: Trend, At: now.Add(20 * 24 * time.Hour)}}, nil)}, now)
		later := AssetHealth([]Member{ok(1, []Risk{{Likelihood: Trend, At: now.Add(200 * 24 * time.Hour)}}, nil)}, now)
		if *near.Score != 80 || *far.Score != 92 || *later.Score != 100 {
			t.Errorf("near %d far %d later %d", *near.Score, *far.Score, *later.Score)
		}
	})
}

func TestMaskStopped(t *testing.T) {
	s := hourly([]float64{50, 51, 20, 21, 52})
	state := hourly([]float64{1, 1, 0, 0, math.NaN()})
	if n := MaskStopped(&s, state); n != 2 {
		t.Fatalf("hid %d steps, want 2", n)
	}
	if !math.IsNaN(s.Values[2]) || !math.IsNaN(s.Values[3]) || s.Values[4] != 52 || s.Values[0] != 50 {
		t.Errorf("values %v: stopped hidden, unknown state kept", s.Values)
	}
	other := Series{Start: t0, Step: time.Minute, Values: []float64{0}}
	if MaskStopped(&s, other) != 0 {
		t.Error("a state at another step must not mask")
	}
}
