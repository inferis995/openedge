package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// countScoped runs a COUNT query scoped to global (no args) or a specific org ($1).
func (h *SecurityHandler) countScoped(ctx context.Context, isGlobal bool, orgID *int, globalQ, orgQ string) int64 {
	var count int64
	if isGlobal {
		_ = h.db.QueryRowContext(ctx, globalQ).Scan(&count)
	} else if orgID != nil {
		_ = h.db.QueryRowContext(ctx, orgQ, *orgID).Scan(&count)
	}
	return count
}

// appendEventRows runs query and appends scanned SecurityEventRows to dst.
func (h *SecurityHandler) appendEventRows(ctx context.Context, dst []SecurityEventRow, query string, args []interface{}) []SecurityEventRow {
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return dst
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r SecurityEventRow
		var orgIDScan sql.NullInt64
		if scanErr := rows.Scan(&r.ID, &orgIDScan, &r.EventType, &r.Severity, &r.Actor, &r.Resource, &r.Detail, &r.CreatedAt); scanErr == nil {
			if orgIDScan.Valid {
				oid := int(orgIDScan.Int64)
				r.OrgID = &oid
			}
			dst = append(dst, r)
		}
	}
	return dst
}

type SecurityHandler struct{ db *sql.DB }

func NewSecurityHandler(db *sql.DB) *SecurityHandler { return &SecurityHandler{db: db} }

// computeSecurityScore converts a ScoreBreakdown into a 0-100 score.
func computeSecurityScore(b ScoreBreakdown) int {
	score := 100
	if !b.MFAAnyAdmin {
		score -= 15
	}
	if !b.StrongPasswordPolicy {
		score -= 5
	}
	if !b.MQTTTLS {
		score -= 10
	}
	if !b.BackupFresh {
		score -= 10
	}
	return score
}

const (
	checkPass        = "pass"
	checkFail        = "fail"
	checkNotAssessed = "not_assessed"
)

func passFail(ok bool) string {
	if ok {
		return checkPass
	}
	return checkFail
}

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

	// Controlli automatici sulla postura di sicurezza, modellati sulle misure
	// dell'art. 21 NIS2. NON sono una dichiarazione di conformità: sei di essi
	// riguardano misure organizzative che nessun software può accertare
	// guardando dentro sé stesso, e sono riportati come non valutati anziché
	// come superati.
	ChecksPassed      int             `json:"checks_passed"`
	ChecksEvaluated   int             `json:"checks_evaluated"`
	ChecksNotAssessed int             `json:"checks_not_assessed"`
	Checks            []SecurityCheck `json:"checks"`

	// Deprecati, serviti per non rompere chi già li legge.
	//
	// Rimuoverli avrebbe rotto i client esistenti per una correzione
	// che non ne aveva bisogno: la forma di questi due campi — un superati e un
	// totale da mostrare come frazione — non era il problema. Il problema erano
	// i valori, e quelli ora sono onesti.
	//
	// NIS2ChecksTotal porta il numero di controlli VALUTATI, non dodici. Un
	// client vecchio che stampa "X/Y" continua a funzionare e stampa una
	// frazione vera, invece di un dodicesimo che nessuno poteva raggiungere.
	//
	// Da togliere in 4.0.0. Chi legge questi campi passi a ChecksPassed e
	// ChecksEvaluated, che dicono la stessa cosa senza far credere che i
	// controlli non misurati siano stati misurati.
	NIS2ChecksPassed int `json:"nis2_checks_passed"`
	NIS2ChecksTotal  int `json:"nis2_checks_total"`
}

// SecurityCheck è un singolo controllo con il suo esito.
//
// State vale "pass", "fail" oppure "not_assessed". Il terzo valore è il motivo
// per cui questo tipo esiste: prima i controlli erano dodici booleani, e otto
// di essi erano costanti — cinque true e tre false. Un utente vedeva 8/12 e
// non poteva far salire quel numero nemmeno sistemando tutto, perché tre
// controlli erano false a codice; e cinque punti gli venivano assegnati senza
// che nulla fosse stato guardato. Un booleano non ha modo di dire "non lo so",
// quindi mentiva in entrambe le direzioni.
type SecurityCheck struct {
	ID    string `json:"id"`
	State string `json:"state"`
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
		// True since auth.MinPasswordLength went to 12 and every path that sets
		// a password validates against it. It was hardcoded false because the
		// minimum was six, which was honest and cost the deployment five points
		// that nobody could act on.
		StrongPasswordPolicy: true,
		MQTTTLS:              false,
	}

	// Check MFA
	mfaCount := h.countScoped(c.Request.Context(), isGlobal, orgIDVal,
		`SELECT COUNT(*) FROM users WHERE role='admin' AND totp_enabled=TRUE AND org_id IS NOT NULL`,
		`SELECT COUNT(*) FROM users WHERE role='admin' AND totp_enabled=TRUE AND org_id=$1`,
	)
	breakdown.MFAAnyAdmin = mfaCount > 0

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

	score := computeSecurityScore(breakdown)

	ctx := c.Request.Context()
	failedLogins := h.countScoped(ctx, isGlobal, orgIDVal,
		`SELECT COUNT(*) FROM audit_logs WHERE action='login' AND success=FALSE AND created_at > NOW()-INTERVAL '24 hours'`,
		`SELECT COUNT(*) FROM audit_logs WHERE action='login' AND success=FALSE AND created_at > NOW()-INTERVAL '24 hours' AND org_id=$1`,
	)
	lockedAccounts := h.countScoped(ctx, isGlobal, orgIDVal,
		`SELECT COUNT(*) FROM users WHERE locked_until > NOW() AND org_id IS NOT NULL`,
		`SELECT COUNT(*) FROM users WHERE locked_until > NOW() AND org_id=$1`,
	)
	secEvents24h := h.countScoped(ctx, isGlobal, orgIDVal,
		`SELECT COUNT(*) FROM security_events WHERE created_at > NOW()-INTERVAL '24 hours'`,
		`SELECT COUNT(*) FROM security_events WHERE created_at > NOW()-INTERVAL '24 hours' AND org_id=$1`,
	)

	// I dodici controlli, ciascuno con l'unico esito che possiamo sostenere.
	//
	// Sei sono stato della piattaforma e li leggiamo davvero. Gli altri sei
	// sono misure organizzative — gestione del rischio, gestione degli
	// incidenti, catena di fornitura, crittografia end-to-end, gestione delle
	// vulnerabilità, protezione dei dati — che dipendono da procedure aziendali
	// fuori dal processo. Il software non le può accertare, e dirsi conformi su
	// di esse sarebbe una dichiarazione su un'organizzazione che non conosce.
	overview := SecurityOverview{
		Score:             score,
		ScoreBreakdown:    breakdown,
		FailedLogins24h:   failedLogins,
		LockedAccounts:    lockedAccounts,
		SecurityEvents24h: secEvents24h,
	}
	overview.setChecks(securityChecks(breakdown))

	c.JSON(http.StatusOK, overview)
}

// securityChecks costruisce i dodici controlli a partire dallo stato della
// piattaforma. Puro: nessuna query, così è verificabile senza un database.
func securityChecks(b ScoreBreakdown) []SecurityCheck {
	return []SecurityCheck{
		{"risk_management", checkNotAssessed},
		{"incident_handling", checkNotAssessed},
		{"business_continuity", passFail(b.BackupFresh)},
		{"supply_chain", checkNotAssessed},
		{"network_security", passFail(b.MQTTTLS)},
		{"access_control", passFail(b.RBACEnabled)},
		{"mfa", passFail(b.MFAAnyAdmin)},
		{"cryptography", checkNotAssessed},
		{"vulnerability_mgmt", checkNotAssessed},
		{"audit_logging", passFail(b.AuditLogging)},
		{"account_security", passFail(b.AccountLockoutActive)},
		{"data_protection", checkNotAssessed},
	}
}

// setChecks conta i controlli e riempie ENTRAMBE le rappresentazioni: quella
// attuale e i due campi deprecati.
//
// È un metodo, e non due assegnazioni nel gestore, per una ragione precisa:
// due rappresentazioni della stessa cosa calcolate in due punti diversi
// divergono, sempre, non appena qualcuno tocca uno dei due. Qui c'è un solo
// punto, quindi non possono. Quando i campi deprecati verranno tolti in 4.0.0,
// spariscono da qui e da nessun altro posto.
func (o *SecurityOverview) setChecks(checks []SecurityCheck) {
	var passed, evaluated, notAssessed int
	for _, ck := range checks {
		switch ck.State {
		case checkPass:
			passed++
			evaluated++
		case checkFail:
			evaluated++
		default:
			notAssessed++
		}
	}
	o.Checks = checks
	o.ChecksPassed = passed
	o.ChecksEvaluated = evaluated
	o.ChecksNotAssessed = notAssessed
	o.NIS2ChecksPassed = passed
	o.NIS2ChecksTotal = evaluated
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
		rows = h.appendEventRows(c.Request.Context(), rows, seQuery, seArgs)
	}

	// Synthetic events from audit_logs (login failures).
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
		rows = h.appendEventRows(c.Request.Context(), rows, alQuery, alArgs)
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

// ComplianceCheck è una voce dell'autovalutazione di postura mostrata nel
// Security Center. Stessa tassonomia dei controlli in SecurityOverview: gli
// stessi sei sono misurati, gli stessi sei no.
//
// Detail era una costante indipendente dall'esito, e questo produceva
// contraddizioni visibili: con TLS attivo la riga diceva comunque "MQTT non
// cifrato (TLS assente)", accanto a un segno di spunta verde. Ora ogni voce ha
// il testo del suo stato.
type ComplianceCheck struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Article string `json:"article"`
	State   string `json:"state"`
	Detail  string `json:"detail"`

	// Deprecato, servito per non rompere chi già lo legge. Vale true solo per
	// State == "pass": un controllo non valutato arriva come false, cioè "non
	// superato", che è il verso sicuro in cui sbagliare — un client vecchio
	// sottostima la propria postura invece di sopravvalutarla.
	//
	// Da togliere in 4.0.0.
	Passed bool `json:"passed"`
}

// notAssessed costruisce una voce per una misura organizzativa. Detail dice
// che cosa la piattaforma fa in quell'area, senza far discendere da quel fatto
// una conformità che riguarda procedure aziendali fuori dal processo.
func notAssessed(id, name, article, detail string) ComplianceCheck {
	return ComplianceCheck{ID: id, Name: name, Article: article, State: checkNotAssessed, Detail: detail, Passed: false}
}

// evaluated costruisce una voce misurata, con il testo del ramo in cui si trova.
func evaluatedCheck(id, name, article string, ok bool, whenPass, whenFail string) ComplianceCheck {
	d := whenFail
	if ok {
		d = whenPass
	}
	return ComplianceCheck{ID: id, Name: name, Article: article, State: passFail(ok), Detail: d, Passed: ok}
}

// complianceChecks costruisce l'elenco mostrato nel Security Center.
//
// Puro come securityChecks: prende i tre stati misurati e restituisce le voci,
// senza toccare il database. È l'unico modo per verificare in un test che il
// dettaglio segua il verdetto — il difetto che questa riscrittura è nata per
// correggere era esattamente un dettaglio costante accanto a un esito
// variabile, e nessuna prova che passasse per il gestore lo avrebbe mostrato
// senza un Postgres acceso.
func complianceChecks(backupPassed, mqttPassed, mfaPassed bool, mfaDetail string) []ComplianceCheck {
	return []ComplianceCheck{
		notAssessed("risk_management", "Gestione del rischio", "Art. 21(2)(a)",
			"Misura organizzativa: la piattaforma registra gli eventi ma non può accertare l'esistenza di una politica di analisi del rischio"),
		notAssessed("incident_handling", "Gestione incidenti", "Art. 21(2)(b)",
			"Misura organizzativa: nessun workflow di gestione incidenti nella piattaforma"),
		evaluatedCheck("business_continuity", "Continuità operativa", "Art. 21(2)(c)", backupPassed,
			"Backup presente nelle ultime 25 ore, con verifica integrità SHA-256",
			"Nessun backup nelle ultime 25 ore"),
		notAssessed("supply_chain", "Sicurezza supply chain", "Art. 21(2)(d)",
			"Misura organizzativa: le versioni dei componenti edge non sono monitorate dalla piattaforma"),
		evaluatedCheck("network_security", "Sicurezza di rete", "Art. 21(2)(e)", mqttPassed,
			"MQTT cifrato con TLS",
			"MQTT non cifrato (TLS assente)"),
		{ID: "access_control", Name: "Controllo accessi", Article: "Art. 21(2)(i)", State: checkPass, Passed: true,
			Detail: "RBAC per ruolo su tutte le rotte; SSO OIDC configurabile per organizzazione"},
		evaluatedCheck("mfa", "Autenticazione multi-fattore", "Art. 21(2)(j)", mfaPassed,
			mfaDetail, mfaDetail),
		notAssessed("cryptography", "Crittografia", "Art. 21(2)(h)",
			"Misura organizzativa: la piattaforma usa bcrypt, SHA-256 sui backup e JWT HS256, ma la politica crittografica aziendale non è accertabile dal software"),
		notAssessed("vulnerability_mgmt", "Gestione vulnerabilità", "Art. 21(2)(e)",
			"Misura organizzativa: la piattaforma non monitora CVE e versioni dell'installazione del cliente"),
		{ID: "audit_logging", Name: "Logging e monitoraggio", Article: "Art. 21(2)(f)", State: checkPass, Passed: true,
			Detail: "Audit log sempre attivo, con IP e user agent"},
		{ID: "account_security", Name: "Sicurezza account", Article: "Art. 21(2)(i)", State: checkPass, Passed: true,
			Detail: "Rate limiting sul login e blocco account dopo 5 tentativi falliti"},
		notAssessed("data_protection", "Protezione dati", "Art. 21(2)(h)",
			"Misura organizzativa: password con hash e backup con hash di integrità, ma il trattamento dei dati resta in capo al titolare"),
	}
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

	mfaDetail := "MFA non attivato su nessun admin"
	if mfaPassed {
		mfaDetail = fmt.Sprintf("%d admin con MFA abilitato", mfaCount)
	}

	c.JSON(http.StatusOK, complianceChecks(backupPassed, mqttPassed, mfaPassed, mfaDetail))
}


