package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/naming"
)

// MQTTExternalClientsHandler manages the MQTT logins an organization gives to
// its external systems.
//
// Before this, a SCADA or a Node-RED flow that wanted OpenEdge's data had no
// way in: the broker requires a login, the only logins were the edge's (which
// may publish) and the browser's (handed only to a signed-in session), and
// making one meant editing Mosquitto's dynamic-security file by hand.
type MQTTExternalClientsHandler struct {
	db     *sql.DB
	dynsec *mqtt.DynsecClient
}

func NewMQTTExternalClientsHandler(db *sql.DB, dynsec *mqtt.DynsecClient) *MQTTExternalClientsHandler {
	return &MQTTExternalClientsHandler{db: db, dynsec: dynsec}
}

// MQTTExternalClient is one login, without its password.
type MQTTExternalClient struct {
	ID          int       `json:"id"`
	Username    string    `json:"username"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

func (h *MQTTExternalClientsHandler) org(c *gin.Context) (int, bool) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return 0, false
	}
	return orgID, true
}

// List handles GET /api/organizations/:id/mqtt-clients.
func (h *MQTTExternalClientsHandler) List(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT id, username, description, created_at FROM mqtt_external_clients
		WHERE org_id = $1 ORDER BY created_at`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list MQTT logins"})
		return
	}
	defer func() { _ = rows.Close() }()
	out := []MQTTExternalClient{}
	for rows.Next() {
		var m MQTTExternalClient
		if err := rows.Scan(&m.ID, &m.Username, &m.Description, &m.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list MQTT logins"})
			return
		}
		out = append(out, m)
	}
	c.JSON(http.StatusOK, out)
}

// externalUsername is org-{id}-ext-{what it is for}-{random}: recognizable in
// the broker's own logs, and unique without asking the broker first.
func externalUsername(orgID int, description string) string {
	slug := strings.Trim(naming.Slug(description), "-")
	var b strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	slug = b.String()
	if len(slug) > 24 {
		slug = slug[:24]
	}
	if slug == "" {
		slug = "client"
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("org-%d-ext-%s-%s", orgID, slug, hex.EncodeToString(suffix))
}

// Create handles POST /api/organizations/:id/mqtt-clients.
//
// The password is in the response and nowhere else: it is not stored, and
// cannot be shown again. A lost one means revoking the login and making a new
// one, which is also what rotating it is.
func (h *MQTTExternalClientsHandler) Create(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	var req struct {
		Description string `json:"description" binding:"required,max=100"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description required (what this login is for, max 100 characters)"})
		return
	}
	if h.dynsec == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the broker's dynamic security is not available"})
		return
	}
	ctx := c.Request.Context()

	var orgName string
	if err := h.db.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&orgName); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	sites, err := orgSiteNames(ctx, h.db, orgID)
	if err != nil {
		sites = nil // wider Sparkplug grant; narrowed on the next site change, as for the UI login
	}
	password, err := mqtt.GeneratePassword()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate a password"})
		return
	}
	username := externalUsername(orgID, req.Description)

	// Broker first: a row for a login the broker never accepted would look
	// valid here and fail there.
	if err := h.dynsec.CreateExternalReader(orgID, orgName, username, password, sites...); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the broker did not accept the new login"})
		return
	}
	var createdBy interface{}
	if uid, ok := middleware.GetUserID(c); ok {
		createdBy = uid
	}
	var m MQTTExternalClient
	if err := h.db.QueryRowContext(ctx, `
		INSERT INTO mqtt_external_clients (org_id, username, description, created_by)
		VALUES ($1, $2, $3, $4) RETURNING id, username, description, created_at`,
		orgID, username, strings.TrimSpace(req.Description), createdBy,
	).Scan(&m.ID, &m.Username, &m.Description, &m.CreatedAt); err != nil {
		// Do not leave a login on the broker that nothing here lists.
		_ = h.dynsec.DeleteExternalReader(username)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the login"})
		return
	}
	// Where to connect, as the boxes are told (MQTT_PUBLIC_HOST/PORT). Without
	// a configured public host the address the user reached this API on is a
	// better guess than "localhost".
	host := os.Getenv("MQTT_PUBLIC_HOST")
	if host == "" {
		host = os.Getenv("PUBLIC_HOST")
	}
	if host == "" {
		if h, _, splitErr := net.SplitHostPort(c.Request.Host); splitErr == nil {
			host = h
		} else {
			host = c.Request.Host
		}
	}
	port := mqttPublicPort()
	c.JSON(http.StatusCreated, gin.H{
		"id": m.ID, "username": m.Username, "description": m.Description, "created_at": m.CreatedAt,
		"password":    password,
		"broker_host": host,
		"broker_port": port,
		"broker_tls":  mqtt.SchemeForPort(port) == "ssl",
	})
}

// Delete handles DELETE /api/organizations/:id/mqtt-clients/:clientId. The
// system using it is disconnected at its next reconnect and cannot sign in again.
func (h *MQTTExternalClientsHandler) Delete(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("clientId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login id"})
		return
	}
	ctx := c.Request.Context()
	var username string
	err = h.db.QueryRowContext(ctx,
		`SELECT username FROM mqtt_external_clients WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "login not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the login"})
		return
	}
	if h.dynsec == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the broker's dynamic security is not available"})
		return
	}
	// Broker first: the login must stop working before it stops being listed.
	if err := h.dynsec.DeleteExternalReader(username); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the broker did not remove the login"})
		return
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM mqtt_external_clients WHERE id = $1`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the login was revoked on the broker but is still listed"})
		return
	}
	c.Status(http.StatusNoContent)
}
