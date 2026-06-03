// Package handlers — OEE (Overall Equipment Effectiveness).
//
// OEE = Availability × Performance × Quality (ISO 22400).
//
// Modello multi-profilo:
//   - 0 profili → modalità "legacy": un singolo calcolo OEE per
//     l'organizzazione, basato sui settings globali `oee_*`. Funziona
//     out-of-the-box anche senza tag configurati (fallback euristici).
//   - 1+ profili → ogni profilo è un'unità di misura indipendente (una
//     linea, una macchina, un reparto). La dashboard mostra una card
//     per profilo + un rollup di fabbrica (media aritmetica).
//
// Il calcolo è parametrizzato da un `oeeConfig`: 3 tag id + window +
// target. Stesso codice gira sia per i profili che per la modalità legacy.
//
// Fallback euristici (quando i tag id sono 0):
//   - Availability = 1 - (durata totale critical attivi / finestra)
//   - Performance  = % campioni con quality=0 nella finestra
//   - Quality      = idem (in attesa di tag good configurato)
//
// Modalità tag-driven (tag id > 0):
//   - Availability = % campioni di run_time_tag con valore > 0
//   - Performance  = pieces_produced / (target_pph × ore_finestra) × 100
//   - Quality      = pieces_good / pieces_produced × 100
package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// OEEHandler espone gli endpoint /api/oee*.
type OEEHandler struct {
	db *sql.DB
}

func NewOEEHandler(db *sql.DB) *OEEHandler {
	return &OEEHandler{db: db}
}

// oeeConfig è il "ricettario" di un calcolo OEE: cosa misuriamo, su
// quale tag, su quale finestra, contro quale target. Usato sia per i
// profili (letto dal DB) che per la modalità legacy (letto dai settings).
type oeeConfig struct {
	WindowMin          int
	Target             float64 // target OEE % (0 = nessun target)
	RunTimeTagID       int     // 0 = fallback
	ProducedTagID      int     // 0 = fallback
	GoodTagID          int     // 0 = fallback
	TargetPPH          float64 // pezzi/ora atteso
	RespectShifts      bool    // true → Availability divide per PPT, no per wall clock
	RespectMaintenance bool    // true → finestre manutenzione sottratte da PPT
}

// OEESnapshot è un calcolo OEE singolo (per un profilo o per la modalità
// legacy). La UI ne mostra il numero grande + breakdown A/P/Q.
type OEESnapshot struct {
	OEE          float64 `json:"oee"`
	Availability float64 `json:"availability"`
	Performance  float64 `json:"performance"`
	Quality      float64 `json:"quality"`

	AvailabilitySource string `json:"availability_source"`
	PerformanceSource  string `json:"performance_source"`
	QualitySource      string `json:"quality_source"`

	WindowMinutes int      `json:"window_minutes"`
	Target        *float64 `json:"target,omitempty"`

	CriticalDowntimeMin float64 `json:"critical_downtime_min"`
	PiecesProduced      float64 `json:"pieces_produced,omitempty"`
	PiecesGood          float64 `json:"pieces_good,omitempty"`
	TargetPiecesPerHour float64 `json:"target_pieces_per_hour,omitempty"`
}

// OEEProfileSnapshot è uno snapshot OEE etichettato — i campi extra
// (profile_id, name, area_name) servono alla dashboard per renderizzare
// la griglia di card "OEE per profilo".
type OEEProfileSnapshot struct {
	ProfileID   int         `json:"profile_id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	AreaID      *int        `json:"area_id,omitempty"`
	AreaName    string      `json:"area_name,omitempty"`
	Enabled     bool        `json:"enabled"`
	Snapshot    OEESnapshot `json:"snapshot"`
}

// OEEOverview è la response top-level dell'endpoint /api/oee.
// Due modalità:
//   - mode="legacy": un singolo snapshot OEE per l'organizzazione (Legacy).
//   - mode="profiles": lista di profili + rollup (media aritmetica).
type OEEOverview struct {
	Mode     string               `json:"mode"`
	Profiles []OEEProfileSnapshot `json:"profiles,omitempty"`
	Rollup   *OEESnapshot         `json:"rollup,omitempty"`
	Legacy   *OEESnapshot         `json:"legacy,omitempty"`
}

// Snapshot GET /api/oee — restituisce l'overview corrente.
func (h *OEEHandler) Snapshot(c *gin.Context) {
	c.JSON(http.StatusOK, h.overview())
}

// overview è il punto di ingresso che decide tra modalità profiles e
// modalità legacy.
func (h *OEEHandler) overview() OEEOverview {
	profiles := h.loadEnabledProfiles()
	if len(profiles) == 0 {
		// Modalità legacy: una sola snapshot dai settings globali.
		cfg := h.legacyConfig()
		snap := h.computeSnapshotAt(time.Now().UTC(), cfg)
		return OEEOverview{Mode: "legacy", Legacy: &snap}
	}

	out := OEEOverview{Mode: "profiles"}
	now := time.Now().UTC()
	rollupSum := struct{ a, p, q, oee float64 }{}
	count := 0
	for _, p := range profiles {
		snap := h.computeSnapshotAt(now, p.config())
		out.Profiles = append(out.Profiles, OEEProfileSnapshot{
			ProfileID:   p.ID,
			Name:        p.Name,
			Description: p.Description,
			AreaID:      p.AreaID,
			AreaName:    p.AreaName,
			Enabled:     p.Enabled,
			Snapshot:    snap,
		})
		rollupSum.a += snap.Availability
		rollupSum.p += snap.Performance
		rollupSum.q += snap.Quality
		rollupSum.oee += snap.OEE
		count++
	}
	if count > 0 {
		// Rollup = media aritmetica delle 4 metriche tra tutti i profili
		// abilitati. Semplice, prevedibile; volutamente non "weighted by
		// pieces" per non rompersi quando alcuni profili sono in fallback
		// (pieces=0). Quando serve la media pesata, l'admin tipicamente la
		// vede già nel singolo profilo "Fabbrica" se decide di crearlo.
		fn := func(s float64) float64 { return s / float64(count) }
		out.Rollup = &OEESnapshot{
			OEE:          fn(rollupSum.oee),
			Availability: fn(rollupSum.a),
			Performance:  fn(rollupSum.p),
			Quality:      fn(rollupSum.q),
			AvailabilitySource: "rollup",
			PerformanceSource:  "rollup",
			QualitySource:      "rollup",
			WindowMinutes:      profiles[0].WindowMinutes, // info, non sempre uniforme tra profili
		}
		if t := h.floatSetting("oee_target"); t > 0 {
			out.Rollup.Target = &t
		}
	}
	return out
}

// TagTestResult è il "lo provi prima di salvare" della config OEE:
// dato un tag id + un ruolo ("running" o "counter"), il backend legge
// gli ultimi N campioni e dice all'admin "sì, questo tag funziona per
// quel ruolo" o "no, ecco perché".
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
	IsMonotonic  bool    `json:"is_monotonic"`
	IsBoolish    bool    `json:"is_boolish"`
	Warnings     []string `json:"warnings"`
	Ok           bool    `json:"ok"`
}

// TestTag GET /api/oee/test-tag/:id?role=running|counter — verdetto
// "questo tag funziona per il ruolo?" per il wizard config.
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

	var alias sql.NullString
	if err := h.db.QueryRow(`
		SELECT COALESCE(alias,''), code, data_type FROM tags WHERE id = $1`,
		id,
	).Scan(&alias, &out.Code, &out.DataType); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
		return
	}
	out.Alias = alias.String

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

// OEEHistoryPoint è un campione del trend OEE — usato dalla mini-sparkline
// in dashboard "ultime 7g OEE".
type OEEHistoryPoint struct {
	Bucket time.Time `json:"bucket"`
	OEE    float64   `json:"oee"`
}

// History GET /api/oee/history — trend OEE ultimi 7 giorni. In modalità
// profili usa il rollup; in modalità legacy usa il singolo calcolo.
func (h *OEEHandler) History(c *gin.Context) {
	profiles := h.loadEnabledProfiles()
	out := []OEEHistoryPoint{}
	now := time.Now().UTC()
	for i := 6; i >= 0; i-- {
		d := time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, time.UTC)
		end := d.Add(24 * time.Hour)
		if i == 0 {
			end = now
		}
		var oeeVal float64
		if len(profiles) == 0 {
			cfg := h.legacyConfig()
			cfg.WindowMin = 24 * 60
			oeeVal = h.computeSnapshotAt(end, cfg).OEE
		} else {
			sum := 0.0
			for _, p := range profiles {
				cfg := p.config()
				cfg.WindowMin = 24 * 60
				sum += h.computeSnapshotAt(end, cfg).OEE
			}
			oeeVal = sum / float64(len(profiles))
		}
		out = append(out, OEEHistoryPoint{Bucket: d, OEE: oeeVal})
	}
	c.JSON(http.StatusOK, out)
}

// ProfileHistory GET /api/oee/profiles/:id/history — trend OEE 7g per
// un singolo profilo.
func (h *OEEHandler) ProfileHistory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	p, err := h.loadProfile(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
		return
	}
	out := []OEEHistoryPoint{}
	now := time.Now().UTC()
	for i := 6; i >= 0; i-- {
		d := time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, time.UTC)
		end := d.Add(24 * time.Hour)
		if i == 0 {
			end = now
		}
		cfg := p.config()
		cfg.WindowMin = 24 * 60
		out = append(out, OEEHistoryPoint{Bucket: d, OEE: h.computeSnapshotAt(end, cfg).OEE})
	}
	c.JSON(http.StatusOK, out)
}

// computeSnapshotAt è il calcolo del singolo OEE: chiamato da snapshot,
// rollup, history, ovunque. Tutto pesca da `cfg` — niente più letture
// scattered di global_settings.
func (h *OEEHandler) computeSnapshotAt(end time.Time, cfg oeeConfig) OEESnapshot {
	out := OEESnapshot{WindowMinutes: cfg.WindowMin}
	start := end.Add(-time.Duration(cfg.WindowMin) * time.Minute)

	out.Availability, out.AvailabilitySource, out.CriticalDowntimeMin =
		h.computeAvailability(start, end, cfg)
	out.Performance, out.PerformanceSource, out.PiecesProduced, out.TargetPiecesPerHour =
		h.computePerformance(start, end, cfg)
	out.Quality, out.QualitySource, out.PiecesGood =
		h.computeQuality(start, end, out.PiecesProduced, cfg)

	a := clamp01(out.Availability / 100)
	p := clamp01(out.Performance / 100)
	q := clamp01(out.Quality / 100)
	out.OEE = a * p * q * 100

	if cfg.Target > 0 {
		t := cfg.Target
		out.Target = &t
	}
	return out
}

// computeAvailability — tag-driven se cfg.RunTimeTagID > 0, altrimenti
// fallback su downtime allarmi critical.
//
// Quando cfg.RespectShifts=true, il denominatore non è più la wall clock
// window ma il Planned Production Time (vedi computePPT): si esclude il
// tempo fuori turno e le finestre di manutenzione. Una linea che non
// lavora di domenica non viene così penalizzata.
func (h *OEEHandler) computeAvailability(start, end time.Time, cfg oeeConfig) (float64, string, float64) {
	if cfg.RunTimeTagID > 0 {
		var total, running int
		_ = h.db.QueryRow(`
			SELECT COUNT(*), COUNT(*) FILTER (WHERE value > 0)
			FROM tag_history WHERE tag_id = $1 AND time > $2 AND time <= $3`,
			cfg.RunTimeTagID, start, end,
		).Scan(&total, &running)
		if total > 0 {
			return float64(running) / float64(total) * 100, "tag", 0
		}
	}

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
	downMin := downSec / 60

	// Denominatore: PPT se respect_shifts/maintenance, altrimenti wall clock.
	pptMin := float64(cfg.WindowMin)
	if cfg.RespectShifts || cfg.RespectMaintenance {
		pptMin = h.computePPT(start, end, cfg)
	}
	if pptMin <= 0 {
		// Nessun tempo pianificato di produzione → Availability = 100%
		// (non c'è "expected production" da fallire).
		return 100, "fallback", 0
	}
	availability := (1 - downMin/pptMin) * 100
	if availability < 0 {
		availability = 0
	}
	if availability > 100 {
		availability = 100
	}
	return availability, "fallback", downMin
}

// computePerformance — tag-driven se cfg.ProducedTagID > 0 && cfg.TargetPPH > 0.
func (h *OEEHandler) computePerformance(start, end time.Time, cfg oeeConfig) (float64, string, float64, float64) {
	if cfg.ProducedTagID > 0 && cfg.TargetPPH > 0 {
		var firstV, lastV sql.NullFloat64
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history
			WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time ASC LIMIT 1`,
			cfg.ProducedTagID, start, end,
		).Scan(&firstV)
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history
			WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time DESC LIMIT 1`,
			cfg.ProducedTagID, start, end,
		).Scan(&lastV)
		if firstV.Valid && lastV.Valid && lastV.Float64 >= firstV.Float64 {
			pieces := lastV.Float64 - firstV.Float64
			expected := cfg.TargetPPH * (float64(cfg.WindowMin) / 60)
			if expected > 0 {
				p := pieces / expected * 100
				if p > 100 {
					p = 100
				}
				return p, "tag", pieces, cfg.TargetPPH
			}
		}
	}

	var total, good int
	_ = h.db.QueryRow(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE quality = 0)
		FROM tag_history WHERE time > $1 AND time <= $2`,
		start, end,
	).Scan(&total, &good)
	if total == 0 {
		return 100, "fallback", 0, cfg.TargetPPH
	}
	return float64(good) / float64(total) * 100, "fallback", 0, cfg.TargetPPH
}

// computeQuality — tag-driven se cfg.GoodTagID > 0 && produced > 0.
func (h *OEEHandler) computeQuality(start, end time.Time, produced float64, cfg oeeConfig) (float64, string, float64) {
	if cfg.GoodTagID > 0 && produced > 0 {
		var firstV, lastV sql.NullFloat64
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time ASC LIMIT 1`, cfg.GoodTagID, start, end).Scan(&firstV)
		_ = h.db.QueryRow(`
			SELECT value FROM tag_history WHERE tag_id = $1 AND time > $2 AND time <= $3
			ORDER BY time DESC LIMIT 1`, cfg.GoodTagID, start, end).Scan(&lastV)
		if firstV.Valid && lastV.Valid && lastV.Float64 >= firstV.Float64 {
			good := lastV.Float64 - firstV.Float64
			q := good / produced * 100
			if q > 100 {
				q = 100
			}
			return q, "tag", good
		}
	}

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

// ── Profili ─────────────────────────────────────────────────────────────

// OEEProfile è la row del DB. La uso sia per CRUD che per il calcolo
// (via config()).
//
// Nota sul scoping: i 3 tag OEE (run_time/produced/good) possono essere
// su gateway DIVERSI nella stessa org. Per la matematica A/P/Q il calcolo
// legge tag_history[tag_id], niente vincolo gateway. Per gli allarmi
// (loss tree), lo "scope" del profilo viene calcolato dinamicamente
// come l'unione dei gateway dei suoi 3 tag — vedi oee_losses.go.
// Il campo GatewayID resta nello schema per back-compat ma non è più
// usato dal worker loss tree.
type OEEProfile struct {
	ID                  int     `json:"id"`
	OrgID               int     `json:"org_id"`
	Name                string  `json:"name"`
	Description         string  `json:"description,omitempty"`
	AreaID              *int    `json:"area_id,omitempty"`
	AreaName            string  `json:"area_name,omitempty"`
	GatewayID           *int    `json:"gateway_id,omitempty"`
	RunTimeTagID        *int    `json:"run_time_tag_id,omitempty"`
	ProducedTagID       *int    `json:"produced_tag_id,omitempty"`
	GoodTagID           *int    `json:"good_tag_id,omitempty"`
	TargetPiecesPerHour float64 `json:"target_pieces_per_hour"`
	WindowMinutes       int     `json:"window_minutes"`
	TargetOEE           float64 `json:"target_oee"`
	DisplayOrder        int     `json:"display_order"`
	Enabled             bool    `json:"enabled"`
	RespectShifts       bool    `json:"respect_shifts"`
	RespectMaintenance  bool    `json:"respect_maintenance"`
}

func (p OEEProfile) config() oeeConfig {
	cfg := oeeConfig{
		WindowMin:          p.WindowMinutes,
		Target:             p.TargetOEE,
		TargetPPH:          p.TargetPiecesPerHour,
		RespectShifts:      p.RespectShifts,
		RespectMaintenance: p.RespectMaintenance,
	}
	if p.RunTimeTagID != nil {
		cfg.RunTimeTagID = *p.RunTimeTagID
	}
	if p.ProducedTagID != nil {
		cfg.ProducedTagID = *p.ProducedTagID
	}
	if p.GoodTagID != nil {
		cfg.GoodTagID = *p.GoodTagID
	}
	return cfg
}

// loadEnabledProfiles ritorna tutti i profili attivi dell'organizzazione
// corrente (multi-tenant via OrganizationContext middleware). Nessuna org
// = profilo vuoto, vai in legacy mode.
func (h *OEEHandler) loadEnabledProfiles() []OEEProfile {
	rows, err := h.db.Query(`
		SELECT p.id, p.org_id, p.name, COALESCE(p.description,''),
			p.area_id, COALESCE(a.name,''),
			p.gateway_id, p.run_time_tag_id, p.produced_tag_id, p.good_tag_id,
			p.target_pieces_per_hour, p.window_minutes, p.target_oee,
			p.display_order, p.enabled,
			COALESCE(p.respect_shifts, true), COALESCE(p.respect_maintenance, true)
		FROM oee_profiles p
		LEFT JOIN areas a ON a.id = p.area_id
		WHERE p.enabled = true
		ORDER BY p.display_order, p.name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []OEEProfile
	for rows.Next() {
		var p OEEProfile
		if err := rows.Scan(
			&p.ID, &p.OrgID, &p.Name, &p.Description,
			&p.AreaID, &p.AreaName,
			&p.GatewayID, &p.RunTimeTagID, &p.ProducedTagID, &p.GoodTagID,
			&p.TargetPiecesPerHour, &p.WindowMinutes, &p.TargetOEE,
			&p.DisplayOrder, &p.Enabled,
		); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (h *OEEHandler) loadProfile(id int) (OEEProfile, error) {
	var p OEEProfile
	err := h.db.QueryRow(`
		SELECT p.id, p.org_id, p.name, COALESCE(p.description,''),
			p.area_id, COALESCE(a.name,''),
			p.gateway_id, p.run_time_tag_id, p.produced_tag_id, p.good_tag_id,
			p.target_pieces_per_hour, p.window_minutes, p.target_oee,
			p.display_order, p.enabled,
			COALESCE(p.respect_shifts, true), COALESCE(p.respect_maintenance, true)
		FROM oee_profiles p
		LEFT JOIN areas a ON a.id = p.area_id
		WHERE p.id = $1`, id,
	).Scan(
		&p.ID, &p.OrgID, &p.Name, &p.Description,
		&p.AreaID, &p.AreaName,
		&p.GatewayID, &p.RunTimeTagID, &p.ProducedTagID, &p.GoodTagID,
		&p.TargetPiecesPerHour, &p.WindowMinutes, &p.TargetOEE,
		&p.DisplayOrder, &p.Enabled,
		&p.RespectShifts, &p.RespectMaintenance,
	)
	return p, err
}

// ListProfiles GET /api/oee/profiles — admin sees them all (full list).
func (h *OEEHandler) ListProfiles(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT p.id, p.org_id, p.name, COALESCE(p.description,''),
			p.area_id, COALESCE(a.name,''),
			p.gateway_id, p.run_time_tag_id, p.produced_tag_id, p.good_tag_id,
			p.target_pieces_per_hour, p.window_minutes, p.target_oee,
			p.display_order, p.enabled,
			COALESCE(p.respect_shifts, true), COALESCE(p.respect_maintenance, true)
		FROM oee_profiles p
		LEFT JOIN areas a ON a.id = p.area_id
		ORDER BY p.display_order, p.name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []OEEProfile{}
	for rows.Next() {
		var p OEEProfile
		if err := rows.Scan(
			&p.ID, &p.OrgID, &p.Name, &p.Description,
			&p.AreaID, &p.AreaName,
			&p.GatewayID, &p.RunTimeTagID, &p.ProducedTagID, &p.GoodTagID,
			&p.TargetPiecesPerHour, &p.WindowMinutes, &p.TargetOEE,
			&p.DisplayOrder, &p.Enabled,
		); err == nil {
			out = append(out, p)
		}
	}
	c.JSON(http.StatusOK, out)
}

// ProfileRequest è il body di POST/PUT.
type ProfileRequest struct {
	Name                string  `json:"name" binding:"required"`
	Description         string  `json:"description,omitempty"`
	AreaID              *int    `json:"area_id,omitempty"`
	GatewayID           *int    `json:"gateway_id,omitempty"`
	RunTimeTagID        *int    `json:"run_time_tag_id,omitempty"`
	ProducedTagID       *int    `json:"produced_tag_id,omitempty"`
	GoodTagID           *int    `json:"good_tag_id,omitempty"`
	TargetPiecesPerHour float64 `json:"target_pieces_per_hour"`
	WindowMinutes       int     `json:"window_minutes"`
	TargetOEE           float64 `json:"target_oee"`
	DisplayOrder        int     `json:"display_order"`
	Enabled             *bool   `json:"enabled,omitempty"`
	RespectShifts       *bool   `json:"respect_shifts,omitempty"`
	RespectMaintenance  *bool   `json:"respect_maintenance,omitempty"`
}

// CreateProfile POST /api/oee/profiles — admin only.
func (h *OEEHandler) CreateProfile(c *gin.Context) {
	var req ProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	orgID, _ := c.Get("org_id")
	if orgID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "organization context required"})
		return
	}
	if req.WindowMinutes <= 0 {
		req.WindowMinutes = 480
	}
	if req.TargetOEE <= 0 {
		req.TargetOEE = 85
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	respectShifts := true
	if req.RespectShifts != nil {
		respectShifts = *req.RespectShifts
	}
	respectMaint := true
	if req.RespectMaintenance != nil {
		respectMaint = *req.RespectMaintenance
	}

	var id int
	err := h.db.QueryRow(`
		INSERT INTO oee_profiles (
			org_id, name, description, area_id, gateway_id,
			run_time_tag_id, produced_tag_id, good_tag_id,
			target_pieces_per_hour, window_minutes, target_oee,
			display_order, enabled, respect_shifts, respect_maintenance
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id`,
		orgID, req.Name, req.Description, req.AreaID, req.GatewayID,
		req.RunTimeTagID, req.ProducedTagID, req.GoodTagID,
		req.TargetPiecesPerHour, req.WindowMinutes, req.TargetOEE,
		req.DisplayOrder, enabled, respectShifts, respectMaint,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, err := h.loadProfile(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "loaded but read failed"})
		return
	}
	c.JSON(http.StatusCreated, p)
}

// UpdateProfile PUT /api/oee/profiles/:id — admin only.
func (h *OEEHandler) UpdateProfile(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req ProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.WindowMinutes <= 0 {
		req.WindowMinutes = 480
	}
	if req.TargetOEE <= 0 {
		req.TargetOEE = 85
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	respectShifts := true
	if req.RespectShifts != nil {
		respectShifts = *req.RespectShifts
	}
	respectMaint := true
	if req.RespectMaintenance != nil {
		respectMaint = *req.RespectMaintenance
	}
	_, err = h.db.Exec(`
		UPDATE oee_profiles SET
			name=$2, description=$3, area_id=$4, gateway_id=$5,
			run_time_tag_id=$6, produced_tag_id=$7, good_tag_id=$8,
			target_pieces_per_hour=$9, window_minutes=$10, target_oee=$11,
			display_order=$12, enabled=$13,
			respect_shifts=$14, respect_maintenance=$15,
			updated_at=NOW()
		WHERE id=$1`,
		id, req.Name, req.Description, req.AreaID, req.GatewayID,
		req.RunTimeTagID, req.ProducedTagID, req.GoodTagID,
		req.TargetPiecesPerHour, req.WindowMinutes, req.TargetOEE,
		req.DisplayOrder, enabled, respectShifts, respectMaint,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, err := h.loadProfile(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// DeleteProfile DELETE /api/oee/profiles/:id — admin only.
func (h *OEEHandler) DeleteProfile(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := h.db.Exec(`DELETE FROM oee_profiles WHERE id=$1`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ── legacy config ──────────────────────────────────────────────────────

// legacyConfig legge i 6 settings globali oee_* per la modalità single-org.
// Quando l'admin crea anche un solo profilo, questa funzione viene smessa
// di essere usata da Snapshot/History/dashboard.
func (h *OEEHandler) legacyConfig() oeeConfig {
	return oeeConfig{
		WindowMin:     h.intSetting("oee_window_minutes", 480),
		Target:        h.floatSetting("oee_target"),
		RunTimeTagID:  h.intSetting("oee_run_time_tag", 0),
		ProducedTagID: h.intSetting("oee_produced_tag", 0),
		GoodTagID:     h.intSetting("oee_good_tag", 0),
		TargetPPH:     h.floatSetting("oee_target_pieces_per_hour"),
	}
}

// ── helpers ────────────────────────────────────────────────────────────

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

// ── Planned Production Time (PPT) ───────────────────────────────────────
//
// PPT = window - tempo fuori turno - finestre di manutenzione.
// È il denominatore "vero" di Availability: una linea che non lavora
// di domenica non viene penalizzata; una manutenzione programmata non
// fa crollare l'OEE.
//
// Quando nessun turno è definito nel sistema, PPT = window (fallback):
// le installazioni che non hanno configurato turni continuano a vedere
// il comportamento "wall clock" di prima.

// computePPT ritorna i minuti di Planned Production Time in [start, end].
// Se cfg.RespectShifts=false, ritorna semplicemente la durata della
// finestra. Se cfg.RespectMaintenance=true, sottrae anche le finestre
// di manutenzione attive nell'intervallo.
func (h *OEEHandler) computePPT(start, end time.Time, cfg oeeConfig) float64 {
	windowMin := end.Sub(start).Minutes()
	if windowMin <= 0 {
		return 0
	}
	var totalMin float64
	if cfg.RespectShifts {
		totalMin = h.sumShiftIntersections(start, end)
		// Se non esiste nessun turno attivo nel sistema, fallback a wall
		// clock — altrimenti installazioni senza turni vedrebbero PPT=0
		// e Availability sempre 100% (sbagliato).
		if totalMin == 0 && !h.hasActiveShifts() {
			totalMin = windowMin
		}
	} else {
		totalMin = windowMin
	}
	if cfg.RespectMaintenance {
		maintMin := h.sumMaintenanceIntersections(start, end)
		totalMin -= maintMin
		if totalMin < 0 {
			totalMin = 0
		}
	}
	return totalMin
}

// sumShiftIntersections somma le durate dei segmenti di turno che
// intersecano la finestra [start, end]. Itera giorno per giorno;
// per ogni giorno proietta ogni turno attivo su quel weekday in
// timestamp UTC e calcola l'intersezione con [start, end].
//
// Per turni "wrap midnight" (es. 22-06), si proiettano normalmente:
// shiftEnd = day + endMin + 24h. L'intersezione con la finestra del
// giorno successivo viene contata nell'iterazione del giorno corrente.
func (h *OEEHandler) sumShiftIntersections(start, end time.Time) float64 {
	type shiftRow struct {
		startMin, endMin int
		weekdays         []int
		wraps            bool
	}
	rows, err := h.db.Query(`
		SELECT start_time::text, end_time::text, weekdays
		FROM shifts WHERE active = true`)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var shifts []shiftRow
	for rows.Next() {
		var startS, endS string
		var weekdays pq.Int64Array
		if err := rows.Scan(&startS, &endS, &weekdays); err != nil {
			continue
		}
		startS = trimSeconds(startS)
		endS = trimSeconds(endS)
		shifts = append(shifts, shiftRow{
			startMin: minutesOfDay(startS),
			endMin:   minutesOfDay(endS),
			weekdays: int64sToInts(weekdays),
			wraps:    wrapsMidnight(startS, endS),
		})
	}
	if len(shifts) == 0 {
		return 0
	}

	total := 0.0
	// Itero giorno per giorno. dayCursor parte dal giorno di "start"
	// (mezzanotte UTC) e va fino a "end" — con margine 1 giorno per
	// coprire turni wrap.
	dayCursor := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	for !dayCursor.After(end) {
		weekday := int(dayCursor.Weekday()) // 0=Sun .. 6=Sat
		for _, s := range shifts {
			if !contains(s.weekdays, weekday) {
				continue
			}
			shiftStart := dayCursor.Add(time.Duration(s.startMin) * time.Minute)
			shiftEnd := dayCursor.Add(time.Duration(s.endMin) * time.Minute)
			if s.wraps {
				shiftEnd = shiftEnd.Add(24 * time.Hour)
			}
			// Intersezione [max(shiftStart, start), min(shiftEnd, end)]
			iStart := shiftStart
			if start.After(iStart) {
				iStart = start
			}
			iEnd := shiftEnd
			if end.Before(iEnd) {
				iEnd = end
			}
			if iEnd.After(iStart) {
				total += iEnd.Sub(iStart).Minutes()
			}
		}
		dayCursor = dayCursor.Add(24 * time.Hour)
	}
	return total
}

// sumMaintenanceIntersections somma le durate delle finestre di
// manutenzione che intersecano [start, end]. Una finestra che inizia
// prima/termina dopo conta solo per la parte interna alla finestra.
func (h *OEEHandler) sumMaintenanceIntersections(start, end time.Time) float64 {
	rows, err := h.db.Query(`
		SELECT start_at, end_at FROM maintenance_windows
		WHERE start_at < $2 AND end_at > $1`,
		start, end,
	)
	if err != nil {
		return 0
	}
	defer rows.Close()
	total := 0.0
	for rows.Next() {
		var ms, me time.Time
		if err := rows.Scan(&ms, &me); err != nil {
			continue
		}
		iStart := ms
		if start.After(iStart) {
			iStart = start
		}
		iEnd := me
		if end.Before(iEnd) {
			iEnd = end
		}
		if iEnd.After(iStart) {
			total += iEnd.Sub(iStart).Minutes()
		}
	}
	return total
}

// hasActiveShifts indica se almeno un turno attivo esiste nel sistema.
// Usato per il fallback "PPT=window quando non ci sono turni".
func (h *OEEHandler) hasActiveShifts() bool {
	var n int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM shifts WHERE active = true`).Scan(&n)
	return n > 0
}

// ── dashboard integration ───────────────────────────────────────────────

// compute è il punto di ingresso per il DashboardHandler — restituisce
// la stessa OEEOverview di /api/oee.
func (h *OEEHandler) compute() OEEOverview {
	return h.overview()
}

// ── Hierarchy (Site → Area → Profile) ──────────────────────────────────

// OEEHierarchyNode è un nodo dell'albero: snapshot + figli.
type OEEHierarchyNode struct {
	NodeType string             `json:"node_type"` // "site" | "area" | "profile"
	ID       int                `json:"id"`
	Name     string             `json:"name"`
	Snapshot OEESnapshot        `json:"snapshot"`
	Children []OEEHierarchyNode `json:"children,omitempty"`
}

// OEEHierarchy GET /api/oee/hierarchy — rollup multi-livello.
// Snapshot di ogni Site = media aritmetica delle aree figlie.
// Snapshot di ogni Area = media aritmetica dei profili figli abilitati.
// Profili senza area finiscono in un nodo "Unassigned".
func (h *OEEHandler) Hierarchy(c *gin.Context) {
	profiles := h.loadEnabledProfiles()
	now := time.Now().UTC()

	// snapshot per profilo
	type profileNode struct {
		profile OEEProfile
		snap    OEESnapshot
	}
	pnodes := make([]profileNode, 0, len(profiles))
	for _, p := range profiles {
		pnodes = append(pnodes, profileNode{
			profile: p,
			snap:    h.computeSnapshotAt(now, p.config()),
		})
	}

	// area_id → list di profili
	byArea := make(map[int][]profileNode)
	var unassigned []profileNode
	for _, pn := range pnodes {
		if pn.profile.AreaID == nil {
			unassigned = append(unassigned, pn)
		} else {
			byArea[*pn.profile.AreaID] = append(byArea[*pn.profile.AreaID], pn)
		}
	}

	// Carica metadati aree/siti.
	type areaInfo struct {
		id, siteID int
		name       string
		siteName   string
	}
	areaRows, err := h.db.Query(`
		SELECT a.id, a.site_id, a.name, s.name
		FROM areas a
		JOIN sites s ON s.id = a.site_id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	areas := map[int]areaInfo{}
	type siteInfo struct {
		id   int
		name string
	}
	sites := map[int]siteInfo{}
	for areaRows.Next() {
		var a areaInfo
		if err := areaRows.Scan(&a.id, &a.siteID, &a.name, &a.siteName); err == nil {
			areas[a.id] = a
			sites[a.siteID] = siteInfo{id: a.siteID, name: a.siteName}
		}
	}
	areaRows.Close()

	// site_id → []area nodes (con figli profili)
	siteAreas := map[int][]OEEHierarchyNode{}
	for areaID, plist := range byArea {
		ainfo, ok := areas[areaID]
		if !ok {
			continue
		}
		areaNode := OEEHierarchyNode{
			NodeType: "area",
			ID:       areaID,
			Name:     ainfo.name,
		}
		var sum struct{ a, p, q, oee float64 }
		for _, pn := range plist {
			areaNode.Children = append(areaNode.Children, OEEHierarchyNode{
				NodeType: "profile",
				ID:       pn.profile.ID,
				Name:     pn.profile.Name,
				Snapshot: pn.snap,
			})
			sum.a += pn.snap.Availability
			sum.p += pn.snap.Performance
			sum.q += pn.snap.Quality
			sum.oee += pn.snap.OEE
		}
		n := float64(len(plist))
		if n > 0 {
			areaNode.Snapshot = OEESnapshot{
				OEE:          sum.oee / n,
				Availability: sum.a / n,
				Performance:  sum.p / n,
				Quality:      sum.q / n,
				AvailabilitySource: "rollup", PerformanceSource: "rollup", QualitySource: "rollup",
			}
		}
		siteAreas[ainfo.siteID] = append(siteAreas[ainfo.siteID], areaNode)
	}

	// Costruisci nodi site con rollup figli. Inizializzo come slice
	// vuota (non nil) per garantire che la response JSON sia `[]`
	// invece di `null` — il frontend fa `data.sites.length` e
	// crasherebbe su null.
	siteNodes := []OEEHierarchyNode{}
	for sid, sinfo := range sites {
		areasForSite, ok := siteAreas[sid]
		if !ok {
			continue
		}
		siteNode := OEEHierarchyNode{
			NodeType: "site",
			ID:       sid,
			Name:     sinfo.name,
			Children: areasForSite,
		}
		var sum struct{ a, p, q, oee float64 }
		for _, an := range areasForSite {
			sum.a += an.Snapshot.Availability
			sum.p += an.Snapshot.Performance
			sum.q += an.Snapshot.Quality
			sum.oee += an.Snapshot.OEE
		}
		n := float64(len(areasForSite))
		if n > 0 {
			siteNode.Snapshot = OEESnapshot{
				OEE:          sum.oee / n,
				Availability: sum.a / n,
				Performance:  sum.p / n,
				Quality:      sum.q / n,
				AvailabilitySource: "rollup", PerformanceSource: "rollup", QualitySource: "rollup",
			}
		}
		siteNodes = append(siteNodes, siteNode)
	}

	// Unassigned: profili senza area_id finiscono in un nodo virtuale.
	var unassignedNode *OEEHierarchyNode
	if len(unassigned) > 0 {
		node := OEEHierarchyNode{
			NodeType: "area",
			ID:       0,
			Name:     "Senza area",
		}
		var sum struct{ a, p, q, oee float64 }
		for _, pn := range unassigned {
			node.Children = append(node.Children, OEEHierarchyNode{
				NodeType: "profile",
				ID:       pn.profile.ID,
				Name:     pn.profile.Name,
				Snapshot: pn.snap,
			})
			sum.a += pn.snap.Availability
			sum.p += pn.snap.Performance
			sum.q += pn.snap.Quality
			sum.oee += pn.snap.OEE
		}
		n := float64(len(unassigned))
		node.Snapshot = OEESnapshot{
			OEE:          sum.oee / n,
			Availability: sum.a / n,
			Performance:  sum.p / n,
			Quality:      sum.q / n,
			AvailabilitySource: "rollup", PerformanceSource: "rollup", QualitySource: "rollup",
		}
		unassignedNode = &node
	}

	c.JSON(http.StatusOK, gin.H{
		"sites":      siteNodes,
		"unassigned": unassignedNode,
	})
}
