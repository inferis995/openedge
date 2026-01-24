package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// OrganizationsHandler handles organization-related HTTP requests
type OrganizationsHandler struct {
	db *sql.DB
}

// NewOrganizationsHandler creates a new organizations handler
func NewOrganizationsHandler(db *sql.DB) *OrganizationsHandler {
	return &OrganizationsHandler{db: db}
}

// CreateOrganizationRequest represents the request body for creating an organization
type CreateOrganizationRequest struct {
	Name string `json:"name" binding:"required"`
}

// Create handles POST /api/organizations
func (h *OrganizationsHandler) Create(c *gin.Context) {
	var req CreateOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var org models.Organization
	err := h.db.QueryRow(
		"INSERT INTO organizations (name) VALUES ($1) RETURNING id, name, created_at",
		req.Name,
	).Scan(&org.ID, &org.Name, &org.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create organization"})
		return
	}

	c.JSON(http.StatusCreated, org)
}

// List handles GET /api/organizations
func (h *OrganizationsHandler) List(c *gin.Context) {
	rows, err := h.db.Query("SELECT id, name, created_at FROM organizations ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query organizations"})
		return
	}
	defer rows.Close()

	var orgs []models.Organization
	for rows.Next() {
		var org models.Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan organization"})
			return
		}
		orgs = append(orgs, org)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating organizations"})
		return
	}

	c.JSON(http.StatusOK, orgs)
}
