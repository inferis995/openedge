package models

import "time"

// AlarmDefinition represents a configured alarm rule for a tag
type AlarmDefinition struct {
	ID           int       `json:"id" db:"id"`
	TagID        int       `json:"tag_id" db:"tag_id"`
	AlarmType    string    `json:"alarm_type" db:"alarm_type"`       // 'bool_true', 'bool_false', 'high', 'low', 'high_high', 'low_low'
	Threshold    *float64  `json:"threshold" db:"threshold"`         // Nil for boolean
	Deadband     float64   `json:"deadband" db:"deadband"`           // To prevent chattering
	DelaySeconds int       `json:"delay_seconds" db:"delay_seconds"` // Wait seconds before trigger
	Severity     string    `json:"severity" db:"severity"`           // 'info', 'warning', 'critical'
	Message      string    `json:"message" db:"message"`             // Custom message
	Enabled      bool      `json:"enabled" db:"enabled"`             // Is the rule active?
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// AlarmEvent represents an active or historical alarm instance
type AlarmEvent struct {
	ID             int        `json:"id" db:"id"`
	TagID          int        `json:"tag_id" db:"tag_id"`
	DefinitionID   *int       `json:"definition_id,omitempty" db:"definition_id"` // Nil if def was deleted
	Status         string     `json:"status" db:"status"`                          // 'ACTIVE', 'ACKNOWLEDGED', 'CLEARED'
	AlarmType      string     `json:"alarm_type" db:"alarm_type"`
	Severity       string     `json:"severity" db:"severity"`
	Message        string     `json:"message" db:"message"`
	ValueAtTrigger *float64   `json:"value_at_trigger,omitempty" db:"value_at_trigger"`
	TriggerTime    time.Time  `json:"trigger_time" db:"trigger_time"`
	ClearTime      *time.Time `json:"clear_time,omitempty" db:"clear_time"` // Nil if still active
	BgAckUser      *string    `json:"bg_ack_user,omitempty" db:"bg_ack_user"`
	AckTime        *time.Time `json:"ack_time,omitempty" db:"ack_time"` // Nil if unacknowledged
}
