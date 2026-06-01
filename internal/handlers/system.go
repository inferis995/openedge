package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/crypto"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
)

// upsertSetting inserts or updates a setting in global_settings table
func (h *SystemHandler) upsertSetting(key, value string) error {
	// Auto-encrypt passwords before saving
	if strings.Contains(key, "password") {
		encrypted, err := crypto.Encrypt(value)
		if err != nil {
			log.Printf("[WARN] Failed to encrypt password setting '%s': %v - storing as plaintext", key, err)
		} else {
			value = encrypted
		}
	}

	_, err := h.db.Exec(`
		INSERT INTO global_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
	`, key, value)
	return err
}

// SystemHandler handles system-level HTTP requests
type SystemHandler struct {
	db             *sql.DB
	mqttClient     MQTTClient // Use same interface as GatewaysHandler
	settingsMgr    *settings.Manager
	historyHandler *HistoryHandler
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(db *sql.DB, mqttClient MQTTClient, settingsMgr *settings.Manager, historyHandler *HistoryHandler) *SystemHandler {
	return &SystemHandler{
		db:             db,
		mqttClient:     mqttClient,
		settingsMgr:    settingsMgr,
		historyHandler: historyHandler,
	}
}

// Reload publishes a configuration reload command to all drivers via MQTT
func (h *SystemHandler) Reload(c *gin.Context) {
	if h.mqttClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MQTT client not available"})
		return
	}

	payload := map[string]interface{}{
		"command":   "reload",
		"timestamp": time.Now().Unix(),
	}

	payloadBytes, _ := json.Marshal(payload)

	// Publish to system command topic
	if err := h.mqttClient.Publish("sys/command/reload", string(payloadBytes)); err != nil {
		log.Printf("[SYSTEM] Failed to publish reload command: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to publish reload command"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Reload command published"})
}

// ConfigExport represents the exported configuration
type ConfigExport struct {
	Organizations []models.Organization `json:"organizations"`
	Sites         []models.Site         `json:"sites"`
	Areas         []models.Area         `json:"areas"`
	Gateways      []models.Gateway      `json:"gateways"`
	Tags          []models.Tag          `json:"tags"`
}

// ExportConfig dumps the entire database configuration to JSON
func (h *SystemHandler) ExportConfig(c *gin.Context) {
	var config ConfigExport

	// Fetch organizations
	rows, err := h.db.Query("SELECT id, name, created_at FROM organizations ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch organizations"})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var org models.Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.CreatedAt); err == nil {
			config.Organizations = append(config.Organizations, org)
		}
	}

	// Fetch sites
	rows2, err := h.db.Query("SELECT id, org_id, name, created_at FROM sites ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch sites"})
		return
	}
	defer rows2.Close()
	for rows2.Next() {
		var site models.Site
		if err := rows2.Scan(&site.ID, &site.OrgID, &site.Name, &site.CreatedAt); err == nil {
			config.Sites = append(config.Sites, site)
		}
	}

	// Fetch areas
	rows3, err := h.db.Query("SELECT id, site_id, name, created_at FROM areas ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch areas"})
		return
	}
	defer rows3.Close()
	for rows3.Next() {
		var area models.Area
		if err := rows3.Scan(&area.ID, &area.SiteID, &area.Name, &area.CreatedAt); err == nil {
			config.Areas = append(config.Areas, area)
		}
	}

	// Fetch gateways
	rows4, err := h.db.Query("SELECT id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, created_at FROM gateways ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch gateways"})
		return
	}
	defer rows4.Close()
	for rows4.Next() {
		var gw models.Gateway
		if err := rows4.Scan(&gw.ID, &gw.AreaID, &gw.Name, &gw.DriverType, &gw.ConnectionConfig, &gw.ScanRateMs, &gw.Enabled, &gw.CreatedAt); err == nil {
			config.Gateways = append(config.Gateways, gw)
		}
	}

	// Fetch tags (using correct column names from model)
	rows5, err := h.db.Query("SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband, created_at FROM tags ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags"})
		return
	}
	defer rows5.Close()
	for rows5.Next() {
		var tag models.Tag
		if err := rows5.Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize, &tag.HistorizeDeadband, &tag.CreatedAt); err == nil {
			config.Tags = append(config.Tags, tag)
		}
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=edge-config-%s.json", time.Now().Format("2006-01-02")))
	c.JSON(http.StatusOK, config)
}

// ImportConfig restores configuration from JSON (simplified - just returns message)
func (h *SystemHandler) ImportConfig(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer f.Close()

	var config ConfigExport
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	// For safety, we just validate the file format without actually importing
	// A full import would require careful handling of foreign keys
	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration file parsed successfully",
		"counts": gin.H{
			"organizations": len(config.Organizations),
			"sites":         len(config.Sites),
			"areas":         len(config.Areas),
			"gateways":      len(config.Gateways),
			"tags":          len(config.Tags),
		},
	})
}

// GetSettings returns all global settings
func (h *SystemHandler) GetSettings(c *gin.Context) {
	settings := make(map[string]interface{})

	rows, err := h.db.Query("SELECT key, value FROM global_settings")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err == nil {
			// Never return secrets over the wire. Mask anything that looks
			// like a credential (password / token / api_key / secret /
			// private). Empty values on the client mean "leave the stored
			// value unchanged" in UpdateSettings.
			if isSecretKey(key) {
				value = ""
			}
			settings[key] = value
		}
	}

	c.JSON(http.StatusOK, settings)
}

// isSecretKey identifies settings whose values must never leave the server
// in cleartext via GET /settings. The masking is keyword-based on the
// setting NAME (we don't introspect values) so the rule is auditable in
// one place. The UI sends back an empty string when the operator hasn't
// edited the field, and the UPDATE skips empty secret values so the
// stored credential is preserved.
func isSecretKey(key string) bool {
	for _, marker := range []string{"password", "token", "secret", "private", "api_key"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

// UpdateSettingsRequest represents the request body for updating settings
type UpdateSettingsRequest struct {
	PublishMode           *string  `json:"publish_mode"`
	RBEHeartbeatSeconds   *int     `json:"rbe_heartbeat_seconds"`
	RBEDeadbandPercent    *float64 `json:"rbe_deadband_percent"`
	StaleThresholdSeconds *int     `json:"stale_threshold_seconds"`
	MQTTBrokerMode        *string  `json:"mqtt_broker_mode"`
	MQTTExternalHost      *string  `json:"mqtt_external_host"`
	MQTTExternalPort      *int     `json:"mqtt_external_port"`
	MQTTUsername          *string  `json:"mqtt_username"`
	MQTTPassword          *string  `json:"mqtt_password"`
	MQTTClientID          *string  `json:"mqtt_client_id"`
	DBRetentionDays       *int     `json:"db_retention_days"`
	CloudSyncEnabled      *bool    `json:"cloud_sync_enabled"`
	CloudMqttHost         *string  `json:"cloud_mqtt_host"`
	CloudMqttPort         *int     `json:"cloud_mqtt_port"`
	CloudMqttUsername     *string  `json:"cloud_mqtt_username"`
	CloudMqttPassword     *string  `json:"cloud_mqtt_password"`
	CloudMqttTopic        *string  `json:"cloud_mqtt_topic"`
	// Notification channel config — flat key→value map of notif_* keys.
	// Validated to only allow that prefix server-side. Lets the UI add
	// new channels without bumping the API schema every time.
	Notifications map[string]string `json:"notifications,omitempty"`
	// Backup schedule + retention + encryption — same flat-passthrough
	// pattern as Notifications. Keys must start with backup_.
	Backup map[string]string `json:"backup,omitempty"`
}

// applyPrefixedSettings is the shared upsert loop for flat-passthrough
// setting groups (notifications, backup, …). It enforces a key prefix,
// preserves secrets when the incoming value is empty, and surfaces any
// DB error as a 500 to the caller. Returns a sentinel error so the
// caller knows the response has already been written and stops.
func (h *SystemHandler) applyPrefixedSettings(c *gin.Context, prefix string, in map[string]string) error {
	for key, val := range in {
		if !strings.HasPrefix(key, prefix) {
			c.JSON(http.StatusBadRequest, gin.H{"error": prefix + "* keys required (got: " + key + ")"})
			return fmt.Errorf("rejected")
		}
		if isSecretKey(key) && val == "" {
			continue
		}
		if err := h.upsertSetting(key, val); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update " + key})
			return err
		}
	}
	return nil
}

// UpdateSettings updates global settings (admin only)
func (h *SystemHandler) UpdateSettings(c *gin.Context) {
	var req UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate publish_mode if provided
	if req.PublishMode != nil {
		mode := models.PublishMode(*req.PublishMode)
		if !mode.IsValid() {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid publish_mode. Must be one of: dual, sparkplug_only, legacy_only",
			})
			return
		}
	}

	// Update settings in database using UPSERT (insert or update)
	if req.PublishMode != nil {
		if err := h.upsertSetting("publish_mode", *req.PublishMode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update publish_mode"})
			return
		}
	}

	if req.RBEHeartbeatSeconds != nil {
		if err := h.upsertSetting("rbe_heartbeat_seconds", fmt.Sprintf("%d", *req.RBEHeartbeatSeconds)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update rbe_heartbeat_seconds"})
			return
		}
	}

	if req.RBEDeadbandPercent != nil {
		if err := h.upsertSetting("rbe_deadband_percent", fmt.Sprintf("%v", *req.RBEDeadbandPercent)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update rbe_deadband_percent"})
			return
		}
	}

	if req.StaleThresholdSeconds != nil {
		// Validate: must be positive and reasonable (1 second to 1 hour)
		if *req.StaleThresholdSeconds < 1 || *req.StaleThresholdSeconds > 3600 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "stale_threshold_seconds must be between 1 and 3600"})
			return
		}
		if err := h.upsertSetting("stale_threshold_seconds", fmt.Sprintf("%d", *req.StaleThresholdSeconds)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stale_threshold_seconds"})
			return
		}
	}

	// Handle MQTT broker mode setting
	if req.MQTTBrokerMode != nil {
		mode := models.MQTTBrokerMode(*req.MQTTBrokerMode)
		if !mode.IsValid() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid mqtt_broker_mode. Must be 'internal' or 'external'"})
			return
		}
		if err := h.upsertSetting("mqtt_broker_mode", *req.MQTTBrokerMode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_broker_mode"})
			return
		}
	}

	// Handle MQTT external host setting
	if req.MQTTExternalHost != nil {
		if err := h.upsertSetting("mqtt_external_host", *req.MQTTExternalHost); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_external_host"})
			return
		}
	}

	// Handle MQTT external port setting
	if req.MQTTExternalPort != nil {
		// Validate port range
		if *req.MQTTExternalPort < 1 || *req.MQTTExternalPort > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mqtt_external_port must be between 1 and 65535"})
			return
		}
		if err := h.upsertSetting("mqtt_external_port", fmt.Sprintf("%d", *req.MQTTExternalPort)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_external_port"})
			return
		}
	}

	// Handle MQTT username setting
	if req.MQTTUsername != nil {
		if err := h.upsertSetting("mqtt_username", *req.MQTTUsername); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_username"})
			return
		}
	}

	// Handle MQTT password setting
	// Empty password = "leave unchanged" (the UI never sees the current secret,
	// so a blank field must not overwrite the stored value).
	if req.MQTTPassword != nil && *req.MQTTPassword != "" {
		if err := h.upsertSetting("mqtt_password", *req.MQTTPassword); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_password"})
			return
		}
	}

	// Handle MQTT client ID setting
	if req.MQTTClientID != nil {
		// Validate client ID length (MQTT spec allows 1-23 chars, but we allow more for flexibility)
		if len(*req.MQTTClientID) > 127 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mqtt_client_id must be 127 characters or less"})
			return
		}
		if err := h.upsertSetting("mqtt_client_id", *req.MQTTClientID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_client_id"})
			return
		}
	}

	// Handle DB Retention Days setting
	if req.DBRetentionDays != nil {
		if *req.DBRetentionDays < 0 || *req.DBRetentionDays > 3650 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "db_retention_days must be between 0 and 3650"})
			return
		}
		if err := h.upsertSetting("db_retention_days", fmt.Sprintf("%d", *req.DBRetentionDays)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update db_retention_days"})
			return
		}

		// Apply the new retention policy immediately
		if h.historyHandler != nil {
			go h.historyHandler.InitializeRetentionPolicy()
		}
	}

	// Handle Cloud Sync Enabled
	if req.CloudSyncEnabled != nil {
		val := "false"
		if *req.CloudSyncEnabled {
			val = "true"
		}
		if err := h.upsertSetting("cloud_sync_enabled", val); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cloud_sync_enabled"})
			return
		}
	}

	// Handle Cloud MQTT Host
	if req.CloudMqttHost != nil {
		if err := h.upsertSetting("cloud_mqtt_host", *req.CloudMqttHost); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cloud_mqtt_host"})
			return
		}
	}

	// Handle Cloud MQTT Port
	if req.CloudMqttPort != nil {
		if *req.CloudMqttPort < 1 || *req.CloudMqttPort > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cloud_mqtt_port must be between 1 and 65535"})
			return
		}
		if err := h.upsertSetting("cloud_mqtt_port", fmt.Sprintf("%d", *req.CloudMqttPort)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cloud_mqtt_port"})
			return
		}
	}

	// Handle Cloud MQTT Username
	if req.CloudMqttUsername != nil {
		if err := h.upsertSetting("cloud_mqtt_username", *req.CloudMqttUsername); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cloud_mqtt_username"})
			return
		}
	}

	// Handle Cloud MQTT Password
	// Empty cloud password = "leave unchanged" (same logic as MQTTPassword).
	if req.CloudMqttPassword != nil && *req.CloudMqttPassword != "" {
		if err := h.upsertSetting("cloud_mqtt_password", *req.CloudMqttPassword); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cloud_mqtt_password"})
			return
		}
	}

	// Handle Cloud MQTT Topic
	if req.CloudMqttTopic != nil {
		if err := h.upsertSetting("cloud_mqtt_topic", *req.CloudMqttTopic); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cloud_mqtt_topic"})
			return
		}
	}

	// Notification + backup settings are flat passthroughs: a map of
	// key→value where the key must start with the expected prefix. This
	// keeps the API surface stable when new sub-settings are added.
	// Secret keys (token, password, ...) preserve the stored value when
	// the incoming string is empty, matching GetSettings which blanks
	// them on read.
	if err := h.applyPrefixedSettings(c, "notif_", req.Notifications); err != nil {
		return
	}
	if err := h.applyPrefixedSettings(c, "backup_", req.Backup); err != nil {
		return
	}

	// Publish settings-reload command to MQTT
	if h.mqttClient != nil {
		payload := map[string]interface{}{
			"command":   "settings-reload",
			"timestamp": time.Now().Unix(),
		}
		payloadBytes, _ := json.Marshal(payload)
		h.mqttClient.Publish("sys/command/settings-reload", string(payloadBytes))
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}

// GetMetrics returns publish metrics
func (h *SystemHandler) GetMetrics(c *gin.Context) {
	// Get metrics from settings manager if available
	if h.settingsMgr != nil {
		metrics := h.settingsMgr.GetMetrics()
		c.JSON(http.StatusOK, metrics)
		return
	}

	// Fallback: return placeholder metrics
	metrics := models.PublishMetrics{
		Published:    0,
		Skipped:      0,
		SavedPercent: 0,
	}
	c.JSON(http.StatusOK, metrics)
}
