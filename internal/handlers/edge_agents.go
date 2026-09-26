package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// EdgeAgentsHandler manages the boxes installed for an organization, and
// answers the one question an administrator mixing a server with boxes has to
// be able to answer: who polls which PLC.
type EdgeAgentsHandler struct {
	db *sql.DB
}

func NewEdgeAgentsHandler(db *sql.DB) *EdgeAgentsHandler {
	return &EdgeAgentsHandler{db: db}
}

// serverPollerFreshness is how recently the server's own driver-manager must
// have reported for the server to count as polling. It reports on every sync,
// every few seconds; minutes of silence mean it is not running.
const serverPollerFreshness = 5 * time.Minute

// EdgeAgentRow is one installed box.
type EdgeAgentRow struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	Scope        string     `json:"scope"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	AgentVersion string     `json:"agent_version,omitempty"`
	// Gateways is how many gateways this box polls: those assigned to it and,
	// with scope "all", those assigned to nobody.
	Gateways int `json:"gateways"`
}

// List handles GET /api/organizations/:id/edge-agents.
func (h *EdgeAgentsHandler) List(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(scope, 'assigned'), created_at, last_seen_at, COALESCE(agent_version, '')
		FROM edge_agents WHERE org_id = $1 ORDER BY created_at`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list edge agents"})
		return
	}
	defer func() { _ = rows.Close() }()

	agents := []EdgeAgentRow{}
	boxes := []edgesync.Box{}
	for rows.Next() {
		var a EdgeAgentRow
		var lastSeen sql.NullTime
		if scanErr := rows.Scan(&a.ID, &a.Name, &a.Scope, &a.CreatedAt, &lastSeen, &a.AgentVersion); scanErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read an edge agent"})
			return
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			a.LastSeenAt = &t
		}
		agents = append(agents, a)
		boxes = append(boxes, edgesync.Box{ID: a.ID, Scope: a.Scope})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list edge agents"})
		return
	}

	gateways, err := orgGatewayOwners(ctx, h.db, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read gateway assignments"})
		return
	}
	counts := map[int]int{}
	for i := range gateways {
		counts[edgesync.Owner(&gateways[i], boxes)]++
	}
	for i := range agents {
		agents[i].Gateways = counts[agents[i].ID]
	}

	serverPolls := h.serverPolls(ctx)
	serverGateways := counts[edgesync.Server]
	unpolled := 0
	if !serverPolls {
		unpolled = serverGateways
	}

	c.JSON(http.StatusOK, gin.H{
		"agents": agents,
		// Gateways no box is responsible for: the server's.
		"server_gateways": serverGateways,
		// Whether the server's own driver-manager is running. Where it is
		// not — a server in the cloud — the server's gateways are polled by
		// nobody, and that is what "unpolled_gateways" counts.
		"server_polls":      serverPolls,
		"unpolled_gateways": unpolled,
		// Kept for clients written against the first version of this
		// endpoint; it now means exactly what it says.
		"unassigned_gateways": unpolled,
	})
}

// UpdateEdgeAgentRequest changes a box's name or scope.
type UpdateEdgeAgentRequest struct {
	Name  *string `json:"name"`
	Scope *string `json:"scope"`
}

// Update handles PUT /api/organizations/:id/edge-agents/:agentId.
func (h *EdgeAgentsHandler) Update(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	agentID, err := strconv.Atoi(c.Param("agentId"))
	if err != nil || agentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid edge agent id"})
		return
	}
	var req UpdateEdgeAgentRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Name == nil && req.Scope == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to change: send name and/or scope"})
		return
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1 to 100 characters"})
			return
		}
		req.Name = &n
	}
	if req.Scope != nil && !edgesync.ValidScope(*req.Scope) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": `scope must be "assigned" (only the gateways given to this box) or "all" (also every gateway given to no box)`,
		})
		return
	}

	ctx := c.Request.Context()
	if req.Scope != nil && *req.Scope == edgesync.ScopeAll {
		// One per organization: two boxes both taking every unassigned
		// gateway would poll the same PLCs. Said in words, with the name of
		// the box that has it, rather than as a constraint violation.
		var other string
		otherErr := h.db.QueryRowContext(ctx,
			`SELECT name FROM edge_agents WHERE org_id = $1 AND scope = 'all' AND id <> $2`,
			orgID, agentID).Scan(&other)
		if otherErr == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error": `the box "` + other + `" already polls every unassigned gateway; set it to "assigned" first`,
			})
			return
		}
		if !errors.Is(otherErr, sql.ErrNoRows) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check the other boxes"})
			return
		}
	}

	res, err := h.db.ExecContext(ctx, `
		UPDATE edge_agents
		SET name = COALESCE($1, name), scope = COALESCE($2, scope)
		WHERE id = $3 AND org_id = $4`, req.Name, req.Scope, agentID, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update the edge agent"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "edge agent not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete handles DELETE /api/organizations/:id/edge-agents/:agentId.
//
// The box's keys are revoked in the same transaction, and that is the point of
// doing it here. Deleting the row alone sets its keys' edge_agent_id to NULL,
// and a key with no box behind it is treated as one minted before boxes had
// identities — which polls every gateway of the organization. A box removed
// from the list but still plugged in would have started polling every PLC,
// including the server's. Its gateways go back to the server.
func (h *EdgeAgentsHandler) Delete(c *gin.Context) {
	orgID, ok := h.org(c)
	if !ok {
		return
	}
	agentID, err := strconv.Atoi(c.Param("agentId"))
	if err != nil || agentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid edge agent id"})
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove the edge agent"})
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, revokeErr := tx.ExecContext(ctx, `
		UPDATE org_api_keys SET revoked_at = NOW()
		WHERE edge_agent_id = $1 AND org_id = $2 AND revoked_at IS NULL`, agentID, orgID); revokeErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke the edge agent's keys"})
		return
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM edge_agents WHERE id = $1 AND org_id = $2`, agentID, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove the edge agent"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "edge agent not found"})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove the edge agent"})
		return
	}
	log.Printf("[EDGE-AGENTS] box %d of org %d removed; its keys are revoked and its gateways return to the server",
		agentID, orgID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *EdgeAgentsHandler) org(c *gin.Context) (int, bool) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return 0, false
	}
	if !requireOwnOrg(c, orgID) {
		return 0, false
	}
	return orgID, true
}

// serverPolls reports whether the server's own driver-manager has reported
// recently. See edgesync.ServerPollerSeenKey.
func (h *EdgeAgentsHandler) serverPolls(ctx context.Context) bool {
	var v string
	if err := h.db.QueryRowContext(ctx,
		`SELECT value FROM global_settings WHERE key = $1`, edgesync.ServerPollerSeenKey).Scan(&v); err != nil {
		return false
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.UnixMilli(ms)) < serverPollerFreshness
}

// orgGatewayOwners returns the gateways of an organization with just what the
// ownership rule reads.
func orgGatewayOwners(ctx context.Context, db *sql.DB, orgID int) ([]models.Gateway, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT g.id, g.edge_agent_id
		FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []models.Gateway
	for rows.Next() {
		var g models.Gateway
		var agent sql.NullInt64
		if err := rows.Scan(&g.ID, &agent); err != nil {
			return nil, err
		}
		if agent.Valid {
			a := int(agent.Int64)
			g.EdgeAgentID = &a
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
