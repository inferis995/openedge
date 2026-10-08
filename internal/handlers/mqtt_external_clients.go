package handlers

import (
	"context"
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

	"github.com/ralph/industrial-edge-middleware/internal/crypto"
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
	Enabled     bool      `json:"enabled"`
	// OnBroker is false when the broker no longer knows this identity
	// (deleted out-of-band): the registry row is then stale, not authoritative.
	OnBroker bool `json:"on_broker"`
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
		SELECT id, username, description, created_at, COALESCE(enabled, TRUE) FROM mqtt_external_clients
		WHERE org_id = $1 ORDER BY created_at`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list MQTT logins"})
		return
	}
	defer func() { _ = rows.Close() }()
	onBroker := map[string]bool{}
	if h.dynsec != nil {
		if clients, derr := h.dynsec.ListBrokerClients(); derr == nil {
			for _, bc := range clients {
				onBroker[bc.Username] = true
			}
		}
	}
	out := []MQTTExternalClient{}
	for rows.Next() {
		var m MQTTExternalClient
		if err := rows.Scan(&m.ID, &m.Username, &m.Description, &m.CreatedAt, &m.Enabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list MQTT logins"})
			return
		}
		m.OnBroker = onBroker[m.Username]
		out = append(out, m)
	}
	c.JSON(http.StatusOK, out)
}

// Rotate handles POST /api/organizations/:id/mqtt-clients/:clientId/rotate.
// A new password — generated, or operator-chosen (12+ chars) via
// {"password": "..."} — shown once. The username never changes, so the
// external system keeps its configuration and only the secret changes.
func (h *MQTTExternalClientsHandler) Rotate(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("clientId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login id"})
		return
	}
	var opt struct {
		Password string `json:"password"`
	}
	_ = c.ShouldBindJSON(&opt)
	if checkErr := checkCustomPassword(opt.Password); checkErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": checkErr.Error()})
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
	password := strings.TrimSpace(opt.Password)
	if password == "" {
		var genErr error
		password, genErr = mqtt.GeneratePassword()
		if genErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate a password"})
			return
		}
	}
	if err := h.dynsec.SetExternalPassword(username, password); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the broker did not accept the new password"})
		return
	}
	if err := h.storePassword(ctx, id, password); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the broker changed it but storing the secret failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "username": username, "password": password,
		"note": "Copy the password now — it is never shown again."})
}

// SetEnabled handles POST /api/organizations/:id/mqtt-clients/:clientId/enabled.
// Disabling cuts access immediately and keeps the identity (and its audit
// trail); the login can be re-enabled without touching the external system.
func (h *MQTTExternalClientsHandler) SetEnabled(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("clientId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login id"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "enabled is required"})
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
	if err := h.dynsec.SetClientEnabled(username, req.Enabled); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the broker did not accept the change"})
		return
	}
	if _, err := h.db.ExecContext(ctx,
		`UPDATE mqtt_external_clients SET enabled = $1 WHERE id = $2`, req.Enabled, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "the broker changed it but the registry write failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "enabled": req.Enabled})
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
		// Optional operator-chosen secret (12+ chars); empty = generated.
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description required (what this login is for, max 100 characters)"})
		return
	}
	if err := checkCustomPassword(req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	password := strings.TrimSpace(req.Password)
	if password == "" {
		var genErr error
		password, genErr = mqtt.GeneratePassword()
		if genErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate a password"})
			return
		}
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
	if err := h.storePassword(ctx, m.ID, password); err != nil {
		_ = h.dynsec.DeleteExternalReader(username)
		_, _ = h.db.ExecContext(ctx, `DELETE FROM mqtt_external_clients WHERE id = $1`, m.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store the secret"})
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

// checkCustomPassword accepts an operator-chosen secret, or "" for
// "generate one for me". Stored secrets must be strong enough to survive
// offline guessing: 12+ characters, no other rules — passphrases welcome.
func checkCustomPassword(pw string) error {
	if pw == "" {
		return nil
	}
	if len([]rune(strings.TrimSpace(pw))) < 12 {
		return fmt.Errorf("a chosen password needs at least 12 characters")
	}
	return nil
}

// storePassword encrypts the secret for later reveal by an admin. The broker
// only ever sees the plaintext once, at set time; what rests here is opaque
// without the platform key.
func (h *MQTTExternalClientsHandler) storePassword(ctx context.Context, id int, password string) error {
	enc, err := crypto.Encrypt(password)
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx,
		`UPDATE mqtt_external_clients SET password_enc = $1 WHERE id = $2`, enc, id)
	return err
}

// auditReveal records who looked at a secret, without the secret itself.
// Passwords are never written to any log.
func (h *MQTTExternalClientsHandler) auditReveal(c *gin.Context, orgID int, username string) {
	var uid *int
	if id, ok := middleware.GetUserID(c); ok {
		uid = &id
	}
	var actor string
	if uid != nil {
		_ = h.db.QueryRowContext(c.Request.Context(),
			`SELECT username FROM users WHERE id = $1`, *uid).Scan(&actor)
	}
	_, _ = h.db.ExecContext(c.Request.Context(),
		`INSERT INTO audit_logs (user_id, username, action, ip_address, user_agent, details, success)
		 VALUES ($1, $2, 'mqtt.password.reveal', $3, $4, $5, TRUE)`,
		uid, actor, c.ClientIP(), c.Request.UserAgent(),
		fmt.Sprintf(`{"org_id":%d,"mqtt_username":%q}`, orgID, username))
}

// RevealGlobal is the global-admin variant of Reveal: the login id is global,
// no organization scoping. Wired on the system router only.
func (h *MQTTExternalClientsHandler) RevealGlobal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login id"})
		return
	}
	ctx := c.Request.Context()
	var username, enc string
	var rowOrg int
	err = h.db.QueryRowContext(ctx,
		`SELECT username, COALESCE(password_enc,''), org_id FROM mqtt_external_clients WHERE id = $1`,
		id).Scan(&username, &enc, &rowOrg)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "login not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the login"})
		return
	}
	if enc == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no stored password for this login — rotate it once to set one"})
		return
	}
	password, err := crypto.Decrypt(enc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt the password"})
		return
	}
	h.auditReveal(c, rowOrg, username)
	c.JSON(http.StatusOK, gin.H{"id": id, "username": username, "password": password, "org_id": rowOrg})
}

// Reveal handles GET /api/organizations/:id/mqtt-clients/:clientId/password
// — the org-scoped variant. Shows the current secret once more — audited,
// never logged.
func (h *MQTTExternalClientsHandler) Reveal(c *gin.Context) {
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
	var username, enc string
	err = h.db.QueryRowContext(ctx,
		`SELECT username, COALESCE(password_enc,'') FROM mqtt_external_clients WHERE id = $1 AND org_id = $2`,
		id, orgID).Scan(&username, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "login not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the login"})
		return
	}
	if enc == "" {
		// Created before secrets were stored: only rotation can give a
		// showable password from here on.
		c.JSON(http.StatusNotFound, gin.H{"error": "no stored password for this login — rotate it once to set one"})
		return
	}
	password, err := crypto.Decrypt(enc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt the password"})
		return
	}
	h.auditReveal(c, orgID, username)
	c.JSON(http.StatusOK, gin.H{"id": id, "username": username, "password": password})
}

// MaintainedClient is one registry row with its organization, for the
// global-admin maintenance view across tenants.
type MaintainedClient struct {
	MQTTExternalClient
	OrgID   int    `json:"org_id"`
	OrgName string `json:"org_name"`
}

// ListAll handles GET /api/system/mqtt/external-clients?org_id=N — the whole
// registry (or one org), global admin only. Same rows as the per-org list,
// with the organization attached.
func (h *MQTTExternalClientsHandler) ListAll(c *gin.Context) {
	orgID, _ := strconv.Atoi(c.Query("org_id"))
	query := `SELECT m.id, m.username, m.description, m.created_at, COALESCE(m.enabled, TRUE),
	                 m.org_id, o.name
	          FROM mqtt_external_clients m JOIN organizations o ON o.id = m.org_id`
	var rows *sql.Rows
	var err error
	if orgID > 0 {
		query += ` WHERE m.org_id = $1 ORDER BY m.created_at`
		rows, err = h.db.QueryContext(c.Request.Context(), query, orgID)
	} else {
		query += ` ORDER BY m.org_id, m.created_at`
		rows, err = h.db.QueryContext(c.Request.Context(), query)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list MQTT logins"})
		return
	}
	defer func() { _ = rows.Close() }()
	onBroker := map[string]bool{}
	if h.dynsec != nil {
		if clients, derr := h.dynsec.ListBrokerClients(); derr == nil {
			for _, bc := range clients {
				onBroker[bc.Username] = true
			}
		}
	}
	items := []MaintainedClient{}
	for rows.Next() {
		var m MaintainedClient
		if err := rows.Scan(&m.ID, &m.Username, &m.Description, &m.CreatedAt, &m.Enabled, &m.OrgID, &m.OrgName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list MQTT logins"})
			return
		}
		m.OnBroker = onBroker[m.Username]
		items = append(items, m)
	}
	if items == nil {
		items = []MaintainedClient{}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// BrokerList handles GET /api/system/mqtt/broker-clients — every identity the
// broker knows (drivers, UI viewers, external systems), read-only. The admin
// reconciles the broker against the registry here: anything on the broker
// with no registry row was created out-of-band.
func (h *MQTTExternalClientsHandler) BrokerList(c *gin.Context) {
	if h.dynsec == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the broker's dynamic security is not available"})
		return
	}
	clients, err := h.dynsec.ListBrokerClients()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("broker query failed: %v", err)})
		return
	}
	if clients == nil {
		clients = []mqtt.BrokerClient{}
	}
	c.JSON(http.StatusOK, gin.H{"items": clients, "total": len(clients)})
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
