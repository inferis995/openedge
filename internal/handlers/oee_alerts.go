// Package handlers — OEE alert rules.
//
// Permette all'admin di definire regole "OEE Linea A < 70% per 60 min →
// avviso warning via email/Telegram". Valutate dal cron worker ogni 5
// minuti su oee_history (bucket=hour).
//
// Anti-spam: una regola scatta una sola volta finché resta violata. Solo
// quando la condizione "si ripiega" (last_state diventa 'normal'), un
// nuovo trigger può fare scattare un altro alert.
//
// La notifica usa il Dispatcher esistente (internal/notifications/notifier.go)
// — la regola sintetizza un Event con Severity + Description e lo passa al
// dispatcher, che applica i filtri solo-sopra-soglia / rate limit / fuori
// maintenance. Niente nuovo channel da implementare.
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
	"github.com/ralph/industrial-edge-middleware/internal/notifications"
)

// AlertsHandler espone CRUD regole + evaluator (chiamato dal cron).
type AlertsHandler struct {
	db         *sql.DB
	dispatcher *notifications.Dispatcher
}

func NewAlertsHandler(db *sql.DB, dispatcher *notifications.Dispatcher) *AlertsHandler {
	return &AlertsHandler{db: db, dispatcher: dispatcher}
}

// AlertRule è la row della tabella oee_alert_rules.
type AlertRule struct {
	ID               int        `json:"id"`
	OrgID            *int       `json:"org_id,omitempty"`
	ProfileID        *int       `json:"profile_id,omitempty"`
	Name             string     `json:"name"`
	Metric           string     `json:"metric"`
	Op               string     `json:"op"`
	Threshold        float64    `json:"threshold"`
	SustainedMinutes int        `json:"sustained_minutes"`
	Severity         string     `json:"severity"`
	Enabled          bool       `json:"enabled"`
	LastNotifiedAt   *time.Time `json:"last_notified_at,omitempty"`
	LastState        string     `json:"last_state"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// AlertRuleRequest è il body di POST/PUT.
type AlertRuleRequest struct {
	ProfileID        *int    `json:"profile_id,omitempty"`
	Name             string  `json:"name" binding:"required"`
	Metric           string  `json:"metric" binding:"required"`
	Op               string  `json:"op" binding:"required"`
	Threshold        float64 `json:"threshold"`
	SustainedMinutes int     `json:"sustained_minutes"`
	Severity         string  `json:"severity" binding:"required"`
	Enabled          *bool   `json:"enabled,omitempty"`
}

const alertRuleColumns = `id, org_id, profile_id, name, metric, op, threshold, sustained_minutes,
	severity, enabled, last_notified_at, last_state, created_at, updated_at`

func scanAlertRule(sc interface{ Scan(...interface{}) error }) (AlertRule, error) {
	var r AlertRule
	err := sc.Scan(
		&r.ID, &r.OrgID, &r.ProfileID, &r.Name, &r.Metric, &r.Op, &r.Threshold,
		&r.SustainedMinutes, &r.Severity, &r.Enabled,
		&r.LastNotifiedAt, &r.LastState, &r.CreatedAt, &r.UpdatedAt,
	)
	return r, err
}

// ListAlertRules GET /api/oee/alert-rules — the caller's organization's
// rules (every organization's for the global administrator with none
// selected). The list had no organization: every admin saw, and could
// change, every organization's rules.
func (h *AlertsHandler) List(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT `+alertRuleColumns+`
		FROM oee_alert_rules
		WHERE ($1::int IS NULL OR org_id = $1)
		ORDER BY profile_id NULLS FIRST, id`, oeeOrg(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		if r, err := scanAlertRule(rows); err == nil {
			out = append(out, r)
		}
	}
	c.JSON(http.StatusOK, out)
}

// ruleOrg is the organization a rule on profileID belongs to: the profile's,
// which must be visible to the caller, or for a factory rule (no profile)
// the caller's own. A global administrator with no organization selected
// cannot make a factory rule: it would have no rollup to watch. On false
// the response is written.
func (h *AlertsHandler) ruleOrg(c *gin.Context, profileID *int) (int, bool) {
	if profileID != nil {
		var org int
		if err := h.db.QueryRowContext(c.Request.Context(),
			`SELECT org_id FROM oee_profiles WHERE id = $1 AND ($2::int IS NULL OR org_id = $2)`,
			*profileID, middleware.GetOrgFilterForQuery(c)).Scan(&org); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
			return 0, false
		}
		return org, true
	}
	org := oeeOrg(c)
	if org == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "organization context required for a factory rule"})
		return 0, false
	}
	return *org, true
}

// CreateAlertRule POST /api/oee/alert-rules
func (h *AlertsHandler) Create(c *gin.Context) {
	var req AlertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateAlertRule(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SustainedMinutes < 60 {
		req.SustainedMinutes = 60
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	org, ok := h.ruleOrg(c, req.ProfileID)
	if !ok {
		return
	}
	var id int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO oee_alert_rules
			(org_id, profile_id, name, metric, op, threshold, sustained_minutes, severity, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		org, req.ProfileID, req.Name, req.Metric, req.Op, req.Threshold,
		req.SustainedMinutes, req.Severity, enabled,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rule, _ := h.loadRule(c, id, nil)
	c.JSON(http.StatusCreated, rule)
}

// UpdateAlertRule PUT /api/oee/alert-rules/:id — only a rule of the
// caller's organization, and only onto a profile the caller can see.
func (h *AlertsHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req AlertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateAlertRule(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SustainedMinutes < 60 {
		req.SustainedMinutes = 60
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	callerOrg := oeeOrg(c)
	if _, err = h.loadRule(c, id, callerOrg); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	org, ok := h.ruleOrg(c, req.ProfileID)
	if !ok {
		return
	}
	res, err := h.db.ExecContext(c.Request.Context(), `
		UPDATE oee_alert_rules SET
			org_id=$2, profile_id=$3, name=$4, metric=$5, op=$6, threshold=$7,
			sustained_minutes=$8, severity=$9, enabled=$10, updated_at=NOW()
		WHERE id=$1 AND ($11::int IS NULL OR org_id=$11)`,
		id, org, req.ProfileID, req.Name, req.Metric, req.Op, req.Threshold,
		req.SustainedMinutes, req.Severity, enabled, callerOrg,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	rule, err := h.loadRule(c, id, nil)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, rule)
}

// DeleteAlertRule DELETE /api/oee/alert-rules/:id — a rule of another
// organization is not found (it was deleted, whoever it belonged to).
func (h *AlertsHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	res, err := h.db.ExecContext(c.Request.Context(),
		`DELETE FROM oee_alert_rules WHERE id=$1 AND ($2::int IS NULL OR org_id=$2)`, id, oeeOrg(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// loadRule reads a rule of org (nil: of any organization).
func (h *AlertsHandler) loadRule(c *gin.Context, id int, org *int) (AlertRule, error) {
	return scanAlertRule(h.db.QueryRowContext(c.Request.Context(), `
		SELECT `+alertRuleColumns+`
		FROM oee_alert_rules WHERE id = $1 AND ($2::int IS NULL OR org_id = $2)`, id, org))
}

func validateAlertRule(r AlertRuleRequest) error {
	switch r.Metric {
	case "oee", "availability", "performance", "quality":
	default:
		return errMsg("metric must be oee|availability|performance|quality")
	}
	if r.Op != "<" && r.Op != ">" {
		return errMsg("op must be < or >")
	}
	if r.Threshold < 0 || r.Threshold > 100 {
		return errMsg("threshold must be 0-100")
	}
	switch r.Severity {
	case "info", "warning", "critical":
	default:
		return errMsg("severity must be info|warning|critical")
	}
	return nil
}

// ── Evaluator (chiamato dal cron ogni 5 minuti) ─────────────────────────

// EvaluateAlertRules è il worker che valuta tutte le regole abilitate
// contro oee_history. Per ogni regola:
//  1. Determina N = ceil(sustained_minutes / 60) ore da esaminare
//  2. Legge le ultime N righe di oee_history per profile_id (o NULL=rollup)
//  3. Se TUTTE violano la condizione → "violating"
//  4. Se "violating" E (last_state != 'violating' OR last_notified > 2h fa)
//     → invia notifica + aggiorna stato
//  5. Se "normal" E last_state == 'violating' → registra clear (no notif)
//
// Anti-spam: ogni regola può notificare al massimo ogni 2h finché la
// condizione resta violata (in pratica: una sola notifica quando si
// entra in violazione, poi nulla finché non rientra).
func EvaluateAlertRules(db *sql.DB, dispatcher *notifications.Dispatcher, now time.Time) error {
	if dispatcher == nil {
		return nil
	}
	ctx := context.Background()
	// A rule is its organization's: a factory rule was evaluated on the
	// rollup of every organization's lines and notified with no
	// organization. A rule on a profile whose org_id is not set yet takes
	// the profile's.
	rows, err := db.QueryContext(ctx, `
		SELECT r.id, COALESCE(r.org_id, p.org_id), r.profile_id, r.name, r.metric, r.op,
			r.threshold, r.sustained_minutes, r.severity, r.last_notified_at, r.last_state
		FROM oee_alert_rules r
		LEFT JOIN oee_profiles p ON p.id = r.profile_id
		WHERE r.enabled = true`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type ruleEval struct {
		id, sustainedMin                      int
		orgID, profileID                      *int
		name, metric, op, severity, lastState string
		threshold                             float64
		lastNotified                          *time.Time
	}
	var rules []ruleEval
	for rows.Next() {
		var r ruleEval
		if err := rows.Scan(
			&r.id, &r.orgID, &r.profileID, &r.name, &r.metric, &r.op, &r.threshold,
			&r.sustainedMin, &r.severity, &r.lastNotified, &r.lastState,
		); err == nil {
			rules = append(rules, r)
		}
	}
	rows.Close()

	for _, r := range rules {
		nHours := (r.sustainedMin + 59) / 60 // ceil
		if nHours < 1 {
			nHours = 1
		}

		var dbRows *sql.Rows
		var qErr error
		if r.profileID == nil {
			// The rule's organization's rollup. A factory rule of no
			// organization (left from before, on an installation with
			// several) reads the platform's legacy rollup.
			dbRows, qErr = db.QueryContext(ctx, `
				SELECT oee, availability, performance, quality
				FROM oee_history
				WHERE profile_id IS NULL AND org_id IS NOT DISTINCT FROM $1 AND bucket_size='hour'
				ORDER BY bucket_start DESC LIMIT $2`,
				r.orgID, nHours,
			)
		} else {
			dbRows, qErr = db.QueryContext(ctx, `
				SELECT oee, availability, performance, quality
				FROM oee_history WHERE profile_id = $1 AND bucket_size='hour'
				ORDER BY bucket_start DESC LIMIT $2`,
				*r.profileID, nHours,
			)
		}
		if qErr != nil {
			continue
		}

		violations := 0
		samples := 0
		var lastValue float64
		for dbRows.Next() {
			var o, a, p, q float64
			if err := dbRows.Scan(&o, &a, &p, &q); err != nil {
				continue
			}
			samples++
			v := pickMetric(o, a, p, q, r.metric)
			lastValue = v
			if evalCondition(v, r.op, r.threshold) {
				violations++
			}
		}
		dbRows.Close()

		// Necessari N campioni TUTTI in violazione perché la regola scatti.
		violating := samples >= nHours && violations == samples
		if violating {
			// Anti-spam: nota già notificata negli ultimi 2h → skip
			if r.lastNotified != nil && now.Sub(*r.lastNotified) < 2*time.Hour {
				continue
			}
			// Dispatch
			profileLabel := "Fabbrica (rollup)"
			if r.profileID != nil {
				_ = db.QueryRowContext(ctx, `SELECT name FROM oee_profiles WHERE id=$1`,
					*r.profileID).Scan(&profileLabel)
			}
			desc := fmt.Sprintf("Regola \"%s\": %s di %s %s %.1f%% per %d min (valore attuale: %.1f%%)",
				r.name, r.metric, profileLabel, r.op, r.threshold, r.sustainedMin, lastValue)
			dispatcher.Dispatch(notifications.Event{
				AlarmID:     0, // synthetic, no row in alarm_events
				TagAlias:    "OEE/" + r.metric,
				Severity:    r.severity,
				Status:      "ACTIVE",
				Threshold:   r.threshold,
				Value:       lastValue,
				Description: desc,
				OccurredAt:  now,
				OrgID:       orgOrZero(r.orgID),
			})
			_, _ = db.ExecContext(ctx, `UPDATE oee_alert_rules
				SET last_notified_at = $2, last_state = 'violating', updated_at = NOW()
				WHERE id = $1`, r.id, now)
		} else if r.lastState == "violating" {
			// Rientro: passa a normal, niente notifica di "cleared"
			_, _ = db.ExecContext(ctx, `UPDATE oee_alert_rules
				SET last_state = 'normal', updated_at = NOW()
				WHERE id = $1`, r.id)
		}
	}
	return nil
}

// orgOrZero is the notification's organization: 0 is the platform's.
func orgOrZero(org *int) int {
	if org == nil {
		return 0
	}
	return *org
}

func pickMetric(o, a, p, q float64, name string) float64 {
	switch name {
	case "availability":
		return a
	case "performance":
		return p
	case "quality":
		return q
	default:
		return o
	}
}

func evalCondition(v float64, op string, threshold float64) bool {
	if op == "<" {
		return v < threshold
	}
	return v > threshold
}
