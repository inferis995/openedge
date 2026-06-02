// Package handlers — OEE (Overall Equipment Effectiveness).
//
// OEE = Availability × Performance × Quality
//
// Standard industriale ISO 22400. La sfida di renderlo "lampante ma
// direttamente funzionante" è che il calcolo "vero" richiede tag di
// produzione configurati (pezzi prodotti, pezzi buoni, cicli, target
// rate). Senza quelli si potrebbe solo mostrare zero.
//
// Approccio: ogni componente ha un FALLBACK euristico calcolato da
// dati che esistono SEMPRE (alarm_events, gateway_health, tag_history),
// così l'OEE è sempre visibile. Quando l'admin configura i tag reali
// via OEE settings page, i fallback vengono sostituiti dai valori veri.
//
// Fallback (modalità "out-of-the-box"):
//   - Availability = 1 - (durata totale critical attivi / finestra)
//   - Performance  = % tag con quality=0 nelle ultime N letture
//                    (proxy: se i sensori dicono dati buoni, l'impianto
//                    sta producendo regolarmente)
//   - Quality      = 1 - (campioni bad-quality / campioni totali)
//
// Modalità "tag-driven" (settings configurate):
//   - oee_run_time_tag      → tag che indica "macchina in marcia" (BOOL)
//   - oee_produced_tag      → contatore pezzi prodotti
//   - oee_good_tag          → contatore pezzi buoni
//   - oee_target_pph        → target pezzi/ora (numero)
//   - oee_planned_time_min  → tempo pianificato della finestra (default = finestra)
//
// L'OEE è ricalcolato a ogni /api/dashboard/overview (30s di refresh).
package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// OEEHandler espone GET /api/oee (snapshot completo) + GET /api/oee/history
// per il trend giornaliero usato dalla card lampante.
type OEEHandler struct {
	db *sql.DB
}

func NewOEEHandler(db *sql.DB) *OEEHandler {
	return &OEEHandler{db: db}
}

// OEESnapshot è la response del calcolo OEE attuale. Tre componenti
// percentuali (0-100) + il prodotto. Ogni componente porta una nota
// "source" che spiega se è calcolato da fallback o da tag reale —
// così la card UI può mostrare un piccolo badge "tag" o "auto" per
// trasparenza.
type OEESnapshot struct {
	OEE          float64 `json:"oee"`
	Availability float64 `json:"availability"`
	Performance  float64 `json:"performance"`
	Quality      float64 `json:"quality"`

	// Sources: "fallback" o "tag" — la UI rende un badge.
	AvailabilitySource string `json:"availability_source"`
	PerformanceSource  string `json:"performance_source"`
	QualitySource      string `json:"quality_source"`

	// Finestra in cui è stato calcolato l'OEE (per la label "OEE 8h"
	// in dashboard).
	WindowMinutes int `json:"window_minutes"`

	// Target: se settato (oee_target), la UI colora l'OEE in base al
	// rispetto della soglia.
	Target *float64 `json:"target,omitempty"`

	// Dettagli numerici utili per la diagnostica + per il KPI breakdown.
	CriticalDowntimeMin float64 `json:"critical_downtime_min"`
	PiecesProduced      float64 `json:"pieces_produced,omitempty"`
	PiecesGood          float64 `json:"pieces_good,omitempty"`
	TargetPiecesPerHour float64 `json:"target_pieces_per_hour,omitempty"`
}

// Snapshot calcola e ritorna l'OEE corrente. Usato direttamente dal
// frontend per la card e indirettamente dal DashboardHandler.
func (h *OEEHandler) Snapshot(c *gin.Context) {
	c.JSON(http.StatusOK, h.compute())
}

// HistoryPoint è un campione del trend OEE — usato dalla mini-sparkline
// in dashboard "ultime 7g OEE".
type OEEHistoryPoint struct {
	Bucket time.Time `json:"bucket"`
	OEE    float64   `json:"oee"`
}

// TagTestResult è il "lo provi prima di salvare" della config OEE:
// dato un tag id + un ruolo ("running" o "counter"), il backend legge
// gli ultimi N campioni e dice all'admin "sì, questo tag funziona per
// quel ruolo" o "no, ecco perché". Niente più "configuro a caso e
// guardo se la dashboard mostra numeri sensati 8 ore dopo".
type TagTestResult struct {
	TagID        int     `json:"tag_id"`
	Alias        string  `json:"alias"`
	Code         string  `json:"code"`
	DataType     string  `json:"data_type"`
	SamplesCount int     `json:"samples_count"`
	CurrentValue float64 `json:"current_value"`
	FirstValue   float64 `json:"first_value"`
	LastValue    float64 `json:"last_value"`
	MinValue     float64 `json:"min_value"`
	MaxValue     float64 `json:"max_value"`
	Delta        float64 `json:"delta"`
	// IsMonotonic: per ruolo "counter" — il valore non decresce mai
	// nei campioni esaminati. False = il tag fa rollover o non è un
	// counter (es. è già un rate).
	IsMonotonic bool `json:"is_monotonic"`
	// IsBoolish: per ruolo "running" — il tag oscilla tra 0 e qualcosa
	// di non-zero. True = ok come BOOL. False = è un analogico continuo.
	IsBoolish bool `json:"is_boolish"`
	// Warnings: avvisi non bloccanti (es. "nessun campione nelle ultime 24h"
	// o "valore costante da 1h: macchina ferma?")
	Warnings []string `json:"warnings"`
	// Ok: verdetto sintetico "questo tag va bene per il ruolo richiesto".
	Ok bool `json:"ok"`
}

// TestTag GET /api/oee/test-tag/:id?role=running|counter — verdetto
// "questo tag funziona per il ruolo?" usato dal wizard OEE per dare
// feedback verde/giallo/rosso prima di salvare la config.
func (h *OEEHandler) TestTag(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tag id"})
		return
	}
	role := c.Query("role")
	if role != "running" && role != "counter" {
		role = "counter"
	}

	out := TagTestResult{TagID: id, Warnings: []string{}}

	// Metadati tag
	var alias sql.NullString
	if err := h.db.QueryRow(`
		SELECT COALESCE(alias,''), code, data_type FROM tags WHERE id = $1`,
		id,
	).Scan(&alias, &out.Code, &out.DataType); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
		return
	}
	out.Alias = alias.String

	// Ultimi N campioni (ordine crescente per check monotonic).
	rows, err := h.db.Query(`
		SELECT value FROM tag_history WHERE tag_id = $1
		ORDER BY time DESC LIMIT 50`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tag_history query failed"})
		return
	}
	defer rows.Close()

	var vals []float64
	for rows.Next() {
		var v float64
		if rows.Scan(&v) == nil {
			vals = append(vals, v)
		}
	}
	out.SamplesCount = len(vals)
	if len(vals) == 0 {
		out.Warnings = append(out.Warnings, "Nessun campione nelle ultime ore — il tag riceve dati?")
		out.Ok = false
		c.JSON(http.StatusOK, out)
		return
	}

	// vals[0] è il più recente; rovesciamo per analisi cronologica.
	for i, j := 0, len(vals)-1; i < j; i, j = i+1, j-1 {
		vals[i], vals[j] = vals[j], vals[i]
	}

	out.FirstValue = vals[0]
	out.LastValue = vals[len(vals)-1]
	out.CurrentValue = out.LastValue
	out.Delta = out.LastValue - out.FirstValue

	out.MinValue = vals[0]
	out.MaxValue = vals[0]
	monotonic := true
	zeros, nonzeros := 0, 0
	for i, v := range vals {
		if v < out.MinValue {
			out.MinValue = v
		}
		if v > out.MaxValue {
			out.MaxValue = v
		}
		if i > 0 && v < vals[i-1] {
			monotonic = false
		}
		if v == 0 {
			zeros++
		} else {
			nonzeros++
		}
	}
	out.IsMonotonic = monotonic
	out.IsBoolish = zeros > 0 && nonzeros > 0 && out.MinValue >= 0 && out.MaxValue <= 1.0001

	switch role {
	case "running":
		out.Ok = out.IsBoolish
		if !out.IsBoolish {
			if out.MinValue == out.MaxValue {
				out.Warnings = append(out.Warnings,
					"Valore costante: il tag non cambia stato. Macchina ferma o tag sbagliato.")
			} else if out.MaxValue > 1 {
				out.Warnings = append(out.Warnings,
					"Valori > 1: sembra un analogico, non un BOOL on/off. Per running serve 0/1.")
			}
		}
	case "counter":
		out.Ok = monotonic && out.Delta > 0
		if !monotonic {
			out.Warnings = append(out.Warnings,
				"Il valore decresce in alcuni punti: counter resettato a fine turno? Usa un counter monotono.")
		}
		if out.Delta == 0 && monotonic {
			out.Warnings = append(out.Warnings,
				"Counter fermo nei campioni esaminati: linea non sta producendo o tag sbagliato.")
		}
	}
	c.JSON(http.StatusOK, out)
}

// History ritorna l'OEE per ognuno degli ultimi 7 giorni, ricalcolato.
// Implementazione semplice: 7 chiamate a compute() proiettate indietro
// nel tempo. Ok fino a window=8h (i dati ci sono); per finestre più
// lunghe sarebbe meglio cachare, ma per la sparkline a 7 punti la
// performance è accettabile.
func (h *OEEHandler) History(c *gin.Context) {
	out := []OEEHistoryPoint{}
	now := time.Now().UTC()
	for i := 6; i >= 0; i-- {
		d := time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, time.UTC)
		// Per ogni giorno calcoliamo l'OEE delle ultime 24h fino a fine
		// giornata; per "oggi" calcoliamo fino a NOW.
		end := d.Add(24 * time.Hour)
		if i == 0 {
			end = now
		}
		out = append(out, OEEHistoryPoint{
			Bucket: d,
			OEE:    h.computeAt(end, 24*60),
		})
	}
	c.JSON(http.StatusOK, out)
}

// compute è il punto di ingresso "OEE attuale" — usa la finestra
// configurata (oee_window_minutes, default 480 = 8h).
func (h *OEEHandler) compute() OEESnapshot {
	windowMin := h.intSetting("oee_window_minutes", 480)
	return h.computeSnapshotAt(time.Now().UTC(), windowMin)
}

// computeAt calcola l'OEE complessivo (% × % × %) come singolo numero
// terminato al timestamp `end` e con finestra di N minuti. Usato dalla
// history per backfill dei 7 giorni.
func (h *OEEHandler) computeAt(end time.Time, windowMin int) float64 {
	s := h.computeSnapshotAt(end, windowMin)
	return s.OEE
}

// computeSnapshotAt è il calcolo vero — separato per essere chiamato sia
// dall'endpoint /api/oee sia dal dashboard overview.
func (h *OEEHandler) computeSnapshotAt(end time.Time, windowMin int) OEESnapshot {
	out := OEESnapshot{
		WindowMinutes: windowMin,
	}
	start := end.Add(-time.Duration(windowMin) * time.Minute)

	// ── Availability ────────────────────────────────────────────────
	out.Availability, out.AvailabilitySource, out.CriticalDowntimeMin =
		h.computeAvailability(start, end, windowMin)

	// ── Performance ─────────────────────────────────────────────────
	out.Performance, out.PerformanceSource, out.PiecesProduced, out.TargetPiecesPerHour =
		h.computePerformance(start, end, windowMin)

	// ── Quality ─────────────────────────────────────────────────────
	out.Quality, out.QualitySource, out.PiecesGood = h.computeQuality(start, end, out.PiecesProduced)

	// OEE = A × P × Q (tutti in 0-1 internamente, poi × 100 per UI)
	a := clamp01(out.Availability / 100)
	p := clamp01(out.Performance / 100)
	q := clamp01(out.Quality / 100)
	out.OEE = a * p * q * 100

	if t := h.floatSetting("oee_target"); t > 0 {
		out.Target = &t
	}
	return out
}

// computeAvailability — usa oee_run_time_tag se configurato; altrimenti
// fallback su "1 - (downtime critical / finestra)".
func (h *OEEHandler) computeAvailability(start, end time.Time, windowMin int) (float64, string, float64) {
	// Modalità tag-driven: percentuale di tempo in cui run_time_tag = 1.
	if id := h.intSetting("oee_run_time_tag", 0); id > 0 {
		// Frazione di campioni con valore > 0. Approssimazione semplice:
		// non integrale ma sufficiente con scan rate regolare.
		var total, running int
		_ = h.db.QueryRow(`
			SELECT COUNT(*), COUNT(*) FILTER (WHERE value > 0)
			FROM tag_history WHERE tag_id = $1 AND time > $2 AND time <= $3`,
			id, start, end,
		).Scan(&total, &running)
		if total > 0 {
			return float64(running) / float64(total) * 100, "tag", 0
		}
	}

	// Fallback: 1 - (somma durate critical attivi nella finestra) / finestra.
	// Per gli allarmi ancora attivi clear_time è NULL → usiamo NOW().
	var downSec float64
	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(
			EXTRACT(EPOCH FROM (
				LEAST(COALESCE(clear_time, NOW()), $2) -
				GREATEST(trigger_time, $1)
			))
		), 0)
		FROM alarm_events
		WHERE LOWER(severity) = 'critical'
		  AND trigger_time < $2
		  AND COALESCE(clear_time, NOW()) > $1`,
		start, end,
	).Scan(&downSec)

	if downSec < 0 {
		downSec = 0
	}
	windowSec := float64(windowMin * 60)
	availability := 100.0
	if windowSec > 0 {
		availability = (1 - downSec/windowSec) * 100
		if availability < 0 {
			availability = 0
		}
	}
	return availability, "fallback", downSec / 60
}

// computePerformance — usa oee_produced_tag + oee_target_pph se entrambi
// configurati; altrimenti fallback al % di campioni con quality=0
// (proxy: se i sensori riportano dati buoni, l'impianto sta lavorando).
func (h *OEEHandler) computePerformance(start, end time.Time, windowMin int) (float64, string, float64, float64) {
	target := h.floatSetting("oee_target_pieces_per_hour")
	if id := h.intSetting("oee_produced_tag", 0); id > 0 && target > 0 {
		// Pieces in finestra = delta del contatore monotono.
		var firstV, lastV sql.NullFloat64
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history
			WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time ASC LIMIT 1`,
			id, start, end,
		).Scan(&firstV)
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history
			WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time DESC LIMIT 1`,
			id, start, end,
		).Scan(&lastV)
		if firstV.Valid && lastV.Valid && lastV.Float64 >= firstV.Float64 {
			pieces := lastV.Float64 - firstV.Float64
			expected := target * (float64(windowMin) / 60)
			if expected > 0 {
				p := pieces / expected * 100
				if p > 100 {
					p = 100
				}
				return p, "tag", pieces, target
			}
		}
	}

	// Fallback: percentuale di campioni con quality=0 (GOOD).
	var total, good int
	_ = h.db.QueryRow(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE quality = 0)
		FROM tag_history WHERE time > $1 AND time <= $2`,
		start, end,
	).Scan(&total, &good)
	if total == 0 {
		return 100, "fallback", 0, target
	}
	return float64(good) / float64(total) * 100, "fallback", 0, target
}

// computeQuality — usa oee_good_tag + pieces produced se configurato;
// altrimenti fallback identico al Performance (% campioni buoni).
// I due fallback coincidono di proposito quando l'utente non ha
// configurato nulla: la rappresentazione "tutto OK" è preferibile a
// numeri inventati. Quando il tag good è configurato, divide good/produced.
func (h *OEEHandler) computeQuality(start, end time.Time, produced float64) (float64, string, float64) {
	if id := h.intSetting("oee_good_tag", 0); id > 0 && produced > 0 {
		var firstV, lastV sql.NullFloat64
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time ASC LIMIT 1`, id, start, end).Scan(&firstV)
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time DESC LIMIT 1`, id, start, end).Scan(&lastV)
		if firstV.Valid && lastV.Valid && lastV.Float64 >= firstV.Float64 {
			good := lastV.Float64 - firstV.Float64
			q := good / produced * 100
			if q > 100 {
				q = 100
			}
			return q, "tag", good
		}
	}

	// Fallback: percentuale di campioni con quality=0.
	var total, goodSamples int
	_ = h.db.QueryRow(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE quality = 0)
		FROM tag_history WHERE time > $1 AND time <= $2`,
		start, end,
	).Scan(&total, &goodSamples)
	if total == 0 {
		return 100, "fallback", 0
	}
	return float64(goodSamples) / float64(total) * 100, "fallback", 0
}

// ── helpers per leggere settings ────────────────────────────────────────

func (h *OEEHandler) intSetting(key string, def int) int {
	var raw string
	_ = h.db.QueryRow(`SELECT value FROM global_settings WHERE key = $1`, key).Scan(&raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func (h *OEEHandler) floatSetting(key string) float64 {
	var raw string
	_ = h.db.QueryRow(`SELECT value FROM global_settings WHERE key = $1`, key).Scan(&raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return f
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
