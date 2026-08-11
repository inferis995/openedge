package scaling

import (
	"fmt"
	"math"
)

// Config holds per-tag EU conversion settings.
// Linear formula: eu = (raw − RawMin) / (RawMax − RawMin) × (EuMax − EuMin) + EuMin
// Boolean mode:   Invert flips true ↔ false; linear fields are ignored.
type Config struct {
	Enabled bool
	RawMin  float64
	RawMax  float64
	EuMin   float64
	EuMax   float64
	Clamp   bool
	Invert  bool
}

// Apply converts raw to engineering-unit value.
// Returns the value unchanged when scaling is disabled, the raw type is
// non-numeric (and not bool), or the raw span is zero.
func Apply(raw interface{}, cfg Config) interface{} {
	if !cfg.Enabled {
		return raw
	}

	// Boolean inversion — linear scaling makes no sense for BOOL.
	if v, ok := raw.(bool); ok {
		if cfg.Invert {
			return !v
		}
		return v
	}

	// Numeric linear scaling.
	r := toFloat64(raw)
	if r == nil {
		return raw
	}

	span := cfg.RawMax - cfg.RawMin
	if span == 0 {
		return raw
	}

	eu := (*r-cfg.RawMin)/span*(cfg.EuMax-cfg.EuMin) + cfg.EuMin

	if cfg.Clamp {
		lo := math.Min(cfg.EuMin, cfg.EuMax)
		hi := math.Max(cfg.EuMin, cfg.EuMax)
		if eu < lo {
			eu = lo
		} else if eu > hi {
			eu = hi
		}
	}

	return eu
}

// toFloat64 extracts a float64 from any JSON-decoded numeric type.
// JSON numbers decode as float64 by default; the other cases cover
// values written by drivers that use concrete Go types.
func toFloat64(v interface{}) *float64 {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int32:
		f = float64(x)
	case int64:
		f = float64(x)
	case uint:
		f = float64(x)
	case uint32:
		f = float64(x)
	case uint64:
		f = float64(x)
	default:
		return nil
	}
	return &f
}

// ErrOutOfRange reports a setpoint outside the tag's configured engineering
// range. It is returned rather than clamped on purpose: silently converting an
// operator's 500 into a 100 tells them the command was accepted and leaves them
// believing the plant is at 500. Refusing is the only outcome that cannot be
// misread.
type ErrOutOfRange struct {
	Value, Min, Max float64
}

func (e ErrOutOfRange) Error() string {
	return fmt.Sprintf("value %g is outside the engineering range %g..%g", e.Value, e.Min, e.Max)
}

// Reverse converts an engineering-unit value back to the raw value a device
// expects. It is the inverse of Apply and MUST be used on every write path.
//
// Why this exists. Apply is called on the read path, and its result replaces
// the raw value for Redis, the WebSocket broadcast, the historian and the tag
// shadow — so every number a synoptic displays is in engineering units. Writes
// went the other way untouched: the number an operator typed into a setpoint
// travelled through the API, the broker and the driver, and landed in the
// device register verbatim.
//
// On a tag scaled 0..27648 raw to 0..100 bar, that turned a 50 bar setpoint
// into 50 raw — about 0.18 bar. The operator sees the reading fall instead of
// rise, and with the scale running the other way the same bug commands a value
// far LARGER than the one requested, which is the case that hurts somebody.
//
// Returns the value unchanged when scaling is disabled, when the value is not
// numeric, or when the engineering span is zero and no conversion is defined.
func Reverse(eu interface{}, cfg Config) (interface{}, error) {
	if !cfg.Enabled {
		return eu, nil
	}

	// Boolean inversion is its own inverse.
	if v, ok := eu.(bool); ok {
		if cfg.Invert {
			return !v, nil
		}
		return v, nil
	}

	e := toFloat64(eu)
	if e == nil {
		return eu, nil
	}

	euSpan := cfg.EuMax - cfg.EuMin
	if euSpan == 0 {
		return eu, nil
	}

	// Refuse a setpoint the configured range cannot express. Checked in
	// engineering units, which is what the operator typed and what the error
	// message has to quote back to be of any use.
	lo := math.Min(cfg.EuMin, cfg.EuMax)
	hi := math.Max(cfg.EuMin, cfg.EuMax)
	if *e < lo || *e > hi {
		return nil, ErrOutOfRange{Value: *e, Min: lo, Max: hi}
	}

	raw := (*e-cfg.EuMin)/euSpan*(cfg.RawMax-cfg.RawMin) + cfg.RawMin
	return raw, nil
}
