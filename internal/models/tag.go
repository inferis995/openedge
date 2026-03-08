package models

import "time"

// Tag represents a data point to read from a gateway
type Tag struct {
	ID                int       `json:"id" db:"id"`
	GatewayID         int       `json:"gateway_id" db:"gateway_id"`
	Code              string    `json:"code" db:"code"`
	Alias             string    `json:"alias" db:"alias"`
	DataType          string    `json:"data_type" db:"data_type"`
	Historize         bool      `json:"historize" db:"historize"`
	HistorizeDeadband float64   `json:"historize_deadband" db:"historize_deadband"`
	SortOrder         float64   `json:"sort_order" db:"sort_order"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
}
