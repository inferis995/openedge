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

// AreasHandler handles area-related HTTP requests
type AreasHandler struct {
	db *sql.DB
}

// NewAreasHandler creates a new areas handler
func NewAreasHandler(db *sql.DB) *AreasHandler {
	return &AreasHandler{db: db}
}

// CreateAreaRequest represents the request body for creating an area
type CreateAreaRequest struct {
	SiteID int    `json:"site_id" binding:"required"`
	Name   string `json:"name" binding:"required"`
}

// Create handles POST /api/areas
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
		"SELECT org_id FROM sites s JOIN areas a ON s.id = a.site_id WHERE s.id = $1",
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
			"error": "Cannot create area for site in different organization",
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
func (h *AreasHandler) List(c *gin.Context) {
	// Get organization ID from context (set by middleware)
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	siteIDStr := c.Query("site_id")
	if siteIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "site_id query parameter is required"})
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
			"error": "Cannot query areas for site in different organization",
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
