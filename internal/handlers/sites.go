package handlers

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

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
			"error": "Cannot create site for different organization",
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
