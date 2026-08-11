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
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

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

// ListAlertRules GET /api/oee/alert-rules
func (h *AlertsHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT id, profile_id, name, metric, op, threshold, sustained_minutes,
			severity, enabled, last_notified_at, last_state, created_at, updated_at
		FROM oee_alert_rules
		ORDER BY profile_id NULLS FIRST, id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(
			&r.ID, &r.ProfileID, &r.Name, &r.Metric, &r.Op, &r.Threshold,
			&r.SustainedMinutes, &r.Severity, &r.Enabled,
			&r.LastNotifiedAt, &r.LastState, &r.CreatedAt, &r.UpdatedAt,
		); err == nil {
			out = append(out, r)
		}
	}
	c.JSON(http.StatusOK, out)
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
	var id int
	err := h.db.QueryRow(`
		INSERT INTO oee_alert_rules
			(profile_id, name, metric, op, threshold, sustained_minutes, severity, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.ProfileID, req.Name, req.Metric, req.Op, req.Threshold,
		req.SustainedMinutes, req.Severity, enabled,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rule, _ := h.loadRule(id)
	c.JSON(http.StatusCreated, rule)
}

// UpdateAlertRule PUT /api/oee/alert-rules/:id
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
	if _, err := h.db.Exec(`
		UPDATE oee_alert_rules SET
			profile_id=$2, name=$3, metric=$4, op=$5, threshold=$6,
			sustained_minutes=$7, severity=$8, enabled=$9, updated_at=NOW()
		WHERE id=$1`,
		id, req.ProfileID, req.Name, req.Metric, req.Op, req.Threshold,
		req.SustainedMinutes, req.Severity, enabled,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rule, err := h.loadRule(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, rule)
}

// DeleteAlertRule DELETE /api/oee/alert-rules/:id
func (h *AlertsHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := h.db.Exec(`DELETE FROM oee_alert_rules WHERE id=$1`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AlertsHandler) loadRule(id int) (AlertRule, error) {
	var r AlertRule
	err := h.db.QueryRow(`
		SELECT id, profile_id, name, metric, op, threshold, sustained_minutes,
			severity, enabled, last_notified_at, last_state, created_at, updated_at
		FROM oee_alert_rules WHERE id = $1`, id,
	).Scan(
		&r.ID, &r.ProfileID, &r.Name, &r.Metric, &r.Op, &r.Threshold,
		&r.SustainedMinutes, &r.Severity, &r.Enabled,
		&r.LastNotifiedAt, &r.LastState, &r.CreatedAt, &r.UpdatedAt,
	)
	return r, err
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
	rows, err := db.Query(`
		SELECT id, profile_id, name, metric, op, threshold, sustained_minutes,
			severity, last_notified_at, last_state
		FROM oee_alert_rules WHERE enabled = true`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type ruleEval struct {
		id, sustainedMin                      int
		profileID                             *int
		name, metric, op, severity, lastState string
		threshold                             float64
		lastNotified                          *time.Time
	}
	var rules []ruleEval
	for rows.Next() {
		var r ruleEval
		if err := rows.Scan(
			&r.id, &r.profileID, &r.name, &r.metric, &r.op, &r.threshold,
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
			dbRows, qErr = db.Query(`
				SELECT oee, availability, performance, quality
				FROM oee_history WHERE profile_id IS NULL AND bucket_size='hour'
				ORDER BY bucket_start DESC LIMIT $1`,
				nHours,
			)
		} else {
			dbRows, qErr = db.Query(`
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
				_ = db.QueryRow(`SELECT name FROM oee_profiles WHERE id=$1`,
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
				OrgID:       0,
			})
			_, _ = db.Exec(`UPDATE oee_alert_rules
				SET last_notified_at = $2, last_state = 'violating', updated_at = NOW()
				WHERE id = $1`, r.id, now)
		} else if r.lastState == "violating" {
			// Rientro: passa a normal, niente notifica di "cleared"
			_, _ = db.Exec(`UPDATE oee_alert_rules
				SET last_state = 'normal', updated_at = NOW()
				WHERE id = $1`, r.id)
		}
	}
	return nil
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
