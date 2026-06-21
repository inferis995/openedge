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

// Organization represents an organization in the system
type Organization struct {
	ID        int    `json:"id" example:"1"`
	Name      string `json:"name" example:"Acme Corp"`
	CreatedAt string `json:"created_at" example:"2024-01-24T10:00:00Z"`
}

// OrganizationsHandler handles organization-related HTTP requests
type OrganizationsHandler struct {
	db           *sql.DB
	mqttClient   *mqtt.Client
	dynsecClient *mqtt.DynsecClient // nil if MQTT auth is not configured
}

// NewOrganizationsHandler creates a new organizations handler
func NewOrganizationsHandler(db *sql.DB, mqttClient *mqtt.Client, dynsecClient *mqtt.DynsecClient) *OrganizationsHandler {
	return &OrganizationsHandler{
		db:           db,
		mqttClient:   mqttClient,
		dynsecClient: dynsecClient,
	}
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

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
		return
	}
	defer tx.Rollback() //nolint:errcheck

	var org models.Organization
	if err = tx.QueryRowContext(c.Request.Context(),
		"INSERT INTO organizations (name) VALUES ($1) RETURNING id, name, created_at",
		req.Name,
	).Scan(&org.ID, &org.Name, &org.CreatedAt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create organization"})
		return
	}

	// Provision per-org MQTT credentials if Dynamic Security is available
	if h.dynsecClient != nil {
		mqttUser := fmt.Sprintf("org-%d", org.ID)
		mqttPass, genErr := mqtt.GeneratePassword()
		if genErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate MQTT credentials"})
			return
		}

		if dynsecErr := h.dynsecClient.CreateOrgUser(org.ID, org.Name, mqttUser, mqttPass); dynsecErr != nil {
			// Log but don't block org creation — MQTT creds can be re-provisioned later
			fmt.Printf("[ORG] WARNING: failed to create MQTT user for org %d: %v\n", org.ID, dynsecErr)
		} else {
			// Store credentials so edge manager can pull them via the config API
			if _, err := tx.ExecContext(c.Request.Context(),
				`INSERT INTO org_mqtt_credentials (org_id, username, password)
				 VALUES ($1, $2, $3)
				 ON CONFLICT (org_id) DO UPDATE SET username = $2, password = $3`,
				org.ID, mqttUser, mqttPass,
			); err != nil {
				fmt.Printf("[ORG] WARNING: failed to store MQTT credentials for org %d: %v\n", org.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
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
	var rows *sql.Rows
	var err error

	if middleware.IsGlobalAdmin(c) {
		rows, err = h.db.Query("SELECT id, name, created_at FROM organizations ORDER BY id")
	} else {
		orgID, _ := middleware.GetOrganizationID(c)
		rows, err = h.db.Query("SELECT id, name, created_at FROM organizations WHERE id = $1", orgID)
	}
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

	// Non-admin users can only access their own organization
	if !middleware.IsGlobalAdmin(c) {
		orgID, _ := middleware.GetOrganizationID(c)
		requestedID, err := strconv.Atoi(id)
		if err != nil || requestedID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}
	}

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

	// Update MQTT ACL topic prefix to match the new org name
	if h.dynsecClient != nil {
		if dynsecErr := h.dynsecClient.UpdateOrgRole(org.ID, org.Name); dynsecErr != nil {
			fmt.Printf("[ORG] WARNING: failed to update MQTT role for org %d: %v\n", org.ID, dynsecErr)
		}
	}

	// Trigger reload for all gateways in this organization to update topic names
	if h.mqttClient != nil {
		gatewayIDs, err := h.getGatewayIDsForOrg(org.ID)
		if err == nil {
			for _, gwID := range gatewayIDs {
				topic := fmt.Sprintf("sys/command/reload/%d", gwID)
				h.mqttClient.Publish(topic, "reload")
			}
		}
	}

	c.JSON(http.StatusOK, org)
}

// Helper to get all gateway IDs for an organization
func (h *OrganizationsHandler) getGatewayIDsForOrg(orgID int) ([]int, error) {
	query := `
		SELECT g.id 
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE s.org_id = $1
	`
	rows, err := h.db.Query(query, orgID)
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

	// Check if organization exists and get its ID as int
	idInt, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	var exists bool
	err = h.db.QueryRowContext(c.Request.Context(), "SELECT EXISTS(SELECT 1 FROM organizations WHERE id = $1)", id).Scan(&exists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check organization"})
		return
	}

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Organization not found"})
		return
	}

	// Remove MQTT user before deleting the org
	if h.dynsecClient != nil {
		mqttUser := fmt.Sprintf("org-%d", idInt)
		if dynsecErr := h.dynsecClient.DeleteOrgUser(idInt, mqttUser); dynsecErr != nil {
			fmt.Printf("[ORG] WARNING: failed to delete MQTT user for org %d: %v\n", idInt, dynsecErr)
			// Continue — org_mqtt_credentials will cascade-delete with the org
		}
	}

	// Manual Cascade Delete Transaction
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// 1. Alarms table was removed in migration 004, so skipping relevant deletion step if code wasn't updated.
	// We proceed directly to deleting dependent entities in order.

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

// SetMFARequired handles PUT /api/organizations/:id/mfa-required.
func (h *OrganizationsHandler) SetMFARequired(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		MFARequired bool `json:"mfa_required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.db.ExecContext(c.Request.Context(),
		`UPDATE organizations SET mfa_required=$1 WHERE id=$2`, req.MFARequired, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mfa_required": req.MFARequired})
}
