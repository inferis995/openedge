package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/i3x"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// I3XHandler exposes OpenEdge data via the CESMII i3X Access API standard.
//
// Equipment hierarchy:
//
//	Organization  → i3X Assembly  (id: "org-{id}")
//	  Site        → i3X Assembly  (id: "site-{id}")
//	    Area      → i3X Assembly  (id: "area-{id}")
//	      Gateway → i3X Equipment (id: "gw-{id}")
//
// Data points:
//
//	Tag → i3X Property (id: "tag-{id}"), current value pulled from Redis
type I3XHandler struct {
	db          *sql.DB
	mqttClient  MQTTClient
	redisClient RedisClient
}

// NewI3XHandler creates a new i3X Access API handler
func NewI3XHandler(db *sql.DB, mqttClient MQTTClient, redisClient RedisClient) *I3XHandler {
	return &I3XHandler{db: db, mqttClient: mqttClient, redisClient: redisClient}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func gwEquipmentID(id int) string  { return fmt.Sprintf("gw-%d", id) }
func tagPropertyID(id int) string  { return fmt.Sprintf("tag-%d", id) }
func alarmEventID(id int) string   { return fmt.Sprintf("alarm-%d", id) }
func areaAssemblyID(id int) string { return fmt.Sprintf("area-%d", id) }

func parseGWID(s string) (int, bool) {
	s = strings.TrimPrefix(s, "gw-")
	id, err := strconv.Atoi(s)
	return id, err == nil
}

func parseTagID(s string) (int, bool) {
	s = strings.TrimPrefix(s, "tag-")
	id, err := strconv.Atoi(s)
	return id, err == nil
}

// orgFilterClause returns the SQL WHERE fragment and args for organization filtering
func (h *I3XHandler) orgFilterClause(c *gin.Context) (string, []interface{}) {
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter == nil {
		return "1=1", nil
	}
	return "s.org_id = $1", []interface{}{*orgFilter}
}

// currentValueFromRedis reads the current tag value from Redis (best-effort)
func (h *I3XHandler) currentValueFromRedis(tagID int) *i3x.PropertyValue {
	if h.redisClient == nil {
		return nil
	}
	raw, err := h.redisClient.Get(fmt.Sprintf("realtime:%d", tagID))
	if err != nil {
		return nil
	}
	var payload struct {
		V  interface{} `json:"v"`
		Ts int64       `json:"ts"`
		Q  int         `json:"q"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	ts := time.Unix(0, payload.Ts*int64(time.Millisecond))
	if payload.Ts == 0 {
		ts = time.Now()
	}
	return &i3x.PropertyValue{
		Value:     payload.V,
		Quality:   i3x.MapQuality(payload.Q, payload.V != nil),
		Timestamp: ts,
	}
}

// ─── Equipment endpoints ─────────────────────────────────────────────────────

// ListEquipment handles GET /api/i3x/v1/equipment
// Returns all gateways as i3X Equipment objects filtered by organization.
func (h *I3XHandler) ListEquipment(c *gin.Context) {
	clause, args := h.orgFilterClause(c)

	query := fmt.Sprintf(`
		SELECT g.id, g.name, g.driver_type, g.scan_rate_ms, g.enabled,
		       a.id as area_id, a.name as area_name,
		       s.id as site_id, s.name as site_name,
		       o.id as org_id, o.name as org_name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE %s
		ORDER BY o.name, s.name, a.name, g.name`, clause)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		log.Printf("[i3X] ListEquipment query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query equipment"})
		return
	}
	defer rows.Close()

	var items []i3x.Equipment
	for rows.Next() {
		var (
			gwID, areaID, siteID, orgID int
			gwName, driverType          string
			scanRateMs                  int
			enabled                     bool
			areaName, siteName, orgName string
		)
		if err := rows.Scan(
			&gwID, &gwName, &driverType, &scanRateMs, &enabled,
			&areaID, &areaName,
			&siteID, &siteName,
			&orgID, &orgName,
		); err != nil {
			log.Printf("[i3X] ListEquipment scan error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_ERROR", "message": "Failed to read equipment"})
			return
		}
		items = append(items, i3x.Equipment{
			ID:          gwEquipmentID(gwID),
			Name:        gwName,
			Type:        i3x.EquipmentTypeEquipment,
			Description: driverType,
			ParentID:    areaAssemblyID(areaID),
			Path:        fmt.Sprintf("%s / %s / %s / %s", orgName, siteName, areaName, gwName),
			Attributes: map[string]interface{}{
				"driver_type":  driverType,
				"scan_rate_ms": scanRateMs,
				"enabled":      enabled,
			},
		})
	}
	if items == nil {
		items = []i3x.Equipment{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.Equipment]{Items: items, Total: len(items)})
}

// GetEquipment handles GET /api/i3x/v1/equipment/:id
// Returns a single gateway in i3X Equipment format.
func (h *I3XHandler) GetEquipment(c *gin.Context) {
	gwID, ok := parseGWID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Equipment ID must be in the form gw-{n}"})
		return
	}

	var (
		gwName, driverType          string
		scanRateMs                  int
		enabled                     bool
		areaID, siteID, orgID       int
		areaName, siteName, orgName string
	)
	err := h.db.QueryRow(`
		SELECT g.name, g.driver_type, g.scan_rate_ms, g.enabled,
		       a.id, a.name, s.id, s.name, o.id, o.name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1`, gwID).Scan(
		&gwName, &driverType, &scanRateMs, &enabled,
		&areaID, &areaName, &siteID, &siteName, &orgID, &orgName,
	)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Equipment not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query equipment"})
		return
	}

	// Org isolation for non-admins
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil && *orgFilter != orgID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
		return
	}

	c.JSON(http.StatusOK, i3x.Equipment{
		ID:          gwEquipmentID(gwID),
		Name:        gwName,
		Type:        i3x.EquipmentTypeEquipment,
		Description: driverType,
		ParentID:    areaAssemblyID(areaID),
		Path:        fmt.Sprintf("%s / %s / %s / %s", orgName, siteName, areaName, gwName),
		Attributes: map[string]interface{}{
			"driver_type":  driverType,
			"scan_rate_ms": scanRateMs,
			"enabled":      enabled,
		},
	})
}

// ─── Properties on Equipment ─────────────────────────────────────────────────

// ListEquipmentProperties handles GET /api/i3x/v1/equipment/:id/properties
// Returns all tags for a gateway in i3X Property format, with current values.
func (h *I3XHandler) ListEquipmentProperties(c *gin.Context) {
	gwID, ok := parseGWID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Equipment ID must be in the form gw-{n}"})
		return
	}

	// Verify gateway exists and enforce org isolation
	var ownerOrgID int
	err := h.db.QueryRow(`
		SELECT s.org_id FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE g.id = $1`, gwID).Scan(&ownerOrgID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Equipment not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to verify equipment"})
		return
	}
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil && *orgFilter != ownerOrgID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
		return
	}

	rows, err := h.db.Query(`
		SELECT id, alias, data_type, historize
		FROM tags WHERE gateway_id = $1
		ORDER BY sort_order ASC, id ASC`, gwID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query properties"})
		return
	}
	defer rows.Close()

	var items []i3x.Property
	for rows.Next() {
		var tagID int
		var alias, dataType string
		var historize bool
		if err := rows.Scan(&tagID, &alias, &dataType, &historize); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_ERROR", "message": "Failed to read property"})
			return
		}
		items = append(items, i3x.Property{
			ID:          tagPropertyID(tagID),
			Name:        alias,
			EquipmentID: gwEquipmentID(gwID),
			DataType:    i3x.MapDataType(dataType),
			Historize:   historize,
			Current:     h.currentValueFromRedis(tagID),
		})
	}
	if items == nil {
		items = []i3x.Property{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.Property]{Items: items, Total: len(items)})
}

// GetEquipmentProperty handles GET /api/i3x/v1/equipment/:id/properties/:propId
func (h *I3XHandler) GetEquipmentProperty(c *gin.Context) {
	gwID, ok := parseGWID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Equipment ID must be in the form gw-{n}"})
		return
	}
	tagID, ok := parseTagID(c.Param("propId"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Property ID must be in the form tag-{n}"})
		return
	}

	var alias, dataType string
	var historize bool
	var actualGWID int
	err := h.db.QueryRow(`
		SELECT alias, data_type, historize, gateway_id
		FROM tags WHERE id = $1`, tagID).Scan(&alias, &dataType, &historize, &actualGWID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Property not found on this equipment"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query property"})
		return
	}
	if actualGWID != gwID {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Property not found on this equipment"})
		return
	}

	c.JSON(http.StatusOK, i3x.Property{
		ID:          tagPropertyID(tagID),
		Name:        alias,
		EquipmentID: gwEquipmentID(gwID),
		DataType:    i3x.MapDataType(dataType),
		Historize:   historize,
		Current:     h.currentValueFromRedis(tagID),
	})
}

// ─── Properties (flat, cross-equipment) ─────────────────────────────────────

// ListProperties handles GET /api/i3x/v1/properties
// Returns all tags visible to the caller, with current values from Redis.
func (h *I3XHandler) ListProperties(c *gin.Context) {
	clause, args := h.orgFilterClause(c)

	query := fmt.Sprintf(`
		SELECT t.id, t.alias, t.data_type, t.historize, t.gateway_id
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE %s
		ORDER BY t.sort_order ASC, t.id ASC`, clause)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		log.Printf("[i3X] ListProperties query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query properties"})
		return
	}
	defer rows.Close()

	var items []i3x.Property
	for rows.Next() {
		var tagID, gwID int
		var alias, dataType string
		var historize bool
		if err := rows.Scan(&tagID, &alias, &dataType, &historize, &gwID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_ERROR", "message": "Failed to read property"})
			return
		}
		items = append(items, i3x.Property{
			ID:          tagPropertyID(tagID),
			Name:        alias,
			EquipmentID: gwEquipmentID(gwID),
			DataType:    i3x.MapDataType(dataType),
			Historize:   historize,
			Current:     h.currentValueFromRedis(tagID),
		})
	}
	if items == nil {
		items = []i3x.Property{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.Property]{Items: items, Total: len(items)})
}

// GetProperty handles GET /api/i3x/v1/properties/:id
func (h *I3XHandler) GetProperty(c *gin.Context) {
	tagID, ok := parseTagID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Property ID must be in the form tag-{n}"})
		return
	}

	var alias, dataType string
	var historize bool
	var gwID, ownerOrgID int
	err := h.db.QueryRow(`
		SELECT t.alias, t.data_type, t.historize, t.gateway_id, s.org_id
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id = $1`, tagID).Scan(&alias, &dataType, &historize, &gwID, &ownerOrgID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Property not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query property"})
		return
	}
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil && *orgFilter != ownerOrgID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
		return
	}

	c.JSON(http.StatusOK, i3x.Property{
		ID:          tagPropertyID(tagID),
		Name:        alias,
		EquipmentID: gwEquipmentID(gwID),
		DataType:    i3x.MapDataType(dataType),
		Historize:   historize,
		Current:     h.currentValueFromRedis(tagID),
	})
}

// WritePropertyValue handles PUT /api/i3x/v1/properties/:id/value
// Sends a write command via MQTT to the owning gateway driver.
// Requires the caller to have the i3x_write permission (admin always allowed).
func (h *I3XHandler) WritePropertyValue(c *gin.Context) {
	if !middleware.HasI3xWrite(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "i3X write permission required"})
		return
	}

	tagID, ok := parseTagID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Property ID must be in the form tag-{n}"})
		return
	}

	var req i3x.WritePropertyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	var code, dataType string
	var gwID, ownerOrgID int
	err := h.db.QueryRow(`
		SELECT t.code, t.data_type, t.gateway_id, s.org_id
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id = $1`, tagID).Scan(&code, &dataType, &gwID, &ownerOrgID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Property not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query property"})
		return
	}
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil && *orgFilter != ownerOrgID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
		return
	}

	if h.mqttClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "MQTT_UNAVAILABLE", "message": "MQTT broker not connected"})
		return
	}

	cmd := struct {
		TagID    int         `json:"tag_id"`
		Code     string      `json:"code"`
		Value    interface{} `json:"value"`
		DataType string      `json:"data_type"`
	}{TagID: tagID, Code: code, Value: req.Value, DataType: dataType}

	payload, _ := json.Marshal(cmd)
	topic := fmt.Sprintf("cmd/write/%d", gwID)
	if err := h.mqttClient.Publish(topic, string(payload)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "PUBLISH_ERROR", "message": "Failed to send write command"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Write command sent"})
}

// ─── Alarms ──────────────────────────────────────────────────────────────────

// ListAlarms handles GET /api/i3x/v1/alarms
// Returns currently active (non-cleared) alarm events in i3X format.
func (h *I3XHandler) ListAlarms(c *gin.Context) {
	clause, args := h.orgFilterClause(c)

	query := fmt.Sprintf(`
		SELECT e.id, e.tag_id, e.status, e.alarm_type, e.severity,
		       e.message, e.value_at_trigger, e.trigger_time,
		       e.clear_time, e.bg_ack_user, e.ack_time,
		       t.gateway_id, t.alias AS tag_name, g.name AS gw_name
		FROM alarm_events e
		JOIN tags t ON e.tag_id = t.id
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE %s AND e.status IN ('ACTIVE', 'ACKNOWLEDGED')
		ORDER BY e.trigger_time DESC`, clause)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		log.Printf("[i3X] ListAlarms query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query alarms"})
		return
	}
	defer rows.Close()

	items := h.scanAlarmRows(c, rows)
	if items == nil {
		items = []i3x.Alarm{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.Alarm]{Items: items, Total: len(items)})
}

// ListAlarmHistory handles GET /api/i3x/v1/alarms/history
// Returns paginated alarm event history in i3X format.
func (h *I3XHandler) ListAlarmHistory(c *gin.Context) {
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	clause, args := h.orgFilterClause(c)
	// append LIMIT / OFFSET as next positional args
	limitPos := len(args) + 1
	offsetPos := limitPos + 1
	args = append(args, limit, offset)

	query := fmt.Sprintf(`
		SELECT e.id, e.tag_id, e.status, e.alarm_type, e.severity,
		       e.message, e.value_at_trigger, e.trigger_time,
		       e.clear_time, e.bg_ack_user, e.ack_time,
		       t.gateway_id, t.alias AS tag_name, g.name AS gw_name
		FROM alarm_events e
		JOIN tags t ON e.tag_id = t.id
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE %s
		ORDER BY e.trigger_time DESC
		LIMIT $%d OFFSET $%d`, clause, limitPos, offsetPos)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		log.Printf("[i3X] ListAlarmHistory query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query alarm history"})
		return
	}
	defer rows.Close()

	items := h.scanAlarmRows(c, rows)
	if items == nil {
		items = []i3x.Alarm{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.Alarm]{Items: items, Total: len(items)})
}

// scanAlarmRows is a shared helper that reads alarm_events rows into i3X Alarm structs
func (h *I3XHandler) scanAlarmRows(c *gin.Context, rows *sql.Rows) []i3x.Alarm {
	var items []i3x.Alarm
	for rows.Next() {
		var (
			evtID, tagID, gwID int
			status, alarmType  string
			severity, message  string
			valueAtTrigger     sql.NullString
			triggerTime        time.Time
			clearTime, ackTime sql.NullTime
			bgAckUser          sql.NullString
			tagName, gwName    string
		)
		if err := rows.Scan(
			&evtID, &tagID, &status, &alarmType, &severity,
			&message, &valueAtTrigger, &triggerTime,
			&clearTime, &bgAckUser, &ackTime,
			&gwID, &tagName, &gwName,
		); err != nil {
			log.Printf("[i3X] scanAlarmRows scan error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_ERROR", "message": "Failed to read alarm"})
			return nil
		}

		alarm := i3x.Alarm{
			ID:            alarmEventID(evtID),
			PropertyID:    tagPropertyID(tagID),
			PropertyName:  tagName,
			EquipmentID:   gwEquipmentID(gwID),
			EquipmentName: gwName,
			Severity:      i3x.MapSeverity(severity),
			Status:        i3x.MapStatus(status),
			AlarmType:     alarmType,
			Message:       message,
			TriggerTime:   triggerTime,
		}
		if valueAtTrigger.Valid {
			alarm.Value = valueAtTrigger.String
		}
		if clearTime.Valid {
			t := clearTime.Time
			alarm.ClearTime = &t
		}
		if ackTime.Valid {
			t := ackTime.Time
			alarm.AckTime = &t
		}
		if bgAckUser.Valid {
			alarm.AckUser = &bgAckUser.String
		}
		items = append(items, alarm)
	}
	return items
}
