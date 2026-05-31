package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/notifications"
)

// NotificationsHandler exposes the "send test notification" endpoint
// used by the System / Notifications admin page. The notifier itself is
// owned by core-api's main.go (it needs to fan out alarm events on the
// hot path); this handler just calls into it.
type NotificationsHandler struct {
	dispatcher *notifications.Dispatcher
}

// NewNotificationsHandler wires the handler with the dispatcher that
// the rest of the process already shares.
func NewNotificationsHandler(d *notifications.Dispatcher) *NotificationsHandler {
	return &NotificationsHandler{dispatcher: d}
}

// SendTest pushes a synthetic alarm event to every enabled channel.
// Returns per-channel results so the operator UI can show a clear
// "email: OK, telegram: chat not found" report. Admin-only.
func (h *NotificationsHandler) SendTest(c *gin.Context) {
	if h.dispatcher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifier not initialised"})
		return
	}
	errs := h.dispatcher.TestSend(c.Request.Context())
	if len(errs) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "errors": []string{}})
		return
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	// Partial success is still useful for the operator — return 200 with
	// the per-channel error list rather than a generic 500.
	c.JSON(http.StatusOK, gin.H{"ok": false, "errors": msgs})
}
