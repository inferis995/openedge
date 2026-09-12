package scaling_test

import (
	"math"
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/scaling"
)

// Each field is given a value nothing else uses, so a mapping that crosses two
// of them over — or drops one — cannot pass.
func TestEveryScalingFieldReachesTheConfig(t *testing.T) {
	got := scaling.FromTag(&models.Tag{
		ScalingEnabled: true,
		ScalingRawMin:  1,
		ScalingRawMax:  2,
		ScalingEuMin:   3,
		ScalingEuMax:   4,
		ScalingClamp:   true,
		Invert:         true,
	})
	want := scaling.Config{Enabled: true, RawMin: 1, RawMax: 2, EuMin: 3, EuMax: 4, Clamp: true, Invert: true}
	if got != want {
		t.Fatalf("FromTag mapped the row to %+v, want %+v", got, want)
	}
}

// A tag with scaling switched off must convert nothing at all. This is the
// common case — the column defaults to false — and getting it wrong would
// rescale every plant that works in raw counts.
func TestATagWithoutScalingConvertsNothing(t *testing.T) {
	cfg := scaling.FromTag(&models.Tag{ScalingRawMin: 0, ScalingRawMax: 27648, ScalingEuMax: 100})
	if cfg.Enabled {
		t.Fatal("a tag with scaling_enabled = false produced an enabled config")
	}
	if got := scaling.Apply(13824.0, cfg); got != 13824.0 {
		t.Fatalf("Apply changed %v to %v with scaling disabled", 13824.0, got)
	}
}

// The whole point of moving the conversion to the driver is that it then
// happens exactly once. This pins what happens if it ever happens twice,
// because the result is not obviously wrong on a screen: a pressure reading
// of 0.36 bar where the plant is at 100 is a plausible number, and nothing
// anywhere reports an error.
func TestConvertingTwiceIsCatastrophicAndSilent(t *testing.T) {
	cfg := scaling.FromTag(&models.Tag{
		ScalingEnabled: true,
		ScalingRawMin:  0, ScalingRawMax: 27648,
		ScalingEuMin: 0, ScalingEuMax: 100,
	})

	once := scaling.Apply(27648.0, cfg) // full scale
	if math.Abs(once.(float64)-100) > 1e-9 {
		t.Fatalf("one conversion of full scale gave %v, want 100", once)
	}

	twice := scaling.Apply(once, cfg)
	if math.Abs(twice.(float64)-100) < 1 {
		t.Fatal("converting twice returned roughly the right answer, so this test " +
			"can no longer detect a double conversion")
	}
	t.Logf("full scale converted twice reads %v bar instead of 100 — a number an "+
		"operator has no reason to question", twice)
}
