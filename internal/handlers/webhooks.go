package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// WebhooksHandler manages webhook CRUD and delivery.
type WebhooksHandler struct {
	db *sql.DB
}

// NewWebhooksHandler creates a new WebhooksHandler.
func NewWebhooksHandler(db *sql.DB) *WebhooksHandler {
	return &WebhooksHandler{db: db}
}

// Webhook is the DB/API shape for a single webhook subscription.
type Webhook struct {
	ID              int        `json:"id"`
	OrgID           int        `json:"org_id"`
	URL             string     `json:"url"`
	Events          []string   `json:"events"`
	Enabled         bool       `json:"enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	LastTriggeredAt *time.Time `json:"last_triggered_at"`
	LastStatusCode  *int       `json:"last_status_code"`
	LastError       *string    `json:"last_error"`
}

// Create handles POST /api/organizations/:id/webhooks.
func (h *WebhooksHandler) Create(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization id"})
		return
	}
	if !middleware.IsGlobalAdmin(c) {
		if cOrgID, ok := middleware.GetOrganizationID(c); !ok || cOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	}

	var req struct {
		URL    string   `json:"url"    binding:"required,url"`
		Events []string `json:"events" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	secret, err := generateWebhookSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate secret"})
		return
	}

	var wh Webhook
	err = h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO webhooks (org_id, url, secret, events)
		VALUES ($1, $2, $3, $4)
		RETURNING id, org_id, url, events, enabled, created_at`,
		orgID, req.URL, secret, pq.Array(req.Events),
	).Scan(&wh.ID, &wh.OrgID, &wh.URL, pq.Array(&wh.Events), &wh.Enabled, &wh.CreatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create webhook"})
		return
	}
	// Return the secret once — it won't be shown again.
	c.JSON(http.StatusCreated, gin.H{
		"id":         wh.ID,
		"org_id":     wh.OrgID,
		"url":        wh.URL,
		"events":     wh.Events,
		"enabled":    wh.Enabled,
		"created_at": wh.CreatedAt,
		"secret":     secret,
	})
}

// List handles GET /api/organizations/:id/webhooks.
func (h *WebhooksHandler) List(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization id"})
		return
	}
	if !middleware.IsGlobalAdmin(c) {
		if cOrgID, ok := middleware.GetOrganizationID(c); !ok || cOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT id, org_id, url, events, enabled, created_at, last_triggered_at, last_status_code, last_error
		FROM webhooks WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list webhooks"})
		return
	}
	defer rows.Close()

	var list []Webhook
	for rows.Next() {
		var wh Webhook
		if err := rows.Scan(&wh.ID, &wh.OrgID, &wh.URL, pq.Array(&wh.Events),
			&wh.Enabled, &wh.CreatedAt, &wh.LastTriggeredAt, &wh.LastStatusCode, &wh.LastError); err != nil {
			continue
		}
		list = append(list, wh)
	}
	if list == nil {
		list = []Webhook{}
	}
	c.JSON(http.StatusOK, list)
}

// Delete handles DELETE /api/organizations/:id/webhooks/:webhook_id.
func (h *WebhooksHandler) Delete(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	webhookID, err2 := strconv.Atoi(c.Param("webhook_id"))
	if err != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if !middleware.IsGlobalAdmin(c) {
		if cOrgID, ok := middleware.GetOrganizationID(c); !ok || cOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	}

	res, err := h.db.ExecContext(c.Request.Context(),
		`DELETE FROM webhooks WHERE id = $1 AND org_id = $2`, webhookID, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete webhook"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// DeliverWebhookEvent sends the event payload to all enabled webhooks in an org
// that subscribe to the given event type. Runs in a goroutine and retries up to 3 times.
func DeliverWebhookEvent(db *sql.DB, orgID int, event string, payload interface{}) {
	rows, err := db.Query(`
		SELECT id, url, secret FROM webhooks
		WHERE org_id = $1 AND enabled = true AND $2 = ANY(events)`, orgID, event)
	if err != nil {
		return
	}
	defer rows.Close()

	type hook struct {
		id          int
		url, secret string
	}
	var hooks []hook
	for rows.Next() {
		var h hook
		if rows.Scan(&h.id, &h.url, &h.secret) == nil {
			hooks = append(hooks, h)
		}
	}

	if len(hooks) == 0 {
		return
	}

	body, err := json.Marshal(map[string]interface{}{
		"event":        event,
		"org_id":       orgID,
		"payload":      payload,
		"delivered_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}

	for _, h := range hooks {
		go deliverWithRetry(db, h.id, h.url, h.secret, body)
	}
}

func deliverWithRetry(db *sql.DB, webhookID int, url, secret string, body []byte) {
	sig := computeSignature(secret, body)
	client := &http.Client{Timeout: 10 * time.Second}

	var lastCode int
	var lastErr string
	backoff := 2 * time.Second

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			lastErr = err.Error()
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-OpenEdge-Signature", "sha256="+sig)
		req.Header.Set("X-OpenEdge-Event", fmt.Sprintf("delivery-%d", webhookID))

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err.Error()
			slog.Warn("webhook delivery failed", "url", url, "attempt", attempt+1, "err", err)
			continue
		}
		resp.Body.Close()
		lastCode = resp.StatusCode
		lastErr = ""
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}
		lastErr = fmt.Sprintf("HTTP %d", resp.StatusCode)
		slog.Warn("webhook delivery non-2xx", "url", url, "status", resp.StatusCode, "attempt", attempt+1)
	}

	now := time.Now()
	var errArg interface{}
	if lastErr != "" {
		errArg = lastErr
	}
	db.Exec(`UPDATE webhooks SET last_triggered_at = $1, last_status_code = $2, last_error = $3 WHERE id = $4`,
		now, lastCode, errArg, webhookID)
}

func computeSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func generateWebhookSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
