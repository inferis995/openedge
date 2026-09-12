package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
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

// DBStats handles GET /api/db/stats — database size and historian statistics.
func (h *HealthHandler) DBStats(c *gin.Context) {
	ctx := c.Request.Context()

	var dbSizeMB float64
	_ = h.db.QueryRowContext(ctx, `SELECT pg_database_size(current_database()) / 1048576.0`).Scan(&dbSizeMB)

	var histRows int64
	var histSizeMB float64
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tag_history`).Scan(&histRows)
	_ = h.db.QueryRowContext(ctx, `SELECT pg_total_relation_size('tag_history') / 1048576.0`).Scan(&histSizeMB)

	var oldestTs, newestTs sql.NullTime
	_ = h.db.QueryRowContext(ctx, `SELECT MIN(time), MAX(time) FROM tag_history`).Scan(&oldestTs, &newestTs)

	type tableRow struct {
		Table  string  `json:"table"`
		Rows   int64   `json:"rows"`
		SizeMB float64 `json:"size_mb"`
	}
	rows, _ := h.db.QueryContext(ctx, `
		SELECT relname, n_live_tup, pg_total_relation_size(relid)/1048576.0
		FROM pg_stat_user_tables ORDER BY pg_total_relation_size(relid) DESC LIMIT 6`)
	var tables []tableRow
	if rows != nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var t tableRow
			if err := rows.Scan(&t.Table, &t.Rows, &t.SizeMB); err != nil {
				log.Printf("Warning: health stats scan error: %v", err)
				continue
			}
			tables = append(tables, t)
		}
	}

	resp := gin.H{
		"db_size_mb":        dbSizeMB,
		"historian_rows":    histRows,
		"historian_size_mb": histSizeMB,
		"tables":            tables,
	}
	if oldestTs.Valid {
		resp["oldest_ts"] = oldestTs.Time
	}
	if newestTs.Valid {
		resp["newest_ts"] = newestTs.Time
	}
	c.JSON(200, resp)
}

// EdgeHeartbeat handles POST /api/edge/heartbeat.
//
// Authenticated with the box's API key, which is also what says which box it
// is. It used to sit behind RequireAuth and read the organization out of the
// request body — two problems in one line. The body is written by the caller,
// so any authenticated user of any tenant could refresh another organization's
// gateways and make a dead plant look alive; and the only thing that ever calls
// this sends an API key, which RequireAuth parses as a JWT and rejects. The
// heartbeat has never once arrived.
func (h *HealthHandler) EdgeHeartbeat(c *gin.Context) {
	orgRaw, ok := c.Get(middleware.APIKeyContextKey)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing org context"})
		return
	}
	orgID, _ := orgRaw.(int)
	agentRaw, _ := c.Get(middleware.APIKeyAgentContextKey)
	agentID, _ := agentRaw.(int)

	var body struct {
		AgentVersion string `json:"agent_version"`
		Ts           int64  `json:"ts"`
	}
	// A missing or malformed body is not a reason to lose the heartbeat: what
	// it proves — that this box is alive — is carried by the credential.
	_ = c.ShouldBindJSON(&body)

	ctx := c.Request.Context()

	if agentID > 0 {
		if _, err := h.db.ExecContext(ctx,
			`UPDATE edge_agents SET last_seen_at = NOW(), agent_version = $2
			 WHERE id = $1 AND org_id = $3`,
			agentID, body.AgentVersion, orgID); err != nil {
			log.Printf("[HEARTBEAT] recording agent %d: %v", agentID, err)
		}
	}

	var agentsInOrg int
	if err := h.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM edge_agents WHERE org_id = $1`, orgID).Scan(&agentsInOrg); err != nil {
		log.Printf("[HEARTBEAT] counting the boxes of org %d: %v", orgID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// A box vouches for the gateways it is responsible for, and for no others.
	// Refreshing every gateway of the organization is how two dead boxes out of
	// three were made to look alive by the survivor.
	var err error
	if edgesync.ScopeIsWholeOrg(agentID, agentsInOrg) {
		_, err = h.db.ExecContext(ctx,
			`UPDATE gateways SET last_seen_at = NOW(), agent_version = $1
			 WHERE area_id IN (
			   SELECT a.id FROM areas a
			   JOIN sites s ON s.id = a.site_id
			   WHERE s.org_id = $2
			 )`, body.AgentVersion, orgID)
	} else {
		_, err = h.db.ExecContext(ctx,
			`UPDATE gateways SET last_seen_at = NOW(), agent_version = $1
			 WHERE edge_agent_id = $2`, body.AgentVersion, agentID)
	}
	if err != nil {
		log.Printf("[HEARTBEAT] recording the gateways of org %d: %v", orgID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "agent_id": agentID})
}
