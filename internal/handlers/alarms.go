package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
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
	State          string     `json:"state"`
	Message        string     `json:"message"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty"`
}

// List handles GET /api/alarms
// @Summary List alarms
// @Description Get alarms with optional state filter
// @Tags alarms
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param state query string false "Filter by state (active, acknowledged, cleared, rtn, all)"
// @Success 200 {array} AlarmListItem
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/alarms [get]
func (h *AlarmsHandler) List(c *gin.Context) {
	stateFilter := c.Query("state")

	var query string
	var args []interface{}

	baseQuery := `
		SELECT a.id, a.tag_id, COALESCE(t.alias, '') as tag_alias, a.state, 
		       COALESCE(a.message, '') as message, a.triggered_at, a.acknowledged_at, a.cleared_at
		FROM alarms a
		LEFT JOIN tags t ON t.id = a.tag_id
	`

	switch stateFilter {
	case "active":
		query = baseQuery + " WHERE LOWER(a.state) = 'active' ORDER BY a.triggered_at DESC"
	case "acknowledged":
		query = baseQuery + " WHERE LOWER(a.state) = 'acknowledged' ORDER BY a.triggered_at DESC"
	case "cleared":
		query = baseQuery + " WHERE LOWER(a.state) = 'cleared' ORDER BY a.triggered_at DESC"
	case "rtn":
		query = baseQuery + " WHERE LOWER(a.state) = 'rtn' ORDER BY a.triggered_at DESC"
	case "all", "":
		query = baseQuery + " ORDER BY a.triggered_at DESC LIMIT 100"
	default:
		query = baseQuery + " WHERE LOWER(a.state) = $1 ORDER BY a.triggered_at DESC"
		args = append(args, stateFilter)
	}

	rows, err := h.db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query alarms"})
		return
	}
	defer rows.Close()

	var alarms []AlarmListItem
	for rows.Next() {
		var alarm AlarmListItem
		if err := rows.Scan(&alarm.ID, &alarm.TagID, &alarm.TagAlias, &alarm.State,
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
