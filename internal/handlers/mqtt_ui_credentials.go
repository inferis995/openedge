package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
)

// The credentials the browser opens its MQTT connection with.
//
// Why this exists at all: the web UI watches live values over a WebSocket
// straight to the broker (mqtt-client.ts -> nginx /mqtt -> mosquitto:9001), and
// it used to do that ANONYMOUSLY. On the cloud deployment nginx sits behind
// Traefik on 443, so "anonymous" meant anyone on the internet: subscribe to #
// and read every tenant's live plant data, publish cmd/write/{gateway} and
// drive a setpoint on somebody's machine. The broker listener had no ACLs at
// all, because with per_listener_settings the dynsec plugin was attached to the
// other listener only.
//
// So the listener is authenticated again, and this endpoint is how the UI gets
// an identity: per organization, read-only, issued only to a session that is
// already authenticated for that organization.
//
// What this deliberately does NOT hand out is the org's EDGE credential from the
// same table. That one may publish tag data — a gateway has to — and giving it
// to a browser would let any signed-in user, including a read-only one, inject
// readings indistinguishable from a real PLC's.

type MQTTUICredentialsHandler struct {
	db     *sql.DB
	dynsec *mqtt.DynsecClient
}

func NewMQTTUICredentialsHandler(db *sql.DB, dynsec *mqtt.DynsecClient) *MQTTUICredentialsHandler {
	return &MQTTUICredentialsHandler{db: db, dynsec: dynsec}
}

type mqttUICredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// The path the UI connects to, so the frontend does not hard-code the proxy
	// layout in two places.
	Path string `json:"path"`
}

// Get handles GET /api/mqtt/ui-credentials.
//
// Org-scoped by the JWT, never by anything the caller sends: middleware
// .GetOrganizationID reads the claim, not the X-Organization-ID header, which is
// what stops a signed-in user of one tenant asking for another tenant's
// identity. A global admin has no org of their own, so they must select one —
// and that selection goes through the same middleware.
func (h *MQTTUICredentialsHandler) Get(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok || orgID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "select an organization first — MQTT credentials are per organization",
		})
		return
	}

	ctx := c.Request.Context()

	var orgName string
	if err := h.db.QueryRowContext(ctx,
		`SELECT name FROM organizations WHERE id = $1`, orgID,
	).Scan(&orgName); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}

	// Already provisioned? Hand it back.
	var username, password sql.NullString
	err := h.db.QueryRowContext(ctx,
		`SELECT ui_username, ui_password FROM org_mqtt_credentials WHERE org_id = $1`, orgID,
	).Scan(&username, &password)
	if err != nil && err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read MQTT credentials"})
		return
	}
	if username.Valid && username.String != "" && password.Valid && password.String != "" {
		c.JSON(http.StatusOK, mqttUICredentials{
			Username: username.String, Password: password.String, Path: "/mqtt",
		})
		return
	}

	// Not provisioned: this organization predates the viewer identity. Make one
	// now rather than making the operator run a backfill — the alternative is a
	// deployment where the UI silently stays disconnected for older tenants.
	if h.dynsec == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "the broker's dynamic security is not available; MQTT credentials cannot be issued",
		})
		return
	}

	newPassword, err := mqtt.GeneratePassword()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate MQTT credentials"})
		return
	}
	newUsername := mqtt.OrgViewerUsername(orgID)

	// Broker first, database second. The other order would store a secret the
	// broker never accepted, and the UI would fail to connect with a credential
	// that looks perfectly valid in the database.
	if err := h.dynsec.EnsureOrgViewer(orgID, orgName, newUsername, newPassword); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "failed to provision MQTT credentials on the broker",
		})
		return
	}

	if _, err := h.db.ExecContext(ctx,
		`INSERT INTO org_mqtt_credentials (org_id, username, password, ui_username, ui_password)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (org_id) DO UPDATE SET ui_username = $4, ui_password = $5`,
		orgID, newUsername, newPassword, newUsername, newPassword,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store MQTT credentials"})
		return
	}

	c.JSON(http.StatusOK, mqttUICredentials{
		Username: newUsername, Password: newPassword, Path: "/mqtt",
	})
}
