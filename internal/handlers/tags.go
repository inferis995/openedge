package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/audit"
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
	// JsonPath: for MQTT tags whose payload is JSON, extract this dotted path
	// (e.g. "temp" from {"temp":22}). Empty/omitted = whole payload as value.
	JsonPath *string `json:"json_path"`

	// EU Scaling
	ScalingEnabled *bool    `json:"scaling_enabled"`
	ScalingRawMin  *float64 `json:"scaling_raw_min"`
	ScalingRawMax  *float64 `json:"scaling_raw_max"`
	ScalingEuMin   *float64 `json:"scaling_eu_min"`
	ScalingEuMax   *float64 `json:"scaling_eu_max"`
	ScalingClamp   *bool    `json:"scaling_clamp"`
	EuUnit         *string  `json:"eu_unit"`
	EuDecimals     *int     `json:"eu_decimals"`
	Invert         *bool    `json:"invert"`
}

// UpdateTagRequest represents the request body for updating a tag
type UpdateTagRequest struct {
	Code              *string  `json:"code"`
	Alias             *string  `json:"alias"`
	DataType          *string  `json:"data_type"`
	Historize         *bool    `json:"historize"`
	HistorizeDeadband *float64 `json:"historize_deadband"`
	JsonPath          *string  `json:"json_path"`

	// EU Scaling
	ScalingEnabled *bool    `json:"scaling_enabled"`
	ScalingRawMin  *float64 `json:"scaling_raw_min"`
	ScalingRawMax  *float64 `json:"scaling_raw_max"`
	ScalingEuMin   *float64 `json:"scaling_eu_min"`
	ScalingEuMax   *float64 `json:"scaling_eu_max"`
	ScalingClamp   *bool    `json:"scaling_clamp"`
	EuUnit         *string  `json:"eu_unit"`
	EuDecimals     *int     `json:"eu_decimals"`
	Invert         *bool    `json:"invert"`
}

// validateDataType checks if the data_type is valid
func validateDataType(dataType string) bool {
	switch dataType {
	case "INT", "REAL", "BOOL", "DINT", "STRING":
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

	scalingEnabled := false
	if req.ScalingEnabled != nil {
		scalingEnabled = *req.ScalingEnabled
	}
	scalingRawMin := 0.0
	if req.ScalingRawMin != nil {
		scalingRawMin = *req.ScalingRawMin
	}
	scalingRawMax := 100.0
	if req.ScalingRawMax != nil {
		scalingRawMax = *req.ScalingRawMax
	}
	scalingEuMin := 0.0
	if req.ScalingEuMin != nil {
		scalingEuMin = *req.ScalingEuMin
	}
	scalingEuMax := 100.0
	if req.ScalingEuMax != nil {
		scalingEuMax = *req.ScalingEuMax
	}
	scalingClamp := true
	if req.ScalingClamp != nil {
		scalingClamp = *req.ScalingClamp
	}
	euUnit := ""
	if req.EuUnit != nil {
		euUnit = *req.EuUnit
	}
	euDecimals := 2
	if req.EuDecimals != nil {
		euDecimals = *req.EuDecimals
	}
	invert := false
	if req.Invert != nil {
		invert = *req.Invert
	}

	var tag models.Tag
	err = h.db.QueryRow(
		`INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband, json_path, sort_order,
		                   scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min, scaling_eu_max,
		                   scaling_clamp, eu_unit, eu_decimals, invert)
		 VALUES ($1, $2, $3, $4, $5, $6, $7,
		         (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM tags WHERE gateway_id = $1),
		         $8, $9, $10, $11, $12, $13, $14, $15, $16)
		 RETURNING id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, json_path,
		           scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min, scaling_eu_max,
		           scaling_clamp, eu_unit, eu_decimals, invert, created_at`,
		req.GatewayID, req.Code, req.Alias, req.DataType, historize, historizeDeadband, req.JsonPath,
		scalingEnabled, scalingRawMin, scalingRawMax, scalingEuMin, scalingEuMax,
		scalingClamp, euUnit, euDecimals, invert,
	).Scan(
		&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType,
		&tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.JsonPath,
		&tag.ScalingEnabled, &tag.ScalingRawMin, &tag.ScalingRawMax, &tag.ScalingEuMin, &tag.ScalingEuMax,
		&tag.ScalingClamp, &tag.EuUnit, &tag.EuDecimals, &tag.Invert, &tag.CreatedAt,
	)

	if err != nil {
		log.Printf("[API] Tag Creation DB Error: %v (gateway_id=%d, code=%s, alias=%s, data_type=%s)", err, req.GatewayID, req.Code, req.Alias, req.DataType)
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
// @Param X-Organization-ID header int false "Organization ID (optional for global admin)"
// @Param gateway_id query int true "Gateway ID"
// @Success 200 {array} models.Tag
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Gateway not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/tags [get]
func (h *TagsHandler) List(c *gin.Context) {
	// Check if user is global admin
	isGlobalAdmin := middleware.IsGlobalAdmin(c)

	gatewayIDStr := c.Query("gateway_id")
	areaIDStr := c.Query("area_id")

	var rows *sql.Rows
	var err error

	if gatewayIDStr != "" {
		// Case 1: Filter by Specific Gateway
		gatewayID, err := strconv.Atoi(gatewayIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gateway_id parameter"})
			return
		}

		// For global admin, skip org verification. For regular users, verify ownership.
		if !isGlobalAdmin {
			orgID, ok := middleware.GetOrganizationID(c)
			if !ok {
				c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
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
		}

		rows, err = h.db.Query(
			`SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, json_path,
			        scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min, scaling_eu_max,
			        scaling_clamp, eu_unit, eu_decimals, invert, created_at
			 FROM tags WHERE gateway_id = $1 ORDER BY sort_order ASC, id ASC`,
			gatewayID,
		)
	} else if areaIDStr != "" {
		// Case 2: Filter by Area — tags from all gateways in the area.
		// Verify the area belongs to the authenticated org to prevent cross-org data access.
		areaID, err2 := strconv.Atoi(areaIDStr)
		if err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid area_id parameter"})
			return
		}
		orgFilter := middleware.GetOrgFilterForQuery(c)
		if orgFilter != nil {
			// Org-scoped user: verify area belongs to their org via sites→organizations chain.
			var areaOrgID int
			if err3 := h.db.QueryRow(
				`SELECT s.org_id FROM areas a JOIN sites s ON s.id = a.site_id WHERE a.id = $1`, areaID,
			).Scan(&areaOrgID); err3 != nil || areaOrgID != *orgFilter {
				c.JSON(http.StatusForbidden, gin.H{"error": "area not found or access denied"})
				return
			}
		}
		rows, err = h.db.Query(
			`SELECT t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize, t.historize_deadband, t.sort_order, t.json_path,
			        t.scaling_enabled, t.scaling_raw_min, t.scaling_raw_max, t.scaling_eu_min, t.scaling_eu_max,
			        t.scaling_clamp, t.eu_unit, t.eu_decimals, t.invert, t.created_at
			 FROM tags t
			 JOIN gateways g ON t.gateway_id = g.id
			 WHERE g.area_id = $1
			 ORDER BY t.sort_order ASC, t.id ASC`,
			areaID,
		)
	} else {
		// Case 2: List All Tags for Organization
		orgFilter := middleware.GetOrgFilterForQuery(c)

		if orgFilter == nil {
			// Global admin - get all tags across all organizations
			rows, err = h.db.Query(
				`SELECT t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize, t.historize_deadband, t.sort_order, t.json_path,
				        t.scaling_enabled, t.scaling_raw_min, t.scaling_raw_max, t.scaling_eu_min, t.scaling_eu_max,
				        t.scaling_clamp, t.eu_unit, t.eu_decimals, t.invert, t.created_at
				 FROM tags t
				 ORDER BY t.sort_order ASC, t.id ASC`,
			)
		} else {
			// Regular user - filter by their organization
			rows, err = h.db.Query(
				`SELECT t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize, t.historize_deadband, t.sort_order, t.json_path,
				        t.scaling_enabled, t.scaling_raw_min, t.scaling_raw_max, t.scaling_eu_min, t.scaling_eu_max,
				        t.scaling_clamp, t.eu_unit, t.eu_decimals, t.invert, t.created_at
				 FROM tags t
				 JOIN gateways g ON t.gateway_id = g.id
				 JOIN areas a ON g.area_id = a.id
				 JOIN sites s ON a.site_id = s.id
				 WHERE s.org_id = $1
				 ORDER BY t.sort_order ASC, t.id ASC`,
				*orgFilter,
			)
		}
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
		if err := rows.Scan(
			&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType,
			&tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.JsonPath,
			&tag.ScalingEnabled, &tag.ScalingRawMin, &tag.ScalingRawMax, &tag.ScalingEuMin, &tag.ScalingEuMax,
			&tag.ScalingClamp, &tag.EuUnit, &tag.EuDecimals, &tag.Invert, &tag.CreatedAt,
		); err != nil {
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

	// Ensure we return an empty array instead of null
	if tags == nil {
		tags = []models.Tag{}
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
		`SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, json_path,
		        scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min, scaling_eu_max,
		        scaling_clamp, eu_unit, eu_decimals, invert, created_at
		 FROM tags WHERE id = $1`,
		id,
	).Scan(
		&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType,
		&tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.JsonPath,
		&tag.ScalingEnabled, &tag.ScalingRawMin, &tag.ScalingRawMax, &tag.ScalingEuMin, &tag.ScalingEuMax,
		&tag.ScalingClamp, &tag.EuUnit, &tag.EuDecimals, &tag.Invert, &tag.CreatedAt,
	)

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
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tag id"})
		return
	}

	// Check if tag exists AND get gateway_id for reload
	var gatewayID int
	var tagOrgID int
	scanErr := h.db.QueryRow(`
		SELECT t.gateway_id, s.org_id
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id = $1`, id).Scan(&gatewayID, &tagOrgID)

	if scanErr != nil {
		err = scanErr
	}
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

	// 0. MQTT Retained Message Cleanup (Must be done BEFORE deletion to get topic names)
	if h.mqttClient != nil {
		var org, site, area, gateway, tagAlias string
		err := h.db.QueryRow(`
			SELECT 
				o.name, s.name, a.name, g.name, t.alias
			FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			JOIN organizations o ON s.org_id = o.id
			WHERE t.id = $1`, id).Scan(&org, &site, &area, &gateway, &tagAlias)

		if err == nil {
			// Helper to create slug (simple version matching typical driver logic)
			slug := func(s string) string {
				return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
			}

			// 1. Try Raw Format (just in case)
			topic1 := fmt.Sprintf("data/%s/%s/%s/%s/%s", org, site, area, gateway, tagAlias)
			h.mqttClient.PublishWithQoS(topic1, "", 1, true)

			// 2. Try Slugified Format (Standard)
			topic2 := fmt.Sprintf("data/%s/%s/%s/%s/%s", slug(org), slug(site), slug(area), slug(gateway), slug(tagAlias))
			if topic2 != topic1 {
				h.mqttClient.PublishWithQoS(topic2, "", 1, true)
			}
		}
	}

	// Delete Tag
	_, err = h.db.Exec("DELETE FROM tags WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tag"})
		return
	}

	// Publish reload command to driver (after deletion)
	if h.mqttClient != nil {
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
	if req.JsonPath != nil {
		// Empty string clears the path (NULL in DB), so the driver reverts to
		// using the whole payload as the value.
		updates = append(updates, "json_path = $"+strconv.Itoa(argPos))
		if *req.JsonPath == "" {
			args = append(args, nil)
		} else {
			args = append(args, *req.JsonPath)
		}
		argPos++
	}
	if req.ScalingEnabled != nil {
		updates = append(updates, "scaling_enabled = $"+strconv.Itoa(argPos))
		args = append(args, *req.ScalingEnabled)
		argPos++
	}
	if req.ScalingRawMin != nil {
		updates = append(updates, "scaling_raw_min = $"+strconv.Itoa(argPos))
		args = append(args, *req.ScalingRawMin)
		argPos++
	}
	if req.ScalingRawMax != nil {
		updates = append(updates, "scaling_raw_max = $"+strconv.Itoa(argPos))
		args = append(args, *req.ScalingRawMax)
		argPos++
	}
	if req.ScalingEuMin != nil {
		updates = append(updates, "scaling_eu_min = $"+strconv.Itoa(argPos))
		args = append(args, *req.ScalingEuMin)
		argPos++
	}
	if req.ScalingEuMax != nil {
		updates = append(updates, "scaling_eu_max = $"+strconv.Itoa(argPos))
		args = append(args, *req.ScalingEuMax)
		argPos++
	}
	if req.ScalingClamp != nil {
		updates = append(updates, "scaling_clamp = $"+strconv.Itoa(argPos))
		args = append(args, *req.ScalingClamp)
		argPos++
	}
	if req.EuUnit != nil {
		updates = append(updates, "eu_unit = $"+strconv.Itoa(argPos))
		args = append(args, *req.EuUnit)
		argPos++
	}
	if req.EuDecimals != nil {
		updates = append(updates, "eu_decimals = $"+strconv.Itoa(argPos))
		args = append(args, *req.EuDecimals)
		argPos++
	}
	if req.Invert != nil {
		updates = append(updates, "invert = $"+strconv.Itoa(argPos))
		args = append(args, *req.Invert)
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
	query += ` WHERE id = $` + strconv.Itoa(argPos) +
		` RETURNING id, gateway_id, code, alias, data_type, historize, historize_deadband, sort_order, json_path,
		            scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min, scaling_eu_max,
		            scaling_clamp, eu_unit, eu_decimals, invert, created_at`

	var tag models.Tag
	err = h.db.QueryRow(query, args...).Scan(
		&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType,
		&tag.Historize, &tag.HistorizeDeadband, &tag.SortOrder, &tag.JsonPath,
		&tag.ScalingEnabled, &tag.ScalingRawMin, &tag.ScalingRawMax, &tag.ScalingEuMin, &tag.ScalingEuMax,
		&tag.ScalingClamp, &tag.EuUnit, &tag.EuDecimals, &tag.Invert, &tag.CreatedAt,
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

	// Verify org access (unless global admin)
	if !middleware.IsGlobalAdmin(c) {
		orgID, _ := middleware.GetOrganizationID(c)
		var tagOrgID int
		err = h.db.QueryRow(`
			SELECT s.org_id FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			WHERE t.id = $1`, id).Scan(&tagOrgID)
		if err != nil || tagOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this tag"})
			return
		}
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
		audit.Log(c, h.db, audit.Entry{
			Action:  "tag.write",
			Success: false,
			Details: map[string]interface{}{
				"tag_id": tag.ID, "code": tag.Code, "value": req.Value, "error": err.Error(),
			},
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to publish write command"})
		return
	}

	// Audit: an operator just wrote a setpoint to a PLC. Goes into
	// audit_logs with the actor's user_id + username so the QA/maintenance
	// team can trace "who changed temperature at 03:00?".
	audit.Log(c, h.db, audit.Entry{
		Action:  "tag.write",
		Success: true,
		Details: map[string]interface{}{
			"tag_id":     tag.ID,
			"code":       tag.Code,
			"data_type":  tag.DataType,
			"value":      req.Value,
			"gateway_id": tag.GatewayID,
		},
	})

	c.JSON(http.StatusOK, gin.H{"message": "Write command sent"})
}

// TagWithHierarchy represents a tag with full hierarchy information
type TagWithHierarchy struct {
	ID             int     `json:"id"`
	GatewayID      int     `json:"gateway_id"`
	Code           string  `json:"code"`
	Alias          string  `json:"alias"`
	DataType       string  `json:"data_type"`
	Historize      bool    `json:"historize"`
	GatewayName    string  `json:"gateway_name,omitempty"`
	DriverType     string  `json:"driver_type,omitempty"`
	AreaID         int     `json:"area_id,omitempty"`
	AreaName       string  `json:"area_name,omitempty"`
	SiteID         int     `json:"site_id,omitempty"`
	SiteName       string  `json:"site_name,omitempty"`
	OrgID          int     `json:"org_id,omitempty"`
	OrgName        string  `json:"org_name,omitempty"`
	ScalingEnabled bool    `json:"scaling_enabled"`
	ScalingRawMin  float64 `json:"scaling_raw_min"`
	ScalingRawMax  float64 `json:"scaling_raw_max"`
	ScalingEuMin   float64 `json:"scaling_eu_min"`
	ScalingEuMax   float64 `json:"scaling_eu_max"`
	ScalingClamp   bool    `json:"scaling_clamp"`
	EuUnit         string  `json:"eu_unit"`
	EuDecimals     int     `json:"eu_decimals"`
	Invert         bool    `json:"invert"`
}

// ListWithHierarchy handles GET /api/tags/with-hierarchy
// Returns all tags with full hierarchy information for the tag browser
func (h *TagsHandler) ListWithHierarchy(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	query := `
		SELECT
			t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize,
			g.name as gateway_name, g.driver_type,
			a.id as area_id, a.name as area_name,
			s.id as site_id, s.name as site_name,
			o.id as org_id, o.name as org_name,
			t.scaling_enabled, t.scaling_raw_min, t.scaling_raw_max, t.scaling_eu_min, t.scaling_eu_max, t.scaling_clamp, t.eu_unit, t.eu_decimals, t.invert
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE o.id = $1
		ORDER BY o.name, s.name, a.name, g.name, t.alias
	`

	rows, err := h.db.Query(query, orgID)
	if err != nil {
		log.Printf("[API] Query error in ListWithHierarchy: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query tags with hierarchy"})
		return
	}
	defer rows.Close()

	var tags []TagWithHierarchy
	for rows.Next() {
		var tag TagWithHierarchy
		var gatewayName, driverType, areaName, siteName, orgName sql.NullString
		var areaID, siteID, orgIDVal sql.NullInt64

		err := rows.Scan(
			&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize,
			&gatewayName, &driverType,
			&areaID, &areaName,
			&siteID, &siteName,
			&orgIDVal, &orgName,
			&tag.ScalingEnabled, &tag.ScalingRawMin, &tag.ScalingRawMax, &tag.ScalingEuMin, &tag.ScalingEuMax, &tag.ScalingClamp, &tag.EuUnit, &tag.EuDecimals, &tag.Invert,
		)
		if err != nil {
			log.Printf("[API] Scan error in ListWithHierarchy: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan tag"})
			return
		}

		tag.GatewayName = gatewayName.String
		tag.DriverType = driverType.String
		if areaID.Valid {
			tag.AreaID = int(areaID.Int64)
		}
		tag.AreaName = areaName.String
		if siteID.Valid {
			tag.SiteID = int(siteID.Int64)
		}
		tag.SiteName = siteName.String
		if orgIDVal.Valid {
			tag.OrgID = int(orgIDVal.Int64)
		}
		tag.OrgName = orgName.String

		tags = append(tags, tag)
	}

	if tags == nil {
		tags = []TagWithHierarchy{}
	}

	c.JSON(http.StatusOK, tags)
}

// GatewayHierarchy represents a gateway in the hierarchy
type GatewayHierarchy struct {
	ID         int                `json:"id"`
	Name       string             `json:"name"`
	DriverType string             `json:"driver_type"`
	Tags       []TagWithHierarchy `json:"tags"`
}

// AreaHierarchy represents an area in the hierarchy
type AreaHierarchy struct {
	ID       int                `json:"id"`
	Name     string             `json:"name"`
	Gateways []GatewayHierarchy `json:"gateways"`
}

// SiteHierarchy represents a site in the hierarchy
type SiteHierarchy struct {
	ID    int            `json:"id"`
	Name  string         `json:"name"`
	Areas []AreaHierarchy `json:"areas"`
}

// OrganizationHierarchy represents an organization in the hierarchy
type OrganizationHierarchy struct {
	ID    int            `json:"id"`
	Name  string         `json:"name"`
	Sites []SiteHierarchy `json:"sites"`
}

// TagHierarchyResponse represents the response for the hierarchy endpoint
type TagHierarchyResponse struct {
	Organizations []OrganizationHierarchy `json:"organizations"`
}

// GetHierarchy handles GET /api/tags/hierarchy
// Returns the full tag hierarchy structure for the tag browser
func (h *TagsHandler) GetHierarchy(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Get all tags with hierarchy for this organization
	tags, err := h.getTagsWithHierarchy(orgID)
	if err != nil {
		log.Printf("[API] Error getting tags with hierarchy: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get tag hierarchy"})
		return
	}

	// Build hierarchy structure
	hierarchy := buildTagHierarchy(tags)

	c.JSON(http.StatusOK, TagHierarchyResponse{Organizations: hierarchy})
}

func (h *TagsHandler) getTagsWithHierarchy(orgID int) ([]TagWithHierarchy, error) {
	query := `
		SELECT
			t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize,
			g.name as gateway_name, g.driver_type,
			a.id as area_id, a.name as area_name,
			s.id as site_id, s.name as site_name,
			o.id as org_id, o.name as org_name,
			t.scaling_enabled, t.scaling_raw_min, t.scaling_raw_max, t.scaling_eu_min, t.scaling_eu_max, t.scaling_clamp, t.eu_unit, t.eu_decimals, t.invert
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE o.id = $1
		ORDER BY o.name, s.name, a.name, g.name, t.alias
	`

	rows, err := h.db.Query(query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []TagWithHierarchy
	for rows.Next() {
		var tag TagWithHierarchy
		var gatewayName, driverType, areaName, siteName, orgName sql.NullString
		var areaID, siteID, orgIDVal sql.NullInt64

		err := rows.Scan(
			&tag.ID, &tag.GatewayID, &tag.Code, &tag.Alias, &tag.DataType, &tag.Historize,
			&gatewayName, &driverType,
			&areaID, &areaName,
			&siteID, &siteName,
			&orgIDVal, &orgName,
			&tag.ScalingEnabled, &tag.ScalingRawMin, &tag.ScalingRawMax, &tag.ScalingEuMin, &tag.ScalingEuMax, &tag.ScalingClamp, &tag.EuUnit, &tag.EuDecimals, &tag.Invert,
		)
		if err != nil {
			return nil, err
		}

		tag.GatewayName = gatewayName.String
		tag.DriverType = driverType.String
		if areaID.Valid {
			tag.AreaID = int(areaID.Int64)
		}
		tag.AreaName = areaName.String
		if siteID.Valid {
			tag.SiteID = int(siteID.Int64)
		}
		tag.SiteName = siteName.String
		if orgIDVal.Valid {
			tag.OrgID = int(orgIDVal.Int64)
		}
		tag.OrgName = orgName.String

		tags = append(tags, tag)
	}

	return tags, nil
}

func buildTagHierarchy(tags []TagWithHierarchy) []OrganizationHierarchy {
	if len(tags) == 0 {
		return []OrganizationHierarchy{}
	}

	// Maps to store unique entities
	orgMap := make(map[int]*OrganizationHierarchy)
	siteMap := make(map[int]*SiteHierarchy)
	areaMap := make(map[int]*AreaHierarchy)
	gatewayMap := make(map[int]*GatewayHierarchy)

	// Gateway to area mapping (we need this because gateway doesn't store area_id)
	gatewayToArea := make(map[int]int)

	// First pass: create all entities and add tags to gateways
	for _, tag := range tags {
		// Create organization if needed
		if tag.OrgID > 0 {
			if _, exists := orgMap[tag.OrgID]; !exists {
				orgMap[tag.OrgID] = &OrganizationHierarchy{
					ID:    tag.OrgID,
					Name:  tag.OrgName,
					Sites: []SiteHierarchy{},
				}
			}
		}

		// Create site if needed
		if tag.SiteID > 0 {
			if _, exists := siteMap[tag.SiteID]; !exists {
				siteMap[tag.SiteID] = &SiteHierarchy{
					ID:    tag.SiteID,
					Name:  tag.SiteName,
					Areas: []AreaHierarchy{},
				}
			}
		}

		// Create area if needed
		if tag.AreaID > 0 {
			if _, exists := areaMap[tag.AreaID]; !exists {
				areaMap[tag.AreaID] = &AreaHierarchy{
					ID:       tag.AreaID,
					Name:     tag.AreaName,
					Gateways: []GatewayHierarchy{},
				}
			}
		}

		// Create gateway if needed
		if tag.GatewayID > 0 {
			if _, exists := gatewayMap[tag.GatewayID]; !exists {
				gatewayMap[tag.GatewayID] = &GatewayHierarchy{
					ID:         tag.GatewayID,
					Name:       tag.GatewayName,
					DriverType: tag.DriverType,
					Tags:       []TagWithHierarchy{},
				}
			}
			// Track which area this gateway belongs to
			gatewayToArea[tag.GatewayID] = tag.AreaID
			// Add tag to gateway
			gatewayMap[tag.GatewayID].Tags = append(gatewayMap[tag.GatewayID].Tags, tag)
		}
	}

	// Second pass: link gateways to areas
	for gatewayID, areaID := range gatewayToArea {
		gateway := gatewayMap[gatewayID]
		area := areaMap[areaID]
		if gateway != nil && area != nil {
			// Check if already linked
			found := false
			for _, g := range area.Gateways {
				if g.ID == gatewayID {
					found = true
					break
				}
			}
			if !found {
				area.Gateways = append(area.Gateways, *gateway)
			}
		}
	}

	// Third pass: link areas to sites
	areaToSite := make(map[int]int)
	for _, tag := range tags {
		if tag.AreaID > 0 && tag.SiteID > 0 {
			areaToSite[tag.AreaID] = tag.SiteID
		}
	}
	for areaID, siteID := range areaToSite {
		area := areaMap[areaID]
		site := siteMap[siteID]
		if area != nil && site != nil {
			found := false
			for _, a := range site.Areas {
				if a.ID == areaID {
					found = true
					break
				}
			}
			if !found {
				site.Areas = append(site.Areas, *area)
			}
		}
	}

	// Fourth pass: link sites to orgs
	siteToOrg := make(map[int]int)
	for _, tag := range tags {
		if tag.SiteID > 0 && tag.OrgID > 0 {
			siteToOrg[tag.SiteID] = tag.OrgID
		}
	}
	for siteID, orgID := range siteToOrg {
		site := siteMap[siteID]
		org := orgMap[orgID]
		if site != nil && org != nil {
			found := false
			for _, s := range org.Sites {
				if s.ID == siteID {
					found = true
					break
				}
			}
			if !found {
				org.Sites = append(org.Sites, *site)
			}
		}
	}

	// Convert map to slice
	result := make([]OrganizationHierarchy, 0, len(orgMap))
	for _, org := range orgMap {
		result = append(result, *org)
	}

	return result
}

// TagShadowResponse is the Digital Twin "last known value" for a tag.
// source="live" when edge is online; "historic" when reading from DB fallback.
type TagShadowResponse struct {
	TagID  int         `json:"tag_id"`
	Value  interface{} `json:"value"`
	Quality int        `json:"quality"`
	Ts     int64       `json:"ts"`
	Source string      `json:"source"` // "live" | "historic" | "unknown"
}

// GetShadow handles GET /api/tags/:id/shadow.
// Always returns the last known value even when the edge is offline.
func (h *TagsHandler) GetShadow(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tag id"})
		return
	}

	// Verify tag belongs to the authenticated org (prevent cross-org data access).
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil {
		var tagOrgID int
		if err := h.db.QueryRowContext(c.Request.Context(),
			`SELECT s.org_id FROM tags t
			 JOIN gateways g ON g.id = t.gateway_id
			 JOIN areas a ON a.id = g.area_id
			 JOIN sites s ON s.id = a.site_id
			 WHERE t.id = $1`, id,
		).Scan(&tagOrgID); err != nil || tagOrgID != *orgFilter {
			c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
			return
		}
	}

	// Try Redis shadow first (written on every live MQTT update)
	if h.redisClient != nil {
		raw, redisErr := h.redisClient.Get(fmt.Sprintf("tag_shadow:%d", id))
		if redisErr == nil {
			var shadow TagShadowResponse
			if json.Unmarshal([]byte(raw), &shadow) == nil {
				c.JSON(http.StatusOK, shadow)
				return
			}
		}
	}

	// Fallback: last row in tag_history
	var val sql.NullString
	var ts int64
	err = h.db.QueryRowContext(c.Request.Context(),
		`SELECT value, EXTRACT(EPOCH FROM time)::BIGINT * 1000
		 FROM tag_history WHERE tag_id = $1 ORDER BY time DESC LIMIT 1`, id,
	).Scan(&val, &ts)
	if err != nil {
		c.JSON(http.StatusOK, TagShadowResponse{TagID: id, Source: "unknown"})
		return
	}

	var parsed interface{}
	if val.Valid {
		if err := json.Unmarshal([]byte(val.String), &parsed); err != nil {
			parsed = val.String
		}
	}
	c.JSON(http.StatusOK, TagShadowResponse{
		TagID:   id,
		Value:   parsed,
		Quality: 64, // Uncertain — data from history, not live
		Ts:      ts,
		Source:  "historic",
	})
}

// GetShadowBatch handles GET /api/tags/shadows?gateway_id=X
// Returns shadows for all tags of a gateway in one call.
func (h *TagsHandler) GetShadowBatch(c *gin.Context) {
	gwIDStr := c.Query("gateway_id")
	if gwIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "gateway_id required"})
		return
	}
	gwID, err := strconv.Atoi(gwIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gateway_id"})
		return
	}

	// Verify gateway belongs to the authenticated org.
	orgFilter := middleware.GetOrgFilterForQuery(c)
	if orgFilter != nil {
		var gwOrgID int
		if err := h.db.QueryRowContext(c.Request.Context(),
			`SELECT s.org_id FROM gateways g
			 JOIN areas a ON a.id = g.area_id
			 JOIN sites s ON s.id = a.site_id
			 WHERE g.id = $1`, gwID,
		).Scan(&gwOrgID); err != nil || gwOrgID != *orgFilter {
			c.JSON(http.StatusNotFound, gin.H{"error": "gateway not found"})
			return
		}
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `SELECT id FROM tags WHERE gateway_id = $1`, gwID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer func() { _ = rows.Close() }()

	results := []TagShadowResponse{}
	for rows.Next() {
		var tagID int
		if rows.Scan(&tagID) != nil {
			continue
		}
		// Try Redis shadow
		if h.redisClient != nil {
			raw, err := h.redisClient.Get(fmt.Sprintf("tag_shadow:%d", tagID))
			if err == nil {
				var shadow TagShadowResponse
				if json.Unmarshal([]byte(raw), &shadow) == nil {
					results = append(results, shadow)
					continue
				}
			}
		}
		// Fallback: last value from history
		var val sql.NullString
		var ts int64
		if err := h.db.QueryRowContext(c.Request.Context(),
			`SELECT value, EXTRACT(EPOCH FROM time)::BIGINT * 1000
			 FROM tag_history WHERE tag_id = $1 ORDER BY time DESC LIMIT 1`, tagID,
		).Scan(&val, &ts); err == nil && val.Valid {
			var parsed interface{}
			if json.Unmarshal([]byte(val.String), &parsed) != nil {
				parsed = val.String
			}
			results = append(results, TagShadowResponse{
				TagID: tagID, Value: parsed, Quality: 64, Ts: ts, Source: "historic",
			})
		} else {
			results = append(results, TagShadowResponse{TagID: tagID, Source: "unknown"})
		}
	}
	c.JSON(http.StatusOK, results)
}
