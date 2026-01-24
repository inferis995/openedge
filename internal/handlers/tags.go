package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// RedisClient defines the interface for Redis operations
type RedisClient interface {
	Get(key string) (string, error)
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
	GatewayID        int     `json:"gateway_id" binding:"required"`
	Code             string  `json:"code" binding:"required"`
	Alias            string  `json:"alias" binding:"required"`
	DataType         string  `json:"data_type" binding:"required"`
	Historize        *bool   `json:"historize"`
	HistorizeDeadband *float64 `json:"historize_deadband"`
	AlarmEnabled     *bool   `json:"alarm_enabled"`
	AlarmThreshold   *float64 `json:"alarm_threshold"`
	AlarmOperator    string  `json:"alarm_operator"`
	AlarmPriority    *int    `json:"alarm_priority"`
}

// UpdateTagRequest represents the request body for updating a tag
type UpdateTagRequest struct {
	Code             *string  `json:"code"`
	Alias            *string  `json:"alias"`
	DataType         *string  `json:"data_type"`
	Historize        *bool    `json:"historize"`
	HistorizeDeadband *float64 `json:"historize_deadband"`
	AlarmEnabled     *bool    `json:"alarm_enabled"`
	AlarmThreshold   *float64 `json:"alarm_threshold"`
	AlarmOperator    *string  `json:"alarm_operator"`
	AlarmPriority    *int     `json:"alarm_priority"`
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

// validateAlarmOperator checks if the alarm_operator is valid
func validateAlarmOperator(op string) bool {
	switch op {
	case ">", "<", "=", "":
		return true
	default:
		return false
	}
}

// validateAlarmPriority checks if the alarm_priority is valid
func validateAlarmPriority(priority int) bool {
	return priority >= 1 && priority <= 5
}

// Create handles POST /api/tags
func (h *TagsHandler) Create(c *gin.Context) {
	var req CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate data_type
	if !validateDataType(req.DataType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "data_type must be 'INT', 'REAL', 'BOOL', or 'DINT'"})
		return
	}

	// Validate alarm_operator if provided
	if req.AlarmOperator != "" && !validateAlarmOperator(req.AlarmOperator) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alarm_operator must be '>', '<', or '='"})
		return
	}

	// Validate alarm_priority if provided
	alarmPriority := 1
	if req.AlarmPriority != nil {
		if !validateAlarmPriority(*req.AlarmPriority) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "alarm_priority must be between 1 and 5"})
			return
		}
		alarmPriority = *req.AlarmPriority
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

	alarmEnabled := false
	if req.AlarmEnabled != nil {
		alarmEnabled = *req.AlarmEnabled
	}

	var tag models.Tag
	err := h.db.QueryRow(
		`INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband, alarm_enabled, alarm_threshold, alarm_operator, alarm_priority)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, gateway_id, code, alias, data_type, historize, historize_deadband, alarm_enabled, alarm_threshold, alarm_operator, alarm_priority, created_at`,
		req.GatewayID,
		req.Code,
		req.Alias,
		req.DataType,
		historize,
		historizeDeadband,
		alarmEnabled,
		req.AlarmThreshold,
		req.AlarmOperator,
		alarmPriority,
	).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize, &tag.HistorizeDeadband, &tag.AlarmEnabled, &tag.AlarmThreshold, &tag.AlarmOperator, &tag.AlarmPriority, &tag.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tag"})
		return
	}

	c.JSON(http.StatusCreated, tag)
}

// List handles GET /api/tags?gateway_id={id}
func (h *TagsHandler) List(c *gin.Context) {
	gatewayIDStr := c.Query("gateway_id")
	if gatewayIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "gateway_id query parameter is required"})
		return
	}

	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gateway_id parameter"})
		return
	}

	rows, err := h.db.Query(
		"SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband, alarm_enabled, alarm_threshold, alarm_operator, alarm_priority, created_at FROM tags WHERE gateway_id = $1 ORDER BY id",
		gatewayID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query tags"})
		return
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize, &tag.HistorizeDeadband, &tag.AlarmEnabled, &tag.AlarmThreshold, &tag.AlarmOperator, &tag.AlarmPriority, &tag.CreatedAt); err != nil {
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

// Update handles PUT /api/tags/{id}
func (h *TagsHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
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

	// Validate alarm_operator if provided
	if req.AlarmOperator != nil && !validateAlarmOperator(*req.AlarmOperator) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alarm_operator must be '>', '<', or '='"})
		return
	}

	// Validate alarm_priority if provided
	if req.AlarmPriority != nil && !validateAlarmPriority(*req.AlarmPriority) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alarm_priority must be between 1 and 5"})
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
	if req.AlarmEnabled != nil {
		updates = append(updates, "alarm_enabled = $"+strconv.Itoa(argPos))
		args = append(args, *req.AlarmEnabled)
		argPos++
	}
	if req.AlarmThreshold != nil {
		updates = append(updates, "alarm_threshold = $"+strconv.Itoa(argPos))
		args = append(args, *req.AlarmThreshold)
		argPos++
	}
	if req.AlarmOperator != nil {
		updates = append(updates, "alarm_operator = $"+strconv.Itoa(argPos))
		args = append(args, *req.AlarmOperator)
		argPos++
	}
	if req.AlarmPriority != nil {
		updates = append(updates, "alarm_priority = $"+strconv.Itoa(argPos))
		args = append(args, *req.AlarmPriority)
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
	query += " WHERE id = $" + strconv.Itoa(argPos) + " RETURNING id, gateway_id, code, alias, data_type, historize, historize_deadband, alarm_enabled, alarm_threshold, alarm_operator, alarm_priority, created_at"

	var tag models.Tag
	err = h.db.QueryRow(query, args...).Scan(
		&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType,
		&tag.Historize, &tag.HistorizeDeadband, &tag.AlarmEnabled,
		&tag.AlarmThreshold, &tag.AlarmOperator, &tag.AlarmPriority, &tag.CreatedAt,
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
	V      interface{} `json:"v"`
	Ts     int64       `json:"ts"`
	Q      int         `json:"q"`
}

// GetCurrentValue handles GET /api/tags/{id}/current
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "No current value available for this tag"})
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
