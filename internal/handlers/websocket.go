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
	redisClient RedisClient
}

// NewRealtimeHandler creates a new real-time handler
func NewRealtimeHandler(redisClient RedisClient) *RealtimeHandler {
	return &RealtimeHandler{
		redisClient: redisClient,
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
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

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
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
