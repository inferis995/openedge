package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// VendorRiskHandler handles Art.18 NIS2 supply chain vendor risk endpoints.
type VendorRiskHandler struct {
	db *sql.DB
}

// NewVendorRiskHandler creates a new vendor risk handler.
func NewVendorRiskHandler(db *sql.DB) *VendorRiskHandler {
	return &VendorRiskHandler{db: db}
}

// OTVendor represents a supply-chain vendor entry.
type OTVendor struct {
	ID              int        `json:"id"`
	OrgID           int        `json:"org_id"`
	Name            string     `json:"name"`
	Country         *string    `json:"country"`
	Website         *string    `json:"website"`
	ContactEmail    *string    `json:"contact_email"`
	ProductsUsed    *string    `json:"products_used"`
	HasISO27001     bool       `json:"has_iso27001"`
	HasSOC2         bool       `json:"has_soc2"`
	HasIEC62443     bool       `json:"has_iec62443"`
	LastAuditDate   *time.Time `json:"last_audit_date"`
	DataAccessLevel string     `json:"data_access_level"`
	NetworkAccess   bool       `json:"network_access"`
	RemoteAccess    bool       `json:"remote_access"`
	ContractStart   *time.Time `json:"contract_start"`
	ContractEnd     *time.Time `json:"contract_end"`
	SecurityClauses bool       `json:"security_clauses"`
	RiskScore       int        `json:"risk_score"`
	Criticality     string     `json:"criticality"`
	Notes           *string    `json:"notes"`
	AutoImported    bool       `json:"auto_imported"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CreateVendorRequest is the body for creating a vendor.
type CreateVendorRequest struct {
	Name            string  `json:"name" binding:"required"`
	Country         *string `json:"country"`
	Website         *string `json:"website"`
	ContactEmail    *string `json:"contact_email"`
	ProductsUsed    *string `json:"products_used"`
	HasISO27001     bool    `json:"has_iso27001"`
	HasSOC2         bool    `json:"has_soc2"`
	HasIEC62443     bool    `json:"has_iec62443"`
	LastAuditDate   *string `json:"last_audit_date"`
	DataAccessLevel string  `json:"data_access_level"`
	NetworkAccess   bool    `json:"network_access"`
	RemoteAccess    bool    `json:"remote_access"`
	ContractStart   *string `json:"contract_start"`
	ContractEnd     *string `json:"contract_end"`
	SecurityClauses bool    `json:"security_clauses"`
	Notes           *string `json:"notes"`
}

// UpdateVendorRequest is the body for updating a vendor.
type UpdateVendorRequest struct {
	Name            *string `json:"name"`
	Country         *string `json:"country"`
	Website         *string `json:"website"`
	ContactEmail    *string `json:"contact_email"`
	ProductsUsed    *string `json:"products_used"`
	HasISO27001     *bool   `json:"has_iso27001"`
	HasSOC2         *bool   `json:"has_soc2"`
	HasIEC62443     *bool   `json:"has_iec62443"`
	LastAuditDate   *string `json:"last_audit_date"`
	DataAccessLevel *string `json:"data_access_level"`
	NetworkAccess   *bool   `json:"network_access"`
	RemoteAccess    *bool   `json:"remote_access"`
	ContractStart   *string `json:"contract_start"`
	ContractEnd     *string `json:"contract_end"`
	SecurityClauses *bool   `json:"security_clauses"`
	Notes           *string `json:"notes"`
}

const vendorSelectCols = `
	id, org_id, name, country, website, contact_email, products_used,
	has_iso27001, has_soc2, has_iec62443, last_audit_date,
	data_access_level, network_access, remote_access,
	contract_start, contract_end, security_clauses,
	risk_score, criticality, notes, auto_imported,
	created_at, updated_at`

func scanOTVendor(row interface {
	Scan(...interface{}) error
}) (*OTVendor, error) {
	var v OTVendor
	err := row.Scan(
		&v.ID, &v.OrgID, &v.Name, &v.Country, &v.Website, &v.ContactEmail, &v.ProductsUsed,
		&v.HasISO27001, &v.HasSOC2, &v.HasIEC62443, &v.LastAuditDate,
		&v.DataAccessLevel, &v.NetworkAccess, &v.RemoteAccess,
		&v.ContractStart, &v.ContractEnd, &v.SecurityClauses,
		&v.RiskScore, &v.Criticality, &v.Notes, &v.AutoImported,
		&v.CreatedAt, &v.UpdatedAt,
	)
	return &v, err
}

// euCountries is the set of EU/EEA country codes for the risk score formula.
var euCountries = map[string]bool{
	"IT": true, "DE": true, "FR": true, "ES": true, "NL": true,
	"BE": true, "AT": true, "SE": true, "FI": true, "DK": true,
	"NO": true, "IE": true, "PT": true, "LU": true, "PL": true,
	"CZ": true, "HU": true, "RO": true, "BG": true, "HR": true,
	"SK": true, "SI": true, "EE": true, "LV": true, "LT": true,
	"CY": true, "MT": true, "GR": true,
}

// calcVendorScore computes a 0-100 risk score (higher = less risky).
func calcVendorScore(v *OTVendor) (score int, criticality string) {
	s := 50
	if v.HasISO27001 {
		s += 15
	}
	if v.HasSOC2 {
		s += 10
	}
	if v.HasIEC62443 {
		s += 15
	}
	if v.LastAuditDate != nil {
		age := time.Since(*v.LastAuditDate)
		if age <= 365*24*time.Hour {
			s += 10
		} else if age <= 2*365*24*time.Hour {
			s += 5
		}
	}
	if v.SecurityClauses {
		s += 10
	}
	switch v.DataAccessLevel {
	case "admin":
		s -= 20
	case "write":
		s -= 10
	case "read":
		s -= 5
	}
	if v.RemoteAccess {
		s -= 10
	}
	if v.NetworkAccess {
		s -= 5
	}
	if v.Country != nil && *v.Country != "" && !euCountries[strings.ToUpper(*v.Country)] {
		s -= 10
	}
	if s < 0 {
		s = 0
	}
	if s > 100 {
		s = 100
	}
	switch {
	case s <= 25:
		criticality = "critical"
	case s <= 50:
		criticality = "high"
	case s <= 75:
		criticality = "medium"
	default:
		criticality = "low"
	}
	return s, criticality
}

// ListVendors GET /api/compliance/vendors
func (h *VendorRiskHandler) ListVendors(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT `+vendorSelectCols+` FROM ot_vendors WHERE org_id = $1 ORDER BY risk_score ASC`,
		orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var vendors []*OTVendor
	for rows.Next() {
		v, err := scanOTVendor(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		vendors = append(vendors, v)
	}
	if vendors == nil {
		vendors = []*OTVendor{}
	}
	c.JSON(http.StatusOK, vendors)
}

// CreateVendor POST /api/compliance/vendors
func (h *VendorRiskHandler) CreateVendor(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	var req CreateVendorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dal := req.DataAccessLevel
	if dal == "" {
		dal = "none"
	}

	// Build a temporary OTVendor to compute score.
	tmp := &OTVendor{
		HasISO27001:     req.HasISO27001,
		HasSOC2:         req.HasSOC2,
		HasIEC62443:     req.HasIEC62443,
		SecurityClauses: req.SecurityClauses,
		DataAccessLevel: dal,
		NetworkAccess:   req.NetworkAccess,
		RemoteAccess:    req.RemoteAccess,
		Country:         req.Country,
	}
	if req.LastAuditDate != nil {
		if t, err := time.Parse("2006-01-02", *req.LastAuditDate); err == nil {
			tmp.LastAuditDate = &t
		}
	}
	score, crit := calcVendorScore(tmp)

	var id int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO ot_vendors
			(org_id, name, country, website, contact_email, products_used,
			 has_iso27001, has_soc2, has_iec62443, last_audit_date,
			 data_access_level, network_access, remote_access,
			 contract_start, contract_end, security_clauses,
			 risk_score, criticality, notes, auto_imported)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,false)
		RETURNING id`,
		orgID, req.Name, req.Country, req.Website, req.ContactEmail, req.ProductsUsed,
		req.HasISO27001, req.HasSOC2, req.HasIEC62443,
		req.LastAuditDate, dal, req.NetworkAccess, req.RemoteAccess,
		req.ContractStart, req.ContractEnd, req.SecurityClauses,
		score, crit, req.Notes,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	v, err := h.getVendorByID(c, orgID, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, v)
}

// UpdateVendor PUT /api/compliance/vendors/:id
func (h *VendorRiskHandler) UpdateVendor(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req UpdateVendorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = h.db.ExecContext(c.Request.Context(), `
		UPDATE ot_vendors SET
			name             = COALESCE($3, name),
			country          = COALESCE($4, country),
			website          = COALESCE($5, website),
			contact_email    = COALESCE($6, contact_email),
			products_used    = COALESCE($7, products_used),
			has_iso27001     = COALESCE($8, has_iso27001),
			has_soc2         = COALESCE($9, has_soc2),
			has_iec62443     = COALESCE($10, has_iec62443),
			last_audit_date  = COALESCE($11::date, last_audit_date),
			data_access_level= COALESCE($12, data_access_level),
			network_access   = COALESCE($13, network_access),
			remote_access    = COALESCE($14, remote_access),
			contract_start   = COALESCE($15::date, contract_start),
			contract_end     = COALESCE($16::date, contract_end),
			security_clauses = COALESCE($17, security_clauses),
			notes            = COALESCE($18, notes),
			updated_at       = now()
		WHERE id = $1 AND org_id = $2`,
		id, orgID,
		req.Name, req.Country, req.Website, req.ContactEmail, req.ProductsUsed,
		req.HasISO27001, req.HasSOC2, req.HasIEC62443,
		req.LastAuditDate, req.DataAccessLevel, req.NetworkAccess, req.RemoteAccess,
		req.ContractStart, req.ContractEnd, req.SecurityClauses,
		req.Notes,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Re-fetch and recalculate score.
	v, err := h.getVendorByID(c, orgID, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	score, crit := calcVendorScore(v)
	_, _ = h.db.ExecContext(c.Request.Context(),
		`UPDATE ot_vendors SET risk_score=$3, criticality=$4, updated_at=now() WHERE id=$1 AND org_id=$2`,
		id, orgID, score, crit)
	v.RiskScore = score
	v.Criticality = crit
	c.JSON(http.StatusOK, v)
}

// DeleteVendor DELETE /api/compliance/vendors/:id
func (h *VendorRiskHandler) DeleteVendor(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, err = h.db.ExecContext(c.Request.Context(),
		`DELETE FROM ot_vendors WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// RecalculateScore POST /api/compliance/vendors/:id/score
func (h *VendorRiskHandler) RecalculateScore(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	v, err := h.getVendorByID(c, orgID, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	score, crit := calcVendorScore(v)
	_, err = h.db.ExecContext(c.Request.Context(),
		`UPDATE ot_vendors SET risk_score=$3, criticality=$4, updated_at=now() WHERE id=$1 AND org_id=$2`,
		id, orgID, score, crit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	v.RiskScore = score
	v.Criticality = crit
	c.JSON(http.StatusOK, v)
}

// SyncFromGateways POST /api/compliance/vendors/sync
func (h *VendorRiskHandler) SyncFromGateways(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT g.id, g.name, g.driver_type, g.connection_config
		FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 AND g.enabled = true`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type gatewayRow struct {
		ID               int
		Name             string
		DriverType       string
		ConnectionConfig []byte
	}

	var gws []gatewayRow
	for rows.Next() {
		var g gatewayRow
		if err := rows.Scan(&g.ID, &g.Name, &g.DriverType, &g.ConnectionConfig); err != nil {
			continue
		}
		gws = append(gws, g)
	}

	// Extract unique vendor names.
	seen := map[string]bool{}
	var vendorNames []string
	for _, g := range gws {
		var cfg map[string]interface{}
		if len(g.ConnectionConfig) > 0 {
			_ = json.Unmarshal(g.ConnectionConfig, &cfg)
		}
		name := ""
		if cfg != nil {
			if v, ok := cfg["vendor"].(string); ok && v != "" {
				name = v
			}
		}
		if name == "" {
			parts := strings.Fields(g.Name)
			if len(parts) > 0 {
				name = parts[0]
			}
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		vendorNames = append(vendorNames, name)
	}

	imported := 0
	updated := 0
	for _, name := range vendorNames {
		var existsID int
		scanErr := h.db.QueryRowContext(c.Request.Context(),
			`SELECT id FROM ot_vendors WHERE org_id=$1 AND name=$2`, orgID, name).Scan(&existsID)
		if scanErr == sql.ErrNoRows {
			tmp := &OTVendor{
				DataAccessLevel: "write",
				NetworkAccess:   true,
				RemoteAccess:    false,
			}
			score, crit := calcVendorScore(tmp)
			_, err = h.db.ExecContext(c.Request.Context(), `
				INSERT INTO ot_vendors
					(org_id, name, data_access_level, network_access, risk_score, criticality, auto_imported)
				VALUES ($1,$2,'write',true,$3,$4,true)
				ON CONFLICT (org_id, name) DO NOTHING`,
				orgID, name, score, crit)
			if err == nil {
				imported++
			}
		} else if scanErr == nil {
			tmp := &OTVendor{
				DataAccessLevel: "write",
				NetworkAccess:   true,
			}
			score, crit := calcVendorScore(tmp)
			_, err = h.db.ExecContext(c.Request.Context(), `
				UPDATE ot_vendors SET
					data_access_level = CASE WHEN auto_imported THEN 'write' ELSE data_access_level END,
					network_access    = CASE WHEN auto_imported THEN true ELSE network_access END,
					risk_score        = CASE WHEN auto_imported THEN $2 ELSE risk_score END,
					criticality       = CASE WHEN auto_imported THEN $3 ELSE criticality END,
					updated_at        = now()
				WHERE id=$1`,
				existsID, score, crit)
			if err == nil {
				updated++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"imported": imported, "updated": updated})
}

// Summary GET /api/compliance/vendors/summary
func (h *VendorRiskHandler) Summary(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	type VendorSummary struct {
		Total                 int     `json:"total"`
		Critical              int     `json:"critical"`
		High                  int     `json:"high"`
		AvgScore              float64 `json:"avg_score"`
		ContractsExpiringSoon int     `json:"contracts_expiring_soon"`
	}
	var s VendorSummary

	err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE criticality = 'critical')::int,
			COUNT(*) FILTER (WHERE criticality = 'high')::int,
			COALESCE(AVG(risk_score), 0),
			COUNT(*) FILTER (WHERE contract_end IS NOT NULL AND contract_end BETWEEN now() AND now() + INTERVAL '90 days')::int
		FROM ot_vendors WHERE org_id = $1`, orgID,
	).Scan(&s.Total, &s.Critical, &s.High, &s.AvgScore, &s.ContractsExpiringSoon)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

// getVendorByID is an internal helper.
func (h *VendorRiskHandler) getVendorByID(c *gin.Context, orgID, id int) (*OTVendor, error) {
	row := h.db.QueryRowContext(c.Request.Context(),
		`SELECT `+vendorSelectCols+` FROM ot_vendors WHERE id = $1 AND org_id = $2`,
		id, orgID)
	return scanOTVendor(row)
}
