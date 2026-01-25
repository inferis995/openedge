package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// AlarmsHandler handles alarm-related HTTP requests
type AlarmsHandler struct {
	db         *sql.DB
	mqttClient MQTTClient
}

// NewAlarmsHandler creates a new alarms handler
func NewAlarmsHandler(db *sql.DB, mqttClient MQTTClient) *AlarmsHandler {
	return &AlarmsHandler{
		db:         db,
		mqttClient: mqttClient,
	}
}

// AcknowledgeCommand represents the MQTT command for acknowledge
type AcknowledgeCommand struct {
	AlarmID int `json:"alarm_id"`
}

// AcknowledgeResponse represents the response for acknowledge endpoint
type AcknowledgeResponse struct {
	ID             int        `json:"id"`
	TagID          int        `json:"tag_id"`
	State          string     `json:"state"`
	Message        string     `json:"message"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	AcknowledgedAt time.Time  `json:"acknowledged_at"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty"`
}

// Acknowledge handles POST /api/alarms/{id}/acknowledge
// @Summary Acknowledge an alarm
// @Description Acknowledge an active alarm
// @Tags alarms
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Alarm ID"
// @Success 200 {object} AcknowledgeResponse
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 404 {object} map[string]string "Alarm not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/alarms/{id}/acknowledge [post]
func (h *AlarmsHandler) Acknowledge(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alarm ID"})
		return
	}

	// First, check if alarm exists and is in ACTIVE state
	var alarm models.Alarm
	query := `
		SELECT id, tag_id, state, message, triggered_at, acknowledged_at, cleared_at
		FROM alarms
		WHERE id = $1
	`
	err = h.db.QueryRow(query, id).Scan(
		&alarm.ID,
		&alarm.TagID,
		&alarm.State,
		&alarm.Message,
		&alarm.TriggeredAt,
		&alarm.AcknowledgedAt,
		&alarm.ClearedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Alarm not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query alarm"})
		return
	}

	// Check if alarm is in ACTIVE state
	if alarm.State != "ACTIVE" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Alarm is not in ACTIVE state", "current_state": alarm.State})
		return
	}

	// Update alarm to ACKNOWLEDGED state
	acknowledgedAt := time.Now()
	updateQuery := `
		UPDATE alarms
		SET state = $1, acknowledged_at = $2
		WHERE id = $3
		RETURNING id, tag_id, state, message, triggered_at, acknowledged_at, cleared_at
	`

	var updatedAlarm models.Alarm
	err = h.db.QueryRow(updateQuery, "ACKNOWLEDGED", acknowledgedAt, id).Scan(
		&updatedAlarm.ID,
		&updatedAlarm.TagID,
		&updatedAlarm.State,
		&updatedAlarm.Message,
		&updatedAlarm.TriggeredAt,
		&updatedAlarm.AcknowledgedAt,
		&updatedAlarm.ClearedAt,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to acknowledge alarm"})
		return
	}

	// Publish acknowledge command to MQTT for engine-alarm service
	if h.mqttClient != nil {
		ackCmd := AcknowledgeCommand{AlarmID: id}
		cmdJSON, _ := json.Marshal(ackCmd)
		topic := "sys/command/acknowledge"
		if err := h.mqttClient.Publish(topic, string(cmdJSON)); err != nil {
			// Log error but don't fail the request - database is already updated
			// The engine-alarm service will sync state on next alarm check
		}
	}

	response := AcknowledgeResponse{
		ID:             updatedAlarm.ID,
		TagID:          updatedAlarm.TagID,
		State:          updatedAlarm.State,
		Message:        updatedAlarm.Message,
		TriggeredAt:    updatedAlarm.TriggeredAt,
		AcknowledgedAt: *updatedAlarm.AcknowledgedAt,
		ClearedAt:      updatedAlarm.ClearedAt,
	}

	c.JSON(http.StatusOK, response)
}

// AlarmListItem represents an alarm with tag info for listing
type AlarmListItem struct {
	ID             int        `json:"id"`
	TagID          int        `json:"tag_id"`
	TagAlias       string     `json:"tag_alias,omitempty"`
	TagCode        string     `json:"tag_code,omitempty"`
	State          string     `json:"state"`
	Message        string     `json:"message"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty"`
}

// List handles GET /api/alarms
// @Summary List alarms
// @Description Get alarms with optional filters and pagination
// @Tags alarms
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param tag_id query int false "Filter by tag ID"
// @Param state query string false "Filter by state (ACTIVE, RTN, ACKNOWLEDGED, CLEAR)"
// @Param limit query int false "Limit number of results (default 100)"
// @Param offset query int false "Offset for pagination (default 0)"
// @Success 200 {array} AlarmListItem
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/alarms [get]
func (h *AlarmsHandler) List(c *gin.Context) {
	// Get organization ID from middleware context
	orgIDStr, exists := c.Get("organization_id")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Organization ID not provided"})
		return
	}
	orgID := orgIDStr.(int)

	// Parse query parameters
	tagIDParam := c.Query("tag_id")
	stateFilter := c.Query("state")
	limitParam := c.DefaultQuery("limit", "100")
	offsetParam := c.DefaultQuery("offset", "0")

	// Parse pagination parameters
	limit, err := strconv.Atoi(limitParam)
	if err != nil || limit <= 0 {
		limit = 100
	}
	offset, err := strconv.Atoi(offsetParam)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Build query with multi-tenant filtering
	var args []interface{}
	// argIdx tracks the next parameter index ($1, $2, etc.)
	argIdx := 1
	args = append(args, orgID) // $1 is always orgID

	// Initial query with correct placeholder for orgID ($1)
	baseQuery := fmt.Sprintf(`
		SELECT DISTINCT a.id, a.tag_id, COALESCE(t.alias, '') as tag_alias,
		       COALESCE(t.code, '') as tag_code, a.state,
		       COALESCE(a.message, '') as message, a.triggered_at,
		       a.acknowledged_at, a.cleared_at
		FROM alarms a
		LEFT JOIN tags t ON t.id = a.tag_id
		LEFT JOIN gateways g ON t.gateway_id = g.id
		LEFT JOIN areas ar ON g.area_id = ar.id
		LEFT JOIN sites s ON ar.site_id = s.id
		WHERE s.org_id = $%d`, argIdx)
	argIdx++

	// Add tag_id filter if provided
	if tagIDParam != "" {
		tagID, err := strconv.Atoi(tagIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag_id parameter"})
			return
		}
		baseQuery += fmt.Sprintf(" AND a.tag_id = $%d", argIdx)
		args = append(args, tagID)
		argIdx++
	}

	// Add state filter if provided
	if stateFilter != "" {
		// Validate state values
		validStates := map[string]bool{
			"ACTIVE":       true,
			"RTN":          true,
			"ACKNOWLEDGED": true,
			"CLEAR":        true,
			"active":       true,
			"rtn":          true,
			"acknowledged": true,
			"clear":        true,
		}
		if !validStates[stateFilter] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid state parameter. Must be one of: ACTIVE, RTN, ACKNOWLEDGED, CLEAR"})
			return
		}
		baseQuery += fmt.Sprintf(" AND UPPER(a.state) = $%d", argIdx)
		args = append(args, strings.ToUpper(stateFilter))
		argIdx++
	}

	// Add ordering and pagination
	query := baseQuery + fmt.Sprintf(" ORDER BY a.triggered_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query alarms"})
		return
	}
	defer rows.Close()

	var alarms []AlarmListItem
	for rows.Next() {
		var alarm AlarmListItem
		if err := rows.Scan(&alarm.ID, &alarm.TagID, &alarm.TagAlias, &alarm.TagCode, &alarm.State,
			&alarm.Message, &alarm.TriggeredAt, &alarm.AcknowledgedAt, &alarm.ClearedAt); err != nil {
			continue
		}
		alarms = append(alarms, alarm)
	}

	if alarms == nil {
		alarms = []AlarmListItem{}
	}

	c.JSON(http.StatusOK, alarms)
}
