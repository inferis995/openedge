package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// ThreatMonitorHandler provides REST endpoints for OT threat monitoring:
//
//	GET  /api/compliance/threats                 list threat events (paginated)
//	POST /api/compliance/threats                 create threat event manually (admin)
//	PUT  /api/compliance/threats/:id/resolve     mark event as resolved
//	GET  /api/compliance/threats/summary         counts by severity + last 24h
type ThreatMonitorHandler struct{ db *sql.DB }

func NewThreatMonitorHandler(db *sql.DB) *ThreatMonitorHandler {
	return &ThreatMonitorHandler{db: db}
}

// ThreatEvent mirrors the ot_threat_events table row.
type ThreatEvent struct {
	ID          int        `json:"id"`
	OrgID       int        `json:"org_id"`
	AssetID     *int       `json:"asset_id"`
	EventType   string     `json:"event_type"`
	Severity    string     `json:"severity"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	IPAddress   string     `json:"ip_address"`
	Resolved    bool       `json:"resolved"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ThreatSummary aggregates threat counts for the dashboard.
type ThreatSummary struct {
	Total      int            `json:"total"`
	Unresolved int            `json:"unresolved"`
	Last24h    int            `json:"last_24h"`
	BySeverity map[string]int `json:"by_severity"`
}

// resolveOrgID returns the effective org_id for the current request.
// Global admins may supply ?org_id=N; regular users always get their own.
func (h *ThreatMonitorHandler) resolveOrgID(c *gin.Context) (int, bool) {
	if middleware.IsGlobalAdmin(c) {
		if raw := c.Query("org_id"); raw != "" {
			id, err := strconv.Atoi(raw)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org_id query param"})
				return 0, false
			}
			return id, true
		}
		// Global admin without ?org_id — caller must provide one or we return 0 (all).
		return 0, true
	}
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return 0, false
	}
	return orgID, true
}

// ListThreats returns paginated threat events.
// GET /api/compliance/threats?resolved=false&severity=high&limit=50&offset=0
func (h *ThreatMonitorHandler) ListThreats(c *gin.Context) {
	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if o := c.Query("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	// Build query dynamically so filters compose cleanly.
	args := []interface{}{}
	where := "WHERE 1=1"
	argIdx := 1

	if orgID > 0 {
		where += " AND org_id = $" + strconv.Itoa(argIdx)
		args = append(args, orgID)
		argIdx++
	}

	if resolved := c.Query("resolved"); resolved != "" {
		if resolved == "false" {
			where += " AND resolved = false"
		} else if resolved == "true" {
			where += " AND resolved = true"
		}
	}

	if severity := c.Query("severity"); severity != "" {
		where += " AND severity = $" + strconv.Itoa(argIdx)
		args = append(args, severity)
		argIdx++
	}

	args = append(args, limit, offset)
	limitArg := argIdx
	offsetArg := argIdx + 1

	query := `
		SELECT id, org_id, asset_id, event_type, severity, title,
		       COALESCE(description,''), COALESCE(source,''), COALESCE(ip_address,''),
		       resolved, resolved_at, created_at
		FROM ot_threat_events
		` + where + `
		ORDER BY created_at DESC
		LIMIT $` + strconv.Itoa(limitArg) + ` OFFSET $` + strconv.Itoa(offsetArg)

	rows, err := h.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := []ThreatEvent{}
	for rows.Next() {
		var e ThreatEvent
		var resolvedAt sql.NullTime
		var assetID sql.NullInt64
		if err := rows.Scan(
			&e.ID, &e.OrgID, &assetID, &e.EventType, &e.Severity,
			&e.Title, &e.Description, &e.Source, &e.IPAddress,
			&e.Resolved, &resolvedAt, &e.CreatedAt,
		); err != nil {
			continue
		}
		if assetID.Valid {
			id := int(assetID.Int64)
			e.AssetID = &id
		}
		if resolvedAt.Valid {
			e.ResolvedAt = &resolvedAt.Time
		}
		events = append(events, e)
	}
	c.JSON(http.StatusOK, events)
}

// CreateThreat manually inserts a threat event. Admin-only.
// POST /api/compliance/threats
func (h *ThreatMonitorHandler) CreateThreat(c *gin.Context) {
	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}
	if orgID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id is required for global admin when creating a threat"})
		return
	}

	var req struct {
		AssetID     *int   `json:"asset_id"`
		EventType   string `json:"event_type" binding:"required"`
		Severity    string `json:"severity"`
		Title       string `json:"title" binding:"required"`
		Description string `json:"description"`
		Source      string `json:"source"`
		IPAddress   string `json:"ip_address"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Severity == "" {
		req.Severity = "info"
	}

	var id int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO ot_threat_events
		  (org_id, asset_id, event_type, severity, title, description, source, ip_address)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id`,
		orgID, req.AssetID, req.EventType, req.Severity,
		req.Title, req.Description, req.Source, req.IPAddress,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "threat event created"})
}

// ResolveThreat marks an event as resolved. Admin-only.
// PUT /api/compliance/threats/:id/resolve
func (h *ThreatMonitorHandler) ResolveThreat(c *gin.Context) {
	eventID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}

	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}

	userID, _ := middleware.GetUserID(c)

	var result sql.Result
	if orgID == 0 {
		// Global admin, no org filter
		result, err = h.db.ExecContext(c.Request.Context(), `
			UPDATE ot_threat_events
			SET resolved=true, resolved_at=now(), resolved_by=$1
			WHERE id=$2 AND resolved=false`,
			userID, eventID)
	} else {
		result, err = h.db.ExecContext(c.Request.Context(), `
			UPDATE ot_threat_events
			SET resolved=true, resolved_at=now(), resolved_by=$1
			WHERE id=$2 AND org_id=$3 AND resolved=false`,
			userID, eventID, orgID)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "threat event not found or already resolved"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "threat event resolved"})
}

// ThreatSummaryHandler returns aggregate counts.
// GET /api/compliance/threats/summary
func (h *ThreatMonitorHandler) ThreatSummary(c *gin.Context) {
	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	summary := ThreatSummary{
		BySeverity: map[string]int{
			"critical": 0,
			"high":     0,
			"medium":   0,
			"low":      0,
			"info":     0,
		},
	}

	// Total + by_severity
	var totalQuery, sevQuery, unresolvedQuery, last24hQuery string
	var totalArgs, sevArgs, unresolvedArgs, last24hArgs []interface{}

	if orgID > 0 {
		totalQuery = `SELECT COUNT(*) FROM ot_threat_events WHERE org_id=$1`
		totalArgs = []interface{}{orgID}
		sevQuery = `SELECT severity, COUNT(*) FROM ot_threat_events WHERE org_id=$1 GROUP BY severity`
		sevArgs = []interface{}{orgID}
		unresolvedQuery = `SELECT COUNT(*) FROM ot_threat_events WHERE org_id=$1 AND resolved=false`
		unresolvedArgs = []interface{}{orgID}
		last24hQuery = `SELECT COUNT(*) FROM ot_threat_events WHERE org_id=$1 AND created_at > now()-INTERVAL '24 hours'`
		last24hArgs = []interface{}{orgID}
	} else {
		// Global admin — count across all orgs
		totalQuery = `SELECT COUNT(*) FROM ot_threat_events`
		sevQuery = `SELECT severity, COUNT(*) FROM ot_threat_events GROUP BY severity`
		unresolvedQuery = `SELECT COUNT(*) FROM ot_threat_events WHERE resolved=false`
		last24hQuery = `SELECT COUNT(*) FROM ot_threat_events WHERE created_at > now()-INTERVAL '24 hours'`
	}

	_ = h.db.QueryRowContext(ctx, totalQuery, totalArgs...).Scan(&summary.Total)
	_ = h.db.QueryRowContext(ctx, unresolvedQuery, unresolvedArgs...).Scan(&summary.Unresolved)
	_ = h.db.QueryRowContext(ctx, last24hQuery, last24hArgs...).Scan(&summary.Last24h)

	rows, err := h.db.QueryContext(ctx, sevQuery, sevArgs...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sev string
			var cnt int
			if rows.Scan(&sev, &cnt) == nil {
				summary.BySeverity[sev] = cnt
			}
		}
	}

	c.JSON(http.StatusOK, summary)
}

// ─── Internal helper — used by ComplianceReportHandler ────────────────────────

// fetchThreatEvents returns all threat events for an org in the given time range.
// If periodFrom/periodTo are zero, returns all events.
func fetchThreatEvents(ctx context.Context, db *sql.DB, orgID int, periodFrom, periodTo time.Time) []map[string]interface{} {
	var rows *sql.Rows
	var err error
	if !periodFrom.IsZero() && !periodTo.IsZero() {
		rows, err = db.QueryContext(ctx, `
			SELECT id, event_type, severity, title, COALESCE(description,''),
			       created_at, resolved, resolved_at
			FROM ot_threat_events
			WHERE org_id=$1 AND created_at BETWEEN $2 AND $3
			ORDER BY created_at DESC`,
			orgID, periodFrom, periodTo)
	} else {
		rows, err = db.QueryContext(ctx, `
			SELECT id, event_type, severity, title, COALESCE(description,''),
			       created_at, resolved, resolved_at
			FROM ot_threat_events
			WHERE org_id=$1
			ORDER BY created_at DESC`,
			orgID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	var events []map[string]interface{}
	for rows.Next() {
		var id int
		var eventType, severity, title, description string
		var createdAt time.Time
		var resolved bool
		var resolvedAt sql.NullTime
		if rows.Scan(&id, &eventType, &severity, &title, &description, &createdAt, &resolved, &resolvedAt) != nil {
			continue
		}
		ev := map[string]interface{}{
			"id":          id,
			"event_type":  eventType,
			"severity":    severity,
			"title":       title,
			"description": description,
			"created_at":  createdAt,
			"resolved":    resolved,
			"resolved_at": nil,
		}
		if resolvedAt.Valid {
			ev["resolved_at"] = resolvedAt.Time
		}
		events = append(events, ev)
	}
	if events == nil {
		events = []map[string]interface{}{}
	}
	return events
}
