// Package handlers — maintenance windows.
//
// Finestre di manutenzione programmata. Durante una finestra attiva:
//   - le notifiche email/Telegram non escono (silenziate da notifier)
//   - gli allarmi continuano a essere registrati in alarm_events per audit
//   - la dashboard mostra un badge "manutenzione in corso"
//
// Il dispatcher notifications chiama IsInMaintenance() prima di ogni
// invio; quando true scarta l'evento (con un log).
package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

type MaintenanceHandler struct {
	db *sql.DB
}

func NewMaintenanceHandler(db *sql.DB) *MaintenanceHandler {
	return &MaintenanceHandler{db: db}
}

// MaintenanceWindow è la response shape verso il frontend.
type MaintenanceWindow struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	StartAt   time.Time  `json:"start_at"`
	EndAt     time.Time  `json:"end_at"`
	Reason    string     `json:"reason,omitempty"`
	CreatedBy *int       `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	Active    bool       `json:"active"` // calcolato server-side: NOW() ∈ [start, end]
}

// CreateMaintenanceRequest body per POST + PUT.
type CreateMaintenanceRequest struct {
	Title   string `json:"title" binding:"required"`
	StartAt string `json:"start_at" binding:"required"` // RFC3339
	EndAt   string `json:"end_at" binding:"required"`
	Reason  string `json:"reason"`
}

// List restituisce TUTTE le finestre (passate, attive, future). Default
// ordinate per start_at desc. Filtro ?active=true ritorna solo quelle
// in corso adesso — usato dal widget dashboard.
func (h *MaintenanceHandler) List(c *gin.Context) {
	onlyActive := c.Query("active") == "true"
	query := `
		SELECT id, title, start_at, end_at, COALESCE(reason, ''), created_by, created_at
		FROM maintenance_windows`
	if onlyActive {
		query += ` WHERE NOW() BETWEEN start_at AND end_at`
	}
	query += ` ORDER BY start_at DESC`

	rows, err := h.db.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list maintenance windows"})
		return
	}
	defer rows.Close()
	out := []MaintenanceWindow{}
	now := time.Now()
	for rows.Next() {
		var w MaintenanceWindow
		if err := rows.Scan(&w.ID, &w.Title, &w.StartAt, &w.EndAt, &w.Reason, &w.CreatedBy, &w.CreatedAt); err != nil {
			continue
		}
		w.Active = now.After(w.StartAt) && now.Before(w.EndAt)
		out = append(out, w)
	}
	c.JSON(http.StatusOK, out)
}

// Create inserisce una nuova finestra. Admin only (gated dalla route).
func (h *MaintenanceHandler) Create(c *gin.Context) {
	var req CreateMaintenanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	start, end, err := parseMaintRange(req.StartAt, req.EndAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	actor := actorID(c)
	var id int
	err = h.db.QueryRow(`
		INSERT INTO maintenance_windows (title, start_at, end_at, reason, created_by)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5) RETURNING id`,
		req.Title, start, end, req.Reason, nullableID(actor),
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create maintenance window"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// Update modifica titolo / orari / motivazione.
func (h *MaintenanceHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}
	var req CreateMaintenanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	start, end, err := parseMaintRange(req.StartAt, req.EndAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.db.Exec(`
		UPDATE maintenance_windows SET title = $1, start_at = $2, end_at = $3,
		                               reason = NULLIF($4, '')
		WHERE id = $5`,
		req.Title, start, end, req.Reason, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Maintenance window not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Maintenance window updated"})
}

// Delete rimuove una finestra (anche futura, anche in corso — niente
// safeguard: un admin che cancella sa cosa sta facendo).
func (h *MaintenanceHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}
	res, err := h.db.Exec(`DELETE FROM maintenance_windows WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Maintenance window not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// IsInMaintenance ritorna true se NOW() cade dentro almeno una finestra
// di manutenzione. Chiamato dal notifier prima di inviare; quando true
// l'evento è loggato come "skipped: maintenance" ma NON inviato.
//
// Restituisce false su errore DB — fail-open: meglio una notifica di
// troppo durante un problema DB che silenziare allarmi reali. Logica
// "fail open" è la convenzione industriale per i sistemi di alerting.
func IsInMaintenance(db *sql.DB) bool {
	if db == nil {
		return false
	}
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM maintenance_windows
		WHERE NOW() BETWEEN start_at AND end_at`).Scan(&n)
	if err != nil {
		return false
	}
	return n > 0
}

// ── helpers privati ────────────────────────────────────────────────────────

// parseRange accetta due timestamp RFC3339 e valida che la finestra sia
// non-vuota. Restituisce time.Time UTC.
func parseMaintRange(startStr, endStr string) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		return time.Time{}, time.Time{}, errMsg("start_at must be RFC3339 (e.g. 2026-06-04T22:00:00Z)")
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return time.Time{}, time.Time{}, errMsg("end_at must be RFC3339")
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errMsg("end_at must be after start_at")
	}
	return start.UTC(), end.UTC(), nil
}

// errMsg incapsula il pattern di errore "stringa human-readable" senza
// richiedere l'import di fmt o errors solo per questo file.
type stringError string

func (e stringError) Error() string { return string(e) }

func errMsg(s string) error { return stringError(s) }

// actorID estrae l'user_id dell'admin che sta facendo la richiesta — è
// salvato in created_by così le finestre hanno provenienza per l'audit.
// Restituisce 0 quando non c'è auth context (non dovrebbe succedere
// sulle route protette).
func actorID(c *gin.Context) int {
	v, ok := c.Get(middleware.UserKey)
	if !ok {
		return 0
	}
	claims, ok := v.(jwt.MapClaims)
	if !ok {
		return 0
	}
	switch n := claims["user_id"].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
