package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// SystemHandler handles system-level HTTP requests
type SystemHandler struct {
	db         *sql.DB
	mqttClient MQTTClient // Use same interface as GatewaysHandler
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(db *sql.DB, mqttClient MQTTClient) *SystemHandler {
	return &SystemHandler{
		db:         db,
		mqttClient: mqttClient,
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
