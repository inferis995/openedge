package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// ─── ID helpers ──────────────────────────────────────────────────────────────

func orgAssemblyID(id int) string  { return fmt.Sprintf("org-%d", id) }
func siteAssemblyID(id int) string { return fmt.Sprintf("site-%d", id) }
func areaAssemblyID(id int) string { return fmt.Sprintf("area-%d", id) }
func gwEquipmentID(id int) string  { return fmt.Sprintf("gw-%d", id) }
func tagPropertyID(id int) string  { return fmt.Sprintf("tag-%d", id) }
func alarmEventID(id int) string   { return fmt.Sprintf("alarm-%d", id) }

// paginationParams reads ?limit and ?offset from the request, clamps limit to
// a safe range (1..max), and returns the values. Default limit is 200 — a
// generous page for typical UIs that still bounds payload size for tenants
// with 10k+ tags.
func paginationParams(c *gin.Context, defaultLimit, maxLimit int) (int, int) {
	limit := defaultLimit
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxLimit {
				n = maxLimit
			}
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func parseOrgID(s string) (int, bool) {
	s = strings.TrimPrefix(s, "org-")
	id, err := strconv.Atoi(s)
	return id, err == nil
}

func parseSiteID(s string) (int, bool) {
	s = strings.TrimPrefix(s, "site-")
	id, err := strconv.Atoi(s)
	return id, err == nil
}

func parseAreaID(s string) (int, bool) {
	s = strings.TrimPrefix(s, "area-")
	id, err := strconv.Atoi(s)
	return id, err == nil
}

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
// Returns the full equipment hierarchy: org → site → area assemblies, then gateways.
func (h *I3XHandler) ListEquipment(c *gin.Context) {
	orgFilter := middleware.GetOrgFilterForQuery(c)
	var items []i3x.Equipment

	// 1. Organizations
	{
		var q string
		var args []interface{}
		if orgFilter != nil {
			q = `SELECT id, name FROM organizations WHERE id = $1 ORDER BY name`
			args = []interface{}{*orgFilter}
		} else {
			q = `SELECT id, name FROM organizations ORDER BY name`
		}
		rows, err := h.db.Query(q, args...)
		if err != nil {
			log.Printf("[i3X] ListEquipment orgs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query equipment"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				log.Printf("[i3X] ListEquipment orgs scan: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_ERROR", "message": "Failed to read equipment"})
				return
			}
			items = append(items, i3x.Equipment{
				ID:   orgAssemblyID(id),
				Name: name,
				Type: i3x.EquipmentTypeAssembly,
				Path: name,
			})
		}
	}

	// 2. Sites
	{
		var q string
		var args []interface{}
		if orgFilter != nil {
			q = `SELECT s.id, s.name, s.org_id, o.name
			     FROM sites s JOIN organizations o ON s.org_id = o.id
			     WHERE s.org_id = $1 ORDER BY o.name, s.name`
			args = []interface{}{*orgFilter}
		} else {
			q = `SELECT s.id, s.name, s.org_id, o.name
			     FROM sites s JOIN organizations o ON s.org_id = o.id
			     ORDER BY o.name, s.name`
		}
		rows, err := h.db.Query(q, args...)
		if err != nil {
			log.Printf("[i3X] ListEquipment sites: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query equipment"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var siteID, orgID int
			var siteName, orgName string
			if err := rows.Scan(&siteID, &siteName, &orgID, &orgName); err != nil {
				log.Printf("[i3X] ListEquipment sites scan: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_ERROR", "message": "Failed to read equipment"})
				return
			}
			items = append(items, i3x.Equipment{
				ID:       siteAssemblyID(siteID),
				Name:     siteName,
				Type:     i3x.EquipmentTypeAssembly,
				ParentID: orgAssemblyID(orgID),
				Path:     fmt.Sprintf("%s / %s", orgName, siteName),
			})
		}
	}

	// 3. Areas
	{
		var q string
		var args []interface{}
		if orgFilter != nil {
			q = `SELECT a.id, a.name, a.site_id, s.name, s.org_id, o.name
			     FROM areas a
			     JOIN sites s ON a.site_id = s.id
			     JOIN organizations o ON s.org_id = o.id
			     WHERE s.org_id = $1 ORDER BY o.name, s.name, a.name`
			args = []interface{}{*orgFilter}
		} else {
			q = `SELECT a.id, a.name, a.site_id, s.name, s.org_id, o.name
			     FROM areas a
			     JOIN sites s ON a.site_id = s.id
			     JOIN organizations o ON s.org_id = o.id
			     ORDER BY o.name, s.name, a.name`
		}
		rows, err := h.db.Query(q, args...)
		if err != nil {
			log.Printf("[i3X] ListEquipment areas: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query equipment"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var areaID, siteID, orgID int
			var areaName, siteName, orgName string
			if err := rows.Scan(&areaID, &areaName, &siteID, &siteName, &orgID, &orgName); err != nil {
				log.Printf("[i3X] ListEquipment areas scan: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_ERROR", "message": "Failed to read equipment"})
				return
			}
			items = append(items, i3x.Equipment{
				ID:       areaAssemblyID(areaID),
				Name:     areaName,
				Type:     i3x.EquipmentTypeAssembly,
				ParentID: siteAssemblyID(siteID),
				Path:     fmt.Sprintf("%s / %s / %s", orgName, siteName, areaName),
			})
		}
	}

	// 4. Gateways
	{
		var q string
		var args []interface{}
		if orgFilter != nil {
			q = `SELECT g.id, g.name, g.driver_type, g.scan_rate_ms, g.enabled,
			          a.id, a.name, s.id, s.name, o.id, o.name
			     FROM gateways g
			     JOIN areas a ON g.area_id = a.id
			     JOIN sites s ON a.site_id = s.id
			     JOIN organizations o ON s.org_id = o.id
			     WHERE s.org_id = $1
			     ORDER BY o.name, s.name, a.name, g.name`
			args = []interface{}{*orgFilter}
		} else {
			q = `SELECT g.id, g.name, g.driver_type, g.scan_rate_ms, g.enabled,
			          a.id, a.name, s.id, s.name, o.id, o.name
			     FROM gateways g
			     JOIN areas a ON g.area_id = a.id
			     JOIN sites s ON a.site_id = s.id
			     JOIN organizations o ON s.org_id = o.id
			     ORDER BY o.name, s.name, a.name, g.name`
		}
		rows, err := h.db.Query(q, args...)
		if err != nil {
			log.Printf("[i3X] ListEquipment gateways: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query equipment"})
			return
		}
		defer rows.Close()
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
				log.Printf("[i3X] ListEquipment gateways scan: %v", err)
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
	}

	if items == nil {
		items = []i3x.Equipment{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.Equipment]{Items: items, Total: len(items)})
}

// GetEquipment handles GET /api/i3x/v1/equipment/:id
// Accepts org-{n}, site-{n}, area-{n}, or gw-{n} IDs.
func (h *I3XHandler) GetEquipment(c *gin.Context) {
	rawID := c.Param("id")
	orgFilter := middleware.GetOrgFilterForQuery(c)

	switch {
	case strings.HasPrefix(rawID, "org-"):
		orgID, ok := parseOrgID(rawID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Organization ID must be in the form org-{n}"})
			return
		}
		if orgFilter != nil && *orgFilter != orgID {
			c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
			return
		}
		var name string
		if err := h.db.QueryRow(`SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&name); err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Organization not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query organization"})
			return
		}
		c.JSON(http.StatusOK, i3x.Equipment{
			ID:   rawID,
			Name: name,
			Type: i3x.EquipmentTypeAssembly,
			Path: name,
		})

	case strings.HasPrefix(rawID, "site-"):
		siteID, ok := parseSiteID(rawID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Site ID must be in the form site-{n}"})
			return
		}
		var siteName, orgName string
		var orgID int
		err := h.db.QueryRow(`
			SELECT s.name, o.id, o.name
			FROM sites s JOIN organizations o ON s.org_id = o.id
			WHERE s.id = $1`, siteID).Scan(&siteName, &orgID, &orgName)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Site not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query site"})
			return
		}
		if orgFilter != nil && *orgFilter != orgID {
			c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
			return
		}
		c.JSON(http.StatusOK, i3x.Equipment{
			ID:       rawID,
			Name:     siteName,
			Type:     i3x.EquipmentTypeAssembly,
			ParentID: orgAssemblyID(orgID),
			Path:     fmt.Sprintf("%s / %s", orgName, siteName),
		})

	case strings.HasPrefix(rawID, "area-"):
		areaID, ok := parseAreaID(rawID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Area ID must be in the form area-{n}"})
			return
		}
		var areaName, siteName, orgName string
		var siteID, orgID int
		err := h.db.QueryRow(`
			SELECT a.name, s.id, s.name, o.id, o.name
			FROM areas a
			JOIN sites s ON a.site_id = s.id
			JOIN organizations o ON s.org_id = o.id
			WHERE a.id = $1`, areaID).Scan(&areaName, &siteID, &siteName, &orgID, &orgName)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Area not found"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query area"})
			return
		}
		if orgFilter != nil && *orgFilter != orgID {
			c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
			return
		}
		c.JSON(http.StatusOK, i3x.Equipment{
			ID:       rawID,
			Name:     areaName,
			Type:     i3x.EquipmentTypeAssembly,
			ParentID: siteAssemblyID(siteID),
			Path:     fmt.Sprintf("%s / %s / %s", orgName, siteName, areaName),
		})

	case strings.HasPrefix(rawID, "gw-"):
		gwID, ok := parseGWID(rawID)
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
		if orgFilter != nil && *orgFilter != orgID {
			c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
			return
		}
		c.JSON(http.StatusOK, i3x.Equipment{
			ID:          rawID,
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

	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "ID must be one of: org-{n}, site-{n}, area-{n}, gw-{n}"})
	}
}

// ─── Properties on Equipment ─────────────────────────────────────────────────

// ListEquipmentProperties handles GET /api/i3x/v1/equipment/:id/properties
// Returns all tags for a gateway in i3X Property format, with current values.
// Only valid for gw-{n} IDs — assemblies do not have properties.
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

	// Paginate (a single gateway can carry thousands of tags).
	limit, offset := paginationParams(c, 500, 5000)
	rows, err := h.db.Query(`
		SELECT id, alias, data_type, historize
		FROM tags WHERE gateway_id = $1
		ORDER BY sort_order ASC, id ASC
		LIMIT $2 OFFSET $3`, gwID, limit, offset)
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
	var actualGWID, ownerOrgID int
	err := h.db.QueryRow(`
		SELECT t.alias, t.data_type, t.historize, t.gateway_id, s.org_id
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id = $1`, tagID).Scan(&alias, &dataType, &historize, &actualGWID, &ownerOrgID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Property not found on this equipment"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query property"})
		return
	}
	// Multi-tenant isolation: the tag's gateway must belong to the caller's org.
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil && *orgFilter != ownerOrgID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
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
// Optional query param: equipment_id=gw-{n} to filter by a specific gateway.
func (h *I3XHandler) ListProperties(c *gin.Context) {
	orgFilter := middleware.GetOrgFilterForQuery(c)
	// Paginate to avoid unbounded JSON for tenants with thousands of tags.
	limit, offset := paginationParams(c, 200, 5000)

	equipmentIDFilter := c.Query("equipment_id")
	var gwIDFilter *int
	if equipmentIDFilter != "" {
		if id, ok := parseGWID(equipmentIDFilter); ok {
			gwIDFilter = &id
		}
	}

	var q string
	var args []interface{}
	switch {
	case orgFilter != nil && gwIDFilter != nil:
		q = `SELECT t.id, t.alias, t.data_type, t.historize, t.gateway_id
		     FROM tags t
		     JOIN gateways g ON t.gateway_id = g.id
		     JOIN areas a ON g.area_id = a.id
		     JOIN sites s ON a.site_id = s.id
		     WHERE s.org_id = $1 AND t.gateway_id = $2
		     ORDER BY t.sort_order ASC, t.id ASC
		     LIMIT $3 OFFSET $4`
		args = []interface{}{*orgFilter, *gwIDFilter, limit, offset}
	case orgFilter != nil:
		q = `SELECT t.id, t.alias, t.data_type, t.historize, t.gateway_id
		     FROM tags t
		     JOIN gateways g ON t.gateway_id = g.id
		     JOIN areas a ON g.area_id = a.id
		     JOIN sites s ON a.site_id = s.id
		     WHERE s.org_id = $1
		     ORDER BY t.sort_order ASC, t.id ASC
		     LIMIT $2 OFFSET $3`
		args = []interface{}{*orgFilter, limit, offset}
	case gwIDFilter != nil:
		q = `SELECT t.id, t.alias, t.data_type, t.historize, t.gateway_id
		     FROM tags t
		     JOIN gateways g ON t.gateway_id = g.id
		     JOIN areas a ON g.area_id = a.id
		     JOIN sites s ON a.site_id = s.id
		     WHERE t.gateway_id = $1
		     ORDER BY t.sort_order ASC, t.id ASC
		     LIMIT $2 OFFSET $3`
		args = []interface{}{*gwIDFilter, limit, offset}
	default:
		q = `SELECT t.id, t.alias, t.data_type, t.historize, t.gateway_id
		     FROM tags t
		     JOIN gateways g ON t.gateway_id = g.id
		     JOIN areas a ON g.area_id = a.id
		     JOIN sites s ON a.site_id = s.id
		     ORDER BY t.sort_order ASC, t.id ASC
		     LIMIT $1 OFFSET $2`
		args = []interface{}{limit, offset}
	}

	rows, err := h.db.QueryContext(context.Background(), q, args...)
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

	// Engineering units in, raw out — see internal/handlers/write_scaling.go.
	// The i3X API reports values through the same scaled read path as the rest
	// of the product, so a caller writing back what it read must get the same
	// conversion the synoptic does.
	deviceValue, scaleErr := toDeviceValue(c.Request.Context(), h.db, tagID, req.Value)
	if scaleErr != nil {
		if errors.Is(scaleErr, ErrScalingUnknown) {
			c.JSON(http.StatusServiceUnavailable,
				gin.H{"code": "SCALING_UNAVAILABLE", "message": scaleErr.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "OUT_OF_RANGE", "message": scaleErr.Error()})
		return
	}

	cmd := struct {
		TagID    int         `json:"tag_id"`
		Code     string      `json:"code"`
		Value    interface{} `json:"value"`
		DataType string      `json:"data_type"`
	}{TagID: tagID, Code: code, Value: deviceValue, DataType: dataType}

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
	orgFilter := middleware.GetOrgFilterForQuery(c)

	var q string
	var args []interface{}
	baseSelect := `SELECT e.id, e.tag_id, e.status, e.alarm_type, e.severity,
		       e.message, e.value_at_trigger, e.trigger_time,
		       e.clear_time, e.bg_ack_user, e.ack_time,
		       t.gateway_id, t.alias AS tag_name, g.name AS gw_name
		FROM alarm_events e
		JOIN tags t ON e.tag_id = t.id
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id`

	if orgFilter != nil {
		q = baseSelect + ` WHERE s.org_id = $1 AND e.status IN ('ACTIVE', 'ACKNOWLEDGED') ORDER BY e.trigger_time DESC`
		args = []interface{}{*orgFilter}
	} else {
		q = baseSelect + ` WHERE e.status IN ('ACTIVE', 'ACKNOWLEDGED') ORDER BY e.trigger_time DESC`
	}

	rows, err := h.db.Query(q, args...)
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

	orgFilter := middleware.GetOrgFilterForQuery(c)

	baseSelect := `SELECT e.id, e.tag_id, e.status, e.alarm_type, e.severity,
		       e.message, e.value_at_trigger, e.trigger_time,
		       e.clear_time, e.bg_ack_user, e.ack_time,
		       t.gateway_id, t.alias AS tag_name, g.name AS gw_name
		FROM alarm_events e
		JOIN tags t ON e.tag_id = t.id
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id`

	var q string
	var args []interface{}
	if orgFilter != nil {
		q = baseSelect + ` WHERE s.org_id = $1 ORDER BY e.trigger_time DESC LIMIT $2 OFFSET $3`
		args = []interface{}{*orgFilter, limit, offset}
	} else {
		q = baseSelect + ` ORDER BY e.trigger_time DESC LIMIT $1 OFFSET $2`
		args = []interface{}{limit, offset}
	}

	rows, err := h.db.Query(q, args...)
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

// ─── History ─────────────────────────────────────────────────────────────────

// GetPropertyHistory handles GET /api/i3x/v1/properties/:id/history
// Returns raw time-series samples for a tag from the TimescaleDB historian.
// Query params: from (RFC3339), to (RFC3339), limit (int, default 500, max 5000).
func (h *I3XHandler) GetPropertyHistory(c *gin.Context) {
	tagID, ok := parseTagID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Property ID must be in the form tag-{n}"})
		return
	}

	// Org isolation: verify the tag belongs to the caller's org.
	var ownerOrgID int
	err := h.db.QueryRowContext(context.Background(), `
		SELECT s.org_id
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id = $1`, tagID).Scan(&ownerOrgID)
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

	// Default time range: last hour.
	from := time.Now().Add(-1 * time.Hour)
	to := time.Now()
	if v := c.Query("from"); v != "" {
		if t, err2 := time.Parse(time.RFC3339, v); err2 == nil {
			from = t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err2 := time.Parse(time.RFC3339, v); err2 == nil {
			to = t
		}
	}
	limit := 500
	if v := c.Query("limit"); v != "" {
		if n, err2 := strconv.Atoi(v); err2 == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	rows, err := h.db.QueryContext(context.Background(), `
		SELECT time, value FROM tag_history
		WHERE tag_id = $1 AND time >= $2 AND time <= $3
		ORDER BY time ASC LIMIT $4`, tagID, from, to, limit)
	if err != nil {
		log.Printf("[i3X] GetPropertyHistory query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query history"})
		return
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("[i3X] GetPropertyHistory rows.Close: %v", cerr)
		}
	}()

	var points []i3x.PropertyHistoryPoint
	for rows.Next() {
		var ts time.Time
		var val sql.NullFloat64
		if err2 := rows.Scan(&ts, &val); err2 != nil {
			continue
		}
		p := i3x.PropertyHistoryPoint{Timestamp: ts}
		if val.Valid {
			v := val.Float64
			p.Value = &v
		}
		points = append(points, p)
	}
	if points == nil {
		points = []i3x.PropertyHistoryPoint{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.PropertyHistoryPoint]{Items: points, Total: len(points)})
}

// ─── Equipment children ───────────────────────────────────────────────────────

// childSites returns the sites that belong to an organization.
func (h *I3XHandler) childSites(orgID int, parentID string) ([]i3x.Equipment, error) {
	var orgName string
	if err := h.db.QueryRowContext(context.Background(),
		`SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&orgName); err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(context.Background(),
		`SELECT id, name FROM sites WHERE org_id = $1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("[i3X] childSites rows.Close: %v", cerr)
		}
	}()
	var out []i3x.Equipment
	for rows.Next() {
		var id int
		var name string
		if err2 := rows.Scan(&id, &name); err2 != nil {
			continue
		}
		out = append(out, i3x.Equipment{
			ID: siteAssemblyID(id), Name: name, Type: i3x.EquipmentTypeAssembly,
			ParentID: parentID, Path: fmt.Sprintf("%s / %s", orgName, name),
		})
	}
	return out, nil
}

// childAreas returns the areas that belong to a site.
func (h *I3XHandler) childAreas(siteID int, parentID string) ([]i3x.Equipment, error) {
	var siteName, orgName string
	var orgID int
	err := h.db.QueryRowContext(context.Background(),
		`SELECT s.name, o.id, o.name FROM sites s JOIN organizations o ON s.org_id=o.id WHERE s.id=$1`,
		siteID).Scan(&siteName, &orgID, &orgName)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(context.Background(),
		`SELECT id, name FROM areas WHERE site_id = $1 ORDER BY name`, siteID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("[i3X] childAreas rows.Close: %v", cerr)
		}
	}()
	var out []i3x.Equipment
	for rows.Next() {
		var id int
		var name string
		if err2 := rows.Scan(&id, &name); err2 != nil {
			continue
		}
		out = append(out, i3x.Equipment{
			ID: areaAssemblyID(id), Name: name, Type: i3x.EquipmentTypeAssembly,
			ParentID: parentID, Path: fmt.Sprintf("%s / %s / %s", orgName, siteName, name),
		})
	}
	return out, nil
}

// childGateways returns the gateways that belong to an area.
func (h *I3XHandler) childGateways(areaID int, parentID string) ([]i3x.Equipment, error) {
	var areaName, siteName, orgName string
	var siteID, orgID int
	err := h.db.QueryRowContext(context.Background(), `
		SELECT a.name, s.id, s.name, o.id, o.name
		FROM areas a JOIN sites s ON a.site_id=s.id JOIN organizations o ON s.org_id=o.id
		WHERE a.id=$1`, areaID).Scan(&areaName, &siteID, &siteName, &orgID, &orgName)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(context.Background(), `
		SELECT g.id, g.name, g.driver_type, g.scan_rate_ms, g.enabled
		FROM gateways g WHERE g.area_id = $1 ORDER BY g.name`, areaID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("[i3X] childGateways rows.Close: %v", cerr)
		}
	}()
	var out []i3x.Equipment
	for rows.Next() {
		var id, scanRate int
		var name, driverType string
		var enabled bool
		if err2 := rows.Scan(&id, &name, &driverType, &scanRate, &enabled); err2 != nil {
			continue
		}
		out = append(out, i3x.Equipment{
			ID: gwEquipmentID(id), Name: name, Type: i3x.EquipmentTypeEquipment,
			Description: driverType, ParentID: parentID,
			Path: fmt.Sprintf("%s / %s / %s / %s", orgName, siteName, areaName, name),
			Attributes: map[string]interface{}{
				"driver_type": driverType, "scan_rate_ms": scanRate, "enabled": enabled,
			},
		})
	}
	return out, nil
}

// GetEquipmentChildren handles GET /api/i3x/v1/equipment/:id/children.
// Returns the direct children of a hierarchy node (org→sites, site→areas, area→gateways, gw→empty).
func (h *I3XHandler) GetEquipmentChildren(c *gin.Context) {
	rawID := c.Param("id")
	orgFilter := middleware.GetOrgFilterForQuery(c)

	var (
		items []i3x.Equipment
		err   error
	)

	switch {
	case strings.HasPrefix(rawID, "org-"):
		orgID, ok := parseOrgID(rawID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Invalid org ID"})
			return
		}
		if orgFilter != nil && *orgFilter != orgID {
			c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
			return
		}
		items, err = h.childSites(orgID, rawID)

	case strings.HasPrefix(rawID, "site-"):
		siteID, ok := parseSiteID(rawID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Invalid site ID"})
			return
		}
		items, err = h.childAreas(siteID, rawID)
		if err == nil && orgFilter != nil {
			filtered := items[:0]
			for _, it := range items {
				if it.ParentID != "" {
					filtered = append(filtered, it)
				}
			}
		}

	case strings.HasPrefix(rawID, "area-"):
		areaID, ok := parseAreaID(rawID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Invalid area ID"})
			return
		}
		items, err = h.childGateways(areaID, rawID)

	case strings.HasPrefix(rawID, "gw-"):
		items = []i3x.Equipment{}

	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "ID must be one of: org-{n}, site-{n}, area-{n}, gw-{n}"})
		return
	}

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Node not found"})
		return
	}
	if err != nil {
		log.Printf("[i3X] GetEquipmentChildren error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query children"})
		return
	}

	if items == nil {
		items = []i3x.Equipment{}
	}
	c.JSON(http.StatusOK, i3x.ListResponse[i3x.Equipment]{Items: items, Total: len(items)})
}

// ─── Alarm acknowledge ────────────────────────────────────────────────────────

// AcknowledgeAlarm handles POST /api/i3x/v1/alarms/:id/acknowledge
// Sets alarm status to ACKNOWLEDGED. Requires i3x_write permission.
func (h *I3XHandler) AcknowledgeAlarm(c *gin.Context) {
	if !middleware.HasI3xWrite(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "i3X write permission required"})
		return
	}

	rawID := strings.TrimPrefix(c.Param("id"), "alarm-")
	evtID, err := strconv.Atoi(rawID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Alarm ID must be in the form alarm-{n}"})
		return
	}

	// Org isolation.
	var orgID int
	err = h.db.QueryRowContext(context.Background(), `
		SELECT s.org_id FROM alarm_events e
		JOIN tags t ON e.tag_id = t.id
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE e.id = $1`, evtID).Scan(&orgID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Alarm not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query alarm"})
		return
	}
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil && *orgFilter != orgID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Access denied"})
		return
	}

	// Get username from JWT claims.
	var username string
	if raw, exists := c.Get(middleware.UserKey); exists {
		if claims, ok := raw.(map[string]interface{}); ok {
			username, _ = claims["username"].(string)
		}
	}

	result, err := h.db.ExecContext(context.Background(), `
		UPDATE alarm_events
		SET status = 'ACKNOWLEDGED', ack_time = NOW(), bg_ack_user = $2
		WHERE id = $1 AND status = 'ACTIVE'`, evtID, username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to acknowledge alarm"})
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "NOT_ACTIVE", "message": "Alarm is not in Active state"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Alarm acknowledged"})
}

// ─── Batch read ───────────────────────────────────────────────────────────────

// BatchReadProperties handles POST /api/i3x/v1/properties/values.
// Returns current values for up to 500 properties in a single request.
// Request body: {"ids": ["tag-1", "tag-42", ...]}.
func (h *I3XHandler) BatchReadProperties(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}
	if len(req.IDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "TOO_MANY_IDS", "message": "Maximum 500 IDs per batch request"})
		return
	}

	tagIDs := make([]int, 0, len(req.IDs))
	for _, rid := range req.IDs {
		if id, ok := parseTagID(rid); ok {
			tagIDs = append(tagIDs, id)
		}
	}
	if len(tagIDs) == 0 {
		c.JSON(http.StatusOK, i3x.ListResponse[i3x.Property]{Items: []i3x.Property{}, Total: 0})
		return
	}

	orgFilter := middleware.GetOrgFilterForQuery(c)

	// Build a placeholder list for the IN clause: $1, $2, ...
	placeholders := make([]string, len(tagIDs))
	args := make([]interface{}, len(tagIDs))
	for i, id := range tagIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	q := fmt.Sprintf(`
		SELECT t.id, t.alias, t.data_type, t.historize, t.gateway_id, s.org_id
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id IN (%s)
		ORDER BY t.id`, strings.Join(placeholders, ","))

	rows, err := h.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		log.Printf("[i3X] BatchReadProperties query error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "Failed to query properties"})
		return
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("[i3X] BatchReadProperties rows.Close: %v", cerr)
		}
	}()

	var items []i3x.Property
	for rows.Next() {
		var tagID, gwID, ownerOrgID int
		var alias, dataType string
		var historize bool
		if err2 := rows.Scan(&tagID, &alias, &dataType, &historize, &gwID, &ownerOrgID); err2 != nil {
			continue
		}
		if orgFilter != nil && *orgFilter != ownerOrgID {
			continue
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
