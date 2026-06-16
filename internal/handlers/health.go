package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	internalredis "github.com/ralph/industrial-edge-middleware/internal/redis"
)

var processStart = time.Now()

// HealthHandler provides liveness, readiness, and detailed diagnostics endpoints.
type HealthHandler struct {
	db    *sql.DB
	redis *internalredis.Client
}

// NewHealthHandler creates a HealthHandler. redis may be nil if Redis is unavailable.
func NewHealthHandler(db *sql.DB, redis *internalredis.Client) *HealthHandler {
	return &HealthHandler{db: db, redis: redis}
}

// Live handles GET /health — liveness probe. Always 200 if process is alive.
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "ts": time.Now().Unix()})
}

// Ready handles GET /ready — readiness probe. Checks DB and Redis.
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"error":  "db: " + err.Error(),
		})
		return
	}

	if h.redis != nil {
		if err := h.redis.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "not_ready",
				"error":  "redis: " + err.Error(),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// Detailed handles GET /api/health/detailed — full diagnostics (requires auth).
func (h *HealthHandler) Detailed(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// DB latency + stats
	dbStart := time.Now()
	dbOk := h.db.PingContext(ctx) == nil
	dbLatency := time.Since(dbStart).Milliseconds()
	dbStats := h.db.Stats()

	dbInfo := gin.H{
		"ok":               dbOk,
		"latency_ms":       dbLatency,
		"open_connections": dbStats.OpenConnections,
		"in_use":           dbStats.InUse,
	}

	// Redis latency
	var redisInfo interface{}
	if h.redis != nil {
		redisStart := time.Now()
		redisOk := h.redis.Ping() == nil
		redisLatency := time.Since(redisStart).Milliseconds()
		redisInfo = gin.H{
			"ok":         redisOk,
			"latency_ms": redisLatency,
		}
	}

	// Memory stats
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	memInfo := gin.H{
		"alloc_mb": float64(mem.Alloc) / 1024 / 1024,
		"sys_mb":   float64(mem.Sys) / 1024 / 1024,
		"num_gc":   mem.NumGC,
	}

	c.JSON(http.StatusOK, gin.H{
		"db":             dbInfo,
		"redis":          redisInfo,
		"memory":         memInfo,
		"goroutines":     runtime.NumGoroutine(),
		"uptime_seconds": int64(time.Since(processStart).Seconds()),
	})
}

// EdgeHeartbeat handles POST /api/edge/heartbeat (requires auth).
// Edge agent PINGs core API every 30s; we update gateways.last_seen_at + agent_version.
func (h *HealthHandler) EdgeHeartbeat(c *gin.Context) {
	var body struct {
		OrgID        int    `json:"org_id"`
		AgentVersion string `json:"agent_version"`
		Ts           int64  `json:"ts"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, _ = h.db.ExecContext(c.Request.Context(),
		`UPDATE gateways SET last_seen_at=NOW(), agent_version=$1
		 WHERE area_id IN (
		   SELECT a.id FROM areas a
		   JOIN sites s ON s.id = a.site_id
		   JOIN organizations o ON o.id = s.org_id
		   WHERE o.id = $2
		 )`, body.AgentVersion, body.OrgID)

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
