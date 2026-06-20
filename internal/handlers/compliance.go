package handlers

// ComplianceHandler — OT asset inventory + risk posture + compliance frameworks
// Routes (all under /api/compliance):
//
//	GET    /api/compliance/assets                          list OT assets for org
//	POST   /api/compliance/assets                         create/update asset manually
//	PUT    /api/compliance/assets/:id                     update asset
//	DELETE /api/compliance/assets/:id                     delete asset
//	POST   /api/compliance/assets/:id/cves                add CVE to asset
//	DELETE /api/compliance/assets/:id/cves/:cve           remove CVE from asset
//
//	GET    /api/compliance/frameworks                     list frameworks (NIS2, IEC62443)
//	GET    /api/compliance/frameworks/:code/assessment    get org assessment for a framework
//	PUT    /api/compliance/frameworks/:code/assessment/:req_id  update requirement status
//
//	GET    /api/compliance/risk-posture                   risk summary
//	GET    /api/compliance/score                          overall compliance score (0-100)
//
//	POST   /api/compliance/scan                           trigger simulated scan job
//	GET    /api/compliance/scan/:id                       get scan job status
import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	otSync "github.com/ralph/industrial-edge-middleware/internal/sync"
)

// ComplianceHandler handles all OT compliance endpoints.
type ComplianceHandler struct {
	db *sql.DB
}

// NewComplianceHandler returns a new ComplianceHandler.
func NewComplianceHandler(db *sql.DB) *ComplianceHandler {
	return &ComplianceHandler{db: db}
}

// ─── Models ───────────────────────────────────────────────────────────────────

type OTAsset struct {
	ID           int        `json:"id"`
	OrgID        int        `json:"org_id"`
	GatewayID    *int       `json:"gateway_id"`
	IPAddress    *string    `json:"ip_address"`
	MACAddress   *string    `json:"mac_address"`
	Hostname     *string    `json:"hostname"`
	Vendor       *string    `json:"vendor"`
	DeviceType   *string    `json:"device_type"`
	Model        *string    `json:"model"`
	FirmwareVer  *string    `json:"firmware_ver"`
	Protocol     *string    `json:"protocol"`
	OSInfo       *string    `json:"os_info"`
	IsAuthorized bool       `json:"is_authorized"`
	RiskScore    float64    `json:"risk_score"`
	LastSeen     time.Time  `json:"last_seen"`
	DiscoveredAt time.Time  `json:"discovered_at"`
	Notes        *string    `json:"notes"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CVEs         []CVEMatch `json:"cves"`
}

type CVEMatch struct {
	ID          int       `json:"id"`
	AssetID     int       `json:"asset_id"`
	CVEID       string    `json:"cve_id"`
	Severity    string    `json:"severity"`
	CVSSScore   *float64  `json:"cvss_score"`
	Description *string   `json:"description"`
	Published   *time.Time `json:"published"`
	CreatedAt   time.Time `json:"created_at"`
}

type ComplianceFramework struct {
	ID      int     `json:"id"`
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Version *string `json:"version"`
}

type ComplianceRequirementWithStatus struct {
	ID          int     `json:"id"`
	ReqCode     string  `json:"req_code"`
	Category    *string `json:"category"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Weight      int     `json:"weight"`
	Status      string  `json:"status"`
	Evidence    *string `json:"evidence"`
	Notes       *string `json:"notes"`
	AssessedAt  *time.Time `json:"assessed_at"`
}

type ScanJob struct {
	ID         int        `json:"id"`
	OrgID      int        `json:"org_id"`
	Subnet     string     `json:"subnet"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	FoundCount int        `json:"found_count"`
	NewCount   int        `json:"new_count"`
	Error      *string    `json:"error"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// resolveOrgID returns the org_id to use for the query.
// Global admins may pass ?org_id=N to scope to a specific org.
// Regular users always get their own org from the JWT.
// Returns (orgID, ok) — ok=false means global admin with no filter (access all).
func (h *ComplianceHandler) resolveOrgID(c *gin.Context) (int, bool) {
	if middleware.IsGlobalAdmin(c) {
		if raw := c.Query("org_id"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err == nil && n > 0 {
				return n, true
			}
		}
		return 0, false
	}
	orgID, ok := middleware.GetOrganizationID(c)
	return orgID, ok
}

// loadCVEs loads CVE matches for a slice of assets in one query.
func (h *ComplianceHandler) loadCVEs(ctx context.Context, assets []OTAsset) []OTAsset {
	if len(assets) == 0 {
		return assets
	}
	// Build id list
	ids := make([]interface{}, len(assets))
	idIndex := make(map[int]*OTAsset, len(assets))
	for i := range assets {
		ids[i] = assets[i].ID
		idIndex[assets[i].ID] = &assets[i]
		assets[i].CVEs = []CVEMatch{}
	}

	// Build $1,$2,... placeholder
	placeholders := ""
	for i := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "$" + strconv.Itoa(i+1)
	}

	rows, err := h.db.QueryContext(ctx,
		`SELECT id, asset_id, cve_id, severity, cvss_score, description, published, created_at
		 FROM ot_asset_cves WHERE asset_id IN (`+placeholders+`) ORDER BY severity, cve_id`,
		ids...)
	if err != nil {
		return assets
	}
	defer rows.Close()
	for rows.Next() {
		var cve CVEMatch
		if err := rows.Scan(&cve.ID, &cve.AssetID, &cve.CVEID, &cve.Severity,
			&cve.CVSSScore, &cve.Description, &cve.Published, &cve.CreatedAt); err != nil {
			continue
		}
		if a, ok := idIndex[cve.AssetID]; ok {
			a.CVEs = append(a.CVEs, cve)
		}
	}
	return assets
}

// ─── Asset CRUD ───────────────────────────────────────────────────────────────

// ListAssets returns all OT assets for the caller's org.
// GET /api/compliance/assets
func (h *ComplianceHandler) ListAssets(c *gin.Context) {
	ctx := c.Request.Context()
	orgID, hasOrg := h.resolveOrgID(c)

	var (
		rows *sql.Rows
		err  error
	)
	if hasOrg {
		rows, err = h.db.QueryContext(ctx, `
			SELECT id, org_id, gateway_id, ip_address, mac_address, hostname,
			       vendor, device_type, model, firmware_ver, protocol, os_info,
			       is_authorized, risk_score, last_seen, discovered_at, notes, created_at, updated_at
			FROM ot_assets
			WHERE org_id = $1
			ORDER BY risk_score DESC, id
		`, orgID)
	} else {
		rows, err = h.db.QueryContext(ctx, `
			SELECT id, org_id, gateway_id, ip_address, mac_address, hostname,
			       vendor, device_type, model, firmware_ver, protocol, os_info,
			       is_authorized, risk_score, last_seen, discovered_at, notes, created_at, updated_at
			FROM ot_assets
			ORDER BY org_id, risk_score DESC, id
		`)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var assets []OTAsset
	for rows.Next() {
		var a OTAsset
		if err := rows.Scan(&a.ID, &a.OrgID, &a.GatewayID, &a.IPAddress, &a.MACAddress,
			&a.Hostname, &a.Vendor, &a.DeviceType, &a.Model, &a.FirmwareVer,
			&a.Protocol, &a.OSInfo, &a.IsAuthorized, &a.RiskScore,
			&a.LastSeen, &a.DiscoveredAt, &a.Notes, &a.CreatedAt, &a.UpdatedAt); err != nil {
			continue
		}
		assets = append(assets, a)
	}
	if assets == nil {
		assets = []OTAsset{}
	}
	assets = h.loadCVEs(ctx, assets)
	c.JSON(http.StatusOK, assets)
}

// CreateAsset creates a new OT asset manually.
// POST /api/compliance/assets
func (h *ComplianceHandler) CreateAsset(c *gin.Context) {
	ctx := c.Request.Context()
	orgID, hasOrg := h.resolveOrgID(c)
	if !hasOrg && orgID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id required for global admin"})
		return
	}

	var body struct {
		GatewayID   *int     `json:"gateway_id"`
		IPAddress   *string  `json:"ip_address"`
		MACAddress  *string  `json:"mac_address"`
		Hostname    *string  `json:"hostname"`
		Vendor      *string  `json:"vendor"`
		DeviceType  *string  `json:"device_type"`
		Model       *string  `json:"model"`
		FirmwareVer *string  `json:"firmware_ver"`
		Protocol    *string  `json:"protocol"`
		OSInfo      *string  `json:"os_info"`
		IsAuthorized *bool   `json:"is_authorized"`
		RiskScore   *float64 `json:"risk_score"`
		Notes       *string  `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isAuth := true
	if body.IsAuthorized != nil {
		isAuth = *body.IsAuthorized
	}
	riskScore := 0.0
	if body.RiskScore != nil {
		riskScore = *body.RiskScore
	}

	var a OTAsset
	err := h.db.QueryRowContext(ctx, `
		INSERT INTO ot_assets
		  (org_id, gateway_id, ip_address, mac_address, hostname, vendor,
		   device_type, model, firmware_ver, protocol, os_info,
		   is_authorized, risk_score, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, org_id, gateway_id, ip_address, mac_address, hostname,
		          vendor, device_type, model, firmware_ver, protocol, os_info,
		          is_authorized, risk_score, last_seen, discovered_at, notes, created_at, updated_at
	`, orgID, body.GatewayID, body.IPAddress, body.MACAddress, body.Hostname,
		body.Vendor, body.DeviceType, body.Model, body.FirmwareVer,
		body.Protocol, body.OSInfo, isAuth, riskScore, body.Notes,
	).Scan(&a.ID, &a.OrgID, &a.GatewayID, &a.IPAddress, &a.MACAddress,
		&a.Hostname, &a.Vendor, &a.DeviceType, &a.Model, &a.FirmwareVer,
		&a.Protocol, &a.OSInfo, &a.IsAuthorized, &a.RiskScore,
		&a.LastSeen, &a.DiscoveredAt, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	a.CVEs = []CVEMatch{}
	c.JSON(http.StatusCreated, a)
}

// UpdateAsset updates an existing OT asset.
// PUT /api/compliance/assets/:id
func (h *ComplianceHandler) UpdateAsset(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid asset id"})
		return
	}
	orgID, hasOrg := h.resolveOrgID(c)

	var body struct {
		GatewayID   *int     `json:"gateway_id"`
		IPAddress   *string  `json:"ip_address"`
		MACAddress  *string  `json:"mac_address"`
		Hostname    *string  `json:"hostname"`
		Vendor      *string  `json:"vendor"`
		DeviceType  *string  `json:"device_type"`
		Model       *string  `json:"model"`
		FirmwareVer *string  `json:"firmware_ver"`
		Protocol    *string  `json:"protocol"`
		OSInfo      *string  `json:"os_info"`
		IsAuthorized *bool   `json:"is_authorized"`
		RiskScore   *float64 `json:"risk_score"`
		Notes       *string  `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify org ownership
	var assetOrgID int
	if err := h.db.QueryRowContext(ctx, `SELECT org_id FROM ot_assets WHERE id=$1`, id).Scan(&assetOrgID); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if hasOrg && assetOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "asset not found"})
		return
	}

	var a OTAsset
	err = h.db.QueryRowContext(ctx, `
		UPDATE ot_assets SET
			gateway_id   = COALESCE($2, gateway_id),
			ip_address   = COALESCE($3, ip_address),
			mac_address  = COALESCE($4, mac_address),
			hostname     = COALESCE($5, hostname),
			vendor       = COALESCE($6, vendor),
			device_type  = COALESCE($7, device_type),
			model        = COALESCE($8, model),
			firmware_ver = COALESCE($9, firmware_ver),
			protocol     = COALESCE($10, protocol),
			os_info      = COALESCE($11, os_info),
			is_authorized= COALESCE($12, is_authorized),
			risk_score   = COALESCE($13, risk_score),
			notes        = COALESCE($14, notes),
			updated_at   = now()
		WHERE id = $1
		RETURNING id, org_id, gateway_id, ip_address, mac_address, hostname,
		          vendor, device_type, model, firmware_ver, protocol, os_info,
		          is_authorized, risk_score, last_seen, discovered_at, notes, created_at, updated_at
	`, id, body.GatewayID, body.IPAddress, body.MACAddress, body.Hostname,
		body.Vendor, body.DeviceType, body.Model, body.FirmwareVer,
		body.Protocol, body.OSInfo, body.IsAuthorized, body.RiskScore, body.Notes,
	).Scan(&a.ID, &a.OrgID, &a.GatewayID, &a.IPAddress, &a.MACAddress,
		&a.Hostname, &a.Vendor, &a.DeviceType, &a.Model, &a.FirmwareVer,
		&a.Protocol, &a.OSInfo, &a.IsAuthorized, &a.RiskScore,
		&a.LastSeen, &a.DiscoveredAt, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	assets := h.loadCVEs(ctx, []OTAsset{a})
	c.JSON(http.StatusOK, assets[0])
}

// DeleteAsset removes an OT asset.
// DELETE /api/compliance/assets/:id
func (h *ComplianceHandler) DeleteAsset(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid asset id"})
		return
	}
	orgID, hasOrg := h.resolveOrgID(c)

	var q string
	var args []interface{}
	if hasOrg {
		q = `DELETE FROM ot_assets WHERE id=$1 AND org_id=$2`
		args = []interface{}{id, orgID}
	} else {
		q = `DELETE FROM ot_assets WHERE id=$1`
		args = []interface{}{id}
	}

	res, err := h.db.ExecContext(ctx, q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "asset deleted"})
}

// ─── CVE management ───────────────────────────────────────────────────────────

// AddCVE attaches a CVE to an asset.
// POST /api/compliance/assets/:id/cves
func (h *ComplianceHandler) AddCVE(c *gin.Context) {
	ctx := c.Request.Context()
	assetID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid asset id"})
		return
	}
	orgID, hasOrg := h.resolveOrgID(c)

	// Verify asset belongs to org
	var assetOrgID int
	if err := h.db.QueryRowContext(ctx, `SELECT org_id FROM ot_assets WHERE id=$1`, assetID).Scan(&assetOrgID); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if hasOrg && assetOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "asset not found"})
		return
	}

	var body struct {
		CVEID       string   `json:"cve_id" binding:"required"`
		Severity    string   `json:"severity" binding:"required"`
		CVSSScore   *float64 `json:"cvss_score"`
		Description *string  `json:"description"`
		Published   *time.Time `json:"published"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var cve CVEMatch
	err = h.db.QueryRowContext(ctx, `
		INSERT INTO ot_asset_cves (asset_id, cve_id, severity, cvss_score, description, published)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (asset_id, cve_id) DO UPDATE
		  SET severity=EXCLUDED.severity, cvss_score=EXCLUDED.cvss_score,
		      description=EXCLUDED.description, published=EXCLUDED.published
		RETURNING id, asset_id, cve_id, severity, cvss_score, description, published, created_at
	`, assetID, body.CVEID, body.Severity, body.CVSSScore, body.Description, body.Published,
	).Scan(&cve.ID, &cve.AssetID, &cve.CVEID, &cve.Severity, &cve.CVSSScore,
		&cve.Description, &cve.Published, &cve.CreatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, cve)
}

// RemoveCVE removes a CVE from an asset.
// DELETE /api/compliance/assets/:id/cves/:cve
func (h *ComplianceHandler) RemoveCVE(c *gin.Context) {
	ctx := c.Request.Context()
	assetID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid asset id"})
		return
	}
	cveID := c.Param("cve")
	orgID, hasOrg := h.resolveOrgID(c)

	// Verify asset belongs to org
	var assetOrgID int
	if err := h.db.QueryRowContext(ctx, `SELECT org_id FROM ot_assets WHERE id=$1`, assetID).Scan(&assetOrgID); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if hasOrg && assetOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "asset not found"})
		return
	}

	res, err := h.db.ExecContext(ctx,
		`DELETE FROM ot_asset_cves WHERE asset_id=$1 AND cve_id=$2`, assetID, cveID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "CVE not found on asset"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "CVE removed"})
}

// ─── Frameworks & Assessment ──────────────────────────────────────────────────

// ListFrameworks returns all compliance frameworks.
// GET /api/compliance/frameworks
func (h *ComplianceHandler) ListFrameworks(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT id, code, name, version FROM compliance_frameworks ORDER BY id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var frameworks []ComplianceFramework
	for rows.Next() {
		var f ComplianceFramework
		if err := rows.Scan(&f.ID, &f.Code, &f.Name, &f.Version); err != nil {
			continue
		}
		frameworks = append(frameworks, f)
	}
	if frameworks == nil {
		frameworks = []ComplianceFramework{}
	}
	c.JSON(http.StatusOK, frameworks)
}

// GetAssessment returns all requirements for a framework with this org's assessment status.
// GET /api/compliance/frameworks/:code/assessment
func (h *ComplianceHandler) GetAssessment(c *gin.Context) {
	ctx := c.Request.Context()
	code := c.Param("code")
	orgID, hasOrg := h.resolveOrgID(c)

	// If no org context (global admin with no ?org_id), we need one
	if !hasOrg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id query parameter required"})
		return
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT cr.id, cr.req_code, cr.category, cr.title, cr.description, cr.weight,
		       COALESCE(ca.status, 'not_assessed') AS status,
		       ca.evidence, ca.notes, ca.assessed_at
		FROM compliance_requirements cr
		JOIN compliance_frameworks cf ON cf.id = cr.framework_id
		LEFT JOIN compliance_assessments ca
		  ON ca.requirement_id = cr.id AND ca.org_id = $2
		WHERE cf.code = $1
		ORDER BY cr.id
	`, code, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var reqs []ComplianceRequirementWithStatus
	for rows.Next() {
		var r ComplianceRequirementWithStatus
		if err := rows.Scan(&r.ID, &r.ReqCode, &r.Category, &r.Title, &r.Description,
			&r.Weight, &r.Status, &r.Evidence, &r.Notes, &r.AssessedAt); err != nil {
			continue
		}
		reqs = append(reqs, r)
	}
	if reqs == nil {
		reqs = []ComplianceRequirementWithStatus{}
	}
	c.JSON(http.StatusOK, reqs)
}

// UpdateAssessment upserts the compliance status of a requirement for an org.
// PUT /api/compliance/frameworks/:code/assessment/:req_id
func (h *ComplianceHandler) UpdateAssessment(c *gin.Context) {
	ctx := c.Request.Context()
	reqID, err := strconv.Atoi(c.Param("req_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid req_id"})
		return
	}
	orgID, hasOrg := h.resolveOrgID(c)
	if !hasOrg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id query parameter required"})
		return
	}
	userID, _ := middleware.GetUserID(c)

	var body struct {
		Status   string  `json:"status" binding:"required"`
		Evidence *string `json:"evidence"`
		Notes    *string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validStatuses := map[string]bool{
		"not_assessed": true, "compliant": true, "partial": true, "non_compliant": true,
	}
	if !validStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status: must be not_assessed|compliant|partial|non_compliant"})
		return
	}

	var assessedAt *time.Time
	if body.Status != "not_assessed" {
		now := time.Now()
		assessedAt = &now
	}

	_, err = h.db.ExecContext(ctx, `
		INSERT INTO compliance_assessments
		  (org_id, requirement_id, status, evidence, notes, assessed_by, assessed_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,now())
		ON CONFLICT (org_id, requirement_id) DO UPDATE
		  SET status=$3, evidence=$4, notes=$5, assessed_by=$6, assessed_at=$7, updated_at=now()
	`, orgID, reqID, body.Status, body.Evidence, body.Notes, userID, assessedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "assessment updated", "status": body.Status})
}

// ─── Risk Posture ─────────────────────────────────────────────────────────────

type ByTypeRow struct {
	DeviceType string  `json:"device_type"`
	Count      int     `json:"count"`
	AvgScore   float64 `json:"avg_score"`
}

type RiskPostureResponse struct {
	TotalAssets        int         `json:"total_assets"`
	AvgRiskScore       float64     `json:"avg_risk_score"`
	CriticalCVEs       int         `json:"critical_cves"`
	HighCVEs           int         `json:"high_cves"`
	UnauthorizedDevices int        `json:"unauthorized_devices"`
	ByType             []ByTypeRow `json:"by_type"`
	TopRisky           []OTAsset   `json:"top_risky"`
}

// RiskPosture returns a risk summary for the org.
// GET /api/compliance/risk-posture
func (h *ComplianceHandler) RiskPosture(c *gin.Context) {
	ctx := c.Request.Context()
	orgID, hasOrg := h.resolveOrgID(c)
	if !hasOrg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id query parameter required"})
		return
	}

	var resp RiskPostureResponse

	// Summary counts
	err := h.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) AS total_assets,
			COALESCE(AVG(risk_score), 0) AS avg_risk_score,
			COUNT(*) FILTER (WHERE NOT is_authorized) AS unauthorized_devices
		FROM ot_assets WHERE org_id = $1
	`, orgID).Scan(&resp.TotalAssets, &resp.AvgRiskScore, &resp.UnauthorizedDevices)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// CVE severity counts
	_ = h.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE c.severity = 'CRITICAL') AS critical_cves,
			COUNT(*) FILTER (WHERE c.severity = 'HIGH') AS high_cves
		FROM ot_asset_cves c
		JOIN ot_assets a ON a.id = c.asset_id
		WHERE a.org_id = $1
	`, orgID).Scan(&resp.CriticalCVEs, &resp.HighCVEs)

	// By device type
	typeRows, err := h.db.QueryContext(ctx, `
		SELECT COALESCE(device_type, 'Unknown') AS device_type,
		       COUNT(*) AS cnt,
		       COALESCE(AVG(risk_score), 0) AS avg_score
		FROM ot_assets
		WHERE org_id = $1
		GROUP BY device_type
		ORDER BY cnt DESC
	`, orgID)
	if err == nil {
		defer typeRows.Close()
		for typeRows.Next() {
			var row ByTypeRow
			if err := typeRows.Scan(&row.DeviceType, &row.Count, &row.AvgScore); err == nil {
				resp.ByType = append(resp.ByType, row)
			}
		}
	}
	if resp.ByType == nil {
		resp.ByType = []ByTypeRow{}
	}

	// Top risky assets (max 5)
	topRows, err := h.db.QueryContext(ctx, `
		SELECT id, org_id, gateway_id, ip_address, mac_address, hostname,
		       vendor, device_type, model, firmware_ver, protocol, os_info,
		       is_authorized, risk_score, last_seen, discovered_at, notes, created_at, updated_at
		FROM ot_assets
		WHERE org_id = $1
		ORDER BY risk_score DESC
		LIMIT 5
	`, orgID)
	if err == nil {
		defer topRows.Close()
		for topRows.Next() {
			var a OTAsset
			if err := topRows.Scan(&a.ID, &a.OrgID, &a.GatewayID, &a.IPAddress, &a.MACAddress,
				&a.Hostname, &a.Vendor, &a.DeviceType, &a.Model, &a.FirmwareVer,
				&a.Protocol, &a.OSInfo, &a.IsAuthorized, &a.RiskScore,
				&a.LastSeen, &a.DiscoveredAt, &a.Notes, &a.CreatedAt, &a.UpdatedAt); err == nil {
				a.CVEs = []CVEMatch{}
				resp.TopRisky = append(resp.TopRisky, a)
			}
		}
	}
	if resp.TopRisky == nil {
		resp.TopRisky = []OTAsset{}
	}
	resp.TopRisky = h.loadCVEs(ctx, resp.TopRisky)

	c.JSON(http.StatusOK, resp)
}

// ─── Compliance Score ─────────────────────────────────────────────────────────

type FrameworkScore struct {
	Score        float64 `json:"score"`
	Compliant    int     `json:"compliant"`
	Partial      int     `json:"partial"`
	NonCompliant int     `json:"non_compliant"`
	NotAssessed  int     `json:"not_assessed"`
	Total        int     `json:"total"`
}

type ComplianceScoreResponse struct {
	NIS2     FrameworkScore `json:"nis2"`
	IEC62443 FrameworkScore `json:"iec62443"`
	Overall  float64        `json:"overall"`
}

// ComplianceScore returns a weighted compliance score per framework and overall.
// GET /api/compliance/score
func (h *ComplianceHandler) ComplianceScore(c *gin.Context) {
	ctx := c.Request.Context()
	orgID, hasOrg := h.resolveOrgID(c)
	if !hasOrg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id query parameter required"})
		return
	}

	resp := ComplianceScoreResponse{}

	codes := []string{"NIS2", "IEC62443"}
	scores := make(map[string]*FrameworkScore)
	scores["NIS2"] = &resp.NIS2
	scores["IEC62443"] = &resp.IEC62443

	for _, code := range codes {
		rows, err := h.db.QueryContext(ctx, `
			SELECT cr.weight,
			       COALESCE(ca.status, 'not_assessed') AS status
			FROM compliance_requirements cr
			JOIN compliance_frameworks cf ON cf.id = cr.framework_id
			LEFT JOIN compliance_assessments ca
			  ON ca.requirement_id = cr.id AND ca.org_id = $2
			WHERE cf.code = $1
		`, code, orgID)
		if err != nil {
			continue
		}

		fs := scores[code]
		var weightedSum, totalWeight float64

		for rows.Next() {
			var weight int
			var status string
			if err := rows.Scan(&weight, &status); err != nil {
				continue
			}
			fs.Total++
			w := float64(weight)
			totalWeight += w
			switch status {
			case "compliant":
				fs.Compliant++
				weightedSum += w * 100.0
			case "partial":
				fs.Partial++
				weightedSum += w * 50.0
			case "non_compliant":
				fs.NonCompliant++
			default:
				fs.NotAssessed++
			}
		}
		rows.Close()

		if totalWeight > 0 {
			fs.Score = weightedSum / totalWeight
		}
	}

	// Overall = simple average of the two framework scores
	resp.Overall = (resp.NIS2.Score + resp.IEC62443.Score) / 2.0

	c.JSON(http.StatusOK, resp)
}

// ─── Scan Jobs ────────────────────────────────────────────────────────────────

// TriggerScan creates a scan job and runs a simulated discovery (no nmap yet).
// POST /api/compliance/scan
func (h *ComplianceHandler) TriggerScan(c *gin.Context) {
	ctx := c.Request.Context()
	orgID, hasOrg := h.resolveOrgID(c)
	if !hasOrg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id query parameter required"})
		return
	}
	userID, _ := middleware.GetUserID(c)

	var body struct {
		Subnet string `json:"subnet" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var job ScanJob
	err := h.db.QueryRowContext(ctx, `
		INSERT INTO ot_scan_jobs (org_id, subnet, status, created_by)
		VALUES ($1, $2, 'pending', $3)
		RETURNING id, org_id, subnet, status, started_at, finished_at, found_count, new_count, error, created_at
	`, orgID, body.Subnet, userID,
	).Scan(&job.ID, &job.OrgID, &job.Subnet, &job.Status, &job.StartedAt, &job.FinishedAt,
		&job.FoundCount, &job.NewCount, &job.Error, &job.CreatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Simulated scan: background goroutine marks the job as completed after 3s.
	go h.runSimulatedScan(job.ID)

	c.JSON(http.StatusAccepted, job)
}

// runSimulatedScan is a no-op simulation of a network scan (no real nmap).
// After 3 seconds it marks the job completed with found_count=0.
func (h *ComplianceHandler) runSimulatedScan(jobID int) {
	time.Sleep(3 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now()
	_, _ = h.db.ExecContext(ctx, `
		UPDATE ot_scan_jobs
		SET status='completed', started_at=$2, finished_at=$3, found_count=0, new_count=0
		WHERE id=$1
	`, jobID, now.Add(-3*time.Second), now)
}

// GetScanStatus returns the status of a scan job.
// GET /api/compliance/scan/:id
func (h *ComplianceHandler) GetScanStatus(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scan id"})
		return
	}
	orgID, hasOrg := h.resolveOrgID(c)

	var job ScanJob
	var q string
	var args []interface{}
	if hasOrg {
		q = `SELECT id, org_id, subnet, status, started_at, finished_at, found_count, new_count, error, created_at
		     FROM ot_scan_jobs WHERE id=$1 AND org_id=$2`
		args = []interface{}{id, orgID}
	} else {
		q = `SELECT id, org_id, subnet, status, started_at, finished_at, found_count, new_count, error, created_at
		     FROM ot_scan_jobs WHERE id=$1`
		args = []interface{}{id}
	}

	err = h.db.QueryRowContext(ctx, q, args...).Scan(
		&job.ID, &job.OrgID, &job.Subnet, &job.Status, &job.StartedAt, &job.FinishedAt,
		&job.FoundCount, &job.NewCount, &job.Error, &job.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "scan job not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, job)
}

// ─── Gateway Asset Sync + Auto-Assess ────────────────────────────────────────

// SyncAssets POST /api/compliance/sync-assets (admin only)
// Triggers SyncGatewayAssets for the current org and returns created/updated counts.
func (h *ComplianceHandler) SyncAssets(c *gin.Context) {
	orgID, hasOrg := h.resolveOrgID(c)
	if !hasOrg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "organization context required"})
		return
	}

	created, updated, err := otSync.SyncGatewayAssets(c.Request.Context(), h.db, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"created": created, "updated": updated})
}

// autoAssessResult is one item in the auto-assessment output.
type autoAssessResult struct {
	ReqCode  string `json:"req_code"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

// AutoAssess GET /api/compliance/auto-assess
// Runs automated checks against NIS2 requirements and persists the results.
func (h *ComplianceHandler) AutoAssess(c *gin.Context) {
	orgID, hasOrg := h.resolveOrgID(c)
	if !hasOrg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "organization context required"})
		return
	}

	ctx := c.Request.Context()
	now := time.Now()

	var checks []autoAssessResult

	// NIS2-A2: Analisi del rischio periodica — completed OT scan in last 30 days.
	{
		var cnt int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ot_scan_jobs WHERE org_id=$1 AND status='completed' AND finished_at > $2`,
			orgID, now.AddDate(0, 0, -30)).Scan(&cnt)
		status, evidence := "non_compliant", "No completed OT scan in the last 30 days"
		if cnt > 0 {
			status = "compliant"
			evidence = fmt.Sprintf("%d completed OT scan(s) in last 30 days", cnt)
		}
		checks = append(checks, autoAssessResult{"NIS2-A2", status, evidence})
	}

	// NIS2-B1: Rilevamento degli incidenti — threat events logged recently.
	{
		var cnt int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ot_threat_events WHERE org_id=$1 AND created_at > $2`,
			orgID, now.AddDate(0, 0, -30)).Scan(&cnt)
		status, evidence := "non_compliant", "No threat events logged in last 30 days — detection may not be active"
		if cnt > 0 {
			status = "compliant"
			evidence = fmt.Sprintf("%d threat event(s) detected in last 30 days", cnt)
		}
		checks = append(checks, autoAssessResult{"NIS2-B1", status, evidence})
	}

	// NIS2-B3: CSIRT 24h early warning — process active (incident exists).
	{
		var cnt int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM csirt_incidents WHERE org_id=$1`, orgID).Scan(&cnt)
		status, evidence := "non_compliant", "No CSIRT incidents registered — process may not be active"
		if cnt > 0 {
			status = "compliant"
			evidence = fmt.Sprintf("CSIRT process active: %d incident(s) registered", cnt)
		}
		checks = append(checks, autoAssessResult{"NIS2-B3", status, evidence})
	}

	// NIS2-B4: Final incident report (30 days) — no overdue final reports.
	{
		var overdue int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM csirt_incidents
			 WHERE org_id=$1 AND status != 'closed'
			   AND final_report_due < now() AND final_report_sent_at IS NULL`, orgID).Scan(&overdue)
		status, evidence := "compliant", "No overdue final incident reports"
		if overdue > 0 {
			status = "non_compliant"
			evidence = fmt.Sprintf("%d incident(s) with overdue final report", overdue)
		}
		checks = append(checks, autoAssessResult{"NIS2-B4", status, evidence})
	}

	// NIS2-D1: Vendor inventory.
	{
		var cnt int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ot_vendors WHERE org_id=$1`, orgID).Scan(&cnt)
		status, evidence := "non_compliant", "Vendor inventory is empty"
		if cnt > 0 {
			status = "compliant"
			evidence = fmt.Sprintf("%d vendor(s) in inventory", cnt)
		}
		checks = append(checks, autoAssessResult{"NIS2-D1", status, evidence})
	}

	// NIS2-D2: Vendor risk assessment.
	{
		var cnt int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ot_vendors WHERE org_id=$1 AND risk_score > 0`, orgID).Scan(&cnt)
		status, evidence := "non_compliant", "No vendor risk assessments completed"
		if cnt > 0 {
			status = "compliant"
			evidence = fmt.Sprintf("%d vendor(s) with risk score calculated", cnt)
		}
		checks = append(checks, autoAssessResult{"NIS2-D2", status, evidence})
	}

	// NIS2-D3: Security clauses in contracts.
	{
		var total, withClauses int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*), COUNT(*) FILTER (WHERE security_clauses) FROM ot_vendors WHERE org_id=$1`,
			orgID).Scan(&total, &withClauses)
		status := "non_compliant"
		evidence := "No vendors with security clauses in contracts"
		if total > 0 {
			pct := float64(withClauses) / float64(total) * 100
			evidence = fmt.Sprintf("%d/%d vendors (%.0f%%) have security clauses", withClauses, total, pct)
			if pct >= 80 {
				status = "compliant"
			} else if withClauses > 0 {
				status = "partial"
			}
		}
		checks = append(checks, autoAssessResult{"NIS2-D3", status, evidence})
	}

	// NIS2-E2: Secure OT protocols — encrypted protocol percentage.
	{
		var total, encrypted int
		_ = h.db.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(*) FILTER (WHERE UPPER(g.driver_type) IN ('OPC_UA','MQTT'))
			FROM gateways g
			JOIN areas a ON a.id=g.area_id
			JOIN sites  s ON s.id=a.site_id
			WHERE s.org_id=$1 AND g.enabled=true`, orgID).Scan(&total, &encrypted)
		status := "non_compliant"
		evidence := "No gateways found"
		if total > 0 {
			pct := float64(encrypted) / float64(total) * 100
			evidence = fmt.Sprintf("%d/%d gateways (%.0f%%) use encrypted-capable protocols", encrypted, total, pct)
			if pct > 50 {
				status = "compliant"
			} else if pct > 0 {
				status = "partial"
			}
		}
		checks = append(checks, autoAssessResult{"NIS2-E2", status, evidence})
	}

	// NIS2-G3: Patch management — firmware version tracked.
	{
		var cnt int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ot_assets WHERE org_id=$1 AND firmware_ver IS NOT NULL AND firmware_ver != ''`,
			orgID).Scan(&cnt)
		status := "non_compliant"
		evidence := "No OT assets with firmware version tracked"
		if cnt > 0 {
			status = "partial"
			evidence = fmt.Sprintf("%d asset(s) with firmware version tracked — verify patch process manually", cnt)
		}
		checks = append(checks, autoAssessResult{"NIS2-G3", status, evidence})
	}

	// NIS2-H1: Encryption in transit — same metric as E2.
	{
		var total, encrypted int
		_ = h.db.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(*) FILTER (WHERE UPPER(g.driver_type) IN ('OPC_UA','MQTT'))
			FROM gateways g
			JOIN areas a ON a.id=g.area_id
			JOIN sites  s ON s.id=a.site_id
			WHERE s.org_id=$1 AND g.enabled=true`, orgID).Scan(&total, &encrypted)
		status := "non_compliant"
		evidence := "No gateways found"
		if total > 0 {
			pct := float64(encrypted) / float64(total) * 100
			evidence = fmt.Sprintf("%.0f%% of gateways use protocols supporting encryption", pct)
			if pct > 50 {
				status = "compliant"
			} else if pct > 0 {
				status = "partial"
			}
		}
		checks = append(checks, autoAssessResult{"NIS2-H1", status, evidence})
	}

	// NIS2-J1: MFA — any users with TOTP enabled.
	{
		var mfaUsers int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE org_id=$1 AND totp_enabled=true`, orgID).Scan(&mfaUsers)
		evidence := "MFA availability requires manual verification"
		if mfaUsers > 0 {
			evidence = fmt.Sprintf("%d user(s) have TOTP/MFA enabled — verify policy enforcement manually", mfaUsers)
		}
		checks = append(checks, autoAssessResult{"NIS2-J1", "partial", evidence})
	}

	// NIS2-J3: OT/IT segregation — partial if OT assets inventoried.
	{
		var cnt int
		_ = h.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ot_assets WHERE org_id=$1`, orgID).Scan(&cnt)
		status := "non_compliant"
		evidence := "No OT assets inventoried — network cannot be assessed"
		if cnt > 0 {
			status = "partial"
			evidence = fmt.Sprintf("%d OT asset(s) inventoried — verify network segmentation manually", cnt)
		}
		checks = append(checks, autoAssessResult{"NIS2-J3", status, evidence})
	}

	// NIS2-J4: Audit log — always compliant (platform has built-in audit trail).
	checks = append(checks, autoAssessResult{
		"NIS2-J4", "compliant",
		"Platform audit log is active (audit_logs table)",
	})

	// Persist into compliance_assessments.
	userID, _ := middleware.GetUserID(c)
	for _, chk := range checks {
		var reqID int
		err := h.db.QueryRowContext(ctx, `
			SELECT r.id FROM compliance_requirements r
			JOIN compliance_frameworks f ON f.id = r.framework_id
			WHERE r.req_code=$1 AND f.code='NIS2'`, chk.ReqCode).Scan(&reqID)
		if err != nil {
			continue
		}
		_, _ = h.db.ExecContext(ctx, `
			INSERT INTO compliance_assessments
				(org_id, requirement_id, status, evidence, assessed_by, assessed_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,now(),now())
			ON CONFLICT (org_id, requirement_id) DO UPDATE SET
				status      = EXCLUDED.status,
				evidence    = EXCLUDED.evidence,
				assessed_by = EXCLUDED.assessed_by,
				assessed_at = now(),
				updated_at  = now()`,
			orgID, reqID, chk.Status, chk.Evidence, userID)
	}

	c.JSON(http.StatusOK, checks)
}
