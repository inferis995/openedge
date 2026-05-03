package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
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

// HandleRealtime handles GET /api/ws/realtime
func (h *RealtimeHandler) HandleRealtime(c *gin.Context) {
	orgIDStr := c.Query("org_id")
	if orgIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id is required"})
		return
	}

	orgID, err := strconv.Atoi(orgIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org_id"})
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
