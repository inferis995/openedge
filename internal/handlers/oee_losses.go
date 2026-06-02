// Package handlers — OEE loss events (auto-popolato dal cron).
//
// Il loss tree (Six Big Losses ISO 22400-2) viene popolato automaticamente
// dal cron worker leggendo allarmi e finestre di manutenzione. Niente
// data entry operatore: scelta esplicita per non costringere il caporeparto
// a usare un touch screen.
//
// Mapping causa → categoria:
//   - alarm_event critical chiuso  → `breakdown`
//   - maintenance window (title contiene "setup"/"cambio"/"changeover") → `setup`
//   - maintenance window (altri)   → escluso (planned downtime, sottratto da PPT)
//
// Eventi mancanti rispetto alla roadmap "vero pro":
//   - no_run_tag (segmentazione tag_history): rimandato a future iterations
//   - production_defect (gap pieces_produced vs pieces_good): facoltativo
//
// Scope per profilo: un loss_event appartiene a un profilo se:
//   - profile.gateway_id != null E alarm.tag_id appartiene a quel gateway, oppure
//   - profile.gateway_id == null (alarm globale: tutti i profili lo vedono)
// Per maintenance: vale per tutti i profili abilitati (plant-wide).
package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// LossesHandler espone le query del loss tree, Pareto, MTBF/MTTR.
// La scrittura è interna (cron worker chiama PopulateLossEvents).
type LossesHandler struct {
	db *sql.DB
}

func NewLossesHandler(db *sql.DB) *LossesHandler {
	return &LossesHandler{db: db}
}

// LossCategoryAgg è una riga del Pareto cause: categoria + durata totale
// + numero eventi nel range richiesto.
type LossCategoryAgg struct {
	CategoryID    int     `json:"category_id"`
	Code          string  `json:"code"`
	Pillar        string  `json:"pillar"`
	DisplayLabel  string  `json:"display_label"`
	Color         string  `json:"color"`
	EventsCount   int     `json:"events_count"`
	TotalMinutes  float64 `json:"total_minutes"`
	PercentOfTotal float64 `json:"percent_of_total"`
}

// LossTree GET /api/oee/loss-tree?profile_id=X&from=Y&to=Z
//
// Ritorna le categorie ordinate per durata totale decrescente — il
// "Pareto delle cause". Frontend usa questo per il chart top-N.
func (h *LossesHandler) LossTree(c *gin.Context) {
	pid := strings.TrimSpace(c.Query("profile_id"))
	if pid == "" || pid == "0" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile_id required"})
		return
	}
	from, err := parseHistoryTime(c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from': " + err.Error()})
		return
	}
	to, err := parseHistoryTime(c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to': " + err.Error()})
		return
	}

	rows, err := h.db.Query(`
		SELECT c.id, c.code, c.loss_pillar, c.display_label, c.color,
			COUNT(e.id), COALESCE(SUM(e.duration_min), 0)
		FROM oee_loss_categories c
		LEFT JOIN oee_loss_events e ON e.category_id = c.id
			AND e.profile_id = $1
			AND e.start_at >= $2 AND e.start_at < $3
		GROUP BY c.id, c.code, c.loss_pillar, c.display_label, c.color
		ORDER BY SUM(e.duration_min) DESC NULLS LAST, c.id`,
		pid, from, to,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var aggs []LossCategoryAgg
	var totalMin float64
	for rows.Next() {
		var a LossCategoryAgg
		if err := rows.Scan(
			&a.CategoryID, &a.Code, &a.Pillar, &a.DisplayLabel, &a.Color,
			&a.EventsCount, &a.TotalMinutes,
		); err == nil {
			aggs = append(aggs, a)
			totalMin += a.TotalMinutes
		}
	}
	// Percent of total per ogni riga.
	for i := range aggs {
		if totalMin > 0 {
			aggs[i].PercentOfTotal = aggs[i].TotalMinutes / totalMin * 100
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"categories":    aggs,
		"total_minutes": totalMin,
	})
}

// ReliabilityStats è la risposta del calcolo MTBF/MTTR.
type ReliabilityStats struct {
	ProfileID       int     `json:"profile_id"`
	FromBucket      string  `json:"from"`
	ToBucket        string  `json:"to"`
	BreakdownCount  int     `json:"breakdown_count"`
	TotalRunMin     float64 `json:"total_run_min"`
	TotalRepairMin  float64 `json:"total_repair_min"`
	MTBFMinutes     float64 `json:"mtbf_minutes"`
	MTBFHours       float64 `json:"mtbf_hours"`
	MTTRMinutes     float64 `json:"mttr_minutes"`
}

// MTBFMTTR GET /api/oee/profiles/:id/reliability?from=Y&to=Z
//
// MTBF = uptime / num_breakdowns
//   uptime = planned_min(somma oee_history) − repair_min
// MTTR = sum(breakdown_duration) / num_breakdowns
//
// Solo eventi categorizzati come 'breakdown' contano (non setup, non
// minor_stops — quelli pesano in modo diverso sulla manutenzione preventiva).
func (h *LossesHandler) MTBFMTTR(c *gin.Context) {
	pid := c.Param("id")
	if pid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile id required"})
		return
	}
	from, err := parseHistoryTime(c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from'"})
		return
	}
	to, err := parseHistoryTime(c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to'"})
		return
	}

	var count int
	var totalRepair float64
	_ = h.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(duration_min), 0)
		FROM oee_loss_events e
		JOIN oee_loss_categories c ON c.id = e.category_id
		WHERE e.profile_id = $1
		  AND c.code = 'breakdown'
		  AND e.start_at >= $2 AND e.start_at < $3`,
		pid, from, to,
	).Scan(&count, &totalRepair)

	var totalPlanned float64
	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(planned_min), 0)
		FROM oee_history
		WHERE profile_id = $1
		  AND bucket_size = 'hour'
		  AND bucket_start >= $2 AND bucket_start < $3`,
		pid, from, to,
	).Scan(&totalPlanned)

	// Uptime = planned - repair. Se planned è 0 (es. cron non ha ancora
	// salvato dati), MTBF non è calcolabile.
	uptime := totalPlanned - totalRepair
	if uptime < 0 {
		uptime = 0
	}

	stats := ReliabilityStats{
		FromBucket:     from.Format(time.RFC3339),
		ToBucket:       to.Format(time.RFC3339),
		BreakdownCount: count,
		TotalRunMin:    uptime,
		TotalRepairMin: totalRepair,
	}
	if count > 0 {
		stats.MTBFMinutes = uptime / float64(count)
		stats.MTBFHours = stats.MTBFMinutes / 60
		stats.MTTRMinutes = totalRepair / float64(count)
	}
	c.JSON(http.StatusOK, stats)
}

// ── Worker logic (chiamata dal cron) ─────────────────────────────────────

// PopulateLossEvents è il worker chiamato dal cron orario. Per ogni
// profilo abilitato, registra eventi di perdita derivati da:
//   - alarm_events critical chiusi nella finestra [now-2h, now]
//   - maintenance_windows che intersecano la stessa finestra
//
// Idempotente via UNIQUE(profile_id, source, source_ref).
// Backfill: con finestra 2h coprono comodamente eventuali ritardi del cron.
func PopulateLossEvents(db *sql.DB, now time.Time) error {
	windowStart := now.Add(-2 * time.Hour)

	profiles, err := loadEnabledProfilesForLosses(db)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return nil
	}

	// Carico categorie una volta sola.
	cats, err := loadLossCategories(db)
	if err != nil {
		return err
	}
	catBreakdown := cats["breakdown"]
	catSetup := cats["setup"]
	if catBreakdown == 0 {
		return nil // tabella non seedata, niente da fare
	}

	// ── 1. Allarmi critical chiusi nella finestra ──────────────────────
	type alarmRow struct {
		id        int64
		tagID     int
		gatewayID int
		startAt   time.Time
		endAt     time.Time
		message   string
	}
	alarmRows, err := db.Query(`
		SELECT e.id, e.tag_id, COALESCE(t.gateway_id, 0),
			e.trigger_time, e.clear_time, COALESCE(e.message, '')
		FROM alarm_events e
		JOIN tags t ON t.id = e.tag_id
		WHERE LOWER(e.severity) = 'critical'
		  AND e.clear_time IS NOT NULL
		  AND e.clear_time >= $1 AND e.clear_time < $2`,
		windowStart, now,
	)
	if err != nil {
		return err
	}
	var alarms []alarmRow
	for alarmRows.Next() {
		var a alarmRow
		if err := alarmRows.Scan(&a.id, &a.tagID, &a.gatewayID, &a.startAt, &a.endAt, &a.message); err == nil {
			alarms = append(alarms, a)
		}
	}
	alarmRows.Close()

	for _, a := range alarms {
		duration := a.endAt.Sub(a.startAt).Minutes()
		if duration <= 0 {
			continue
		}
		for _, p := range profiles {
			// Scope check: se il profilo ha gateway_id, deve combaciare.
			if p.gatewayID != nil && a.gatewayID != *p.gatewayID {
				continue
			}
			if _, err := db.Exec(`
				INSERT INTO oee_loss_events
					(profile_id, category_id, start_at, end_at, duration_min, source, source_ref, notes)
				VALUES ($1, $2, $3, $4, $5, 'alarm', $6, $7)
				ON CONFLICT (profile_id, source, source_ref) DO UPDATE SET
					end_at = EXCLUDED.end_at,
					duration_min = EXCLUDED.duration_min`,
				p.id, catBreakdown, a.startAt, a.endAt, duration, a.id, a.message,
			); err != nil {
				return err
			}
		}
	}

	// ── 2. Maintenance windows che si intersecano con [windowStart, now] ─
	maintRows, err := db.Query(`
		SELECT id, title, start_at, end_at, COALESCE(reason,'')
		FROM maintenance_windows
		WHERE start_at < $2 AND end_at > $1`,
		windowStart, now,
	)
	if err != nil {
		return err
	}
	type maintRow struct {
		id      int64
		title   string
		startAt time.Time
		endAt   time.Time
		reason  string
	}
	var maints []maintRow
	for maintRows.Next() {
		var m maintRow
		if err := maintRows.Scan(&m.id, &m.title, &m.startAt, &m.endAt, &m.reason); err == nil {
			maints = append(maints, m)
		}
	}
	maintRows.Close()

	for _, m := range maints {
		titleLower := strings.ToLower(m.title)
		isSetup := strings.Contains(titleLower, "setup") ||
			strings.Contains(titleLower, "cambio") ||
			strings.Contains(titleLower, "changeover")
		if !isSetup {
			continue // pura "planned downtime" — esclusa dal PPT, no loss event
		}
		if catSetup == 0 {
			continue
		}
		duration := m.endAt.Sub(m.startAt).Minutes()
		if duration <= 0 {
			continue
		}
		for _, p := range profiles {
			if _, err := db.Exec(`
				INSERT INTO oee_loss_events
					(profile_id, category_id, start_at, end_at, duration_min, source, source_ref, notes)
				VALUES ($1, $2, $3, $4, $5, 'maintenance', $6, $7)
				ON CONFLICT (profile_id, source, source_ref) DO UPDATE SET
					end_at = EXCLUDED.end_at,
					duration_min = EXCLUDED.duration_min`,
				p.id, catSetup, m.startAt, m.endAt, duration, m.id, m.title,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadEnabledProfilesForLosses carica i profili in forma minimale per il
// worker — id + gateway_id sono sufficienti per il scoping.
type profileLite struct {
	id        int
	gatewayID *int
}

func loadEnabledProfilesForLosses(db *sql.DB) ([]profileLite, error) {
	rows, err := db.Query(`SELECT id, gateway_id FROM oee_profiles WHERE enabled = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []profileLite
	for rows.Next() {
		var p profileLite
		if err := rows.Scan(&p.id, &p.gatewayID); err == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func loadLossCategories(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query(`SELECT code, id FROM oee_loss_categories`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var code string
		var id int
		if err := rows.Scan(&code, &id); err == nil {
			out[code] = id
		}
	}
	return out, nil
}

// ListCategories GET /api/oee/loss-categories — lista per UI.
func (h *LossesHandler) ListCategories(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT id, code, loss_pillar, display_label, color
		FROM oee_loss_categories ORDER BY id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type cat struct {
		ID           int    `json:"id"`
		Code         string `json:"code"`
		Pillar       string `json:"pillar"`
		DisplayLabel string `json:"display_label"`
		Color        string `json:"color"`
	}
	out := []cat{}
	for rows.Next() {
		var c1 cat
		if err := rows.Scan(&c1.ID, &c1.Code, &c1.Pillar, &c1.DisplayLabel, &c1.Color); err == nil {
			out = append(out, c1)
		}
	}
	c.JSON(http.StatusOK, out)
}
