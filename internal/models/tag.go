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
	AlarmEnabled      bool      `json:"alarm_enabled" db:"alarm_enabled"`
	AlarmThreshold    *float64  `json:"alarm_threshold,omitempty" db:"alarm_threshold"`
	AlarmOperator     *string   `json:"alarm_operator,omitempty" db:"alarm_operator"`
	AlarmPriority     int       `json:"alarm_priority" db:"alarm_priority"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
}

// Alarm represents an alarm state transition
type Alarm struct {
	ID             int        `json:"id" db:"id"`
	TagID          int        `json:"tag_id" db:"tag_id"`
	State          string     `json:"state" db:"state"`
	Message        string     `json:"message,omitempty" db:"message"`
	TriggeredAt    time.Time  `json:"triggered_at" db:"triggered_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty" db:"acknowledged_at"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty" db:"cleared_at"`
}
