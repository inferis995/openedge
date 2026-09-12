package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// EdgeAgentsHandler lists the boxes installed for an organization.
type EdgeAgentsHandler struct {
	db *sql.DB
}

func NewEdgeAgentsHandler(db *sql.DB) *EdgeAgentsHandler {
	return &EdgeAgentsHandler{db: db}
}

// EdgeAgentRow is one installed box.
type EdgeAgentRow struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	AgentVersion string     `json:"agent_version,omitempty"`
	Gateways     int        `json:"gateways"`
}

// List handles GET /api/organizations/:id/edge-agents.
//
// It exists so the assignment can be made at all: a gateway is given to a box
// by id, and without a way to see the boxes there is no way to know which id.
func (h *EdgeAgentsHandler) List(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}
	if !requireOwnOrg(c, orgID) {
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT a.id, a.name, a.created_at, a.last_seen_at, COALESCE(a.agent_version, ''),
		       (SELECT COUNT(*) FROM gateways g WHERE g.edge_agent_id = a.id)
		FROM edge_agents a
		WHERE a.org_id = $1
		ORDER BY a.created_at`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list edge agents"})
		return
	}
	defer func() { _ = rows.Close() }()

	agents := []EdgeAgentRow{}
	for rows.Next() {
		var a EdgeAgentRow
		var lastSeen sql.NullTime
		if scanErr := rows.Scan(&a.ID, &a.Name, &a.CreatedAt, &lastSeen,
			&a.AgentVersion, &a.Gateways); scanErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read an edge agent"})
			return
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			a.LastSeenAt = &t
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list edge agents"})
		return
	}

	// How many gateways nobody is responsible for. Zero matters as much as a
	// number: with one box the assignment is ignored and this is always zero,
	// so a number here means somebody has to act.
	var unassigned int
	if len(agents) > 1 {
		_ = h.db.QueryRowContext(c.Request.Context(), `
			SELECT COUNT(*)
			FROM gateways g
			JOIN areas ar ON ar.id = g.area_id
			JOIN sites s ON s.id = ar.site_id
			WHERE s.org_id = $1 AND g.edge_agent_id IS NULL`, orgID).Scan(&unassigned)
	}

	c.JSON(http.StatusOK, gin.H{
		"agents":              agents,
		"unassigned_gateways": unassigned,
	})
}
