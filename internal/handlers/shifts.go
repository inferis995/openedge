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

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/shifts"
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
	// Platform: a default shift of the installation (no organization). It
	// applies to an organization until that defines its own, and only the
	// global administrator edits it.
	Platform bool `json:"platform"`
}

// shiftScope is whose shifts the caller sees.
func shiftScope(c *gin.Context) shifts.Scope {
	if orgID, ok := middleware.GetOrganizationID(c); ok {
		return shifts.Scope{OrgID: orgID}
	}
	if middleware.IsGlobalAdmin(c) {
		return shifts.Scope{All: true}
	}
	return shifts.Scope{OrgID: -1}
}

// canEditShift says whether the caller may change shift id: their
// organization's own, or any for the global administrator. It writes the
// response when not.
func (h *ShiftsHandler) canEditShift(c *gin.Context, id int) bool {
	var org sql.NullInt64
	err := h.db.QueryRowContext(c.Request.Context(), `SELECT org_id FROM shifts WHERE id = $1`, id).Scan(&org)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shift not found"})
		return false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read the shift"})
		return false
	}
	if middleware.IsGlobalAdmin(c) {
		return true
	}
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok || !org.Valid || int(org.Int64) != orgID {
		// A platform default or another organization's: not this caller's
		// to change. 404 for another's, so ids of other tenants' shifts are
		// not confirmed.
		if org.Valid {
			c.JSON(http.StatusNotFound, gin.H{"error": "Shift not found"})
		} else {
			c.JSON(http.StatusForbidden, gin.H{"error": "default shifts are changed by the platform administrator; create your own shifts instead"})
		}
		return false
	}
	return true
}

// ShiftAssignment associa un operatore (user) a un turno per un periodo.
// ValidTo null = a tempo indeterminato.
type ShiftAssignment struct {
	ID        int        `json:"id"`
	ShiftID   int        `json:"shift_id"`
	UserID    int        `json:"user_id"`
	Username  string     `json:"username,omitempty"`
	FullName  string     `json:"full_name,omitempty"`
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

// CurrentShift è la risposta a "che turno è adesso?". Operators è la
// lista degli operatori assegnati al turno per oggi (valid_from <= today
// <= valid_to OR valid_to NULL). Vuota = "nessun operatore designato".
type CurrentShift struct {
	Shift       *Shift            `json:"shift"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	EndsAt      *time.Time        `json:"ends_at,omitempty"`
	TimeLeftMin int               `json:"time_left_min"`
	Operators   []ShiftAssignment `json:"operators"`
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
	scope := shiftScope(c)
	q := `SELECT id, name, start_time::text, end_time::text, weekdays, active, org_id IS NULL
		FROM shifts`
	var args []interface{}
	if !scope.All {
		q += ` WHERE org_id = $1 OR org_id IS NULL`
		args = append(args, scope.OrgID)
	}
	rows, err := h.db.QueryContext(c.Request.Context(), q+` ORDER BY start_time, name`, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list shifts"})
		return
	}
	defer rows.Close()
	out := []Shift{}
	for rows.Next() {
		var s Shift
		var weekdays pq.Int64Array
		if err := rows.Scan(&s.ID, &s.Name, &s.StartTime, &s.EndTime, &weekdays, &s.Active, &s.Platform); err != nil {
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
	// The caller's organization's; a global admin with none selected
	// creates a platform default.
	var org interface{}
	if orgID, ok := middleware.GetOrganizationID(c); ok {
		org = orgID
	} else if !middleware.IsGlobalAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context required"})
		return
	}
	var id int
	err := h.db.QueryRow(`
		INSERT INTO shifts (name, start_time, end_time, weekdays, active, org_id)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		strings.TrimSpace(req.Name), req.StartTime, req.EndTime, pq.Array(weekdays), active, org,
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
	if !h.canEditShift(c, id) {
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
	if !h.canEditShift(c, id) {
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
	// Who works a shift is the organization's business: another tenant's
	// people are not listed, even on a shared platform default.
	scope := shiftScope(c)
	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT a.id, a.shift_id, a.user_id, u.username, COALESCE(u.full_name, ''),
		       a.valid_from, a.valid_to
		FROM shift_assignments a
		JOIN users u ON u.id = a.user_id
		JOIN shifts s ON s.id = a.shift_id
		WHERE a.shift_id = $1
		  AND ($2 OR ((s.org_id = $3 OR s.org_id IS NULL) AND u.org_id = $3))
		ORDER BY a.valid_from DESC, u.username`, id, scope.All, scope.OrgID)
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
	from := time.Now()
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
	// The shift must be one the caller sees, and the person one of theirs:
	// assigning another tenant's user, or to another tenant's shift, was
	// accepted.
	scope := shiftScope(c)
	var allowed bool
	if chkErr := h.db.QueryRowContext(c.Request.Context(), `
		SELECT EXISTS (SELECT 1 FROM shifts s, users u
		               WHERE s.id = $1 AND u.id = $2
		                 AND ($3 OR ((s.org_id = $4 OR s.org_id IS NULL) AND u.org_id = $4)))`,
		id, req.UserID, scope.All, scope.OrgID).Scan(&allowed); chkErr != nil || !allowed {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shift or user not found"})
		return
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
	scope := shiftScope(c)
	res, err := h.db.ExecContext(c.Request.Context(), `
		DELETE FROM shift_assignments a USING shifts s, users u
		WHERE a.id = $1 AND s.id = a.shift_id AND u.id = a.user_id
		  AND ($2 OR ((s.org_id = $3 OR s.org_id IS NULL) AND u.org_id = $3))`, aid, scope.All, scope.OrgID)
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
	scope := shiftScope(c)
	list, err := shifts.Load(c.Request.Context(), h.db, scope)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load shifts"})
		return
	}
	now := time.Now()
	resp := CurrentShift{Operators: []ShiftAssignment{}}
	sh, startedAt, endsAt := shifts.ActiveAt(list, now)
	if sh == nil {
		c.JSON(http.StatusOK, resp)
		return
	}
	resp.Shift = &Shift{
		ID: sh.ID, Name: sh.Name,
		StartTime: fmt.Sprintf("%02d:%02d", sh.StartMin/60, sh.StartMin%60),
		EndTime:   fmt.Sprintf("%02d:%02d", sh.EndMin/60, sh.EndMin%60),
		Weekdays:  sh.Weekdays, Active: true, Wraps: sh.Wraps,
	}
	resp.StartedAt = &startedAt
	resp.EndsAt = &endsAt
	resp.TimeLeftMin = int(endsAt.Sub(now).Minutes())

	aRows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT a.id, a.shift_id, a.user_id, u.username, COALESCE(u.full_name, ''),
		       a.valid_from, a.valid_to
		FROM shift_assignments a
		JOIN users u ON u.id = a.user_id
		WHERE a.shift_id = $1
		  AND a.valid_from <= CURRENT_DATE
		  AND (a.valid_to IS NULL OR a.valid_to >= CURRENT_DATE)
		  AND ($2 OR u.org_id = $3)
		ORDER BY u.username`, sh.ID, scope.All, scope.OrgID)
	if err == nil {
		defer aRows.Close()
		for aRows.Next() {
			var a ShiftAssignment
			if scanErr := aRows.Scan(&a.ID, &a.ShiftID, &a.UserID, &a.Username, &a.FullName, &a.ValidFrom, &a.ValidTo); scanErr == nil {
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
