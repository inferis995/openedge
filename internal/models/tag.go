package models

import "time"

// Tag represents a data point to read from a gateway
type Tag struct {
	ID                int     `json:"id" db:"id"`
	GatewayID         int     `json:"gateway_id" db:"gateway_id"`
	Code              string  `json:"code" db:"code"`
	Alias             string  `json:"alias" db:"alias"`
	DataType          string  `json:"data_type" db:"data_type"`
	Historize         bool    `json:"historize" db:"historize"`
	HistorizeDeadband float64 `json:"historize_deadband" db:"historize_deadband"`
	SortOrder         float64 `json:"sort_order" db:"sort_order"`
	// JsonPath: for MQTT tags whose payload is JSON, extract this dotted path.
	// Empty/NULL = whole payload is the value.
	JsonPath *string `json:"json_path,omitempty" db:"json_path"`

	// EU Scaling — raw-to-engineering-unit conversion applied at ingestion.
	// When ScalingEnabled=true the stored/broadcast value is already in EU.
	// Linear formula: eu = (raw − RawMin) / (RawMax − RawMin) × (EuMax − EuMin) + EuMin
	// For BOOL tags only Invert is relevant.
	ScalingEnabled bool    `json:"scaling_enabled" db:"scaling_enabled"`
	ScalingRawMin  float64 `json:"scaling_raw_min" db:"scaling_raw_min"`
	ScalingRawMax  float64 `json:"scaling_raw_max" db:"scaling_raw_max"`
	ScalingEuMin   float64 `json:"scaling_eu_min" db:"scaling_eu_min"`
	ScalingEuMax   float64 `json:"scaling_eu_max" db:"scaling_eu_max"`
	ScalingClamp   bool    `json:"scaling_clamp" db:"scaling_clamp"`
	EuUnit         string  `json:"eu_unit" db:"eu_unit"`
	EuDecimals     int     `json:"eu_decimals" db:"eu_decimals"`
	Invert         bool    `json:"invert" db:"invert"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
