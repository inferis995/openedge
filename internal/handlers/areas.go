package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
)

// Area represents an area in the system
type Area struct {
	ID        int    `json:"id" example:"1"`
	SiteID    int    `json:"site_id" example:"1"`
	Name      string `json:"name" example:"Production Line"`
	CreatedAt string `json:"created_at" example:"2024-01-24T10:00:00Z"`
}

// AreasHandler handles area-related HTTP requests
type AreasHandler struct {
	db         *sql.DB
	mqttClient *mqtt.Client
}

// NewAreasHandler creates a new areas handler
func NewAreasHandler(db *sql.DB, mqttClient *mqtt.Client) *AreasHandler {
	return &AreasHandler{
		db:         db,
		mqttClient: mqttClient,
	}
}

// CreateAreaRequest represents the request body for creating an area
type CreateAreaRequest struct {
	SiteID int    `json:"site_id" binding:"required"`
	Name   string `json:"name" binding:"required"`
}

// Create handles POST /api/areas
// @Summary Create a new area
// @Description Create a new area for the specified site
// @Tags areas
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param request body CreateAreaRequest true "Area creation request"
// @Success 201 {object} Area
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Site not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/areas [post]
func (h *AreasHandler) Create(c *gin.Context) {
	var req CreateAreaRequest
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

	// Verify the site_id belongs to the authorized organization (multi-tenant isolation)
	var siteOrgID int
	err := h.db.QueryRow(
		"SELECT org_id FROM sites WHERE id = $1",
		req.SiteID,
	).Scan(&siteOrgID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Site not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify site ownership"})
		return
	}

	if siteOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Cannot create area for site in different organization",
			"detail": fmt.Sprintf("Site belongs to organization %d, but authorized for organization %d", siteOrgID, orgID),
		})
		return
	}

	var area models.Area
	err = h.db.QueryRow(
		"INSERT INTO areas (site_id, name) VALUES ($1, $2) RETURNING id, site_id, name, created_at",
		req.SiteID,
		req.Name,
	).Scan(&area.ID, &area.SiteID, &area.Name, &area.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create area"})
		return
	}

	c.JSON(http.StatusCreated, area)
}

// List handles GET /api/areas?site_id={id}
// Filters by organization from context (multi-tenant isolation)
// @Summary List areas
// @Description Get a list of areas for the specified site
// @Tags areas
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param site_id query int true "Site ID"
// @Success 200 {array} Area
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Site not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/areas [get]
func (h *AreasHandler) List(c *gin.Context) {
	// Get organization ID from context (set by middleware)
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	siteIDStr := c.Query("site_id")
	if siteIDStr == "" {
		// If no site_id provided, return empty list (no error)
		c.JSON(http.StatusOK, []models.Area{})
		return
	}

	siteID, err := strconv.Atoi(siteIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid site_id parameter"})
		return
	}

	// Verify the site_id belongs to the authorized organization
	var siteOrgID int
	err = h.db.QueryRow("SELECT org_id FROM sites WHERE id = $1", siteID).Scan(&siteOrgID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Site not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify site ownership"})
		return
	}

	if siteOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Cannot query areas for site in different organization",
			"detail": fmt.Sprintf("Site belongs to organization %d, but authorized for organization %d", siteOrgID, orgID),
		})
		return
	}

	rows, err := h.db.Query(
		"SELECT id, site_id, name, created_at FROM areas WHERE site_id = $1 ORDER BY id",
		siteID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query areas"})
		return
	}
	defer rows.Close()

	var areas []models.Area
	for rows.Next() {
		var area models.Area
		if err := rows.Scan(&area.ID, &area.SiteID, &area.Name, &area.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan area"})
			return
		}
		areas = append(areas, area)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating areas"})
		return
	}

	c.JSON(http.StatusOK, areas)
}

// Get handles GET /api/areas/{id}
// @Summary Get an area
// @Description Get a single area by ID
// @Tags areas
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Area ID"
// @Success 200 {object} Area
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Area not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/areas/{id} [get]
func (h *AreasHandler) Get(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	id := c.Param("id")

	var area models.Area
	err := h.db.QueryRow(
		"SELECT id, site_id, name, created_at FROM areas WHERE id = $1",
		id,
	).Scan(&area.ID, &area.SiteID, &area.Name, &area.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Area not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get area"})
		return
	}

	// Verify the area belongs to the authorized organization (via site)
	var areaOrgID int
	err = h.db.QueryRow("SELECT org_id FROM sites WHERE id = $1", area.SiteID).Scan(&areaOrgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify area ownership"})
		return
	}

	if areaOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to area from another organization"})
		return
	}

	c.JSON(http.StatusOK, area)
}

// Delete handles DELETE /api/areas/{id}
// @Summary Delete an area
// @Description Delete an area by ID (cascades to gateways, tags)
// @Tags areas
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Area ID"
// @Success 204 "Area deleted"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Area not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/areas/{id} [delete]
func (h *AreasHandler) Delete(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	id := c.Param("id")

	// Check if area exists and check ownership
	var areaOrgID int
	err := h.db.QueryRow(`
		SELECT s.org_id 
		FROM areas a 
		JOIN sites s ON a.site_id = s.id 
		WHERE a.id = $1`, id).Scan(&areaOrgID)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Area not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check area"})
		return
	}

	if areaOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete area from another organization"})
		return
	}

	// Manual Cascade Delete Transaction
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// 1. Delete Tags (cascade delete handled manually)
	_, err = tx.Exec(`
		DELETE FROM tags WHERE gateway_id IN (
			SELECT id FROM gateways WHERE area_id = $1
		)`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related tags"})
		return
	}

	// 2. Delete Gateways
	_, err = tx.Exec("DELETE FROM gateways WHERE area_id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related gateways"})
		return
	}

	// 3. Delete Area
	_, err = tx.Exec("DELETE FROM areas WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete area"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.Status(http.StatusNoContent)
}

// UpdateAreaRequest represents the request body for updating an area
type UpdateAreaRequest struct {
	Name *string `json:"name" binding:"required"`
}

// Update handles PUT /api/areas/{id}
// Filters by organization from context (multi-tenant isolation)
// @Summary Update an area
// @Description Update an area by ID
// @Tags areas
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Area ID"
// @Param request body UpdateAreaRequest true "Area update request"
// @Success 200 {object} Area
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Area not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/areas/{id} [put]
func (h *AreasHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid area ID"})
		return
	}

	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Check if area exists and check ownership
	var areaOrgID int
	err = h.db.QueryRow(`
		SELECT s.org_id 
		FROM areas a 
		JOIN sites s ON a.site_id = s.id 
		WHERE a.id = $1`, id).Scan(&areaOrgID)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Area not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check area"})
		return
	}

	if areaOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot update area from another organization"})
		return
	}

	var req UpdateAreaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var area models.Area
	err = h.db.QueryRow(
		"UPDATE areas SET name = $1 WHERE id = $2 RETURNING id, site_id, name, created_at",
		*req.Name,
		id,
	).Scan(&area.ID, &area.SiteID, &area.Name, &area.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update area"})
		return
	}

	// Trigger reload for all gateways in this area to update topic names
	if h.mqttClient != nil {
		gatewayIDs, err := h.getGatewayIDsForArea(area.ID)
		if err == nil {
			for _, gwID := range gatewayIDs {
				topic := fmt.Sprintf("sys/command/reload/%d", gwID)
				h.mqttClient.Publish(topic, "reload")
			}
		}
	}

	c.JSON(http.StatusOK, area)
}

// Helper to get all gateway IDs for an area
func (h *AreasHandler) getGatewayIDsForArea(areaID int) ([]int, error) {
	query := `SELECT id FROM gateways WHERE area_id = $1`
	rows, err := h.db.Query(query, areaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
