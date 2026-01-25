package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// Gateway represents a PLC or device connection point
type Gateway struct {
	ID               int              `json:"id" db:"id"`
	AreaID           int              `json:"area_id" db:"area_id"`
	Name             string           `json:"name" db:"name"`
	DriverType       string           `json:"driver_type" db:"driver_type"`
	ConnectionConfig ConnectionConfig `json:"connection_config" db:"connection_config"`
	ScanRateMs       int              `json:"scan_rate_ms" db:"scan_rate_ms"`
	Enabled          bool             `json:"enabled" db:"enabled"`
	ZeroBased        bool             `json:"zero_based" db:"zero_based"`
	CreatedAt        time.Time        `json:"created_at" db:"created_at"`
}

// ConnectionConfig is a JSONB field storing driver-specific connection parameters
type ConnectionConfig map[string]interface{}

// Value implements the driver.Valuer interface for JSONB encoding
func (c ConnectionConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface for JSONB decoding
func (c *ConnectionConfig) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, &c)
}
