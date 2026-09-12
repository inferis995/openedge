package scaling

import "github.com/ralph/industrial-edge-middleware/internal/models"

// FromTag builds a tag's engineering-unit conversion from its row.
//
// It exists so the mapping is written once. Seven fields have to be carried
// across, and every way of getting it wrong is quiet: forget Enabled and the
// tag stays in raw counts; forget RawMax and the span is zero, which Apply
// treats as "no conversion"; forget Clamp and a transmitter reading slightly
// out of range produces an out-of-range engineering value. None of those
// raise anything — they produce a number that looks like a reading.
func FromTag(t *models.Tag) Config {
	return Config{
		Enabled: t.ScalingEnabled,
		RawMin:  t.ScalingRawMin,
		RawMax:  t.ScalingRawMax,
		EuMin:   t.ScalingEuMin,
		EuMax:   t.ScalingEuMax,
		Clamp:   t.ScalingClamp,
		Invert:  t.Invert,
	}
}
