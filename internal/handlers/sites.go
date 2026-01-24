package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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

// List handles GET /api/sites?org_id={id}
func (h *SitesHandler) List(c *gin.Context) {
	orgIDStr := c.Query("org_id")
	if orgIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id query parameter is required"})
		return
	}

	orgID, err := strconv.Atoi(orgIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid org_id parameter"})
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
