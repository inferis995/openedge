package sparkplug_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

// This platform runs two scales for the same idea, on purpose:
//
//	192 = GOOD   the OPC UA convention, what the alarm engine reads
//	  0 = GOOD   the payload and historian convention, 1 uncertain, 2 bad
//
// This function is the hinge between them, and it had no test. What sits
// immediately downstream of it is the first line of the historian's decision to
// keep a reading:
//
//	if newQuality > 0 { return true }
//
// So if this ever returned anything but 0 for a good reading, every sample
// would be written unconditionally: historize_deadband and report-by-exception
// would become dead code, the disk would fill at the full scan rate, and
// nothing anywhere would report an error.
func TestGoodBecomesZeroForTheHistorian(t *testing.T) {
	if got := sparkplug.ConvertSparkplugToLegacyQuality(192); got != 0 {
		t.Fatalf("a GOOD reading converted to %d, not 0. Downstream that is read as "+
			"\"bad quality, store unconditionally\", and the deadband stops existing", got)
	}
}

func TestTheThreeBandsConvert(t *testing.T) {
	cases := []struct {
		name string
		in   int32
		want int
	}{
		{"good, the OPC UA value", 192, 0},
		{"good, above the threshold", 255, 0},
		{"uncertain, at the threshold", 64, 1},
		{"uncertain, inside the band", 100, 1},
		{"bad, just under uncertain", 63, 2},
		{"bad, zero", 0, 2},
		{"bad, negative", -1, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sparkplug.ConvertSparkplugToLegacyQuality(c.in); got != c.want {
				t.Errorf("ConvertSparkplugToLegacyQuality(%d) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// The two thresholds are the whole function. Pinned separately because an
// off-by-one at 192 turns every good reading into an uncertain one — which is
// stored, so nothing breaks visibly, it just quietly stops honoring the
// deadband.
func TestTheThresholdsAreWhereTheyAreSaidToBe(t *testing.T) {
	if sparkplug.ConvertSparkplugToLegacyQuality(191) == 0 {
		t.Error("191 was treated as good; the OPC UA threshold is 192")
	}
	if sparkplug.ConvertSparkplugToLegacyQuality(192) != 0 {
		t.Error("192 was not treated as good")
	}
	if sparkplug.ConvertSparkplugToLegacyQuality(63) == 1 {
		t.Error("63 was treated as uncertain; the threshold is 64")
	}
	if sparkplug.ConvertSparkplugToLegacyQuality(64) != 1 {
		t.Error("64 was not treated as uncertain")
	}
}

// Whatever else changes, the two ends must never agree on a number: 192 means
// good on one scale and is meaningless on the other, and the day somebody
// "simplifies" this to a pass-through the historian starts storing everything.
func TestTheTwoScalesStayDistinct(t *testing.T) {
	if sparkplug.ConvertSparkplugToLegacyQuality(192) == 192 {
		t.Fatal("the conversion became a pass-through: 192 on the historian's scale is " +
			"read as bad quality, and every reading would be stored unconditionally")
	}
}
