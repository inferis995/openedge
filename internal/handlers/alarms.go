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
