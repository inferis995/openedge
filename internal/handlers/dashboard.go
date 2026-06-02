// Package handlers — dashboard.
//
// Un singolo endpoint /api/dashboard/overview aggrega tutto quello che la
// pagina dashboard deve mostrare. Disegnato per essere richiamato ogni 30s
// dal frontend; ogni sezione è indipendente — un errore in una sotto-query
// non blocca le altre (ognuna torna zero values invece di propagare).
package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// DashboardHandler aggrega lo stato del sistema in una sola response.
type DashboardHandler struct {
	db        *sql.DB
	startedAt time.Time
}

// NewDashboardHandler costruisce l'handler e cattura l'uptime di processo.
func NewDashboardHandler(db *sql.DB) *DashboardHandler {
	return &DashboardHandler{db: db, startedAt: time.Now()}
}

// DashboardOverview è la response shape consumata dal frontend.
type DashboardOverview struct {
	GeneratedAt time.Time       `json:"generated_at"`
	System      SystemStatus    `json:"system"`
	Alarms      AlarmsBlock     `json:"alarms"`
	Gateways    GatewaysBlock   `json:"gateways"`
	Operations  OperationsBlock `json:"operations"`
	Activity    []ActivityEvent `json:"activity"`
	KPI         []KPIWidget     `json:"kpi"`
}

// SystemStatus risponde alla domanda "è tutto su?".
type SystemStatus struct {
	Ready        bool  `json:"ready"`
	DBOk         bool  `json:"db_ok"`
	APIUptimeSec int64 `json:"api_uptime_sec"`
}

// AlarmsBlock conta gli allarmi attivi per livello + trend ultimi 7 giorni.
// Il frontend lo usa per il widget grande in alto (status bar + card allarmi).
type AlarmsBlock struct {
	ActiveByLevel map[string]int `json:"active_by_level"`
	ActiveTotal   int            `json:"active_total"`
	Last24hFired  int            `json:"last_24h_fired"`
	Trend7d       []TrendBucket  `json:"trend_7d"`
	RecentTop5    []AlarmSummary `json:"recent_top5"`
}

// AlarmSummary è un alarm event compatto per la timeline.
type AlarmSummary struct {
	ID          int64     `json:"id"`
	Severity    string    `json:"severity"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	TriggerTime time.Time `json:"trigger_time"`
	TagAlias    string    `json:"tag_alias"`
	GatewayName string    `json:"gateway_name"`
}

// TrendBucket è un giorno della sparkline a 7 punti.
type TrendBucket struct {
	Bucket time.Time `json:"bucket"`
	Count  int       `json:"count"`
}

// GatewaysBlock è lo stato di connettività dei PLC.
type GatewaysBlock struct {
	Online      int            `json:"online"`
	Offline     int            `json:"offline"`
	Unknown     int            `json:"unknown"`
	OfflineList []GatewayBrief `json:"offline_list"`
}

// GatewayBrief è quanto basta per mostrare un gateway in lista.
type GatewayBrief struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	DriverType string `json:"driver_type"`
}

// OperationsBlock dà un colpo d'occhio su backup / notifiche / attività operatori.
type OperationsBlock struct {
	NotifEmailEnabled    bool   `json:"notif_email_enabled"`
	NotifTelegramEnabled bool   `json:"notif_telegram_enabled"`
	NotifMinSeverity     string `json:"notif_min_severity"`
	RecipeLoads24h       int    `json:"recipe_loads_24h"`
	Writes24h            int    `json:"writes_24h"`
	Logins24h            int    `json:"logins_24h"`
}

// ActivityEvent è una riga della timeline unificata (alarms+recipes+writes+logins).
type ActivityEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Title     string    `json:"title"`
	Details   string    `json:"details"`
	Severity  string    `json:"severity,omitempty"`
}

// KPIWidget è una metrica derivata mostrata come "big number + trend".
// Il design dei KPI è volutamente uniforme — l'operatore guarda il valore
// e la freccia e capisce in 1 secondo se siamo meglio o peggio.
type KPIWidget struct {
	Key       string  `json:"key"`        // identificatore stabile per il frontend
	Label     string  `json:"label"`      // etichetta human-friendly
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Trend     string  `json:"trend"`      // up | down | flat
	DeltaPct  float64 `json:"delta_pct"`  // % vs periodo precedente
	GoodWhen  string  `json:"good_when"`  // "up" o "down" — guida il colore
}

// Overview risponde al GET. Ogni blocco è popolato indipendentemente —
// un fallimento in una query NON impedisce alle altre di tornare valori.
func (h *DashboardHandler) Overview(c *gin.Context) {
	resp := DashboardOverview{
		GeneratedAt: time.Now().UTC(),
		System:      h.system(),
	}
	resp.Alarms = h.alarms()
	resp.Gateways = h.gateways()
	resp.Operations = h.operations()
	resp.Activity = h.activity()
	resp.KPI = h.kpis()
	c.JSON(http.StatusOK, resp)
}

func (h *DashboardHandler) system() SystemStatus {
	dbOk := h.db.Ping() == nil
	return SystemStatus{
		Ready:        dbOk,
		DBOk:         dbOk,
		APIUptimeSec: int64(time.Since(h.startedAt).Seconds()),
	}
}

func (h *DashboardHandler) alarms() AlarmsBlock {
	out := AlarmsBlock{
		ActiveByLevel: map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
		Trend7d:       []TrendBucket{},
		RecentTop5:    []AlarmSummary{},
	}
	// Active counts.
	rows, err := h.db.Query(`
		SELECT LOWER(severity), COUNT(*) FROM alarm_events
		WHERE status = 'ACTIVE' GROUP BY LOWER(severity)`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sev string
			var n int
			if err := rows.Scan(&sev, &n); err == nil {
				out.ActiveByLevel[sev] = n
				out.ActiveTotal += n
			}
		}
	}
	// Fired last 24h.
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM alarm_events WHERE trigger_time > NOW() - INTERVAL '24 hours'`).
		Scan(&out.Last24hFired)

	// 7d trend — one bucket per day, oldest first. Empty days are zero
	// so the sparkline doesn't have gaps the frontend has to fill.
	now := time.Now().UTC()
	rows2, err := h.db.Query(`
		SELECT date_trunc('day', trigger_time) AS bucket, COUNT(*)
		FROM alarm_events
		WHERE trigger_time > NOW() - INTERVAL '7 days'
		GROUP BY bucket ORDER BY bucket`)
	if err == nil {
		defer rows2.Close()
		seen := map[time.Time]int{}
		for rows2.Next() {
			var t time.Time
			var n int
			if err := rows2.Scan(&t, &n); err == nil {
				seen[t.UTC()] = n
			}
		}
		// Backfill missing days with zero to make the sparkline always 7 long.
		for i := 6; i >= 0; i-- {
			d := time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, time.UTC)
			out.Trend7d = append(out.Trend7d, TrendBucket{Bucket: d, Count: seen[d]})
		}
	}

	// Recent top 5 with tag + gateway names — joins are cheap on small tables.
	rows3, err := h.db.Query(`
		SELECT e.id, e.severity, e.status, COALESCE(e.message,''), e.trigger_time,
		       COALESCE(t.alias,''), COALESCE(g.name,'')
		FROM alarm_events e
		LEFT JOIN tags t ON t.id = e.tag_id
		LEFT JOIN gateways g ON g.id = t.gateway_id
		ORDER BY e.trigger_time DESC LIMIT 5`)
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var a AlarmSummary
			if err := rows3.Scan(&a.ID, &a.Severity, &a.Status, &a.Message, &a.TriggerTime, &a.TagAlias, &a.GatewayName); err == nil {
				out.RecentTop5 = append(out.RecentTop5, a)
			}
		}
	}
	return out
}

func (h *DashboardHandler) gateways() GatewaysBlock {
	out := GatewaysBlock{OfflineList: []GatewayBrief{}}
	// connection_status non è in DB — viene da Redis. Per evitare la
	// dipendenza Redis qui, contiamo i gateway totali e li mostriamo come
	// "Unknown" finché un altro endpoint live (es. /api/gateways) aggiorna
	// il widget. Il frontend già fa quella chiamata; usiamo questo blocco
	// per il conteggio TOTALE che è quello che pilota la status bar.
	var total int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM gateways WHERE enabled = true`).Scan(&total)
	out.Unknown = total
	return out
}

func (h *DashboardHandler) operations() OperationsBlock {
	out := OperationsBlock{
		NotifMinSeverity: "medium",
	}
	// Settings notifiche.
	get := func(key string) string {
		var v string
		_ = h.db.QueryRow(`SELECT value FROM global_settings WHERE key = $1`, key).Scan(&v)
		return v
	}
	out.NotifEmailEnabled = strings.EqualFold(get("notif_email_enabled"), "true")
	out.NotifTelegramEnabled = strings.EqualFold(get("notif_telegram_enabled"), "true")
	if s := get("notif_min_severity"); s != "" {
		out.NotifMinSeverity = s
	}

	// Conteggio attività operatori ultime 24h — recipe_runs, audit_logs(tag.write), audit_logs(login).
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM recipe_runs WHERE triggered_at > NOW() - INTERVAL '24 hours'`).Scan(&out.RecipeLoads24h)
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'tag.write' AND created_at > NOW() - INTERVAL '24 hours'`).Scan(&out.Writes24h)
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'login' AND success = true AND created_at > NOW() - INTERVAL '24 hours'`).Scan(&out.Logins24h)
	return out
}

func (h *DashboardHandler) activity() []ActivityEvent {
	// Timeline unificata: ultimi 5 di ogni tipo, fusi e riordinati per timestamp.
	events := []ActivityEvent{}

	// Alarms (recent).
	rows, err := h.db.Query(`
		SELECT trigger_time, severity, COALESCE(message,''), COALESCE((
			SELECT alias FROM tags WHERE id = tag_id), '')
		FROM alarm_events ORDER BY trigger_time DESC LIMIT 5`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t time.Time
			var sev, msg, alias string
			if err := rows.Scan(&t, &sev, &msg, &alias); err == nil {
				title := "Allarme " + alias
				if msg != "" {
					title = msg
				}
				events = append(events, ActivityEvent{
					Type: "alarm", Timestamp: t, Title: title,
					Details: alias, Severity: sev,
				})
			}
		}
	}

	// Recipe loads.
	rows2, err := h.db.Query(`
		SELECT triggered_at, COALESCE(triggered_username,''),
		       COALESCE((SELECT name FROM recipes WHERE id = recipe_id), '?'), status
		FROM recipe_runs ORDER BY triggered_at DESC LIMIT 5`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var t time.Time
			var who, name, status string
			if err := rows2.Scan(&t, &who, &name, &status); err == nil {
				events = append(events, ActivityEvent{
					Type: "recipe", Timestamp: t,
					Title:   "Ricetta " + name,
					Details: who + " — " + status,
				})
			}
		}
	}

	// Tag writes (audit).
	rows3, err := h.db.Query(`
		SELECT created_at, COALESCE(username,''), COALESCE(details::text,'')
		FROM audit_logs WHERE action = 'tag.write' AND success = true
		ORDER BY created_at DESC LIMIT 5`)
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var t time.Time
			var who, details string
			if err := rows3.Scan(&t, &who, &details); err == nil {
				events = append(events, ActivityEvent{
					Type: "write", Timestamp: t,
					Title:   "Write PLC",
					Details: who,
				})
			}
		}
	}

	// Logins.
	rows4, err := h.db.Query(`
		SELECT created_at, COALESCE(username,''), COALESCE(ip_address,'')
		FROM audit_logs WHERE action = 'login' AND success = true
		ORDER BY created_at DESC LIMIT 5`)
	if err == nil {
		defer rows4.Close()
		for rows4.Next() {
			var t time.Time
			var who, ip string
			if err := rows4.Scan(&t, &who, &ip); err == nil {
				events = append(events, ActivityEvent{
					Type: "login", Timestamp: t,
					Title:   "Login " + who,
					Details: ip,
				})
			}
		}
	}

	// Ordina desc per timestamp e taglia a 12.
	sortByTimestampDesc(events)
	if len(events) > 12 {
		events = events[:12]
	}
	return events
}

// sortByTimestampDesc fa insertion sort — la lista è ≤20 elementi quindi
// non vale la pena importare "sort" solo per questa chiamata.
func sortByTimestampDesc(events []ActivityEvent) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j-1].Timestamp.Before(events[j].Timestamp); j-- {
			events[j-1], events[j] = events[j], events[j-1]
		}
	}
}

func (h *DashboardHandler) kpis() []KPIWidget {
	out := []KPIWidget{}

	// KPI #1 — Allarmi/giorno (media ultimi 7gg vs 7gg precedenti).
	currentAvg := h.alarmAvgPerDay(7, 0)
	prevAvg := h.alarmAvgPerDay(7, 7)
	out = append(out, kpi("alarms_per_day", "Allarmi al giorno", currentAvg, "/g", prevAvg, "down"))

	// KPI #2 — Allarmi critical aperti (snapshot, no delta).
	var crit int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM alarm_events WHERE status='ACTIVE' AND LOWER(severity)='critical'`).Scan(&crit)
	out = append(out, KPIWidget{
		Key: "open_critical", Label: "Critical attivi", Value: float64(crit),
		Unit: "", Trend: "flat", DeltaPct: 0, GoodWhen: "down",
	})

	// KPI #3 — Write PLC nelle 24h vs 24h precedenti.
	cur24w := h.countSince(`audit_logs`, "action='tag.write' AND success=true", "24 hours", 0)
	prev24w := h.countSince(`audit_logs`, "action='tag.write' AND success=true", "24 hours", 24)
	out = append(out, kpi("writes_24h", "Write PLC (24h)", float64(cur24w), "", float64(prev24w), "up"))

	// KPI #4 — Caricamenti ricette nelle 24h.
	cur24r := h.countSince(`recipe_runs`, "1=1", "24 hours", 0)
	prev24r := h.countSince(`recipe_runs`, "1=1", "24 hours", 24)
	out = append(out, kpi("recipe_loads_24h", "Ricette caricate (24h)", float64(cur24r), "", float64(prev24r), "up"))

	// KPI #5 — Login operatori nelle 24h.
	cur24l := h.countSince(`audit_logs`, "action='login' AND success=true", "24 hours", 0)
	prev24l := h.countSince(`audit_logs`, "action='login' AND success=true", "24 hours", 24)
	out = append(out, kpi("logins_24h", "Login (24h)", float64(cur24l), "", float64(prev24l), "flat"))

	// KPI #6 — Tag con quality BAD nell'ultima ora.
	var bad int
	_ = h.db.QueryRow(`
		SELECT COUNT(DISTINCT tag_id) FROM tag_history
		WHERE time > NOW() - INTERVAL '1 hour' AND quality > 0`).Scan(&bad)
	out = append(out, KPIWidget{
		Key: "bad_quality_1h", Label: "Tag in errore (1h)", Value: float64(bad),
		Unit: "", Trend: "flat", GoodWhen: "down",
	})

	return out
}

// alarmAvgPerDay calcola la media giornaliera nelle ultime `windowDays`
// giorni, traslata indietro di `offsetDays` (per il confronto periodo
// precedente). Restituisce 0 se non ci sono dati — l'UI mostra "—".
func (h *DashboardHandler) alarmAvgPerDay(windowDays, offsetDays int) float64 {
	var n int
	q := `SELECT COUNT(*) FROM alarm_events
	      WHERE trigger_time > NOW() - INTERVAL '` +
		itoaDash(windowDays+offsetDays) + ` days'
	      AND trigger_time <= NOW() - INTERVAL '` +
		itoaDash(offsetDays) + ` days'`
	_ = h.db.QueryRow(q).Scan(&n)
	if windowDays == 0 {
		return 0
	}
	return float64(n) / float64(windowDays)
}

// countSince conta righe in `table` che soddisfano `where`, nella finestra
// `interval` traslata di `offsetHours` indietro.
func (h *DashboardHandler) countSince(table, where, interval string, offsetHours int) int {
	var n int
	q := `SELECT COUNT(*) FROM ` + table + `
	      WHERE ` + where + ` AND ` + timestampCol(table) + ` > NOW() - INTERVAL '` + itoaDash(offsetHours) + ` hours' - INTERVAL '` + interval + `'
	      AND ` + timestampCol(table) + ` <= NOW() - INTERVAL '` + itoaDash(offsetHours) + ` hours'`
	_ = h.db.QueryRow(q).Scan(&n)
	return n
}

// timestampCol restituisce il nome della colonna timestamp per le tabelle
// che la dashboard interroga — i nomi differiscono tra audit_logs e recipe_runs.
func timestampCol(table string) string {
	switch table {
	case "recipe_runs":
		return "triggered_at"
	case "audit_logs":
		return "created_at"
	}
	return "created_at"
}

// itoa minimale per evitare l'import di "strconv" per uno scopo banale.
func itoaDash(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// kpi costruisce un KPIWidget calcolando trend + delta% rispetto al periodo
// precedente. Trend "flat" quando il delta è < 2% (rumore).
func kpi(key, label string, current float64, unit string, prev float64, goodWhen string) KPIWidget {
	w := KPIWidget{Key: key, Label: label, Value: current, Unit: unit, GoodWhen: goodWhen, Trend: "flat"}
	if prev == 0 {
		if current > 0 {
			w.Trend = "up"
			w.DeltaPct = 100
		}
		return w
	}
	delta := (current - prev) / prev * 100
	w.DeltaPct = delta
	switch {
	case delta > 2:
		w.Trend = "up"
	case delta < -2:
		w.Trend = "down"
	}
	return w
}
