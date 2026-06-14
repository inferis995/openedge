package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// FleetHandler manages OTA updates and fleet-level operations.
type FleetHandler struct {
	db          *sql.DB
	mqttClient  MQTTClient
	redisClient RedisClient
}

func NewFleetHandler(db *sql.DB, mqttClient MQTTClient, redisClient RedisClient) *FleetHandler {
	return &FleetHandler{db: db, mqttClient: mqttClient, redisClient: redisClient}
}

// EdgeFleetStatus is the per-org edge status for the fleet view.
type EdgeFleetStatus struct {
	OrgID    int     `json:"org_id"`
	OrgName  string  `json:"org_name"`
	Online   bool    `json:"online"`
	LastPing *string `json:"last_ping"`
	GwCount  int     `json:"gateway_count"`
}

// GetFleetStatus handles GET /api/fleet/status (global admin only).
// Returns the online/offline status of every org's edge agent.
func (h *FleetHandler) GetFleetStatus(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT o.id, o.name, COUNT(g.id) as gw_count
		FROM organizations o
		LEFT JOIN gateways g ON g.org_id = o.id
		GROUP BY o.id, o.name
		ORDER BY o.name
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer func() { _ = rows.Close() }()

	results := []EdgeFleetStatus{}
	for rows.Next() {
		var s EdgeFleetStatus
		if err := rows.Scan(&s.OrgID, &s.OrgName, &s.GwCount); err != nil {
			continue
		}
		// Check Redis heartbeat
		if h.redisClient != nil {
			key := fmt.Sprintf("edge:ping:%d", s.OrgID)
			val, redisErr := h.redisClient.Get(key)
			if redisErr == nil && val != "" {
				s.Online = true
				s.LastPing = &val
			}
		}
		results = append(results, s)
	}
	c.JSON(http.StatusOK, results)
}

// RestartEdge handles POST /api/organizations/:id/edge-restart.
// Publishes sys/restart/{org_id} to trigger a driver restart on the edge.
func (h *FleetHandler) RestartEdge(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}
	if h.mqttClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MQTT not connected"})
		return
	}

	topic := fmt.Sprintf("sys/restart/%d", orgID)
	payload, _ := json.Marshal(map[string]interface{}{
		"org_id":       orgID,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err := h.mqttClient.Publish(topic, payload); err != nil {
		log.Printf("[FLEET] failed to publish restart for org %d: %v", orgID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "publish failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "restart command sent", "org_id": orgID})
}

// UpdateEdge handles POST /api/organizations/:id/edge-update.
// Publishes sys/update/{org_id} with the new image tag for OTA update.
func (h *FleetHandler) UpdateEdge(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}

	var req struct {
		Version string `json:"version" binding:"required"`
		Image   string `json:"image"`
	}
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
		return
	}
	if req.Image == "" {
		req.Image = fmt.Sprintf("ghcr.io/inferis995/openedge-driver-manager:%s", req.Version)
	}

	if h.mqttClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MQTT not connected"})
		return
	}

	topic := fmt.Sprintf("sys/update/%d", orgID)
	payload, _ := json.Marshal(map[string]interface{}{
		"org_id":       orgID,
		"version":      req.Version,
		"image":        req.Image,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err := h.mqttClient.Publish(topic, payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "publish failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "OTA update command sent",
		"org_id":  orgID,
		"version": req.Version,
		"image":   req.Image,
	})
}

// GetSSOProviders handles GET /api/organizations/:id/sso-providers (admin).
func (h *FleetHandler) GetSSOProviders(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT id, provider, client_id, COALESCE(tenant_id,''), COALESCE(domain_hint,''), enabled, created_at
		FROM sso_providers WHERE org_id = $1 ORDER BY provider
	`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer func() { _ = rows.Close() }()

	type SSOProviderRow struct {
		ID         int    `json:"id"`
		Provider   string `json:"provider"`
		ClientID   string `json:"client_id"`
		TenantID   string `json:"tenant_id,omitempty"`
		DomainHint string `json:"domain_hint,omitempty"`
		Enabled    bool   `json:"enabled"`
		CreatedAt  string `json:"created_at"`
	}

	providers := []SSOProviderRow{}
	for rows.Next() {
		var p SSOProviderRow
		var createdAt time.Time
		if err := rows.Scan(&p.ID, &p.Provider, &p.ClientID, &p.TenantID, &p.DomainHint, &p.Enabled, &createdAt); err == nil {
			p.CreatedAt = createdAt.Format(time.RFC3339)
			providers = append(providers, p)
		}
	}
	c.JSON(http.StatusOK, providers)
}

// UpsertSSOProvider handles POST /api/organizations/:id/sso-providers.
func (h *FleetHandler) UpsertSSOProvider(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}

	var req struct {
		Provider     string `json:"provider" binding:"required"`
		ClientID     string `json:"client_id" binding:"required"`
		ClientSecret string `json:"client_secret" binding:"required"`
		TenantID     string `json:"tenant_id"`
		DomainHint   string `json:"domain_hint"`
		Enabled      bool   `json:"enabled"`
	}
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
		return
	}

	_, err = h.db.ExecContext(c.Request.Context(), `
		INSERT INTO sso_providers (org_id, provider, client_id, client_secret, tenant_id, domain_hint, enabled)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), $7)
		ON CONFLICT (org_id, provider) DO UPDATE SET
			client_id = EXCLUDED.client_id,
			client_secret = EXCLUDED.client_secret,
			tenant_id = EXCLUDED.tenant_id,
			domain_hint = EXCLUDED.domain_hint,
			enabled = EXCLUDED.enabled
	`, orgID, req.Provider, req.ClientID, req.ClientSecret, req.TenantID, req.DomainHint, req.Enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "SSO provider saved"})
}

// DeleteSSOProvider handles DELETE /api/organizations/:id/sso-providers/:provider.
func (h *FleetHandler) DeleteSSOProvider(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}
	provider := c.Param("provider")

	if _, err := h.db.ExecContext(c.Request.Context(), `DELETE FROM sso_providers WHERE org_id = $1 AND provider = $2`, orgID, provider); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "SSO provider deleted"})
}

// GetUserPermissions handles GET /api/users/:id/permissions.
func (h *FleetHandler) GetUserPermissions(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	var p struct {
		UserID               int  `json:"user_id"`
		CanWriteTags         bool `json:"can_write_tags"`
		CanAckAlarms         bool `json:"can_ack_alarms"`
		CanExportData        bool `json:"can_export_data"`
		CanManageRecipes     bool `json:"can_manage_recipes"`
		CanManageShifts      bool `json:"can_manage_shifts"`
		CanViewAudit         bool `json:"can_view_audit"`
		CanDownloadInstaller bool `json:"can_download_installer"`
	}
	p.UserID = userID

	// No row → return defaults (all false).
	_ = h.db.QueryRowContext(c.Request.Context(), `
		SELECT can_write_tags, can_ack_alarms, can_export_data, can_manage_recipes,
		       can_manage_shifts, can_view_audit, can_download_installer
		FROM role_permissions WHERE user_id = $1
	`, userID).Scan(&p.CanWriteTags, &p.CanAckAlarms, &p.CanExportData, &p.CanManageRecipes,
		&p.CanManageShifts, &p.CanViewAudit, &p.CanDownloadInstaller)

	c.JSON(http.StatusOK, p)
}

// UpdateUserPermissions handles PUT /api/users/:id/permissions.
func (h *FleetHandler) UpdateUserPermissions(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	var req struct {
		CanWriteTags         bool `json:"can_write_tags"`
		CanAckAlarms         bool `json:"can_ack_alarms"`
		CanExportData        bool `json:"can_export_data"`
		CanManageRecipes     bool `json:"can_manage_recipes"`
		CanManageShifts      bool `json:"can_manage_shifts"`
		CanViewAudit         bool `json:"can_view_audit"`
		CanDownloadInstaller bool `json:"can_download_installer"`
	}
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
		return
	}

	_, err = h.db.ExecContext(c.Request.Context(), `
		INSERT INTO role_permissions
			(user_id, can_write_tags, can_ack_alarms, can_export_data, can_manage_recipes,
			 can_manage_shifts, can_view_audit, can_download_installer, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			can_write_tags = EXCLUDED.can_write_tags,
			can_ack_alarms = EXCLUDED.can_ack_alarms,
			can_export_data = EXCLUDED.can_export_data,
			can_manage_recipes = EXCLUDED.can_manage_recipes,
			can_manage_shifts = EXCLUDED.can_manage_shifts,
			can_view_audit = EXCLUDED.can_view_audit,
			can_download_installer = EXCLUDED.can_download_installer,
			updated_at = NOW()
	`, userID, req.CanWriteTags, req.CanAckAlarms, req.CanExportData, req.CanManageRecipes,
		req.CanManageShifts, req.CanViewAudit, req.CanDownloadInstaller)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "permissions updated"})
}
