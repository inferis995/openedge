package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// ComplianceReportHandler provides REST endpoints for compliance report generation:
//
//	POST   /api/compliance/reports          generate a new report (async)
//	GET    /api/compliance/reports          list reports for org
//	GET    /api/compliance/reports/:id      get single report (with content)
//	DELETE /api/compliance/reports/:id      delete report
type ComplianceReportHandler struct{ db *sql.DB }

func NewComplianceReportHandler(db *sql.DB) *ComplianceReportHandler {
	return &ComplianceReportHandler{db: db}
}

// ComplianceReport is the JSON representation of a compliance_reports row.
type ComplianceReport struct {
	ID          int                    `json:"id"`
	OrgID       int                    `json:"org_id"`
	ReportType  string                 `json:"report_type"`
	Title       string                 `json:"title"`
	PeriodFrom  *time.Time             `json:"period_from"`
	PeriodTo    *time.Time             `json:"period_to"`
	Status      string                 `json:"status"`
	Format      string                 `json:"format"`
	Content     map[string]interface{} `json:"content,omitempty"`
	GeneratedBy *int                   `json:"generated_by"`
	GeneratedAt time.Time              `json:"generated_at"`
	Error       string                 `json:"error,omitempty"`
}

// resolveOrgIDForReport mirrors the pattern in ThreatMonitorHandler.
func (h *ComplianceReportHandler) resolveOrgID(c *gin.Context) (int, bool) {
	if middleware.IsGlobalAdmin(c) {
		if raw := c.Query("org_id"); raw != "" {
			id, err := strconv.Atoi(raw)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org_id query param"})
				return 0, false
			}
			return id, true
		}
		return 0, true
	}
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return 0, false
	}
	return orgID, true
}

// GenerateReport creates a report record and launches an async builder goroutine.
// POST /api/compliance/reports
func (h *ComplianceReportHandler) GenerateReport(c *gin.Context) {
	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}
	if orgID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id is required"})
		return
	}

	var req struct {
		ReportType string     `json:"report_type" binding:"required"`
		Title      string     `json:"title"`
		PeriodFrom *time.Time `json:"period_from"`
		PeriodTo   *time.Time `json:"period_to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validTypes := map[string]bool{
		"nis2_assessment":    true,
		"iec62443_assessment": true,
		"asset_inventory":    true,
		"incident_timeline":  true,
		"full_compliance":    true,
	}
	if !validTypes[req.ReportType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid report_type"})
		return
	}

	if req.Title == "" {
		req.Title = fmt.Sprintf("%s — %s", req.ReportType, time.Now().UTC().Format("2006-01-02"))
	}

	userID, _ := middleware.GetUserID(c)

	var reportID int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO compliance_reports
		  (org_id, report_type, title, period_from, period_to, status, format, generated_by)
		VALUES ($1,$2,$3,$4,$5,'generating','json',$6)
		RETURNING id`,
		orgID, req.ReportType, req.Title, req.PeriodFrom, req.PeriodTo, userID,
	).Scan(&reportID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Launch async builder.
	go h.buildReport(reportID, orgID, req.ReportType, req.PeriodFrom, req.PeriodTo)

	c.JSON(http.StatusAccepted, gin.H{
		"id":     reportID,
		"status": "generating",
	})
}

// ListReports returns reports for the caller's org.
// GET /api/compliance/reports
func (h *ComplianceReportHandler) ListReports(c *gin.Context) {
	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}

	var rows *sql.Rows
	var err error
	if orgID > 0 {
		rows, err = h.db.QueryContext(c.Request.Context(), `
			SELECT id, org_id, report_type, title, period_from, period_to,
			       status, format, generated_by, generated_at, COALESCE(error,'')
			FROM compliance_reports
			WHERE org_id=$1
			ORDER BY generated_at DESC
			LIMIT 100`, orgID)
	} else {
		rows, err = h.db.QueryContext(c.Request.Context(), `
			SELECT id, org_id, report_type, title, period_from, period_to,
			       status, format, generated_by, generated_at, COALESCE(error,'')
			FROM compliance_reports
			ORDER BY generated_at DESC
			LIMIT 100`)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	reports := []ComplianceReport{}
	for rows.Next() {
		var r ComplianceReport
		var pf, pt sql.NullTime
		var genBy sql.NullInt64
		if err := rows.Scan(
			&r.ID, &r.OrgID, &r.ReportType, &r.Title, &pf, &pt,
			&r.Status, &r.Format, &genBy, &r.GeneratedAt, &r.Error,
		); err != nil {
			continue
		}
		if pf.Valid {
			r.PeriodFrom = &pf.Time
		}
		if pt.Valid {
			r.PeriodTo = &pt.Time
		}
		if genBy.Valid {
			id := int(genBy.Int64)
			r.GeneratedBy = &id
		}
		reports = append(reports, r)
	}
	c.JSON(http.StatusOK, reports)
}

// GetReport returns a single report including its content.
// GET /api/compliance/reports/:id
func (h *ComplianceReportHandler) GetReport(c *gin.Context) {
	reportID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid report id"})
		return
	}

	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}

	var r ComplianceReport
	var pf, pt sql.NullTime
	var genBy sql.NullInt64
	var contentJSON []byte
	var contentNull sql.NullString

	query := `
		SELECT id, org_id, report_type, title, period_from, period_to,
		       status, format, content, generated_by, generated_at, COALESCE(error,'')
		FROM compliance_reports
		WHERE id=$1`
	args := []interface{}{reportID}
	if orgID > 0 {
		query += " AND org_id=$2"
		args = append(args, orgID)
	}

	err = h.db.QueryRowContext(c.Request.Context(), query, args...).Scan(
		&r.ID, &r.OrgID, &r.ReportType, &r.Title, &pf, &pt,
		&r.Status, &r.Format, &contentNull, &genBy, &r.GeneratedAt, &r.Error,
	)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if pf.Valid {
		r.PeriodFrom = &pf.Time
	}
	if pt.Valid {
		r.PeriodTo = &pt.Time
	}
	if genBy.Valid {
		id := int(genBy.Int64)
		r.GeneratedBy = &id
	}
	if contentNull.Valid {
		contentJSON = []byte(contentNull.String)
		_ = json.Unmarshal(contentJSON, &r.Content)
	}

	c.JSON(http.StatusOK, r)
}

// DeleteReport removes a report. Admin-only.
// DELETE /api/compliance/reports/:id
func (h *ComplianceReportHandler) DeleteReport(c *gin.Context) {
	reportID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid report id"})
		return
	}

	orgID, ok := h.resolveOrgID(c)
	if !ok {
		return
	}

	var result sql.Result
	if orgID > 0 {
		result, err = h.db.ExecContext(c.Request.Context(),
			`DELETE FROM compliance_reports WHERE id=$1 AND org_id=$2`, reportID, orgID)
	} else {
		result, err = h.db.ExecContext(c.Request.Context(),
			`DELETE FROM compliance_reports WHERE id=$1`, reportID)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "report deleted"})
}

// ─── Async report builder ─────────────────────────────────────────────────────

func (h *ComplianceReportHandler) buildReport(
	reportID, orgID int,
	reportType string,
	periodFrom, periodTo *time.Time,
) {
	ctx := context.Background()

	var pf, pt time.Time
	if periodFrom != nil {
		pf = *periodFrom
	} else {
		pf = time.Now().UTC().AddDate(0, -3, 0) // default: last 3 months
	}
	if periodTo != nil {
		pt = *periodTo
	} else {
		pt = time.Now().UTC()
	}

	var content map[string]interface{}
	var buildErr error

	switch reportType {
	case "nis2_assessment":
		content, buildErr = h.buildNIS2Assessment(ctx, orgID, pf, pt)
	case "iec62443_assessment":
		content, buildErr = h.buildIEC62443Assessment(ctx, orgID, pf, pt)
	case "asset_inventory":
		content, buildErr = h.buildAssetInventory(ctx, orgID)
	case "incident_timeline":
		content, buildErr = h.buildIncidentTimeline(ctx, orgID, pf, pt)
	case "full_compliance":
		content, buildErr = h.buildFullCompliance(ctx, orgID, pf, pt)
	default:
		buildErr = fmt.Errorf("unknown report type: %s", reportType)
	}

	if buildErr != nil {
		_, _ = h.db.ExecContext(ctx,
			`UPDATE compliance_reports SET status='failed', error=$1 WHERE id=$2`,
			buildErr.Error(), reportID)
		return
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		_, _ = h.db.ExecContext(ctx,
			`UPDATE compliance_reports SET status='failed', error=$1 WHERE id=$2`,
			err.Error(), reportID)
		return
	}

	_, _ = h.db.ExecContext(ctx,
		`UPDATE compliance_reports SET status='ready', content=$1 WHERE id=$2`,
		string(contentJSON), reportID)
}

// orgName returns the org display name (falls back to "Organization N" on error).
func (h *ComplianceReportHandler) orgName(ctx context.Context, orgID int) string {
	var name string
	if err := h.db.QueryRowContext(ctx,
		`SELECT name FROM organizations WHERE id=$1`, orgID).Scan(&name); err != nil {
		return fmt.Sprintf("Organization %d", orgID)
	}
	return name
}

// ─── NIS2 assessment ──────────────────────────────────────────────────────────

func (h *ComplianceReportHandler) buildNIS2Assessment(
	ctx context.Context, orgID int, periodFrom, periodTo time.Time,
) (map[string]interface{}, error) {
	org := h.orgName(ctx, orgID)

	rows, err := h.db.QueryContext(ctx, `
		SELECT cr.req_code, cr.title,
		       COALESCE(ca.status,'not_assessed'),
		       COALESCE(ca.evidence,''),
		       COALESCE(ca.notes,''),
		       ca.assessed_at
		FROM compliance_requirements cr
		JOIN compliance_frameworks cf ON cf.id = cr.framework_id AND cf.code = 'NIS2'
		LEFT JOIN compliance_assessments ca
		       ON ca.requirement_id = cr.id AND ca.org_id = $1
		ORDER BY cr.req_code`, orgID)
	if err != nil {
		return nil, fmt.Errorf("query NIS2 requirements: %w", err)
	}
	defer rows.Close()

	type Req struct {
		ReqCode    string     `json:"req_code"`
		Title      string     `json:"title"`
		Status     string     `json:"status"`
		Evidence   string     `json:"evidence"`
		Notes      string     `json:"notes"`
		AssessedAt *time.Time `json:"assessed_at"`
	}

	var reqs []Req
	counts := map[string]int{"compliant": 0, "partial": 0, "non_compliant": 0, "not_assessed": 0}
	for rows.Next() {
		var r Req
		var at sql.NullTime
		if err := rows.Scan(&r.ReqCode, &r.Title, &r.Status, &r.Evidence, &r.Notes, &at); err != nil {
			continue
		}
		if at.Valid {
			r.AssessedAt = &at.Time
		}
		reqs = append(reqs, r)
		counts[r.Status]++
	}
	if reqs == nil {
		reqs = []Req{}
	}

	total := len(reqs)
	score := 0
	if total > 0 {
		score = (counts["compliant"]*100 + counts["partial"]*50) / total
	}

	return map[string]interface{}{
		"framework":    "NIS2",
		"org_name":     org,
		"generated_at": time.Now().UTC(),
		"period":       map[string]interface{}{"from": periodFrom, "to": periodTo},
		"score":        score,
		"requirements": reqs,
		"summary":      counts,
	}, nil
}

// ─── IEC 62443 assessment ─────────────────────────────────────────────────────

func (h *ComplianceReportHandler) buildIEC62443Assessment(
	ctx context.Context, orgID int, periodFrom, periodTo time.Time,
) (map[string]interface{}, error) {
	org := h.orgName(ctx, orgID)

	rows, err := h.db.QueryContext(ctx, `
		SELECT cr.req_code, cr.title,
		       COALESCE(ca.status,'not_assessed'),
		       COALESCE(ca.evidence,''),
		       COALESCE(ca.notes,''),
		       ca.assessed_at
		FROM compliance_requirements cr
		JOIN compliance_frameworks cf ON cf.id = cr.framework_id AND cf.code = 'IEC62443'
		LEFT JOIN compliance_assessments ca
		       ON ca.requirement_id = cr.id AND ca.org_id = $1
		ORDER BY cr.req_code`, orgID)
	if err != nil {
		return nil, fmt.Errorf("query IEC62443 requirements: %w", err)
	}
	defer rows.Close()

	type Req struct {
		ReqCode    string     `json:"req_code"`
		Title      string     `json:"title"`
		Status     string     `json:"status"`
		Evidence   string     `json:"evidence"`
		Notes      string     `json:"notes"`
		AssessedAt *time.Time `json:"assessed_at"`
	}

	var reqs []Req
	counts := map[string]int{"compliant": 0, "partial": 0, "non_compliant": 0, "not_assessed": 0}
	for rows.Next() {
		var r Req
		var at sql.NullTime
		if err := rows.Scan(&r.ReqCode, &r.Title, &r.Status, &r.Evidence, &r.Notes, &at); err != nil {
			continue
		}
		if at.Valid {
			r.AssessedAt = &at.Time
		}
		reqs = append(reqs, r)
		counts[r.Status]++
	}
	if reqs == nil {
		reqs = []Req{}
	}

	total := len(reqs)
	score := 0
	if total > 0 {
		score = (counts["compliant"]*100 + counts["partial"]*50) / total
	}

	return map[string]interface{}{
		"framework":    "IEC62443",
		"org_name":     org,
		"generated_at": time.Now().UTC(),
		"period":       map[string]interface{}{"from": periodFrom, "to": periodTo},
		"score":        score,
		"requirements": reqs,
		"summary":      counts,
	}, nil
}

// ─── Asset inventory ──────────────────────────────────────────────────────────

func (h *ComplianceReportHandler) buildAssetInventory(
	ctx context.Context, orgID int,
) (map[string]interface{}, error) {
	org := h.orgName(ctx, orgID)

	rows, err := h.db.QueryContext(ctx, `
		SELECT a.id,
		       COALESCE(a.ip_address,''),
		       COALESCE(a.hostname,''),
		       COALESCE(a.vendor,''),
		       COALESCE(a.model,''),
		       COALESCE(a.device_type,''),
		       a.risk_score,
		       a.is_authorized
		FROM ot_assets a
		WHERE a.org_id=$1
		ORDER BY a.risk_score DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("query ot_assets: %w", err)
	}
	defer rows.Close()

	type AssetRow struct {
		ID           int      `json:"id"`
		IPAddress    string   `json:"ip"`
		Hostname     string   `json:"hostname"`
		Vendor       string   `json:"vendor"`
		Model        string   `json:"model"`
		DeviceType   string   `json:"device_type"`
		RiskScore    float64  `json:"risk_score"`
		IsAuthorized bool     `json:"is_authorized"`
		CVEs         []string `json:"cves"`
	}

	var assets []AssetRow
	var totalRisk float64
	unauthorized := 0

	for rows.Next() {
		var a AssetRow
		if err := rows.Scan(
			&a.ID, &a.IPAddress, &a.Hostname, &a.Vendor,
			&a.Model, &a.DeviceType, &a.RiskScore, &a.IsAuthorized,
		); err != nil {
			continue
		}
		a.CVEs = []string{}

		// Fetch CVEs for this asset
		cveRows, _ := h.db.QueryContext(ctx,
			`SELECT cve_id FROM ot_asset_cves WHERE asset_id=$1 ORDER BY severity DESC LIMIT 20`,
			a.ID)
		if cveRows != nil {
			for cveRows.Next() {
				var cveID string
				if cveRows.Scan(&cveID) == nil {
					a.CVEs = append(a.CVEs, cveID)
				}
			}
			cveRows.Close()
		}

		totalRisk += a.RiskScore
		if !a.IsAuthorized {
			unauthorized++
		}
		assets = append(assets, a)
	}
	if assets == nil {
		assets = []AssetRow{}
	}

	// Count critical CVEs across all assets
	var criticalCVEs int
	_ = h.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ot_asset_cves c
		JOIN ot_assets a ON a.id = c.asset_id
		WHERE a.org_id=$1 AND c.severity='critical'`, orgID).Scan(&criticalCVEs)

	avgRisk := 0.0
	if len(assets) > 0 {
		avgRisk = totalRisk / float64(len(assets))
	}

	return map[string]interface{}{
		"org_name":      org,
		"generated_at":  time.Now().UTC(),
		"total_assets":  len(assets),
		"avg_risk_score": avgRisk,
		"assets":        assets,
		"critical_cves": criticalCVEs,
		"unauthorized":  unauthorized,
	}, nil
}

// ─── Incident timeline ────────────────────────────────────────────────────────

func (h *ComplianceReportHandler) buildIncidentTimeline(
	ctx context.Context, orgID int, periodFrom, periodTo time.Time,
) (map[string]interface{}, error) {
	org := h.orgName(ctx, orgID)

	events := fetchThreatEvents(ctx, h.db, orgID, periodFrom, periodTo)

	return map[string]interface{}{
		"org_name":     org,
		"period":       map[string]interface{}{"from": periodFrom, "to": periodTo},
		"generated_at": time.Now().UTC(),
		"total_events": len(events),
		"events":       events,
	}, nil
}

// ─── Full compliance ──────────────────────────────────────────────────────────

func (h *ComplianceReportHandler) buildFullCompliance(
	ctx context.Context, orgID int, periodFrom, periodTo time.Time,
) (map[string]interface{}, error) {
	nis2, err := h.buildNIS2Assessment(ctx, orgID, periodFrom, periodTo)
	if err != nil {
		return nil, fmt.Errorf("nis2: %w", err)
	}
	iec, err := h.buildIEC62443Assessment(ctx, orgID, periodFrom, periodTo)
	if err != nil {
		return nil, fmt.Errorf("iec62443: %w", err)
	}
	inv, err := h.buildAssetInventory(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("asset_inventory: %w", err)
	}
	timeline, err := h.buildIncidentTimeline(ctx, orgID, periodFrom, periodTo)
	if err != nil {
		return nil, fmt.Errorf("incident_timeline: %w", err)
	}

	return map[string]interface{}{
		"generated_at":       time.Now().UTC(),
		"period":             map[string]interface{}{"from": periodFrom, "to": periodTo},
		"nis2_assessment":    nis2,
		"iec62443_assessment": iec,
		"asset_inventory":    inv,
		"incident_timeline":  timeline,
	}, nil
}
