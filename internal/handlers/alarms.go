package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

type AlarmHandler struct {
	db         *sql.DB
	mqttClient MQTTClient
}

func NewAlarmHandler(db *sql.DB, mqttClient MQTTClient) *AlarmHandler {
	return &AlarmHandler{
		db:         db,
		mqttClient: mqttClient,
	}
}

// GetTagAlarmConfig returns the alarm configuration for a specific tag
// @Summary Get tag alarm configuration
// @Description Returns the array of alarm definitions configured for the given tag
// @Tags alarms
// @Produce json
// @Param id path int true "Tag ID"
// @Success 200 {array} models.AlarmDefinition
// @Failure 500 {object} map[string]string
// @Router /api/tags/{id}/alarms [get]
func (h *AlarmHandler) GetTagAlarmConfig(c *gin.Context) {
	tagID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	rows, err := h.db.Query(`
		SELECT id, tag_id, alarm_type, threshold, deadband, delay_seconds, severity, message, enabled, created_at, updated_at
		FROM alarm_definitions
		WHERE tag_id = $1
		ORDER BY id ASC
	`, tagID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query alarms"})
		return
	}
	defer rows.Close()

	var alarms []models.AlarmDefinition
	for rows.Next() {
		var a models.AlarmDefinition
		if err := rows.Scan(
			&a.ID, &a.TagID, &a.AlarmType, &a.Threshold, &a.Deadband, &a.DelaySeconds,
			&a.Severity, &a.Message, &a.Enabled, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan alarm definition"})
			return
		}
		alarms = append(alarms, a)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query iteration error"})
		return
	}

	// Ensure we return an empty array instead of null
	if alarms == nil {
		alarms = []models.AlarmDefinition{}
	}

	c.JSON(http.StatusOK, alarms)
}

// SaveTagAlarmConfig replaces all alarm configurations for a tag
// @Summary Save tag alarm configuration
// @Description Overwrites the alarm definitions for the given tag
// @Tags alarms
// @Accept json
// @Produce json
// @Param id path int true "Tag ID"
// @Param alarms body []models.AlarmDefinition true "List of alarm definitions"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// GetGatewayAlarmCounts returns the number of alarm definitions for each tag in a gateway
// @Summary Get gateway alarm counts
// @Description Returns a map of tag ID to alarm definition count
// @Tags alarms
// @Produce json
// @Param id path int true "Gateway ID"
// @Success 200 {object} map[string]int
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/gateways/{id}/alarms/count [get]
func (h *AlarmHandler) GetGatewayAlarmCounts(c *gin.Context) {
	gatewayIDStr := c.Param("id")
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gateway ID"})
		return
	}

	// Use the middleware helper for organization filtering
	orgFilter := middleware.GetOrgFilterForQuery(c)

	// Double-check gateway belongs to the org (or exists for global admin)
	var validGateway int
	var checkErr error

	if orgFilter == nil {
		// Global admin - just check gateway exists
		checkErr = h.db.QueryRow(`SELECT id FROM gateways WHERE id = $1`, gatewayID).Scan(&validGateway)
	} else {
		// Regular user - verify gateway belongs to their org
		checkErr = h.db.QueryRow(`
			SELECT g.id
			FROM gateways g
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			WHERE g.id = $1 AND s.org_id = $2
		`, gatewayID, *orgFilter).Scan(&validGateway)
	}

	if checkErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found or access denied"})
		return
	}

	rows, err := h.db.Query(`
		SELECT t.id, COUNT(a.id)
		FROM tags t
		JOIN alarm_definitions a ON t.id = a.tag_id
		WHERE t.gateway_id = $1
		GROUP BY t.id
	`, gatewayID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count alarms"})
		return
	}
	defer rows.Close()

	countsInt := make(map[int]int)
	for rows.Next() {
		var tagID, count int
		if err := rows.Scan(&tagID, &count); err == nil {
			countsInt[tagID] = count
		}
	}

	c.JSON(http.StatusOK, countsInt)
}

// GetAllAlarmCounts returns the number of alarm definitions for all tags in an organization
// @Summary Get all alarm counts
// @Description Returns a map of tag ID to alarm definition count for the organization
// @Tags alarms
// @Produce json
// @Success 200 {object} map[string]int
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/alarms/count/all [get]
func (h *AlarmHandler) GetAllAlarmCounts(c *gin.Context) {
	// Use the middleware helper for organization filtering
	orgFilter := middleware.GetOrgFilterForQuery(c)

	var rows *sql.Rows
	var err error

	if orgFilter == nil {
		// Global admin - get all alarms across all organizations
		rows, err = h.db.Query(`
			SELECT t.id, COUNT(a.id)
			FROM tags t
			JOIN alarm_definitions a ON t.id = a.tag_id
			GROUP BY t.id
		`)
	} else {
		// Regular user - filter by their organization
		rows, err = h.db.Query(`
			SELECT t.id, COUNT(a.id)
			FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas ar ON g.area_id = ar.id
			JOIN sites s ON ar.site_id = s.id
			JOIN alarm_definitions a ON t.id = a.tag_id
			WHERE s.org_id = $1
			GROUP BY t.id
		`, *orgFilter)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count alarms"})
		return
	}
	defer rows.Close()

	countsInt := make(map[int]int)
	for rows.Next() {
		var tagID, count int
		if err := rows.Scan(&tagID, &count); err == nil {
			countsInt[tagID] = count
		}
	}

	c.JSON(http.StatusOK, countsInt)
}

// SaveTagAlarmConfig replaces the alarm configuration for a specific tag
// @Summary Save tag alarm configuration
// @Description Updates the alarm rules for a given tag
// @Tags alarms
// @Accept json
// @Produce json
// @Param id path int true "Tag ID"
// @Param alarms body []models.AlarmDefinition true "Alarm Definitions"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/tags/{id}/alarms [put]
func (h *AlarmHandler) SaveTagAlarmConfig(c *gin.Context) {
	tagID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	// Check if user is global admin
	isGlobalAdmin := middleware.IsGlobalAdmin(c)

	// For regular users, verify tag belongs to their org
	if !isGlobalAdmin {
		orgFilter := middleware.GetOrgFilterForQuery(c)
		if orgFilter == nil || *orgFilter == -1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Unable to determine organization"})
			return
		}

		// Verify tag belongs to org
		var validTag int
		err := h.db.QueryRow(`
			SELECT t.id
			FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			WHERE t.id = $1 AND s.org_id = $2
		`, tagID, *orgFilter).Scan(&validTag)

		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found or not in organization"})
			return
		}
	} else {
		// Global admin: just verify tag exists
		var validTag int
		err := h.db.QueryRow(`SELECT id FROM tags WHERE id = $1`, tagID).Scan(&validTag)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
	}

	var alarms []models.AlarmDefinition
	if err := c.ShouldBindJSON(&alarms); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validTypes := map[string]bool{"bool_true": true, "bool_false": true, "high": true, "low": true, "high_high": true, "low_low": true}
	validSeverities := map[string]bool{"info": true, "warning": true, "critical": true}

	for _, a := range alarms {
		if !validTypes[a.AlarmType] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alarm_type: " + a.AlarmType})
			return
		}
		if !validSeverities[a.Severity] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid severity: " + a.Severity})
			return
		}
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// 0. Mark any currently active alarms for this tag as CLEARED
	// since their definitions are about to be deleted/re-created.
	_, err = tx.Exec(`
		UPDATE alarm_events
		SET status = 'CLEARED', clear_time = $1
		WHERE tag_id = $2 AND status IN ('ACTIVE', 'ACKNOWLEDGED')
	`, time.Now(), tagID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear existing active events"})
		return
	}

	// 1. Delete existing configurations for this tag
	_, err = tx.Exec(`DELETE FROM alarm_definitions WHERE tag_id = $1`, tagID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear old alarms"})
		return
	}

	// 2. Insert new ones
	stmt, err := tx.Prepare(`
		INSERT INTO alarm_definitions (tag_id, alarm_type, threshold, deadband, delay_seconds, severity, message, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare insert"})
		return
	}
	defer stmt.Close()

	for _, a := range alarms {
		_, err := stmt.Exec(
			tagID, a.AlarmType, a.Threshold, a.Deadband, a.DelaySeconds,
			a.Severity, a.Message, a.Enabled,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert alarm definition"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// 3. Notify the driver to reload alarm definitions
	if h.mqttClient != nil {
		var gatewayID int
		if err := h.db.QueryRow("SELECT gateway_id FROM tags WHERE id = $1", tagID).Scan(&gatewayID); err == nil {
			topic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
			log.Printf("[ALARM-HANDLER] Publishing reload to %s for tag %d", topic, tagID)
			h.mqttClient.PublishWithQoS(topic, "alarm-reload", 1, false)
		} else {
			log.Printf("[ALARM-HANDLER] Failed to find gateway for tag %d: %v", tagID, err)
		}
	} else {
		log.Printf("[ALARM-HANDLER] MQTT client is nil, cannot notify driver")
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alarms saved successfully"})
}

// GetActiveAlarms returns all currently active unacknowledged or uncleared alarms
// @Summary Get active alarms
// @Description Returns list of active alarm events
// @Tags alarms
// @Produce json
// @Success 200 {array} models.AlarmEvent
// @Failure 500 {object} map[string]string
func (h *AlarmHandler) GetActiveAlarms(c *gin.Context) {
	orgFilter := middleware.GetOrgFilterForQuery(c)

	var rows *sql.Rows
	var err error

	if orgFilter == nil {
		// Global admin - get all alarms across all organizations
		rows, err = h.db.Query(`
			SELECT e.id, e.tag_id, e.definition_id, e.status, e.alarm_type, e.severity, e.message, e.value_at_trigger, e.trigger_time, e.clear_time, e.bg_ack_user, e.ack_time
			FROM alarm_events e
			WHERE e.status IN ('ACTIVE', 'ACKNOWLEDGED')
			ORDER BY e.trigger_time DESC
		`)
	} else {
		// Regular user - filter by their organization
		rows, err = h.db.Query(`
			SELECT e.id, e.tag_id, e.definition_id, e.status, e.alarm_type, e.severity, e.message, e.value_at_trigger, e.trigger_time, e.clear_time, e.bg_ack_user, e.ack_time
			FROM alarm_events e
			JOIN tags t ON e.tag_id = t.id
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			WHERE s.org_id = $1 AND e.status IN ('ACTIVE', 'ACKNOWLEDGED')
			ORDER BY e.trigger_time DESC
		`, *orgFilter)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query active alarms"})
		return
	}
	defer rows.Close()

	var events []models.AlarmEvent
	for rows.Next() {
		var e models.AlarmEvent
		if err := rows.Scan(
			&e.ID, &e.TagID, &e.DefinitionID, &e.Status, &e.AlarmType, &e.Severity,
			&e.Message, &e.ValueAtTrigger, &e.TriggerTime, &e.ClearTime, &e.BgAckUser, &e.AckTime,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan alarm event"})
			return
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query iteration error"})
		return
	}

	// Ensure we return an empty array instead of null
	if events == nil {
		events = []models.AlarmEvent{}
	}

	c.JSON(http.StatusOK, events)
}

// GetAlarmHistory returns historical alarm events
// @Summary Get alarm history
// @Description Returns paginated history of alarm events
// @Tags alarms
// @Produce json
// @Param limit query int false "Limit" default(100)
// @Param offset query int false "Offset" default(0)
// @Success 200 {array} models.AlarmEvent
// @Failure 500 {object} map[string]string
// @Router /api/alarms/history [get]
func (h *AlarmHandler) GetAlarmHistory(c *gin.Context) {
	orgFilter := middleware.GetOrgFilterForQuery(c)

	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	var rows *sql.Rows
	var err error

	if orgFilter == nil {
		// Global admin - get all alarm history across all organizations
		rows, err = h.db.Query(`
			SELECT e.id, e.tag_id, e.definition_id, e.status, e.alarm_type, e.severity, e.message, e.value_at_trigger, e.trigger_time, e.clear_time, e.bg_ack_user, e.ack_time
			FROM alarm_events e
			ORDER BY e.trigger_time DESC
			LIMIT $1 OFFSET $2
		`, limit, offset)
	} else {
		// Regular user - filter by their organization
		rows, err = h.db.Query(`
			SELECT e.id, e.tag_id, e.definition_id, e.status, e.alarm_type, e.severity, e.message, e.value_at_trigger, e.trigger_time, e.clear_time, e.bg_ack_user, e.ack_time
			FROM alarm_events e
			JOIN tags t ON e.tag_id = t.id
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			WHERE s.org_id = $1
			ORDER BY e.trigger_time DESC
			LIMIT $2 OFFSET $3
		`, *orgFilter, limit, offset)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query alarm history"})
		return
	}
	defer rows.Close()

	var events []models.AlarmEvent
	for rows.Next() {
		var e models.AlarmEvent
		if err := rows.Scan(
			&e.ID, &e.TagID, &e.DefinitionID, &e.Status, &e.AlarmType, &e.Severity,
			&e.Message, &e.ValueAtTrigger, &e.TriggerTime, &e.ClearTime, &e.BgAckUser, &e.AckTime,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan alarm event"})
			return
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query iteration error"})
		return
	}

	// Ensure we return an empty array instead of null
	if events == nil {
		events = []models.AlarmEvent{}
	}

	c.JSON(http.StatusOK, events)
}

// AcknowledgeAlarm marks an active alarm as acknowledged
// @Summary Acknowledge an alarm
// @Description Sets an active alarm to ACKNOWLEDGED state
// @Tags alarms
// @Produce json
// @Param id path int true "Alarm Event ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/alarms/{id}/ack [post]
func (h *AlarmHandler) AcknowledgeAlarm(c *gin.Context) {
	eventID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid alarm ID"})
		return
	}

	username := "System Admin" // Ideally from auth JWT in a real system
	if u, exists := c.Get("username"); exists {
		username = u.(string)
	}

	result, err := h.db.Exec(`
		UPDATE alarm_events
		SET status = 'ACKNOWLEDGED',
		    bg_ack_user = $1,
		    ack_time = $2
		WHERE id = $3
		  AND ack_time IS NULL
		  AND clear_time IS NULL
	`, username, time.Now(), eventID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to acknowledge alarm"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Alarm not found or already acknowledged"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alarm acknowledged successfully"})
}

// DeleteAlarmHistory deletes an alarm event from history
// @Summary Delete alarm history
// @Description Deletes an alarm event by ID
// @Tags alarms
// @Produce json
// @Param id path int true "Alarm Event ID"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/alarms/history/{id} [delete]
func (h *AlarmHandler) DeleteAlarmHistory(c *gin.Context) {
	eventID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	// Check if user is global admin
	isGlobalAdmin := middleware.IsGlobalAdmin(c)

	var result sql.Result
	if isGlobalAdmin {
		// Global admin - can delete any alarm event
		result, err = h.db.Exec(`DELETE FROM alarm_events WHERE id = $1`, eventID)
	} else {
		// Regular user - can only delete events from their org
		orgFilter := middleware.GetOrgFilterForQuery(c)
		if orgFilter == nil || *orgFilter == -1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Unable to determine organization"})
			return
		}

		// Verify the event belongs to this org before deleting
		result, err = h.db.Exec(`
			DELETE FROM alarm_events
			WHERE id = $1 AND tag_id IN (
				SELECT t.id FROM tags t
				JOIN gateways g ON t.gateway_id = g.id
				JOIN areas a ON g.area_id = a.id
				JOIN sites s ON a.site_id = s.id
				WHERE s.org_id = $2
			)
		`, eventID, *orgFilter)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete alarm history"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Alarm not found or not in organization"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alarm history deleted successfully"})
}

// DeleteAllAlarmHistory deletes all alarm events for the organization from history
// @Summary Delete all alarm history
// @Description Deletes all alarm events for the current organization
// @Tags alarms
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/alarms/history/all [delete]
func (h *AlarmHandler) DeleteAllAlarmHistory(c *gin.Context) {
	// Check if user is global admin
	isGlobalAdmin := middleware.IsGlobalAdmin(c)

	var result sql.Result
	var err error

	if isGlobalAdmin {
		// Global admin - delete all alarm events
		result, err = h.db.Exec(`DELETE FROM alarm_events`)
	} else {
		// Regular user - only delete events from their org
		orgFilter := middleware.GetOrgFilterForQuery(c)
		if orgFilter == nil || *orgFilter == -1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Unable to determine organization"})
			return
		}

		result, err = h.db.Exec(`
			DELETE FROM alarm_events
			WHERE tag_id IN (
				SELECT t.id FROM tags t
				JOIN gateways g ON t.gateway_id = g.id
				JOIN areas a ON g.area_id = a.id
				JOIN sites s ON a.site_id = s.id
				WHERE s.org_id = $1
			)
		`, *orgFilter)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear alarm history"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	c.JSON(http.StatusOK, gin.H{
		"message":       "Alarm history cleared successfully",
		"rows_affected": rowsAffected,
	})
}
