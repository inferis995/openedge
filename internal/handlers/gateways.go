package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// MQTTClient interface for publishing reload commands
type MQTTClient interface {
	Publish(topic string, payload interface{}) error
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
	AreaID           int                        `json:"area_id" binding:"required"`
	Name             string                     `json:"name" binding:"required"`
	DriverType       string                     `json:"driver_type" binding:"required"`
	ConnectionConfig models.ConnectionConfig   `json:"connection_config" binding:"required"`
	ScanRateMs       int                        `json:"scan_rate_ms"`
}

// UpdateGatewayRequest represents the request body for updating a gateway
type UpdateGatewayRequest struct {
	Name             *string                    `json:"name"`
	DriverType       *string                    `json:"driver_type"`
	ConnectionConfig *models.ConnectionConfig   `json:"connection_config"`
	ScanRateMs       *int                       `json:"scan_rate_ms"`
	Enabled          *bool                      `json:"enabled"`
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
	CreatedAt        time.Time               `json:"created_at"`
	ConnectionStatus string                  `json:"connection_status,omitempty"` // "online" or "offline"
	LastSeen         *int64                  `json:"last_seen,omitempty"`         // Unix timestamp in milliseconds
}

// getGatewayHealth retrieves the health status for a gateway from Redis
func (h *GatewaysHandler) getGatewayHealth(gatewayID int) (string, *int64) {
	if h.redisClient == nil {
		return "", nil
	}

	key := fmt.Sprintf("gateway_health:%d", gatewayID)
	healthJSON, err := h.redisClient.Get(key)
	if err != nil {
		// No health status found - assume offline
		return "offline", nil
	}

	var health GatewayHealthStatus
	if err := json.Unmarshal([]byte(healthJSON), &health); err != nil {
		return "offline", nil
	}

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
		CreatedAt:        gateway.CreatedAt,
		ConnectionStatus: status,
		LastSeen:         lastSeen,
	}
}

// Create handles POST /api/gateways
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
	if req.DriverType != "S7" && req.DriverType != "MODBUS_TCP" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "driver_type must be 'S7' or 'MODBUS_TCP'"})
		return
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
			"error": "Cannot create gateway for area in different organization",
			"detail": fmt.Sprintf("Area belongs to organization %d, but authorized for organization %d", areaOrgID, orgID),
		})
		return
	}

	// Set default scan_rate_ms if not provided
	scanRateMs := req.ScanRateMs
	if scanRateMs == 0 {
		scanRateMs = 1000
	}

	var gateway models.Gateway
	err = h.db.QueryRow(
		`INSERT INTO gateways (area_id, name, driver_type, connection_config, scan_rate_ms, enabled)
		 VALUES ($1, $2, $3, $4, $5, TRUE)
		 RETURNING id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, created_at`,
		req.AreaID,
		req.Name,
		req.DriverType,
		req.ConnectionConfig,
		scanRateMs,
	).Scan(&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create gateway"})
		return
	}

	c.JSON(http.StatusCreated, gateway)
}

// List handles GET /api/gateways?area_id={id}
// Filters by organization from context (multi-tenant isolation)
func (h *GatewaysHandler) List(c *gin.Context) {
	// Get organization ID from context (set by middleware)
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	areaIDStr := c.Query("area_id")
	if areaIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "area_id query parameter is required"})
		return
	}

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
			"error": "Cannot query gateways for area in different organization",
			"detail": fmt.Sprintf("Area belongs to organization %d, but authorized for organization %d", areaOrgID, orgID),
		})
		return
	}

	rows, err := h.db.Query(
		"SELECT id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, created_at FROM gateways WHERE area_id = $1 ORDER BY id",
		areaID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query gateways"})
		return
	}
	defer rows.Close()

	var gatewaysWithHealth []GatewayWithHealth
	for rows.Next() {
		var gateway models.Gateway
		if err := rows.Scan(&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.CreatedAt); err != nil {
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
// Filters by organization from context (multi-tenant isolation)
func (h *GatewaysHandler) Get(c *gin.Context) {
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

	// Query gateway with organization join to verify ownership
	var gateway models.Gateway
	var gatewayOrgID int
	err = h.db.QueryRow(
		`SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled, g.created_at, s.org_id
		 FROM gateways g
		 JOIN areas a ON g.area_id = a.id
		 JOIN sites s ON a.site_id = s.id
		 WHERE g.id = $1`,
		id,
	).Scan(&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.CreatedAt, &gatewayOrgID)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query gateway"})
		return
	}

	// Verify organization ownership
	if gatewayOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Cannot access gateway from different organization",
			"detail": fmt.Sprintf("Gateway belongs to organization %d, but authorized for organization %d", gatewayOrgID, orgID),
		})
		return
	}

	// Enrich with health status from Redis
	gatewayWithHealth := h.enrichGatewayWithHealth(gateway)

	c.JSON(http.StatusOK, gatewayWithHealth)
}

// Update handles PUT /api/gateways/{id}
// Filters by organization from context (multi-tenant isolation)
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
			"error": "Cannot update gateway from different organization",
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
	if req.DriverType != nil && *req.DriverType != "S7" && *req.DriverType != "MODBUS_TCP" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "driver_type must be 'S7' or 'MODBUS_TCP'"})
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
		args = append(args, *req.ConnectionConfig)
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
	query += " WHERE id = $" + strconv.Itoa(argPos) + " RETURNING id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, created_at"

	var gateway models.Gateway
	err = h.db.QueryRow(query, args...).Scan(
		&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType,
		&gateway.ConnectionConfig, &gateway.ScanRateMs, &gateway.Enabled, &gateway.CreatedAt,
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
			// Log error but don't fail the request
			// The MQTT connection might be down temporarily
			// The driver will still work with old config until reconnected
		}
	}

	// Enrich with health status from Redis
	gatewayWithHealth := h.enrichGatewayWithHealth(gateway)

	c.JSON(http.StatusOK, gatewayWithHealth)
}
