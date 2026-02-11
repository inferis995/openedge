package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/redis/go-redis/v9"
)

// RedisClient defines the interface for Redis operations
type RedisClient interface {
	Get(key string) (string, error)
	Subscribe(channel string) *redis.PubSub
}

// TagsHandler handles tag-related HTTP requests
type TagsHandler struct {
	db          *sql.DB
	mqttClient  MQTTClient
	redisClient RedisClient
}

// NewTagsHandler creates a new tags handler
func NewTagsHandler(db *sql.DB, mqttClient MQTTClient, redisClient RedisClient) *TagsHandler {
	return &TagsHandler{
		db:          db,
		mqttClient:  mqttClient,
		redisClient: redisClient,
	}
}

// CreateTagRequest represents the request body for creating a tag
type CreateTagRequest struct {
	GatewayID         int      `json:"gateway_id" binding:"required"`
	Code              string   `json:"code" binding:"required"`
	Alias             string   `json:"alias" binding:"required"`
	DataType          string   `json:"data_type" binding:"required"`
	Historize         *bool    `json:"historize"`
	HistorizeDeadband *float64 `json:"historize_deadband"`
}

// UpdateTagRequest represents the request body for updating a tag
type UpdateTagRequest struct {
	Code              *string  `json:"code"`
	Alias             *string  `json:"alias"`
	DataType          *string  `json:"data_type"`
	Historize         *bool    `json:"historize"`
	HistorizeDeadband *float64 `json:"historize_deadband"`
}

// validateDataType checks if the data_type is valid
func validateDataType(dataType string) bool {
	switch dataType {
	case "INT", "REAL", "BOOL", "DINT":
		return true
	default:
		return false
	}
}

// @Param X-Organization-ID header int true "Organization ID"
// @Param request body CreateTagRequest true "Tag creation request"
// @Success 201 {object} models.Tag
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Gateway not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/tags [post]
func (h *TagsHandler) Create(c *gin.Context) {
	var req CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[API] Tag Creation Failed: JSON binding error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Verify the gateway_id belongs to the authorized organization
	var gatewayOrgID int
	err := h.db.QueryRow(
		`SELECT s.org_id
		 FROM gateways g
		 JOIN areas a ON g.area_id = a.id
		 JOIN sites s ON a.site_id = s.id
		 WHERE g.id = $1`,
		req.GatewayID,
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
			"error":  "Cannot create tag for gateway in different organization",
			"detail": fmt.Sprintf("Gateway belongs to organization %d, but authorized for organization %d", gatewayOrgID, orgID),
		})
		return
	}

	// Validate data_type
	if !validateDataType(req.DataType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "data_type must be 'INT', 'REAL', 'BOOL', or 'DINT'"})
		return
	}

	// Set defaults
	historize := false
	if req.Historize != nil {
		historize = *req.Historize
	}

	historizeDeadband := 0.0
	if req.HistorizeDeadband != nil {
		historizeDeadband = *req.HistorizeDeadband
	}

	var tag models.Tag
	err = h.db.QueryRow(
		`INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM tags WHERE gateway_id = $1))
		 RETURNING id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, created_at`,
		req.GatewayID,
		req.Code,
		req.Alias,
		req.DataType,
		historize,
		historizeDeadband,
	).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tag"})
		return
	}

	// Publish reload command to MQTT so the driver picks up the new tag
	if h.mqttClient != nil {
		topic := fmt.Sprintf("sys/command/reload/%d", tag.GatewayID)
		log.Printf("[API] Sending reload signal for gateway %d to topic %s", tag.GatewayID, topic)
		if err := h.mqttClient.Publish(topic, "reload"); err != nil {
			log.Printf("[API] Failed to publish reload signal: %v", err)
		}
	}

	c.JSON(http.StatusCreated, tag)
}

// List handles GET /api/tags?gateway_id={id}
// Filters by organization from context (multi-tenant isolation)
// @Summary List tags
// @Description Get a list of tags for the specified gateway
// @Tags tags
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param gateway_id query int true "Gateway ID"
// @Success 200 {array} models.Tag
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Gateway not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/tags [get]
func (h *TagsHandler) List(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	gatewayIDStr := c.Query("gateway_id")
	log.Printf("[API] List Tags: org=%d, gateway_id=%s", orgID, gatewayIDStr)

	var rows *sql.Rows
	var err error

	if gatewayIDStr != "" {
		// Case 1: Filter by Specific Gateway
		gatewayID, err := strconv.Atoi(gatewayIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gateway_id parameter"})
			return
		}

		// Verify the gateway_id belongs to the authorized organization
		var gatewayOrgID int
		err = h.db.QueryRow(
			`SELECT s.org_id
			 FROM gateways g
			 JOIN areas a ON g.area_id = a.id
			 JOIN sites s ON a.site_id = s.id
			 WHERE g.id = $1`,
			gatewayID,
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
				"error":  "Cannot query tags for gateway in different organization",
				"detail": fmt.Sprintf("Gateway belongs to organization %d, but authorized for organization %d", gatewayOrgID, orgID),
			})
			return
		}

		rows, err = h.db.Query(
			"SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, created_at FROM tags WHERE gateway_id = $1 ORDER BY sort_order ASC, id ASC",
			gatewayID,
		)
	} else {
		// Case 2: List All Tags for Organization
		rows, err = h.db.Query(
			`SELECT t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize, t.historize_deadband, t.sort_order, t.created_at
			 FROM tags t
			 JOIN gateways g ON t.gateway_id = g.id
			 JOIN areas a ON g.area_id = a.id
			 JOIN sites s ON a.site_id = s.id
			 WHERE s.org_id = $1
			 ORDER BY t.sort_order ASC, t.id ASC`,
			orgID,
		)
	}

	if err != nil {
		log.Printf("[API] Query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query tags"})
		return
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.CreatedAt); err != nil {
			log.Printf("[API] Scan error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan tag"})
			return
		}
		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating tags"})
		return
	}

	c.JSON(http.StatusOK, tags)
}

// Get handles GET /api/tags/{id}
// @Summary Get a tag
// @Description Get a single tag by ID
// @Tags tags
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Tag ID"
// @Success 200 {object} models.Tag
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Tag not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/tags/{id} [get]
func (h *TagsHandler) Get(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	id := c.Param("id")

	var tag models.Tag
	err := h.db.QueryRow(
		"SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, created_at FROM tags WHERE id = $1",
		id,
	).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get tag"})
		return
	}

	// Verify ownership
	var tagOrgID int
	err = h.db.QueryRow(
		`SELECT s.org_id
		 FROM gateways g
		 JOIN areas a ON g.area_id = a.id
		 JOIN sites s ON a.site_id = s.id
		 WHERE g.id = $1`,
		tag.GatewayID,
	).Scan(&tagOrgID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify tag ownership"})
		return
	}

	if tagOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to tag from another organization"})
		return
	}

	c.JSON(http.StatusOK, tag)
}

// Delete handles DELETE /api/tags/{id}
// @Summary Delete a tag
// @Description Delete a tag by ID
// @Tags tags
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Tag ID"
// @Success 204 "Tag deleted"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Tag not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/tags/{id} [delete]
func (h *TagsHandler) Delete(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)

	// Check if tag exists AND get gateway_id for reload
	var gatewayID int
	var tagOrgID int
	err := h.db.QueryRow(`
		SELECT t.gateway_id, s.org_id 
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id 
		WHERE t.id = $1`, id).Scan(&gatewayID, &tagOrgID)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check tag"})
		return
	}

	if tagOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete tag from another organization"})
		return
	}

	// Delete Tag
	_, err = h.db.Exec("DELETE FROM tags WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tag"})
		return
	}

	// Publish reload command to MQTT AND clear retained message for this tag
	if h.mqttClient != nil {
		// 1. Clear retained message for this tag by publishing empty retained message
		dataTopic := fmt.Sprintf("data/%d", id)
		h.mqttClient.PublishWithQoS(dataTopic, "", 1, true) // Empty retained = clears old data

		// 2. Send reload command to driver
		reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
		h.mqttClient.Publish(reloadTopic, "reload")
	}

	c.Status(http.StatusNoContent)
}

// Update handles PUT /api/tags/{id}
// Filters by organization from context (multi-tenant isolation)
// @Summary Update a tag
// @Description Update a tag by ID
// @Tags tags
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Tag ID"
// @Param request body UpdateTagRequest true "Tag update request"
// @Success 200 {object} models.Tag
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Tag not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/tags/{id} [put]
func (h *TagsHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Verify tag ownership first
	var tagOrgID int
	err = h.db.QueryRow(
		`SELECT s.org_id
		 FROM tags t
		 JOIN gateways g ON t.gateway_id = g.id
		 JOIN areas a ON g.area_id = a.id
		 JOIN sites s ON a.site_id = s.id
		 WHERE t.id = $1`,
		id,
	).Scan(&tagOrgID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify tag ownership"})
		return
	}

	if tagOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Cannot update tag from different organization",
			"detail": fmt.Sprintf("Tag belongs to organization %d, but authorized for organization %d", tagOrgID, orgID),
		})
		return
	}

	var req UpdateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate data_type if provided
	if req.DataType != nil && !validateDataType(*req.DataType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "data_type must be 'INT', 'REAL', 'BOOL', or 'DINT'"})
		return
	}

	// Build dynamic update query
	updates := []string{}
	args := []interface{}{}
	argPos := 1

	if req.Code != nil {
		updates = append(updates, "code = $"+strconv.Itoa(argPos))
		args = append(args, *req.Code)
		argPos++
	}
	if req.Alias != nil {
		updates = append(updates, "alias = $"+strconv.Itoa(argPos))
		args = append(args, *req.Alias)
		argPos++
	}
	if req.DataType != nil {
		updates = append(updates, "data_type = $"+strconv.Itoa(argPos))
		args = append(args, *req.DataType)
		argPos++
	}
	if req.Historize != nil {
		updates = append(updates, "historize = $"+strconv.Itoa(argPos))
		args = append(args, *req.Historize)
		argPos++
	}
	if req.HistorizeDeadband != nil {
		updates = append(updates, "historize_deadband = $"+strconv.Itoa(argPos))
		args = append(args, *req.HistorizeDeadband)
		argPos++
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No fields to update"})
		return
	}

	// Add WHERE parameter
	args = append(args, id)
	query := "UPDATE tags SET " + updates[0]
	for i := 1; i < len(updates); i++ {
		query += ", " + updates[i]
	}
	query += " WHERE id = $" + strconv.Itoa(argPos) + " RETURNING id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, created_at"

	var tag models.Tag
	err = h.db.QueryRow(query, args...).Scan(
		&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType,
		&tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tag"})
		return
	}

	// Publish reload command to MQTT
	if h.mqttClient != nil {
		topic := fmt.Sprintf("sys/command/reload/%d", tag.GatewayID)
		if err := h.mqttClient.Publish(topic, "reload"); err != nil {
			// Log error but don't fail the request
			// The MQTT connection might be down temporarily
			// The driver will still work with old config until reconnected
		}
	}

	c.JSON(http.StatusOK, tag)
}

// CurrentValueResponse represents the response for current value endpoint
type CurrentValueResponse struct {
	V  interface{} `json:"v"`
	Ts int64       `json:"ts"`
	Q  int         `json:"q"`
}

// GetCurrentValue handles GET /api/tags/{id}/current
// @Summary Get current tag value
// @Description Get the current value of a tag from Redis cache
// @Tags tags
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Tag ID"
// @Success 200 {object} CurrentValueResponse
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 404 {object} map[string]string "Tag not found"
// @Failure 500 {object} map[string]string "Server error"
// @Failure 503 {object} map[string]string "Redis not available"
// @Router /api/tags/{id}/current [get]
func (h *TagsHandler) GetCurrentValue(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	// Verify tag exists in database
	var tagExists bool
	err = h.db.QueryRow("SELECT EXISTS(SELECT 1 FROM tags WHERE id = $1)", id).Scan(&tagExists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify tag"})
		return
	}
	if !tagExists {
		c.JSON(http.StatusOK, CurrentValueResponse{
			V:  nil,
			Ts: 0,
			Q:  1, // Bad quality for non-existent tag
		})
		return
	}

	// Check if Redis client is available
	if h.redisClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Redis not available"})
		return
	}

	// Get current value from Redis
	redisKey := fmt.Sprintf("realtime:%d", id)
	valueJSON, err := h.redisClient.Get(redisKey)
	if err != nil {
		// Instead of returning 404, return a valid response with null value
		// This prevents frontend errors when polling new tags
		c.JSON(http.StatusOK, CurrentValueResponse{
			V:  nil,
			Ts: 0,
			Q:  0, // Good/Bad quality can be defined, 0 usually means Bad/Unknown here? Let's say 0 is unknown/init.
		})
		return
	}

	// Parse JSON response
	var response CurrentValueResponse
	if err := json.Unmarshal([]byte(valueJSON), &response); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse current value"})
		return
	}

	c.JSON(http.StatusOK, response)
}

// WriteTagRequest represents the request body for writing a tag value
type WriteTagRequest struct {
	Value interface{} `json:"value" binding:"required"`
}

// Write handles POST /api/tags/{id}/write
// @Summary Write tag value
// @Description Send a write command to the gateway for the specified tag
// @Tags tags
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Tag ID"
// @Param request body WriteTagRequest true "Write request"
// @Success 200 {object} map[string]string "Write command sent"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 404 {object} map[string]string "Tag not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/tags/{id}/write [post]
func (h *TagsHandler) Write(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	var req WriteTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1. Get Tag details (specifically gateway_id and code)
	var tag struct {
		ID        int
		GatewayID int
		Code      string
		DataType  string
	}
	err = h.db.QueryRow("SELECT id, gateway_id, code, data_type FROM tags WHERE id = $1", id).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.DataType)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tag details"})
		return
	}

	// 2. Publish write command to MQTT
	if h.mqttClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MQTT client not available"})
		return
	}

	cmd := struct {
		TagID    int         `json:"tag_id"`
		Code     string      `json:"code"`
		Value    interface{} `json:"value"`
		DataType string      `json:"data_type"`
	}{
		TagID:    tag.ID,
		Code:     tag.Code,
		Value:    req.Value,
		DataType: tag.DataType,
	}

	payload, _ := json.Marshal(cmd)
	topic := fmt.Sprintf("cmd/write/%d", tag.GatewayID)

	if err := h.mqttClient.Publish(topic, string(payload)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to publish write command"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Write command sent"})
}
