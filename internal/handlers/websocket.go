package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// RealtimeHandler handles WebSocket connections for real-time updates
type RealtimeHandler struct {
	redisClient    RedisClient
	allowedOrigins map[string]struct{}
}

// NewRealtimeHandler creates a new real-time handler.
// allowedOrigins is the same list used for HTTP CORS (from ALLOWED_ORIGINS env var).
func NewRealtimeHandler(redisClient RedisClient, allowedOrigins []string) *RealtimeHandler {
	h := &RealtimeHandler{
		redisClient:    redisClient,
		allowedOrigins: make(map[string]struct{}, len(allowedOrigins)),
	}
	for _, o := range allowedOrigins {
		h.allowedOrigins[o] = struct{}{}
	}
	return h
}

func (h *RealtimeHandler) newUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		// Browsers authenticate by sending ["bearer", <jwt>] as subprotocols
		// (see middleware.WebSocketAuth). Echoing "bearer" back is required
		// for the browser to accept the handshake.
		Subprotocols: []string{"bearer"},
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // same-origin request (no Origin header)
			}
			_, ok := h.allowedOrigins[origin]
			return ok
		},
	}
}

// HandleRealtime handles GET /api/ws/realtime.
//
// The organization is taken from the caller's JWT (via OrganizationContext),
// never from a client-supplied parameter: this socket streams every live tag
// value of an organization, so trusting a query string would let any caller
// read any tenant's process data by incrementing a number.
func (h *RealtimeHandler) HandleRealtime(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		// Global admins have no implicit org: they must name one explicitly
		// (OrganizationContext validates it and sets the context key).
		c.JSON(http.StatusBadRequest, gin.H{"error": "organization context required (set X-Organization-ID or organization_id)"})
		return
	}

	u := h.newUpgrader()
	conn, err := u.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[WS] New connection for organization %d", orgID)

	// Subscribe to organization-specific Redis channel
	channel := fmt.Sprintf("realtime_updates:%d", orgID)
	pubsub := h.redisClient.Subscribe(channel)
	defer pubsub.Close()

	ch := pubsub.Channel()

	// Keep-alive/ping loop (optional but recommended)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(30 * time.Second):
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()
	defer close(stop)

	// Pump messages from Redis to WebSocket
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				log.Printf("[WS] Error writing message: %v", err)
				return
			}
		case <-c.Request.Context().Done():
			return
		}
	}
}

// Note: I will need to update the RedisClient interface to support PubSub
