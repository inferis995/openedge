package models

import "time"

// PublishMode represents the MQTT publish mode
type PublishMode string

const (
	PublishModeDual          PublishMode = "dual"
	PublishModeSparkplugOnly PublishMode = "sparkplug_only"
	PublishModeLegacyOnly    PublishMode = "legacy_only"
)

// ValidPublishModes contains all valid publish modes
var ValidPublishModes = []PublishMode{
	PublishModeDual,
	PublishModeSparkplugOnly,
	PublishModeLegacyOnly,
}

// IsValid checks if the publish mode is valid
func (p PublishMode) IsValid() bool {
	for _, mode := range ValidPublishModes {
		if p == mode {
			return true
		}
	}
	return false
}

// MQTTBrokerMode represents the MQTT broker selection mode
type MQTTBrokerMode string

const (
	MQTTBrokerModeInternal MQTTBrokerMode = "internal" // Use embedded Mosquitto
	MQTTBrokerModeExternal MQTTBrokerMode = "external" // Use custom external broker
)

// IsValid checks if the broker mode is valid
func (m MQTTBrokerMode) IsValid() bool {
	return m == MQTTBrokerModeInternal || m == MQTTBrokerModeExternal
}

// GlobalSettings represents the global system settings
type GlobalSettings struct {
	PublishMode           PublishMode    `json:"publish_mode"`
	RBEHeartbeatSeconds   int            `json:"rbe_heartbeat_seconds"`
	RBEDeadbandPercent    float64        `json:"rbe_deadband_percent"`
	StaleThresholdSeconds int            `json:"stale_threshold_seconds"`
	MQTTBrokerMode        MQTTBrokerMode `json:"mqtt_broker_mode"`
	MQTTExternalHost      string         `json:"mqtt_external_host"`
	MQTTExternalPort      int            `json:"mqtt_external_port"`
	MQTTUsername          string         `json:"mqtt_username"`
	MQTTPassword          string         `json:"mqtt_password"` // Note: stored as-is, consider encryption in production
	MQTTClientID          string         `json:"mqtt_client_id"`
}

// GlobalSetting represents a single setting row in the database
type GlobalSetting struct {
	Key         string     `json:"key" db:"key"`
	Value       string     `json:"value" db:"value"`
	Description string     `json:"description" db:"description"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	UpdatedBy   *int       `json:"updated_by,omitempty" db:"updated_by"`
}

// PublishMetrics represents statistics about MQTT publishing
type PublishMetrics struct {
	Published    int     `json:"published"`
	Skipped      int     `json:"skipped"`
	SavedPercent float64 `json:"saved_percent"`
}

// SettingsUpdateRequest represents the request body for updating settings
type SettingsUpdateRequest struct {
	PublishMode           *string  `json:"publish_mode,omitempty"`
	RBEHeartbeatSeconds   *int     `json:"rbe_heartbeat_seconds,omitempty"`
	RBEDeadbandPercent    *float64 `json:"rbe_deadband_percent,omitempty"`
	StaleThresholdSeconds *int     `json:"stale_threshold_seconds,omitempty"`
	MQTTBrokerMode        *string  `json:"mqtt_broker_mode,omitempty"`
	MQTTExternalHost      *string  `json:"mqtt_external_host,omitempty"`
	MQTTExternalPort      *int     `json:"mqtt_external_port,omitempty"`
	MQTTUsername          *string  `json:"mqtt_username,omitempty"`
	MQTTPassword          *string  `json:"mqtt_password,omitempty"`
	MQTTClientID          *string  `json:"mqtt_client_id,omitempty"`
}
