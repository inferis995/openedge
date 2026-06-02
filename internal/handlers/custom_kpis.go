// Package handlers — custom KPIs.
//
// Permette all'operatore di definire metriche di produzione direttamente
// da UI senza scrivere codice. Una KPI è (tag, aggregazione, finestra,
// moltiplicatore, target). L'evaluator calcola il valore a ogni richiesta
// di /api/dashboard/overview interrogando tag_history (TimescaleDB) o
// Redis per il "last".
package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type CustomKPIsHandler struct {
	db *sql.DB
}

func NewCustomKPIsHandler(db *sql.DB) *CustomKPIsHandler {
	return &CustomKPIsHandler{db: db}
}

// CustomKPI è la response shape per UI + per l'evaluator interno.
type CustomKPI struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	TagID         int      `json:"tag_id"`
	TagAlias      string   `json:"tag_alias,omitempty"`
	Aggregation   string   `json:"aggregation"`   // avg|sum|min|max|last|delta|count
	WindowMinutes int      `json:"window_minutes"`
	Unit          string   `json:"unit"`
	Multiplier    float64  `json:"multiplier"`
	GoodWhen      string   `json:"good_when"`
	TargetValue   *float64 `json:"target_value,omitempty"`
	Active        bool     `json:"active"`
}

// CreateCustomKPIRequest body per POST + PUT.
type CreateCustomKPIRequest struct {
	Name          string   `json:"name" binding:"required"`
	TagID         int      `json:"tag_id" binding:"required"`
	Aggregation   string   `json:"aggregation" binding:"required"`
	WindowMinutes int      `json:"window_minutes"`
	Unit          string   `json:"unit"`
	Multiplier    *float64 `json:"multiplier"`
	GoodWhen      string   `json:"good_when"`
	TargetValue   *float64 `json:"target_value"`
	Active        *bool    `json:"active"`
}

// List restituisce tutte le custom KPI configurate, con alias del tag.
func (h *CustomKPIsHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT k.id, k.name, k.tag_id, COALESCE(t.alias, ''), k.aggregation,
		       k.window_minutes, COALESCE(k.unit,''), k.multiplier, k.good_when,
		       k.target_value, k.active
		FROM custom_kpis k
		LEFT JOIN tags t ON t.id = k.tag_id
		ORDER BY k.name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list custom KPIs"})
		return
	}
	defer rows.Close()
	out := []CustomKPI{}
	for rows.Next() {
		var k CustomKPI
		if err := rows.Scan(&k.ID, &k.Name, &k.TagID, &k.TagAlias, &k.Aggregation,
			&k.WindowMinutes, &k.Unit, &k.Multiplier, &k.GoodWhen, &k.TargetValue, &k.Active); err != nil {
			continue
		}
		out = append(out, k)
	}
	c.JSON(http.StatusOK, out)
}

// Create inserisce una nuova custom KPI con validazione campi enum.
func (h *CustomKPIsHandler) Create(c *gin.Context) {
	var req CreateCustomKPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateCustomKPI(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mult := 1.0
	if req.Multiplier != nil {
		mult = *req.Multiplier
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	goodWhen := "up"
	if req.GoodWhen != "" {
		goodWhen = req.GoodWhen
	}
	windowMin := req.WindowMinutes
	if windowMin <= 0 {
		windowMin = 1440 // 24h default
	}
	var id int
	err := h.db.QueryRow(`
		INSERT INTO custom_kpis (name, tag_id, aggregation, window_minutes, unit,
		                         multiplier, good_when, target_value, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		req.Name, req.TagID, req.Aggregation, windowMin, req.Unit,
		mult, goodWhen, req.TargetValue, active,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to create (duplicate name? tag not found?)"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// Update modifica una KPI esistente.
func (h *CustomKPIsHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}
	var req CreateCustomKPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateCustomKPI(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mult := 1.0
	if req.Multiplier != nil {
		mult = *req.Multiplier
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	goodWhen := "up"
	if req.GoodWhen != "" {
		goodWhen = req.GoodWhen
	}
	windowMin := req.WindowMinutes
	if windowMin <= 0 {
		windowMin = 1440
	}
	res, err := h.db.Exec(`
		UPDATE custom_kpis SET name=$1, tag_id=$2, aggregation=$3, window_minutes=$4,
		                       unit=$5, multiplier=$6, good_when=$7,
		                       target_value=$8, active=$9
		WHERE id=$10`,
		req.Name, req.TagID, req.Aggregation, windowMin, req.Unit,
		mult, goodWhen, req.TargetValue, active, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Custom KPI not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Custom KPI updated"})
}

// Delete rimuove una custom KPI.
func (h *CustomKPIsHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}
	res, err := h.db.Exec(`DELETE FROM custom_kpis WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Custom KPI not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// EvaluateAll calcola TUTTE le custom KPI attive in una sola passata.
// Chiamata dal DashboardHandler per popolare il blocco custom_kpi della
// response overview. Per ogni KPI esegue UNA query — la performance è
// OK fino a ~50 KPI attive (la maggior parte degli impianti ne ha 5-10).
func EvaluateAll(db *sql.DB) []KPIWidget {
	out := []KPIWidget{}
	rows, err := db.Query(`
		SELECT k.id, k.name, k.tag_id, k.aggregation, k.window_minutes,
		       COALESCE(k.unit,''), k.multiplier, k.good_when, k.target_value
		FROM custom_kpis k WHERE k.active = true ORDER BY k.name`)
	if err != nil {
		return out
	}
	defer rows.Close()
	type defRow struct {
		id, tagID, windowMin int
		name, agg, unit, goodWhen string
		mult float64
		target sql.NullFloat64
	}
	defs := []defRow{}
	for rows.Next() {
		var d defRow
		if err := rows.Scan(&d.id, &d.name, &d.tagID, &d.agg, &d.windowMin,
			&d.unit, &d.mult, &d.goodWhen, &d.target); err == nil {
			defs = append(defs, d)
		}
	}
	for _, d := range defs {
		current := evalAggregation(db, d.tagID, d.agg, d.windowMin)
		// Periodo precedente per il delta% — stessa finestra ma traslata.
		previous := evalAggregationOffset(db, d.tagID, d.agg, d.windowMin, d.windowMin)

		w := kpi("custom_"+strconv.Itoa(d.id), d.name,
			current*d.mult, d.unit, previous*d.mult, d.goodWhen)
		if d.target.Valid {
			t := d.target.Float64
			w.Target = &t
			met := false
			if d.goodWhen == "down" {
				met = w.Value <= t
			} else {
				met = w.Value >= t
			}
			w.TargetMet = &met
		}
		out = append(out, w)
	}
	return out
}

// evalAggregation esegue la query SQL appropriata in base al tipo di
// aggregazione. La finestra è in minuti, fino a NOW().
func evalAggregation(db *sql.DB, tagID int, agg string, windowMin int) float64 {
	return evalAggregationOffset(db, tagID, agg, windowMin, 0)
}

// evalAggregationOffset come sopra ma traslato di `offsetMin` minuti
// indietro — per calcolare il valore del periodo precedente.
func evalAggregationOffset(db *sql.DB, tagID int, agg string, windowMin, offsetMin int) float64 {
	startCutoff := fmt.Sprintf(`NOW() - INTERVAL '%d minutes' - INTERVAL '%d minutes'`, offsetMin, windowMin)
	endCutoff := fmt.Sprintf(`NOW() - INTERVAL '%d minutes'`, offsetMin)

	var q string
	switch agg {
	case "avg":
		q = `SELECT COALESCE(AVG(value), 0) FROM tag_history WHERE tag_id = $1 AND time > ` + startCutoff + ` AND time <= ` + endCutoff
	case "sum":
		q = `SELECT COALESCE(SUM(value), 0) FROM tag_history WHERE tag_id = $1 AND time > ` + startCutoff + ` AND time <= ` + endCutoff
	case "min":
		q = `SELECT COALESCE(MIN(value), 0) FROM tag_history WHERE tag_id = $1 AND time > ` + startCutoff + ` AND time <= ` + endCutoff
	case "max":
		q = `SELECT COALESCE(MAX(value), 0) FROM tag_history WHERE tag_id = $1 AND time > ` + startCutoff + ` AND time <= ` + endCutoff
	case "count":
		q = `SELECT COUNT(*) FROM tag_history WHERE tag_id = $1 AND time > ` + startCutoff + ` AND time <= ` + endCutoff
	case "last":
		// "Last" prende sempre il valore più recente NEL periodo; per offset=0
		// è il valore corrente. Quando offset>0 è il valore al limite finestra.
		q = `SELECT COALESCE(value, 0) FROM tag_history WHERE tag_id = $1 AND time <= ` + endCutoff + ` ORDER BY time DESC LIMIT 1`
	case "delta":
		// Differenza tra last e first nella finestra — utile per contatori
		// monotoni (es. pezzi prodotti = delta di un counter).
		q = `SELECT COALESCE(
		         (SELECT value FROM tag_history WHERE tag_id = $1 AND time > ` + startCutoff + ` AND time <= ` + endCutoff + ` ORDER BY time DESC LIMIT 1)
		         -
		         (SELECT value FROM tag_history WHERE tag_id = $1 AND time > ` + startCutoff + ` AND time <= ` + endCutoff + ` ORDER BY time ASC LIMIT 1)
		     , 0)`
	default:
		return 0
	}
	var v sql.NullFloat64
	_ = db.QueryRow(q, tagID).Scan(&v)
	if !v.Valid {
		return 0
	}
	return v.Float64
}

// validateCustomKPI verifica enum + range dei campi del request.
func validateCustomKPI(req *CreateCustomKPIRequest) error {
	allowedAgg := map[string]bool{"avg": true, "sum": true, "min": true, "max": true,
		"last": true, "delta": true, "count": true}
	if !allowedAgg[req.Aggregation] {
		return errMsg("aggregation must be one of: avg, sum, min, max, last, delta, count")
	}
	if req.GoodWhen != "" && req.GoodWhen != "up" && req.GoodWhen != "down" {
		return errMsg("good_when must be 'up' or 'down'")
	}
	if req.WindowMinutes < 0 {
		return errMsg("window_minutes must be positive")
	}
	if req.Multiplier != nil && *req.Multiplier == 0 {
		return errMsg("multiplier cannot be zero")
	}
	return nil
}

// _ tiene "time" import-only per il futuro reuse in evaluation timeline.
var _ = time.Now
