package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// Site represents a site in the system
type Site struct {
	ID        int    `json:"id" example:"1"`
	OrgID     int    `json:"org_id" example:"1"`
	Name      string `json:"name" example:"Factory 1"`
	CreatedAt string `json:"created_at" example:"2024-01-24T10:00:00Z"`
}

// SitesHandler handles site-related HTTP requests
type SitesHandler struct {
	db *sql.DB
}

// NewSitesHandler creates a new sites handler
func NewSitesHandler(db *sql.DB) *SitesHandler {
	return &SitesHandler{db: db}
}

// CreateSiteRequest represents the request body for creating a site
type CreateSiteRequest struct {
	OrgID int    `json:"org_id" binding:"required"`
	Name  string `json:"name" binding:"required"`
}

// Create handles POST /api/sites
// @Summary Create a new site
// @Description Create a new site for the specified organization
// @Tags sites
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param request body CreateSiteRequest true "Site creation request"
// @Success 201 {object} Site
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/sites [post]
func (h *SitesHandler) Create(c *gin.Context) {
	var req CreateSiteRequest
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

	// Verify the requested org_id matches the context org_id (multi-tenant isolation)
	if req.OrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Cannot create site for different organization",
			"detail": fmt.Sprintf("Request org_id (%d) does not match authorized organization (%d)", req.OrgID, orgID),
		})
		return
	}

	var site models.Site
	err := h.db.QueryRow(
		"INSERT INTO sites (org_id, name) VALUES ($1, $2) RETURNING id, org_id, name, created_at",
		req.OrgID,
		req.Name,
	).Scan(&site.ID, &site.OrgID, &site.Name, &site.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create site"})
		return
	}

	c.JSON(http.StatusCreated, site)
}

// List handles GET /api/sites
// Filters by organization from context (multi-tenant isolation)
// @Summary List sites
// @Description Get a list of sites for the authorized organization
// @Tags sites
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Success 200 {array} Site
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/sites [get]
func (h *SitesHandler) List(c *gin.Context) {
	// Get organization ID from context (set by middleware)
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	rows, err := h.db.Query(
		"SELECT id, org_id, name, created_at FROM sites WHERE org_id = $1 ORDER BY id",
		orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query sites"})
		return
	}
	defer rows.Close()

	var sites []models.Site
	for rows.Next() {
		var site models.Site
		if err := rows.Scan(&site.ID, &site.OrgID, &site.Name, &site.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan site"})
			return
		}
		sites = append(sites, site)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating sites"})
		return
	}

	c.JSON(http.StatusOK, sites)
}

// Get handles GET /api/sites/{id}
// @Summary Get a site
// @Description Get a single site by ID
// @Tags sites
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Site ID"
// @Success 200 {object} Site
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Site not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/sites/{id} [get]
func (h *SitesHandler) Get(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	id := c.Param("id")

	var site models.Site
	err := h.db.QueryRow(
		"SELECT id, org_id, name, created_at FROM sites WHERE id = $1",
		id,
	).Scan(&site.ID, &site.OrgID, &site.Name, &site.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Site not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get site"})
		return
	}

	// Multi-tenant check
	if site.OrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to site from another organization"})
		return
	}

	c.JSON(http.StatusOK, site)
}

// Delete handles DELETE /api/sites/{id}
// @Summary Delete a site
// @Description Delete a site by ID (cascades to areas, gateways, tags)
// @Tags sites
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Site ID"
// @Success 204 "Site deleted"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Site not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/sites/{id} [delete]
func (h *SitesHandler) Delete(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	id := c.Param("id")

	// Check if site exists and belongs to org
	var siteOrgID int
	err := h.db.QueryRow("SELECT org_id FROM sites WHERE id = $1", id).Scan(&siteOrgID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Site not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check site"})
		return
	}

	if siteOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete site from another organization"})
		return
	}

	// Manual Cascade Delete Transaction
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// 1. Delete Alarms (via tags -> gateways -> areas)
	_, err = tx.Exec(`
		DELETE FROM alarms WHERE tag_id IN (
			SELECT t.id FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			WHERE a.site_id = $1
		)`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related alarms"})
		return
	}

	// 2. Delete Tags
	_, err = tx.Exec(`
		DELETE FROM tags WHERE gateway_id IN (
			SELECT g.id FROM gateways g
			JOIN areas a ON g.area_id = a.id
			WHERE a.site_id = $1
		)`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related tags"})
		return
	}

	// 3. Delete Gateways
	_, err = tx.Exec(`
		DELETE FROM gateways WHERE area_id IN (
			SELECT id FROM areas WHERE site_id = $1
		)`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related gateways"})
		return
	}

	// 4. Delete Areas
	_, err = tx.Exec("DELETE FROM areas WHERE site_id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related areas"})
		return
	}

	// 5. Delete Site
	_, err = tx.Exec("DELETE FROM sites WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete site"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.Status(http.StatusNoContent)
}

// UpdateSiteRequest represents the request body for updating a site
type UpdateSiteRequest struct {
	Name *string `json:"name" binding:"required"`
}

// Update handles PUT /api/sites/{id}
// Filters by organization from context (multi-tenant isolation)
// @Summary Update a site
// @Description Update a site by ID
// @Tags sites
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param id path int true "Site ID"
// @Param request body UpdateSiteRequest true "Site update request"
// @Success 200 {object} Site
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Site not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/sites/{id} [put]
func (h *SitesHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid site ID"})
		return
	}

	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Check if site exists and belongs to org
	var siteOrgID int
	err = h.db.QueryRow("SELECT org_id FROM sites WHERE id = $1", id).Scan(&siteOrgID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Site not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check site"})
		return
	}

	if siteOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot update site from another organization"})
		return
	}

	var req UpdateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var site models.Site
	err = h.db.QueryRow(
		"UPDATE sites SET name = $1 WHERE id = $2 RETURNING id, org_id, name, created_at",
		*req.Name,
		id,
	).Scan(&site.ID, &site.OrgID, &site.Name, &site.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update site"})
		return
	}

	c.JSON(http.StatusOK, site)
}
