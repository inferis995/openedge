package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
)

// SystemHandler handles system-level HTTP requests
type SystemHandler struct {
	db             *sql.DB
	mqttClient     MQTTClient // Use same interface as GatewaysHandler
	settingsMgr    *settings.Manager
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(db *sql.DB, mqttClient MQTTClient, settingsMgr *settings.Manager) *SystemHandler {
	return &SystemHandler{
		db:         db,
		mqttClient: mqttClient,
		settingsMgr: settingsMgr,
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to publish reload command: %v", err)})
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
			settings[key] = value
		}
	}

	c.JSON(http.StatusOK, settings)
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

	// Update settings in database
	if req.PublishMode != nil {
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'publish_mode'", *req.PublishMode)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update publish_mode"})
			return
		}
	}

	if req.RBEHeartbeatSeconds != nil {
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'rbe_heartbeat_seconds'", *req.RBEHeartbeatSeconds)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update rbe_heartbeat_seconds"})
			return
		}
	}

	if req.RBEDeadbandPercent != nil {
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'rbe_deadband_percent'", *req.RBEDeadbandPercent)
		if err != nil {
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
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'stale_threshold_seconds'", *req.StaleThresholdSeconds)
		if err != nil {
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
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'mqtt_broker_mode'", *req.MQTTBrokerMode)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_broker_mode"})
			return
		}
	}

	// Handle MQTT external host setting
	if req.MQTTExternalHost != nil {
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'mqtt_external_host'", *req.MQTTExternalHost)
		if err != nil {
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
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'mqtt_external_port'", *req.MQTTExternalPort)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_external_port"})
			return
		}
	}

	// Handle MQTT username setting
	if req.MQTTUsername != nil {
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'mqtt_username'", *req.MQTTUsername)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_username"})
			return
		}
	}

	// Handle MQTT password setting
	if req.MQTTPassword != nil {
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'mqtt_password'", *req.MQTTPassword)
		if err != nil {
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
		_, err := h.db.Exec("UPDATE global_settings SET value = $1, updated_at = NOW() WHERE key = 'mqtt_client_id'", *req.MQTTClientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mqtt_client_id"})
			return
		}
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
