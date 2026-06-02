// Package handlers — shifts.
//
// Gestione turni: definizione orari di lavoro, assegnazione operatori,
// detection del turno corrente. Disegnato per "MVP funzionante" — non
// gestisce ancora handover form né maintenance windows, ma fornisce
// l'API che la dashboard usa per il widget "turno corrente" e che
// l'admin usa per configurare tutto da UI.
package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// ShiftsHandler espone la CRUD turni + assegnazione operatori +
// detection del turno in corso al momento della chiamata.
type ShiftsHandler struct {
	db *sql.DB
}

func NewShiftsHandler(db *sql.DB) *ShiftsHandler {
	return &ShiftsHandler{db: db}
}

// Shift è il record on-disk + la response shape verso il frontend.
// StartTime/EndTime sono "HH:MM:SS" (UTC del server — l'UI mostra in
// local time se serve). Weekdays è 0=Domenica … 6=Sabato.
type Shift struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Weekdays  []int  `json:"weekdays"`
	Active    bool   `json:"active"`
	// Wraps è true quando StartTime > EndTime (turno notte che attraversa
	// la mezzanotte). Calcolato lato server e ritornato pronto per l'UI.
	Wraps bool `json:"wraps"`
}

// ShiftAssignment associa un operatore (user) a un turno per un periodo.
// ValidTo null = a tempo indeterminato.
type ShiftAssignment struct {
	ID        int       `json:"id"`
	ShiftID   int       `json:"shift_id"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username,omitempty"`
	FullName  string    `json:"full_name,omitempty"`
	ValidFrom time.Time `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

// CurrentShift è la risposta a "che turno è adesso?". Operators è la
// lista degli operatori assegnati al turno per oggi (valid_from <= today
// <= valid_to OR valid_to NULL). Vuota = "nessun operatore designato".
type CurrentShift struct {
	Shift          *Shift             `json:"shift"`
	StartedAt      *time.Time         `json:"started_at,omitempty"`
	EndsAt         *time.Time         `json:"ends_at,omitempty"`
	TimeLeftMin    int                `json:"time_left_min"`
	Operators      []ShiftAssignment  `json:"operators"`
}

// CreateShiftRequest è il body di POST /api/shifts. Anche per PUT lo
// stesso shape — campi come Active opzionali.
type CreateShiftRequest struct {
	Name      string `json:"name" binding:"required"`
	StartTime string `json:"start_time" binding:"required"` // "HH:MM"
	EndTime   string `json:"end_time" binding:"required"`
	Weekdays  []int  `json:"weekdays"`
	Active    *bool  `json:"active"`
}

// List restituisce tutti i turni configurati, attivi e non.
func (h *ShiftsHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT id, name, start_time::text, end_time::text, weekdays, active
		FROM shifts ORDER BY start_time, name`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list shifts"})
		return
	}
	defer rows.Close()
	out := []Shift{}
	for rows.Next() {
		var s Shift
		var weekdays pq.Int64Array
		if err := rows.Scan(&s.ID, &s.Name, &s.StartTime, &s.EndTime, &weekdays, &s.Active); err != nil {
			continue
		}
		s.StartTime = trimSeconds(s.StartTime)
		s.EndTime = trimSeconds(s.EndTime)
		s.Weekdays = int64sToInts(weekdays)
		s.Wraps = wrapsMidnight(s.StartTime, s.EndTime)
		out = append(out, s)
	}
	c.JSON(http.StatusOK, out)
}

// Create inserisce un nuovo turno. Admin only (gated dalla route).
func (h *ShiftsHandler) Create(c *gin.Context) {
	var req CreateShiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !validHHMM(req.StartTime) || !validHHMM(req.EndTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_time and end_time must be in HH:MM format"})
		return
	}
	weekdays := req.Weekdays
	if len(weekdays) == 0 {
		weekdays = []int{1, 2, 3, 4, 5}
	}
	for _, w := range weekdays {
		if w < 0 || w > 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "weekdays must be between 0 (Sun) and 6 (Sat)"})
			return
		}
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	var id int
	err := h.db.QueryRow(`
		INSERT INTO shifts (name, start_time, end_time, weekdays, active)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		strings.TrimSpace(req.Name), req.StartTime, req.EndTime, pq.Array(weekdays), active,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to create (duplicate name?)"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// Update aggiorna nome / orari / weekdays / active. Admin only.
func (h *ShiftsHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}
	var req CreateShiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !validHHMM(req.StartTime) || !validHHMM(req.EndTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_time and end_time must be in HH:MM format"})
		return
	}
	weekdays := req.Weekdays
	if len(weekdays) == 0 {
		weekdays = []int{1, 2, 3, 4, 5}
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	res, err := h.db.Exec(`
		UPDATE shifts SET name = $1, start_time = $2, end_time = $3,
		                  weekdays = $4, active = $5
		WHERE id = $6`,
		strings.TrimSpace(req.Name), req.StartTime, req.EndTime,
		pq.Array(weekdays), active, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shift not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Shift updated"})
}

// Delete rimuove un turno (CASCADE elimina anche le assegnazioni).
func (h *ShiftsHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}
	res, err := h.db.Exec(`DELETE FROM shifts WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shift not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ListAssignments per un turno specifico, joinati con users per ottenere
// username + full_name in una sola query.
func (h *ShiftsHandler) ListAssignments(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id"})
		return
	}
	rows, err := h.db.Query(`
		SELECT a.id, a.shift_id, a.user_id, u.username, COALESCE(u.full_name, ''),
		       a.valid_from, a.valid_to
		FROM shift_assignments a
		JOIN users u ON u.id = a.user_id
		WHERE a.shift_id = $1
		ORDER BY a.valid_from DESC, u.username`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list assignments"})
		return
	}
	defer rows.Close()
	out := []ShiftAssignment{}
	for rows.Next() {
		var a ShiftAssignment
		if err := rows.Scan(&a.ID, &a.ShiftID, &a.UserID, &a.Username, &a.FullName, &a.ValidFrom, &a.ValidTo); err != nil {
			continue
		}
		out = append(out, a)
	}
	c.JSON(http.StatusOK, out)
}

// CreateAssignmentRequest è il body di POST assignments.
type CreateAssignmentRequest struct {
	UserID    int     `json:"user_id" binding:"required"`
	ValidFrom string  `json:"valid_from"` // "YYYY-MM-DD"; vuoto = oggi
	ValidTo   *string `json:"valid_to"`   // "YYYY-MM-DD" o omesso
}

// CreateAssignment assegna un operatore a un turno per un periodo.
func (h *ShiftsHandler) CreateAssignment(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid shift id"})
		return
	}
	var req CreateAssignmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	from := time.Now().UTC()
	if req.ValidFrom != "" {
		t, err := time.Parse("2006-01-02", req.ValidFrom)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid_from must be YYYY-MM-DD"})
			return
		}
		from = t
	}
	var to *time.Time
	if req.ValidTo != nil && *req.ValidTo != "" {
		t, err := time.Parse("2006-01-02", *req.ValidTo)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid_to must be YYYY-MM-DD"})
			return
		}
		to = &t
	}
	var aid int
	err = h.db.QueryRow(`
		INSERT INTO shift_assignments (shift_id, user_id, valid_from, valid_to)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		id, req.UserID, from, to,
	).Scan(&aid)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to assign (duplicate? user exists?)"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": aid})
}

// DeleteAssignment rimuove un'assegnazione specifica.
func (h *ShiftsHandler) DeleteAssignment(c *gin.Context) {
	aid, err := strconv.Atoi(c.Param("aid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid assignment id"})
		return
	}
	res, err := h.db.Exec(`DELETE FROM shift_assignments WHERE id = $1`, aid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete assignment"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Assignment not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// Current risponde alla domanda "che turno è adesso?". Cerca il primo
// turno attivo il cui weekday include oggi e il cui orario contiene
// l'ora attuale (gestendo il wraparound mezzanotte). Se trova match,
// ritorna anche gli operatori designati per oggi.
func (h *ShiftsHandler) Current(c *gin.Context) {
	now := time.Now().UTC()
	weekday := int(now.Weekday())                  // 0=Sun..6=Sat
	prevWeekday := (weekday + 6) % 7                // weekday di "ieri"
	nowMin := now.Hour()*60 + now.Minute()

	rows, err := h.db.Query(`
		SELECT id, name, start_time::text, end_time::text, weekdays, active
		FROM shifts WHERE active = true`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load shifts"})
		return
	}
	defer rows.Close()

	var match *Shift
	var startMin int
	for rows.Next() {
		var s Shift
		var weekdays pq.Int64Array
		if err := rows.Scan(&s.ID, &s.Name, &s.StartTime, &s.EndTime, &weekdays, &s.Active); err != nil {
			continue
		}
		s.StartTime = trimSeconds(s.StartTime)
		s.EndTime = trimSeconds(s.EndTime)
		s.Weekdays = int64sToInts(weekdays)
		s.Wraps = wrapsMidnight(s.StartTime, s.EndTime)
		sMin := minutesOfDay(s.StartTime)
		eMin := minutesOfDay(s.EndTime)

		// Caso 1: turno NON wrap (08:00-16:00). Match se oggi è nei
		// weekday del turno E ora attuale è tra start e end.
		if !s.Wraps {
			if contains(s.Weekdays, weekday) && nowMin >= sMin && nowMin < eMin {
				m := s
				match = &m
				startMin = sMin
				break
			}
			continue
		}
		// Caso 2: turno wrap (22:00-06:00).
		//   a) Inizio in "oggi" (es. è venerdì 23:00, turno notte di venerdì).
		if contains(s.Weekdays, weekday) && nowMin >= sMin {
			m := s
			match = &m
			startMin = sMin
			break
		}
		//   b) Inizio in "ieri" (es. è sabato 04:00, turno notte di venerdì).
		if contains(s.Weekdays, prevWeekday) && nowMin < eMin {
			m := s
			match = &m
			startMin = sMin - 24*60 // start ieri
			break
		}
	}

	resp := CurrentShift{Operators: []ShiftAssignment{}}
	if match == nil {
		c.JSON(http.StatusOK, resp)
		return
	}
	resp.Shift = match

	// Calcola started_at + ends_at proiettando sull'oggi (o ieri per il
	// wrap-b). Approssimazione UTC; sufficiente per la dashboard.
	startedAt := time.Date(now.Year(), now.Month(), now.Day(),
		startMin/60, startMin%60, 0, 0, time.UTC)
	endMin := minutesOfDay(match.EndTime)
	if match.Wraps && startMin >= 0 {
		endMin += 24 * 60 // ends in the next calendar day
	}
	endsAt := time.Date(startedAt.Year(), startedAt.Month(), startedAt.Day(),
		endMin/60, endMin%60, 0, 0, time.UTC)
	resp.StartedAt = &startedAt
	resp.EndsAt = &endsAt
	resp.TimeLeftMin = int(endsAt.Sub(now).Minutes())

	// Operatori designati per oggi.
	aRows, err := h.db.Query(`
		SELECT a.id, a.shift_id, a.user_id, u.username, COALESCE(u.full_name, ''),
		       a.valid_from, a.valid_to
		FROM shift_assignments a
		JOIN users u ON u.id = a.user_id
		WHERE a.shift_id = $1
		  AND a.valid_from <= CURRENT_DATE
		  AND (a.valid_to IS NULL OR a.valid_to >= CURRENT_DATE)
		ORDER BY u.username`, match.ID)
	if err == nil {
		defer aRows.Close()
		for aRows.Next() {
			var a ShiftAssignment
			if err := aRows.Scan(&a.ID, &a.ShiftID, &a.UserID, &a.Username, &a.FullName, &a.ValidFrom, &a.ValidTo); err == nil {
				resp.Operators = append(resp.Operators, a)
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// ── helpers privati ────────────────────────────────────────────────────────

// trimSeconds toglie i ":SS" da "HH:MM:SS" per restituire "HH:MM".
func trimSeconds(t string) string {
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

// validHHMM valida che la stringa sia nel formato "HH:MM".
func validHHMM(t string) bool {
	if len(t) != 5 || t[2] != ':' {
		return false
	}
	h, err1 := strconv.Atoi(t[:2])
	m, err2 := strconv.Atoi(t[3:])
	return err1 == nil && err2 == nil && h >= 0 && h < 24 && m >= 0 && m < 60
}

// minutesOfDay converte "HH:MM" in numero di minuti da mezzanotte.
func minutesOfDay(t string) int {
	if len(t) < 5 {
		return 0
	}
	h, _ := strconv.Atoi(t[:2])
	m, _ := strconv.Atoi(t[3:5])
	return h*60 + m
}

// wrapsMidnight è true quando il turno attraversa la mezzanotte
// (es. 22:00-06:00). Calcolato come start > end.
func wrapsMidnight(start, end string) bool {
	return minutesOfDay(start) > minutesOfDay(end)
}

// contains è true se il valore appare nello slice. Lista weekdays è
// sempre piccola (max 7), niente bisogno di optimizzazione.
func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// int64sToInts converte un pq.Int64Array nel []int che la response usa.
func int64sToInts(in pq.Int64Array) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

// Force-import to keep fmt around for future error formatting helpers.
var _ = fmt.Sprintf
