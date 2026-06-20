package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// CSIRTHandler handles Art.23 NIS2 incident reporting endpoints.
type CSIRTHandler struct {
	db *sql.DB
}

// NewCSIRTHandler creates a new CSIRT handler.
func NewCSIRTHandler(db *sql.DB) *CSIRTHandler {
	return &CSIRTHandler{db: db}
}

// CSIRTIncident is the full incident struct returned from the DB with computed fields.
type CSIRTIncident struct {
	ID                   int        `json:"id"`
	OrgID                int        `json:"org_id"`
	ThreatEventID        *int       `json:"threat_event_id"`
	Title                string     `json:"title"`
	Description          *string    `json:"description"`
	Severity             string     `json:"severity"`
	Status               string     `json:"status"`
	AffectedSystems      *string    `json:"affected_systems"`
	ImpactDescription    *string    `json:"impact_description"`
	EarlyWarningDue      time.Time  `json:"early_warning_due"`
	NotificationDue      time.Time  `json:"notification_due"`
	FinalReportDue       time.Time  `json:"final_report_due"`
	EarlyWarningSentAt   *time.Time `json:"early_warning_sent_at"`
	NotificationSentAt   *time.Time `json:"notification_sent_at"`
	FinalReportSentAt    *time.Time `json:"final_report_sent_at"`
	RootCause            *string    `json:"root_cause"`
	Remediation          *string    `json:"remediation"`
	CreatedBy            *int       `json:"created_by"`
	ClosedBy             *int       `json:"closed_by"`
	ClosedAt             *time.Time `json:"closed_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	// Computed deadline fields
	EarlyWarningOverdue    bool    `json:"early_warning_overdue"`
	NotificationOverdue    bool    `json:"notification_overdue"`
	FinalReportOverdue     bool    `json:"final_report_overdue"`
	EarlyWarningRemainingH float64 `json:"early_warning_remaining_h"`
	NotificationRemainingH float64 `json:"notification_remaining_h"`
	FinalReportRemainingH  float64 `json:"final_report_remaining_h"`
}

// CreateIncidentRequest holds the body for creating an incident.
type CreateIncidentRequest struct {
	ThreatEventID     *int    `json:"threat_event_id"`
	Title             string  `json:"title" binding:"required"`
	Description       *string `json:"description"`
	Severity          string  `json:"severity"`
	AffectedSystems   *string `json:"affected_systems"`
	ImpactDescription *string `json:"impact_description"`
}

// UpdateIncidentRequest holds the body for updating an incident.
type UpdateIncidentRequest struct {
	Title             *string `json:"title"`
	Description       *string `json:"description"`
	Severity          *string `json:"severity"`
	Status            *string `json:"status"`
	AffectedSystems   *string `json:"affected_systems"`
	ImpactDescription *string `json:"impact_description"`
	RootCause         *string `json:"root_cause"`
	Remediation       *string `json:"remediation"`
}

const csirtSelectCols = `
	id, org_id, threat_event_id, title, description, severity, status,
	affected_systems, impact_description,
	early_warning_due, notification_due, final_report_due,
	early_warning_sent_at, notification_sent_at, final_report_sent_at,
	root_cause, remediation, created_by, closed_by, closed_at,
	created_at, updated_at`

func scanCSIRTIncident(row interface {
	Scan(...interface{}) error
}) (*CSIRTIncident, error) {
	var i CSIRTIncident
	err := row.Scan(
		&i.ID, &i.OrgID, &i.ThreatEventID, &i.Title, &i.Description,
		&i.Severity, &i.Status, &i.AffectedSystems, &i.ImpactDescription,
		&i.EarlyWarningDue, &i.NotificationDue, &i.FinalReportDue,
		&i.EarlyWarningSentAt, &i.NotificationSentAt, &i.FinalReportSentAt,
		&i.RootCause, &i.Remediation, &i.CreatedBy, &i.ClosedBy, &i.ClosedAt,
		&i.CreatedAt, &i.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	i.computeDeadlines()
	return &i, nil
}

func (i *CSIRTIncident) computeDeadlines() {
	now := time.Now()
	i.EarlyWarningRemainingH = i.EarlyWarningDue.Sub(now).Hours()
	i.NotificationRemainingH = i.NotificationDue.Sub(now).Hours()
	i.FinalReportRemainingH = i.FinalReportDue.Sub(now).Hours()
	i.EarlyWarningOverdue = now.After(i.EarlyWarningDue) && i.EarlyWarningSentAt == nil
	i.NotificationOverdue = now.After(i.NotificationDue) && i.NotificationSentAt == nil
	i.FinalReportOverdue = now.After(i.FinalReportDue) && i.FinalReportSentAt == nil
}

// ListIncidents GET /api/compliance/csirt
func (h *CSIRTHandler) ListIncidents(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	query := `SELECT ` + csirtSelectCols + ` FROM csirt_incidents WHERE org_id = $1`
	args := []interface{}{orgID}
	argIdx := 2

	if status := c.Query("status"); status != "" {
		query += ` AND status = $` + strconv.Itoa(argIdx)
		args = append(args, status)
		argIdx++
	}
	if severity := c.Query("severity"); severity != "" {
		query += ` AND severity = $` + strconv.Itoa(argIdx)
		args = append(args, severity)
		argIdx++
	}
	_ = argIdx
	query += ` ORDER BY created_at DESC`

	rows, err := h.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var incidents []*CSIRTIncident
	for rows.Next() {
		inc, err := scanCSIRTIncident(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		incidents = append(incidents, inc)
	}
	if incidents == nil {
		incidents = []*CSIRTIncident{}
	}
	c.JSON(http.StatusOK, incidents)
}

// CreateIncident POST /api/compliance/csirt
func (h *CSIRTHandler) CreateIncident(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	userID, _ := middleware.GetUserID(c)

	var req CreateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	severity := req.Severity
	if severity == "" {
		severity = "high"
	}

	now := time.Now()
	earlyWarningDue := now.Add(24 * time.Hour)
	notificationDue := now.Add(72 * time.Hour)
	finalReportDue := now.Add(30 * 24 * time.Hour)

	var id int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO csirt_incidents
			(org_id, threat_event_id, title, description, severity, status,
			 affected_systems, impact_description,
			 early_warning_due, notification_due, final_report_due,
			 created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'open',$6,$7,$8,$9,$10,$11,now(),now())
		RETURNING id`,
		orgID, req.ThreatEventID, req.Title, req.Description, severity,
		req.AffectedSystems, req.ImpactDescription,
		earlyWarningDue, notificationDue, finalReportDue,
		userID,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	inc, err := h.getByID(c, orgID, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, inc)
}

// GetIncident GET /api/compliance/csirt/:id
func (h *CSIRTHandler) GetIncident(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	inc, err := h.getByID(c, orgID, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, inc)
}

// UpdateIncident PUT /api/compliance/csirt/:id
func (h *CSIRTHandler) UpdateIncident(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req UpdateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = h.db.ExecContext(c.Request.Context(), `
		UPDATE csirt_incidents SET
			title             = COALESCE($3, title),
			description       = COALESCE($4, description),
			severity          = COALESCE($5, severity),
			status            = COALESCE($6, status),
			affected_systems  = COALESCE($7, affected_systems),
			impact_description= COALESCE($8, impact_description),
			root_cause        = COALESCE($9, root_cause),
			remediation       = COALESCE($10, remediation),
			updated_at        = now()
		WHERE id = $1 AND org_id = $2`,
		id, orgID,
		req.Title, req.Description, req.Severity, req.Status,
		req.AffectedSystems, req.ImpactDescription, req.RootCause, req.Remediation,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	inc, err := h.getByID(c, orgID, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, inc)
}

// MarkEarlyWarning PUT /api/compliance/csirt/:id/early-warning
func (h *CSIRTHandler) MarkEarlyWarning(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, err = h.db.ExecContext(c.Request.Context(), `
		UPDATE csirt_incidents
		SET early_warning_sent_at = now(),
		    status = CASE WHEN status = 'open' THEN 'early_warning_sent' ELSE status END,
		    updated_at = now()
		WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	inc, err := h.getByID(c, orgID, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, inc)
}

// MarkNotification PUT /api/compliance/csirt/:id/notify
func (h *CSIRTHandler) MarkNotification(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, err = h.db.ExecContext(c.Request.Context(), `
		UPDATE csirt_incidents
		SET notification_sent_at = now(),
		    status = CASE WHEN status IN ('open','early_warning_sent') THEN 'notified' ELSE status END,
		    updated_at = now()
		WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	inc, err := h.getByID(c, orgID, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, inc)
}

// CloseIncident PUT /api/compliance/csirt/:id/close
func (h *CSIRTHandler) CloseIncident(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	userID, _ := middleware.GetUserID(c)

	_, err = h.db.ExecContext(c.Request.Context(), `
		UPDATE csirt_incidents
		SET status = 'closed',
		    closed_at = now(),
		    closed_by = $3,
		    updated_at = now()
		WHERE id = $1 AND org_id = $2`, id, orgID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	inc, err := h.getByID(c, orgID, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, inc)
}

// Summary GET /api/compliance/csirt/summary
func (h *CSIRTHandler) Summary(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	type SummaryRow struct {
		Total               int `json:"total"`
		Open                int `json:"open"`
		OverdueEarlyWarning int `json:"overdue_early_warning"`
		OverdueNotification int `json:"overdue_notification"`
		OverdueFinal        int `json:"overdue_final"`
	}
	var s SummaryRow

	err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE status != 'closed')::int,
			COUNT(*) FILTER (WHERE status != 'closed' AND early_warning_due < now() AND early_warning_sent_at IS NULL)::int,
			COUNT(*) FILTER (WHERE status != 'closed' AND notification_due < now() AND notification_sent_at IS NULL)::int,
			COUNT(*) FILTER (WHERE status != 'closed' AND final_report_due < now() AND final_report_sent_at IS NULL)::int
		FROM csirt_incidents WHERE org_id = $1`, orgID,
	).Scan(&s.Total, &s.Open, &s.OverdueEarlyWarning, &s.OverdueNotification, &s.OverdueFinal)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

// getByID is an internal helper to fetch a single incident scoped to an org.
func (h *CSIRTHandler) getByID(c *gin.Context, orgID, id int) (*CSIRTIncident, error) {
	row := h.db.QueryRowContext(c.Request.Context(),
		`SELECT `+csirtSelectCols+` FROM csirt_incidents WHERE id = $1 AND org_id = $2`,
		id, orgID)
	return scanCSIRTIncident(row)
}
