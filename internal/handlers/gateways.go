package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	opcuaclient "github.com/ralph/industrial-edge-middleware/internal/opcua"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

// MQTTClient interface for publishing reload commands
type MQTTClient interface {
	Publish(topic string, payload interface{}) error
	PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error
}

// GatewaysHandler handles gateway-related HTTP requests
type GatewaysHandler struct {
	db          *sql.DB
	mqttClient  MQTTClient
	redisClient RedisClient
}

// NewGatewaysHandler creates a new gateways handler
func NewGatewaysHandler(db *sql.DB, mqttClient MQTTClient, redisClient RedisClient) *GatewaysHandler {
	return &GatewaysHandler{
		db:          db,
		mqttClient:  mqttClient,
		redisClient: redisClient,
	}
}

// CreateGatewayRequest represents the request body for creating a gateway
type CreateGatewayRequest struct {
	AreaID           int                     `json:"area_id" binding:"required"`
	Name             string                  `json:"name" binding:"required"`
	DriverType       string                  `json:"driver_type" binding:"required"`
	ConnectionConfig models.ConnectionConfig `json:"connection_config"` // Optional for MQTT
	ScanRateMs       int                     `json:"scan_rate_ms"`
	ZeroBased        *bool                   `json:"zero_based"` // Optional, default should be true
}

// UpdateGatewayRequest represents the request body for updating a gateway
type UpdateGatewayRequest struct {
	Name             *string                  `json:"name"`
	DriverType       *string                  `json:"driver_type"`
	ConnectionConfig *models.ConnectionConfig `json:"connection_config"`
	ScanRateMs       *int                     `json:"scan_rate_ms"`
	Enabled          *bool                    `json:"enabled"`
	ZeroBased        *bool                    `json:"zero_based"`
}

// GatewayHealthStatus represents the health status from Redis
type GatewayHealthStatus struct {
	Status   string `json:"status"`    // "online" or "offline"
	LastSeen int64  `json:"last_seen"` // Unix timestamp in milliseconds
}

// GatewayWithHealth represents a gateway with health status
type GatewayWithHealth struct {
	ID               int                     `json:"id"`
	AreaID           int                     `json:"area_id"`
	Name             string                  `json:"name"`
	DriverType       string                  `json:"driver_type"`
	ConnectionConfig models.ConnectionConfig `json:"connection_config"`
	ScanRateMs       int                     `json:"scan_rate_ms"`
	Enabled          bool                    `json:"enabled"`
	ZeroBased        bool                    `json:"zero_based"`
	CreatedAt        time.Time               `json:"created_at"`
	ConnectionStatus string                  `json:"connection_status,omitempty"` // "online" or "offline"
	LastSeen         *int64                  `json:"last_seen,omitempty"`         // Unix timestamp in milliseconds
}

// getGatewayHealth retrieves the health status for a gateway from Redis
func (h *GatewaysHandler) getGatewayHealth(gatewayID int) (string, *int64) {
	if h.redisClient == nil {
		log.Printf("[GATEWAY_HEALTH] Redis client is nil for gateway %d", gatewayID)
		return "offline", nil
	}

	key := fmt.Sprintf("gateway_health:%d", gatewayID)
	healthJSON, err := h.redisClient.Get(key)
	if err != nil {
		// No health status found - assume offline
		log.Printf("[GATEWAY_HEALTH] No Redis key found for gateway %d (key: %s), error: %v", gatewayID, key, err)
		return "offline", nil
	}

	log.Printf("[GATEWAY_HEALTH] Found Redis data for gateway %d: %s", gatewayID, healthJSON)

	var health GatewayHealthStatus
	if err := json.Unmarshal([]byte(healthJSON), &health); err != nil {
		log.Printf("[GATEWAY_HEALTH] Failed to parse JSON for gateway %d: %v", gatewayID, err)
		return "offline", nil
	}

	log.Printf("[GATEWAY_HEALTH] Gateway %d status: %s", gatewayID, health.Status)
	return health.Status, &health.LastSeen
}

// enrichGatewayWithHealth adds health status to a gateway
func (h *GatewaysHandler) enrichGatewayWithHealth(gateway models.Gateway) GatewayWithHealth {
	status, lastSeen := h.getGatewayHealth(gateway.ID)

	return GatewayWithHealth{
		ID:               gateway.ID,
		AreaID:           gateway.AreaID,
		Name:             gateway.Name,
		DriverType:       gateway.DriverType,
		ConnectionConfig: gateway.ConnectionConfig,
		ScanRateMs:       gateway.ScanRateMs,
		Enabled:          gateway.Enabled,
		ZeroBased:        gateway.ZeroBased,
		CreatedAt:        gateway.CreatedAt,
		ConnectionStatus: status,
		LastSeen:         lastSeen,
	}
}

// Create handles POST /api/gateways
// @Summary Create a new gateway
// @Description Create a new gateway for the specified area
// @Tags gateways
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param request body CreateGatewayRequest true "Gateway creation request"
// @Success 201 {object} models.Gateway
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Area not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/gateways [post]
func (h *GatewaysHandler) Create(c *gin.Context) {
	var req CreateGatewayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Validate driver_type
	if req.DriverType != "S7" && req.DriverType != "MODBUS_TCP" && req.DriverType != "MQTT" && req.DriverType != "OPC_UA" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "driver_type must be 'S7', 'MODBUS_TCP', 'MQTT', or 'OPC_UA'"})
		return
	}

	// For non-MQTT/non-OPC_UA drivers, connection_config with IP is required
	// OPC_UA uses endpoint URL in connection_config instead of ip_address
	if req.DriverType != "MQTT" && req.DriverType != "OPC_UA" && len(req.ConnectionConfig) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection_config is required for this driver type"})
		return
	}

	// For MQTT, ensure connection_config is at least an empty object
	if req.ConnectionConfig == nil {
		req.ConnectionConfig = models.ConnectionConfig{}
	}

	// Verify the area_id belongs to the authorized organization (multi-tenant isolation)
	var areaOrgID int
	err := h.db.QueryRow(
		"SELECT s.org_id FROM areas a JOIN sites s ON a.site_id = s.id WHERE a.id = $1",
		req.AreaID,
	).Scan(&areaOrgID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Area not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify area ownership"})
		return
	}

	if areaOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Cannot create gateway for area in different organization",
			"detail": fmt.Sprintf("Area belongs to organization %d, but authorized for organization %d", areaOrgID, orgID),
		})
		return
	}

	// Set default scan_rate_ms if not provided
	scanRateMs := req.ScanRateMs
	if scanRateMs == 0 {
		scanRateMs = 1000
	}

	// Set default zero_based to false (Standard Modbus is 1-based addressing)
	zeroBased := false
	if req.ZeroBased != nil {
		zeroBased = *req.ZeroBased
	}

	var gateway models.Gateway
	err = h.db.QueryRow(
		`INSERT INTO gateways (area_id, name, driver_type, connection_config, scan_rate_ms, enabled, zero_based)
		 VALUES ($1, $2, $3, $4, $5, TRUE, $6)
		 RETURNING id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, zero_based, created_at`,
		req.AreaID,
		req.Name,
		req.DriverType,
		req.ConnectionConfig,
		scanRateMs,
		zeroBased,
	).Scan(&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.ZeroBased, &gateway.CreatedAt)

	if err != nil {
		log.Printf("Failed to insert new gateway: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create gateway", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gateway)
}

// List handles GET /api/gateways?area_id={id}
// Filters by organization from context (multi-tenant isolation)
// @Summary List gateways
// @Description Get a list of gateways for the specified area
// @Tags gateways
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param area_id query int true "Area ID"
// @Success 200 {array} GatewayWithHealth
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Area not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/gateways [get]
func (h *GatewaysHandler) List(c *gin.Context) {
	// Get organization ID from context (set by middleware)
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	areaIDStr := c.Query("area_id")

	var rows *sql.Rows
	var err error

	if areaIDStr != "" {
		// Case 1: Filter by Specific Area
		areaID, err := strconv.Atoi(areaIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid area_id parameter"})
			return
		}

		// Verify the area_id belongs to the authorized organization
		var areaOrgID int
		err = h.db.QueryRow(
			"SELECT s.org_id FROM areas a JOIN sites s ON a.site_id = s.id WHERE a.id = $1",
			areaID,
		).Scan(&areaOrgID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Area not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify area ownership"})
			return
		}

		if areaOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{
				"error":  "Cannot query gateways for area in different organization",
				"detail": fmt.Sprintf("Area belongs to organization %d, but authorized for organization %d", areaOrgID, orgID),
			})
			return
		}

		rows, err = h.db.Query(
			"SELECT id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, zero_based, created_at FROM gateways WHERE area_id = $1 ORDER BY id",
			areaID,
		)
	} else {
		// Case 2: List All Gateways for Organization
		rows, err = h.db.Query(
			`SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled, g.zero_based, g.created_at 
			 FROM gateways g
			 JOIN areas a ON g.area_id = a.id
			 JOIN sites s ON a.site_id = s.id
			 WHERE s.org_id = $1
			 ORDER BY g.id`,
			orgID,
		)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query gateways"})
		return
	}
	defer rows.Close()

	var gatewaysWithHealth []GatewayWithHealth
	for rows.Next() {
		var gateway models.Gateway
		if err := rows.Scan(&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.ZeroBased, &gateway.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan gateway"})
			return
		}
		// Enrich with health status from Redis
		gatewaysWithHealth = append(gatewaysWithHealth, h.enrichGatewayWithHealth(gateway))
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating gateways"})
		return
	}

	c.JSON(http.StatusOK, gatewaysWithHealth)
}

// Get handles GET /api/gateways/{id}
// @Summary Get a gateway
// @Description Get a single gateway by ID
// @Tags gateways
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Gateway ID"
// @Success 200 {object} GatewayWithHealth
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Gateway not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/gateways/{id} [get]
func (h *GatewaysHandler) Get(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	id := c.Param("id")

	var gateway models.Gateway
	err := h.db.QueryRow(
		"SELECT id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, zero_based, created_at FROM gateways WHERE id = $1",
		id,
	).Scan(&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.ZeroBased, &gateway.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get gateway"})
		return
	}

	// Verify ownership
	var gatewayOrgID int
	err = h.db.QueryRow(
		"SELECT s.org_id FROM areas a JOIN sites s ON a.site_id = s.id WHERE a.id = $1",
		gateway.AreaID,
	).Scan(&gatewayOrgID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify gateway ownership"})
		return
	}

	if gatewayOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to gateway from another organization"})
		return
	}

	c.JSON(http.StatusOK, h.enrichGatewayWithHealth(gateway))
}

// Delete handles DELETE /api/gateways/{id}
// @Summary Delete a gateway
// @Description Delete a gateway by ID (cascades to tags)
// @Tags gateways
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Gateway ID"
// @Success 204 "Gateway deleted"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Gateway not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/gateways/{id} [delete]
func (h *GatewaysHandler) Delete(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	id := c.Param("id")

	// Check if gateway exists and check ownership
	var gatewayOrgID int
	err := h.db.QueryRow(`
		SELECT s.org_id 
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id 
		WHERE g.id = $1`, id).Scan(&gatewayOrgID)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check gateway"})
		return
	}

	if gatewayOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete gateway from another organization"})
		return
	}

	// 0. MQTT Retained Message Cleanup
	// Fetch all tags to clear their retained messages from broker
	// We need org, site, area, gateway, tag_alias to build the topic
	// Also fetch Sparkplug B group/node IDs for DDEATH messages
	var gatewayName, orgName, siteName, areaName string
	rows, err := h.db.Query(`
		SELECT
			o.name as org,
			s.name as site,
			a.name as area,
			g.name as gateway,
			t.alias
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1`, id)

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var org, site, area, gateway, tagAlias string
			if err := rows.Scan(&org, &site, &area, &gateway, &tagAlias); err == nil {
				// Store first row values for Sparkplug B DDEATH
				if gatewayName == "" {
					gatewayName = gateway
					orgName = org
					siteName = site
					areaName = area
				}

				// Helper to create slug (simple version matching typical driver logic)
				slug := func(s string) string {
					return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
				}

				// Publish empty retained message to clear it
				if h.mqttClient != nil {
					// 1. Try Raw Format
					topic1 := fmt.Sprintf("data/%s/%s/%s/%s/%s", org, site, area, gateway, tagAlias)
					h.mqttClient.PublishWithQoS(topic1, "", 1, true)

					// 2. Try Slugified Format (Standard)
					topic2 := fmt.Sprintf("data/%s/%s/%s/%s/%s", slug(org), slug(site), slug(area), slug(gateway), slug(tagAlias))
					if topic2 != topic1 { // Only publish if the slugified topic is different
						h.mqttClient.PublishWithQoS(topic2, "", 1, true)
					}
				}
			}
		}
	}

	// 0.5. Sparkplug B Cleanup - Send DDEATH and clear retained messages
	if h.mqttClient != nil && gatewayName != "" {
		// Build Sparkplug B topic components
		groupID := sparkplug.BuildGroupID(orgName, siteName)
		edgeNodeID := sparkplug.BuildEdgeNodeID(areaName, gatewayName)

		// Send DDEATH message for the device (gateway acts as device)
		ddeathTopic := sparkplug.BuildDDEATHTopic(groupID, edgeNodeID, gatewayName)
		ddeathPayload := &sparkplug.Payload{
			Timestamp: time.Now().UnixMilli(),
			Seq:       0,
			Metrics:   []sparkplug.Metric{},
		}

		// Encode DDEATH payload as JSON (Protobuf requires generated code)
		ddeathBytes, _ := json.Marshal(ddeathPayload)

		// Publish DDEATH (not retained - just notify, then clear)
		h.mqttClient.PublishWithQoS(ddeathTopic, string(ddeathBytes), 0, false)
		log.Printf("[GATEWAY] Sent DDEATH for gateway %s to topic %s", gatewayName, ddeathTopic)

		// Clear all retained messages for this device (DBIRTH, DDATA, DDEATH)
		dbirthTopic := sparkplug.BuildDBIRTHTopic(groupID, edgeNodeID, gatewayName)
		h.mqttClient.PublishWithQoS(dbirthTopic, "", 0, true)

		ddataTopic := sparkplug.BuildDDATATopic(groupID, edgeNodeID, gatewayName)
		h.mqttClient.PublishWithQoS(ddataTopic, "", 0, true)

		// Clear DDEATH retained message as well so it doesn't reappear on reconnect
		h.mqttClient.PublishWithQoS(ddeathTopic, "", 0, true)
	}

	// Manual Cascade Delete Transaction
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// 1. Delete Tags
	_, err = tx.Exec("DELETE FROM tags WHERE gateway_id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related tags"})
		return
	}

	// 3. Delete Gateway
	_, err = tx.Exec("DELETE FROM gateways WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete gateway"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// Publish reload command and clear health status
	if h.mqttClient != nil {
		// Clear retained health status message by sending empty payload with retained=true
		// This prevents "ghost" gateways from appearing in logs after deletion
		h.mqttClient.PublishWithQoS(fmt.Sprintf("sys/health/%s", id), "", 1, true)

		// Send final reload command (optional, mostly for driver manager if it's still listening)
		h.mqttClient.Publish(fmt.Sprintf("sys/command/reload/%s", id), "deleted")
	}

	c.Status(http.StatusNoContent)
}

// Update handles PUT /api/gateways/{id}
// Filters by organization from context (multi-tenant isolation)
// @Summary Update a gateway
// @Description Update a gateway by ID
// @Tags gateways
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Gateway ID"
// @Param request body UpdateGatewayRequest true "Gateway update request"
// @Success 200 {object} GatewayWithHealth
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Gateway not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/gateways/{id} [put]
func (h *GatewaysHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gateway ID"})
		return
	}

	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Verify gateway ownership first
	var gatewayOrgID int
	err = h.db.QueryRow(
		`SELECT s.org_id
		 FROM gateways g
		 JOIN areas a ON g.area_id = a.id
		 JOIN sites s ON a.site_id = s.id
		 WHERE g.id = $1`,
		id,
	).Scan(&gatewayOrgID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify gateway ownership"})
		return
	}

	if gatewayOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Cannot update gateway from different organization",
			"detail": fmt.Sprintf("Gateway belongs to organization %d, but authorized for organization %d", gatewayOrgID, orgID),
		})
		return
	}

	var req UpdateGatewayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate driver_type if provided
	if req.DriverType != nil && *req.DriverType != "S7" && *req.DriverType != "MODBUS_TCP" && *req.DriverType != "MQTT" && *req.DriverType != "OPC_UA" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "driver_type must be 'S7', 'MODBUS_TCP', 'MQTT', or 'OPC_UA'"})
		return
	}

	// Build dynamic update query
	updates := []string{}
	args := []interface{}{}
	argPos := 1

	if req.Name != nil {
		updates = append(updates, "name = $"+strconv.Itoa(argPos))
		args = append(args, *req.Name)
		argPos++
	}
	if req.DriverType != nil {
		updates = append(updates, "driver_type = $"+strconv.Itoa(argPos))
		args = append(args, *req.DriverType)
		argPos++
	}
	if req.ConnectionConfig != nil {
		updates = append(updates, "connection_config = $"+strconv.Itoa(argPos))
		// Convert to JSON to ensure proper serialization for PostgreSQL JSONB
		configJSON, err := json.Marshal(*req.ConnectionConfig)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize connection config"})
			return
		}
		args = append(args, configJSON)
		argPos++
	}
	if req.ScanRateMs != nil {
		updates = append(updates, "scan_rate_ms = $"+strconv.Itoa(argPos))
		args = append(args, *req.ScanRateMs)
		argPos++
	}
	if req.Enabled != nil {
		updates = append(updates, "enabled = $"+strconv.Itoa(argPos))
		args = append(args, *req.Enabled)
		argPos++
	}
	if req.ZeroBased != nil {
		updates = append(updates, "zero_based = $"+strconv.Itoa(argPos))
		args = append(args, *req.ZeroBased)
		argPos++
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	// Add WHERE parameter
	args = append(args, id)
	query := "UPDATE gateways SET " + updates[0]
	for i := 1; i < len(updates); i++ {
		query += ", " + updates[i]
	}
	query += " WHERE id = $" + strconv.Itoa(argPos) + " RETURNING id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, zero_based, created_at"

	var gateway models.Gateway
	err = h.db.QueryRow(query, args...).Scan(
		&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType,
		&gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.ZeroBased, &gateway.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update gateway"})
		return
	}

	// Publish reload command to MQTT
	if h.mqttClient != nil {
		topic := fmt.Sprintf("sys/command/reload/%d", gateway.ID)
		if err := h.mqttClient.Publish(topic, "reload"); err != nil {
			log.Printf("[WARN] Failed to send reload command to gateway %d: %v", gateway.ID, err)
			// Don't fail the request - the driver will reload on next periodic refresh
		}

		// If explicitly disabled, forcefully publish offline status so historian records gap and event
		if req.Enabled != nil && !*req.Enabled {
			healthTopic := fmt.Sprintf("sys/health/%d", gateway.ID)
			h.mqttClient.PublishWithQoS(healthTopic, "offline", 1, true)
		}
	}

	// Enrich with health status from Redis
	gatewayWithHealth := h.enrichGatewayWithHealth(gateway)

	c.JSON(http.StatusOK, gatewayWithHealth)
}

// TestConnection handles POST /api/gateways/{id}/test
// Checks basic TCP connectivity to the gateway
// @Summary Test gateway connection
// @Description Test TCP connection to the gateway
// @Tags gateways
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Gateway ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string "Gateway not found"
// @Router /api/gateways/{id}/test [post]
func (h *GatewaysHandler) TestConnection(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gateway ID"})
		return
	}

	// Fetch gateway config
	var ipAddress string
	var driverType string
	var connectionConfig []byte // JSONB

	err = h.db.QueryRow(
		"SELECT connection_config, driver_type FROM gateways WHERE id = $1",
		id,
	).Scan(&connectionConfig, &driverType)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
		return
	}

	// Parse config to get IP/Port
	// This uses a generic map because schema depends on driver
	var config map[string]interface{}
	if err := json.Unmarshal(connectionConfig, &config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid connection config"})
		return
	}

	// Basic TCP dial check
	target := ""

	// Handle OPC_UA separately — uses endpoint URL, not ip_address
	if driverType == "OPC_UA" {
		endpoint, _ := config["endpoint"].(string)
		if endpoint == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "No OPC UA endpoint configured"})
			return
		}
		// Parse host:port from opc.tcp://host:port/path
		clean := strings.TrimPrefix(endpoint, "opc.tcp://")
		parts := strings.SplitN(clean, "/", 2)
		hostPort := parts[0]
		if !strings.Contains(hostPort, ":") {
			hostPort += ":4840" // default OPC UA port
		}
		conn, err := net.DialTimeout("tcp", hostPort, 2*time.Second)
		if err != nil {
			log.Printf("[GATEWAY] OPC UA connection test failed for %s: %v", hostPort, err)
			c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("OPC UA connection failed to %s", hostPort)})
			return
		}
		conn.Close()
		c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("Successfully connected to OPC UA server at %s", hostPort)})
		return
	}

	if driverType == "MQTT" {
		// MQTT driver doesn't need TCP test to PLC IP - the PLC connects to our broker
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "MQTT driver: PLC publishes to the system broker. No direct connection to test."})
		return
	}

	if val, ok := config["ip_address"].(string); ok {
		ipAddress = val
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "No IP address configured"})
		return
	}

	port := 0
	if driverType == "S7" {
		port = 102
	} else if driverType == "MODBUS_TCP" {
		if val, ok := config["port"].(float64); ok { // JSON numbers are float64
			port = int(val)
		} else {
			port = 502
		}
	}

	if port == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid port"})
		return
	}

	target = fmt.Sprintf("%s:%d", ipAddress, port)

	// Try to connect (timeout 2s)
	conn, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		log.Printf("[GATEWAY] Connection test failed for %s: %v", target, err)
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("Connection failed to %s", target)})
		return
	}
	conn.Close()

	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("Successfully connected to %s", target)})
}

// BrowseNodes handles POST /api/gateways/{id}/browse
// Connects to an OPC UA server and browses child nodes from a given parent
func (h *GatewaysHandler) BrowseNodes(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gateway ID"})
		return
	}

	// Parse request body
	var req struct {
		NodeID string `json:"node_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.NodeID = "i=84" // Default: Root Objects folder
	}
	if req.NodeID == "" {
		req.NodeID = "i=84"
	}

	// Fetch gateway config
	var driverType string
	var connectionConfig []byte

	err = h.db.QueryRow(
		"SELECT driver_type, connection_config FROM gateways WHERE id = $1",
		id,
	).Scan(&driverType, &connectionConfig)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
		return
	}

	if driverType != "OPC_UA" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Browse is only supported for OPC_UA gateways"})
		return
	}

	// Parse connection config
	var config map[string]interface{}
	if err := json.Unmarshal(connectionConfig, &config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid connection config"})
		return
	}

	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No OPC UA endpoint configured"})
		return
	}

	// Create temporary OPC UA client for browsing
	client := opcuaclient.NewClient(opcuaclient.Config{
		Endpoint: endpoint,
		Timeout:  10 * time.Second,
	})

	if err := client.Connect(); err != nil {
		log.Printf("[GATEWAY] OPC UA connect failed for %s: %v", endpoint, err)
		c.JSON(http.StatusOK, gin.H{"error": "Failed to connect to OPC UA server", "nodes": []interface{}{}})
		return
	}
	defer client.Disconnect()

	nodes, err := client.Browse(req.NodeID)
	if err != nil {
		log.Printf("[GATEWAY] OPC UA browse failed for node %s: %v", req.NodeID, err)
		c.JSON(http.StatusOK, gin.H{"error": "OPC UA browse failed", "nodes": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}
