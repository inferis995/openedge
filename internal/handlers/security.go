package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

type SecurityHandler struct{ db *sql.DB }

func NewSecurityHandler(db *sql.DB) *SecurityHandler { return &SecurityHandler{db: db} }

type ScoreBreakdown struct {
	AuditLogging         bool `json:"audit_logging"`
	RBACEnabled          bool `json:"rbac_enabled"`
	BackupFresh          bool `json:"backup_fresh"`
	RateLimiting         bool `json:"rate_limiting"`
	MFAAnyAdmin          bool `json:"mfa_any_admin"`
	AccountLockoutActive bool `json:"account_lockout_active"`
	StrongPasswordPolicy bool `json:"strong_password_policy"`
	MQTTTLS              bool `json:"mqtt_tls"`
}

type SecurityOverview struct {
	Score             int            `json:"score"`
	ScoreBreakdown    ScoreBreakdown `json:"score_breakdown"`
	FailedLogins24h   int64          `json:"failed_logins_24h"`
	LockedAccounts    int64          `json:"locked_accounts"`
	SecurityEvents24h int64          `json:"security_events_24h"`
	NIS2ChecksPassed  int            `json:"nis2_checks_passed"`
	NIS2ChecksTotal   int            `json:"nis2_checks_total"`
}

func (h *SecurityHandler) Overview(c *gin.Context) {
	isGlobal := middleware.IsGlobalAdmin(c)
	orgID, _ := c.Get("org_id")

	var orgIDVal *int
	if !isGlobal {
		if oid, ok := orgID.(int); ok {
			orgIDVal = &oid
		}
	}

	breakdown := ScoreBreakdown{
		AuditLogging:         true,
		RBACEnabled:          true,
		BackupFresh:          false,
		RateLimiting:         true,
		MFAAnyAdmin:          false,
		AccountLockoutActive: true,
		StrongPasswordPolicy: false,
		MQTTTLS:              false,
	}

	// Check MFA
	var mfaCount int
	var mfaQuery string
	var mfaArgs []interface{}
	if isGlobal {
		mfaQuery = `SELECT COUNT(*) FROM users WHERE role='admin' AND totp_enabled=TRUE AND org_id IS NOT NULL`
	} else if orgIDVal != nil {
		mfaQuery = `SELECT COUNT(*) FROM users WHERE role='admin' AND totp_enabled=TRUE AND org_id=$1`
		mfaArgs = []interface{}{*orgIDVal}
	}
	if mfaQuery != "" {
		_ = h.db.QueryRowContext(c.Request.Context(), mfaQuery, mfaArgs...).Scan(&mfaCount)
		breakdown.MFAAnyAdmin = mfaCount > 0
	}

	// Check MQTT TLS
	var mqttTLSVal string
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT value FROM global_settings WHERE key='mqtt_tls_enabled'`).Scan(&mqttTLSVal); err == nil {
		breakdown.MQTTTLS = mqttTLSVal == "true"
	}

	// Check backup freshness
	var maxBackup sql.NullTime
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT MAX(created_at) FROM backup_catalog`).Scan(&maxBackup); err == nil {
		if maxBackup.Valid && time.Since(maxBackup.Time) < 25*time.Hour {
			breakdown.BackupFresh = true
		}
	}

	// Compute score
	score := 100
	if !breakdown.MFAAnyAdmin {
		score -= 15
	}
	if !breakdown.StrongPasswordPolicy {
		score -= 5
	}
	if !breakdown.MQTTTLS {
		score -= 10
	}
	if !breakdown.BackupFresh {
		score -= 10
	}

	// failed_logins_24h
	var failedLogins int64
	var flQuery string
	var flArgs []interface{}
	if isGlobal {
		flQuery = `SELECT COUNT(*) FROM audit_logs WHERE action='login' AND success=FALSE AND created_at > NOW()-INTERVAL '24 hours'`
	} else if orgIDVal != nil {
		flQuery = `SELECT COUNT(*) FROM audit_logs WHERE action='login' AND success=FALSE AND created_at > NOW()-INTERVAL '24 hours' AND org_id=$1`
		flArgs = []interface{}{*orgIDVal}
	}
	if flQuery != "" {
		_ = h.db.QueryRowContext(c.Request.Context(), flQuery, flArgs...).Scan(&failedLogins)
	}

	// locked_accounts
	var lockedAccounts int64
	var laQuery string
	var laArgs []interface{}
	if isGlobal {
		laQuery = `SELECT COUNT(*) FROM users WHERE locked_until > NOW() AND org_id IS NOT NULL`
	} else if orgIDVal != nil {
		laQuery = `SELECT COUNT(*) FROM users WHERE locked_until > NOW() AND org_id=$1`
		laArgs = []interface{}{*orgIDVal}
	}
	if laQuery != "" {
		_ = h.db.QueryRowContext(c.Request.Context(), laQuery, laArgs...).Scan(&lockedAccounts)
	}

	// security_events_24h
	var secEvents24h int64
	var seQuery string
	var seArgs []interface{}
	if isGlobal {
		seQuery = `SELECT COUNT(*) FROM security_events WHERE created_at > NOW()-INTERVAL '24 hours'`
	} else if orgIDVal != nil {
		seQuery = `SELECT COUNT(*) FROM security_events WHERE created_at > NOW()-INTERVAL '24 hours' AND org_id=$1`
		seArgs = []interface{}{*orgIDVal}
	}
	if seQuery != "" {
		_ = h.db.QueryRowContext(c.Request.Context(), seQuery, seArgs...).Scan(&secEvents24h)
	}

	// NIS2 checks
	nis2Total := 12
	nis2Passed := 0
	checks := []bool{
		true,                           // risk_management
		false,                          // incident_handling
		breakdown.BackupFresh,          // business_continuity
		false,                          // supply_chain
		breakdown.MQTTTLS,              // network_security
		true,                           // access_control
		breakdown.MFAAnyAdmin,          // mfa
		true,                           // cryptography
		false,                          // vulnerability_mgmt
		true,                           // audit_logging
		breakdown.AccountLockoutActive, // account_security
		true,                           // data_protection
	}
	for _, p := range checks {
		if p {
			nis2Passed++
		}
	}

	c.JSON(http.StatusOK, SecurityOverview{
		Score:             score,
		ScoreBreakdown:    breakdown,
		FailedLogins24h:   failedLogins,
		LockedAccounts:    lockedAccounts,
		SecurityEvents24h: secEvents24h,
		NIS2ChecksPassed:  nis2Passed,
		NIS2ChecksTotal:   nis2Total,
	})
}

type SecurityEventRow struct {
	ID        int64     `json:"id"`
	OrgID     *int      `json:"org_id"`
	EventType string    `json:"event_type"`
	Severity  string    `json:"severity"`
	Actor     *string   `json:"actor"`
	Resource  *string   `json:"resource"`
	Detail    *string   `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *SecurityHandler) Events(c *gin.Context) {
	isGlobal := middleware.IsGlobalAdmin(c)
	orgID, _ := c.Get("org_id")

	var orgIDVal *int
	if !isGlobal {
		if oid, ok := orgID.(int); ok {
			orgIDVal = &oid
		}
	}

	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}

	var rows []SecurityEventRow

	// Security events from security_events table
	var seQuery string
	var seArgs []interface{}
	if isGlobal {
		seQuery = `SELECT id, org_id, event_type, severity, actor, resource, detail::text, created_at
			FROM security_events ORDER BY created_at DESC LIMIT $1`
		seArgs = []interface{}{limit}
	} else if orgIDVal != nil {
		seQuery = `SELECT id, org_id, event_type, severity, actor, resource, detail::text, created_at
			FROM security_events WHERE org_id=$1 ORDER BY created_at DESC LIMIT $2`
		seArgs = []interface{}{*orgIDVal, limit}
	}

	if seQuery != "" {
		seRows, err := h.db.QueryContext(c.Request.Context(), seQuery, seArgs...)
		if err == nil {
			defer seRows.Close()
			for seRows.Next() {
				var r SecurityEventRow
				var orgIDScan sql.NullInt64
				if err := seRows.Scan(&r.ID, &orgIDScan, &r.EventType, &r.Severity, &r.Actor, &r.Resource, &r.Detail, &r.CreatedAt); err == nil {
					if orgIDScan.Valid {
						oid := int(orgIDScan.Int64)
						r.OrgID = &oid
					}
					rows = append(rows, r)
				}
			}
		}
	}

	// Synthetic events from audit_logs (login failures)
	var alQuery string
	var alArgs []interface{}
	if isGlobal {
		alQuery = `SELECT 0 as id, NULL::int as org_id, 'login_failed' as event_type, 'medium' as severity,
			username as actor, ip_address as resource, NULL as detail, created_at
			FROM audit_logs WHERE action='login' AND success=FALSE
			AND created_at > NOW()-INTERVAL '7 days'
			ORDER BY created_at DESC LIMIT $1`
		alArgs = []interface{}{limit}
	} else if orgIDVal != nil {
		alQuery = `SELECT 0 as id, NULL::int as org_id, 'login_failed' as event_type, 'medium' as severity,
			username as actor, ip_address as resource, NULL as detail, created_at
			FROM audit_logs WHERE action='login' AND success=FALSE
			AND user_id IN (SELECT id FROM users WHERE org_id=$1)
			AND created_at > NOW()-INTERVAL '7 days'
			ORDER BY created_at DESC LIMIT $2`
		alArgs = []interface{}{*orgIDVal, limit}
	}

	if alQuery != "" {
		alRows, err := h.db.QueryContext(c.Request.Context(), alQuery, alArgs...)
		if err == nil {
			defer alRows.Close()
			for alRows.Next() {
				var r SecurityEventRow
				var orgIDScan sql.NullInt64
				if err := alRows.Scan(&r.ID, &orgIDScan, &r.EventType, &r.Severity, &r.Actor, &r.Resource, &r.Detail, &r.CreatedAt); err == nil {
					if orgIDScan.Valid {
						oid := int(orgIDScan.Int64)
						r.OrgID = &oid
					}
					rows = append(rows, r)
				}
			}
		}
	}

	// Sort by created_at desc and limit
	// Simple insertion sort for small N
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].CreatedAt.After(rows[j-1].CreatedAt); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []SecurityEventRow{}
	}

	c.JSON(http.StatusOK, rows)
}

type ComplianceCheck struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Article string `json:"article"`
	Passed  bool   `json:"passed"`
	Detail  string `json:"detail"`
}

func (h *SecurityHandler) Compliance(c *gin.Context) {
	// Check MFA
	isGlobal := middleware.IsGlobalAdmin(c)
	orgID, _ := c.Get("org_id")
	var orgIDVal *int
	if !isGlobal {
		if oid, ok := orgID.(int); ok {
			orgIDVal = &oid
		}
	}

	mfaPassed := false
	var mfaCount int
	var mfaQuery string
	var mfaArgs []interface{}
	if isGlobal {
		mfaQuery = `SELECT COUNT(*) FROM users WHERE role='admin' AND totp_enabled=TRUE AND org_id IS NOT NULL`
	} else if orgIDVal != nil {
		mfaQuery = `SELECT COUNT(*) FROM users WHERE role='admin' AND totp_enabled=TRUE AND org_id=$1`
		mfaArgs = []interface{}{*orgIDVal}
	}
	if mfaQuery != "" {
		_ = h.db.QueryRowContext(c.Request.Context(), mfaQuery, mfaArgs...).Scan(&mfaCount)
		mfaPassed = mfaCount > 0
	}

	// Check backup
	backupPassed := false
	var maxBackup sql.NullTime
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT MAX(created_at) FROM backup_catalog`).Scan(&maxBackup); err == nil {
		if maxBackup.Valid && time.Since(maxBackup.Time) < 25*time.Hour {
			backupPassed = true
		}
	}

	// Check MQTT TLS
	mqttPassed := false
	var mqttTLSVal string
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT value FROM global_settings WHERE key='mqtt_tls_enabled'`).Scan(&mqttTLSVal); err == nil {
		mqttPassed = mqttTLSVal == "true"
	}

	mfaDetail := "MFA non configurato su nessun admin"
	if mfaPassed {
		mfaDetail = fmt.Sprintf("%d admin con MFA abilitato", mfaCount)
	}

	checks := []ComplianceCheck{
		{ID: "risk_management", Name: "Gestione del rischio", Article: "Art. 21(2)(a)", Passed: true, Detail: "Audit log attivo"},
		{ID: "incident_handling", Name: "Gestione incidenti", Article: "Art. 21(2)(b)", Passed: false, Detail: "Nessun workflow di incident report configurato"},
		{ID: "business_continuity", Name: "Continuità operativa", Article: "Art. 21(2)(c)", Passed: backupPassed, Detail: "Backup con verifica integrità SHA-256"},
		{ID: "supply_chain", Name: "Sicurezza supply chain", Article: "Art. 21(2)(d)", Passed: false, Detail: "Versioni componenti edge non monitorate"},
		{ID: "network_security", Name: "Sicurezza di rete", Article: "Art. 21(2)(e)", Passed: mqttPassed, Detail: "MQTT non cifrato (TLS assente)"},
		{ID: "access_control", Name: "Controllo accessi", Article: "Art. 21(2)(i)", Passed: true, Detail: "RBAC granulare + SSO"},
		{ID: "mfa", Name: "Autenticazione multi-fattore", Article: "Art. 21(2)(j)", Passed: mfaPassed, Detail: mfaDetail},
		{ID: "cryptography", Name: "Crittografia", Article: "Art. 21(2)(h)", Passed: true, Detail: "Bcrypt password, SHA-256 backup, JWT HS256"},
		{ID: "vulnerability_mgmt", Name: "Gestione vulnerabilità", Article: "Art. 21(2)(e)", Passed: false, Detail: "Nessun monitoraggio versioni/CVE"},
		{ID: "audit_logging", Name: "Logging e monitoraggio", Article: "Art. 21(2)(f)", Passed: true, Detail: "Audit log completo con IP e user agent"},
		{ID: "account_security", Name: "Sicurezza account", Article: "Art. 21(2)(i)", Passed: true, Detail: "Rate limiting login + lockout automatico"},
		{ID: "data_protection", Name: "Protezione dati", Article: "Art. 21(2)(h)", Passed: true, Detail: "Backup con hash integrità, password hashed"},
	}

	c.JSON(http.StatusOK, checks)
}
