package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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

	var area models.Area
	err := h.db.QueryRow(
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
func (h *AreasHandler) List(c *gin.Context) {
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
