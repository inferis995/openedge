package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// Organization represents an organization in the system
type Organization struct {
	ID        int    `json:"id" example:"1"`
	Name      string `json:"name" example:"Acme Corp"`
	CreatedAt string `json:"created_at" example:"2024-01-24T10:00:00Z"`
}

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
// @Summary Create a new organization
// @Description Create a new organization with the specified name
// @Tags organizations
// @Accept json
// @Produce json
// @Param request body CreateOrganizationRequest true "Organization creation request"
// @Success 201 {object} Organization
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/organizations [post]
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
// @Summary List all organizations
// @Description Get a list of all organizations
// @Tags organizations
// @Accept json
// @Produce json
// @Success 200 {array} Organization
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/organizations [get]
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

// Get handles GET /api/organizations/{id}
// @Summary Get an organization
// @Description Get a single organization by ID
// @Tags organizations
// @Accept json
// @Produce json
// @Param id path int true "Organization ID"
// @Success 200 {object} Organization
// @Failure 404 {object} map[string]string "Organization not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/organizations/{id} [get]
func (h *OrganizationsHandler) Get(c *gin.Context) {
	id := c.Param("id")

	var org models.Organization
	err := h.db.QueryRow(
		"SELECT id, name, created_at FROM organizations WHERE id = $1",
		id,
	).Scan(&org.ID, &org.Name, &org.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Organization not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get organization"})
		return
	}

	c.JSON(http.StatusOK, org)
}

// UpdateOrganizationRequest represents the request body for updating an organization
type UpdateOrganizationRequest struct {
	Name string `json:"name" binding:"required"`
}

// Update handles PUT /api/organizations/{id}
// @Summary Update an organization
// @Description Update an organization's name by ID
// @Tags organizations
// @Accept json
// @Produce json
// @Param id path int true "Organization ID"
// @Param request body UpdateOrganizationRequest true "Organization update request"
// @Success 200 {object} Organization
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 404 {object} map[string]string "Organization not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/organizations/{id} [put]
func (h *OrganizationsHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var org models.Organization
	err := h.db.QueryRow(
		"UPDATE organizations SET name = $1 WHERE id = $2 RETURNING id, name, created_at",
		req.Name, id,
	).Scan(&org.ID, &org.Name, &org.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Organization not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update organization"})
		return
	}

	c.JSON(http.StatusOK, org)
}

// Delete handles DELETE /api/organizations/{id}
// @Summary Delete an organization
// @Description Delete an organization by ID (cascades to sites, areas, gateways, tags)
// @Tags organizations
// @Accept json
// @Produce json
// @Param id path int true "Organization ID"
// @Success 204 "Organization deleted"
// @Failure 404 {object} map[string]string "Organization not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/organizations/{id} [delete]
func (h *OrganizationsHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	// Check if organization exists
	var exists bool
	err := h.db.QueryRow("SELECT EXISTS(SELECT 1 FROM organizations WHERE id = $1)", id).Scan(&exists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check organization"})
		return
	}

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Organization not found"})
		return
	}

	// Manual Cascade Delete Transaction
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// 1. Delete Alarms (via tags -> gateways -> areas -> sites)
	_, err = tx.Exec(`
		DELETE FROM alarms WHERE tag_id IN (
			SELECT t.id FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			WHERE s.org_id = $1
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
			JOIN sites s ON a.site_id = s.id
			WHERE s.org_id = $1
		)`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related tags"})
		return
	}

	// 3. Delete Gateways
	_, err = tx.Exec(`
		DELETE FROM gateways WHERE area_id IN (
			SELECT a.id FROM areas a
			JOIN sites s ON a.site_id = s.id
			WHERE s.org_id = $1
		)`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related gateways"})
		return
	}

	// 4. Delete Areas
	_, err = tx.Exec(`
		DELETE FROM areas WHERE site_id IN (
			SELECT id FROM sites WHERE org_id = $1
		)`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related areas"})
		return
	}

	// 5. Delete Sites
	_, err = tx.Exec("DELETE FROM sites WHERE org_id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete related sites"})
		return
	}

	// 6. Delete Organization
	_, err = tx.Exec("DELETE FROM organizations WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete organization"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.Status(http.StatusNoContent)
}
