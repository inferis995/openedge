package scaling

import "math"

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
