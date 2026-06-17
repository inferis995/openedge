package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/audit"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// UpdatesHandler manages OTA release publishing, org approval, and edge polling.
type UpdatesHandler struct {
	db *sql.DB
}

// NewUpdatesHandler creates a new UpdatesHandler.
func NewUpdatesHandler(db *sql.DB) *UpdatesHandler {
	return &UpdatesHandler{db: db}
}

// sha256Re validates that a string is exactly 64 lowercase hex chars.
var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ─── Super admin endpoints ──────────────────────────────────────────────────

// ListReleases handles GET /api/releases — returns all releases with per-org status counts.
// Requires global admin.
func (h *UpdatesHandler) ListReleases(c *gin.Context) {
	if !middleware.IsGlobalAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "global admin required"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT
			r.id, r.version, r.release_notes, r.artifact_url, r.sha256_checksum,
			r.published_at, r.is_stable,
			COALESCE(SUM(CASE WHEN a.status='pending'  THEN 1 ELSE 0 END), 0) AS cnt_pending,
			COALESCE(SUM(CASE WHEN a.status='approved' THEN 1 ELSE 0 END), 0) AS cnt_approved,
			COALESCE(SUM(CASE WHEN a.status='success'  THEN 1 ELSE 0 END), 0) AS cnt_success,
			COALESCE(SUM(CASE WHEN a.status='failed'   THEN 1 ELSE 0 END), 0) AS cnt_failed
		FROM edge_releases r
		LEFT JOIN org_update_approvals a ON a.release_id = r.id
		GROUP BY r.id
		ORDER BY r.published_at DESC
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer func() { _ = rows.Close() }()

	type releaseRow struct {
		ID           int    `json:"id"`
		Version      string `json:"version"`
		ReleaseNotes string `json:"release_notes"`
		ArtifactURL  string `json:"artifact_url"`
		SHA256       string `json:"sha256_checksum"`
		PublishedAt  time.Time `json:"published_at"`
		IsStable     bool   `json:"is_stable"`
		OrgStatusCounts struct {
			Pending  int `json:"pending"`
			Approved int `json:"approved"`
			Success  int `json:"success"`
			Failed   int `json:"failed"`
		} `json:"org_status_counts"`
	}

	var results []releaseRow
	for rows.Next() {
		var r releaseRow
		if err := rows.Scan(
			&r.ID, &r.Version, &r.ReleaseNotes, &r.ArtifactURL, &r.SHA256,
			&r.PublishedAt, &r.IsStable,
			&r.OrgStatusCounts.Pending, &r.OrgStatusCounts.Approved,
			&r.OrgStatusCounts.Success, &r.OrgStatusCounts.Failed,
		); err != nil {
			continue
		}
		results = append(results, r)
	}
	if results == nil {
		results = []releaseRow{}
	}
	c.JSON(http.StatusOK, results)
}

// CreateRelease handles POST /api/releases — publishes a new edge release.
// On success, auto-inserts pending approval rows for every existing org.
// Requires global admin.
func (h *UpdatesHandler) CreateRelease(c *gin.Context) {
	if !middleware.IsGlobalAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "global admin required"})
		return
	}

	var body struct {
		Version      string `json:"version"       binding:"required"`
		ReleaseNotes string `json:"release_notes"`
		ArtifactURL  string `json:"artifact_url"  binding:"required"`
		SHA256       string `json:"sha256_checksum" binding:"required"`
		IsStable     bool   `json:"is_stable"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Normalise SHA256 to lowercase and validate.
	sha := body.SHA256
	if len(sha) == 64 {
		// tolerate uppercase input
		lower := make([]byte, 64)
		for i := 0; i < 64; i++ {
			b := sha[i]
			if b >= 'A' && b <= 'F' {
				b += 32
			}
			lower[i] = b
		}
		sha = string(lower)
	}
	if !sha256Re.MatchString(sha) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sha256_checksum must be exactly 64 lowercase hex characters"})
		return
	}

	userID, _ := middleware.GetUserID(c)

	var releaseID int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO edge_releases (version, release_notes, artifact_url, sha256_checksum, published_by, is_stable)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, body.Version, body.ReleaseNotes, body.ArtifactURL, sha, userID, body.IsStable).Scan(&releaseID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "release version already exists or db error: " + err.Error()})
		return
	}

	// Auto-insert pending approval rows for every existing org (best-effort).
	go func() {
		rows, err := h.db.QueryContext(context.Background(), `SELECT id FROM organizations ORDER BY id`)
		if err != nil {
			log.Printf("[UPDATES] failed to fetch orgs for release %d: %v", releaseID, err)
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var orgID int
			if err := rows.Scan(&orgID); err != nil {
				continue
			}
			_, _ = h.db.ExecContext(context.Background(), `
				INSERT INTO org_update_approvals (org_id, release_id, status)
				VALUES ($1, $2, 'pending')
				ON CONFLICT (org_id, release_id) DO NOTHING
			`, orgID, releaseID)
		}
	}()

	audit.Log(c, h.db, audit.Entry{
		Action:  "release.publish",
		Success: true,
		Details: map[string]interface{}{
			"release_id": releaseID,
			"version":    body.Version,
			"is_stable":  body.IsStable,
		},
	})

	c.JSON(http.StatusCreated, gin.H{"id": releaseID})
}

// GetFleetUpdateStatus handles GET /api/releases/fleet — returns per-org latest release status.
// Requires global admin.
func (h *UpdatesHandler) GetFleetUpdateStatus(c *gin.Context) {
	if !middleware.IsGlobalAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "global admin required"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT
			o.id AS org_id,
			o.name AS org_name,
			r.version,
			a.status,
			a.approved_at,
			a.completed_at,
			a.error_msg
		FROM organizations o
		JOIN org_update_approvals a ON a.org_id = o.id
		JOIN edge_releases r ON r.id = a.release_id
		WHERE a.release_id = (
			SELECT MAX(release_id) FROM org_update_approvals WHERE org_id = o.id
		)
		ORDER BY o.name
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer func() { _ = rows.Close() }()

	type fleetRow struct {
		OrgID       int        `json:"org_id"`
		OrgName     string     `json:"org_name"`
		Version     string     `json:"version"`
		Status      string     `json:"status"`
		ApprovedAt  *time.Time `json:"approved_at,omitempty"`
		CompletedAt *time.Time `json:"completed_at,omitempty"`
		ErrorMsg    *string    `json:"error_msg,omitempty"`
	}

	var results []fleetRow
	for rows.Next() {
		var r fleetRow
		var approvedAt, completedAt sql.NullTime
		var errorMsg sql.NullString
		if err := rows.Scan(
			&r.OrgID, &r.OrgName, &r.Version, &r.Status,
			&approvedAt, &completedAt, &errorMsg,
		); err != nil {
			continue
		}
		if approvedAt.Valid {
			r.ApprovedAt = &approvedAt.Time
		}
		if completedAt.Valid {
			r.CompletedAt = &completedAt.Time
		}
		if errorMsg.Valid {
			r.ErrorMsg = &errorMsg.String
		}
		results = append(results, r)
	}
	if results == nil {
		results = []fleetRow{}
	}
	c.JSON(http.StatusOK, results)
}

// ─── Org admin endpoints ────────────────────────────────────────────────────

// GetPendingUpdate handles GET /api/organizations/:id/update.
// Returns the latest release info + org approval status.
func (h *UpdatesHandler) GetPendingUpdate(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}

	// Verify access: global admin or org admin of that org.
	if !middleware.IsGlobalAdmin(c) {
		ctxOrgID, ok := middleware.GetOrganizationID(c)
		if !ok || ctxOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	}

	type updateResp struct {
		Available    bool       `json:"available"`
		ReleaseID    *int       `json:"release_id,omitempty"`
		Version      *string    `json:"version,omitempty"`
		ReleaseNotes *string    `json:"release_notes,omitempty"`
		ArtifactURL  *string    `json:"artifact_url,omitempty"`
		SHA256       *string    `json:"sha256,omitempty"`
		Approved     bool       `json:"approved"`
		Status       *string    `json:"status,omitempty"`
		ApprovedAt   *time.Time `json:"approved_at,omitempty"`
		StartedAt    *time.Time `json:"started_at,omitempty"`
		CompletedAt  *time.Time `json:"completed_at,omitempty"`
		ErrorMsg     *string    `json:"error_msg,omitempty"`
	}

	var resp updateResp
	var releaseID int
	var version, releaseNotes, artifactURL, sha256, status string
	var approvedAt, startedAt, completedAt sql.NullTime
	var errorMsg sql.NullString

	err = h.db.QueryRowContext(c.Request.Context(), `
		SELECT
			r.id, r.version, r.release_notes, r.artifact_url, r.sha256_checksum,
			a.status, a.approved_at, a.started_at, a.completed_at, a.error_msg
		FROM edge_releases r
		JOIN org_update_approvals a ON a.release_id = r.id AND a.org_id = $1
		WHERE a.status NOT IN ('success')
		ORDER BY r.published_at DESC
		LIMIT 1
	`, orgID).Scan(
		&releaseID, &version, &releaseNotes, &artifactURL, &sha256,
		&status, &approvedAt, &startedAt, &completedAt, &errorMsg,
	)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, updateResp{Available: false, Approved: false})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	resp.Available = true
	resp.ReleaseID = &releaseID
	resp.Version = &version
	resp.ReleaseNotes = &releaseNotes
	resp.ArtifactURL = &artifactURL
	resp.SHA256 = &sha256
	resp.Status = &status
	resp.Approved = status == "approved" || status == "updating" || status == "success"
	if approvedAt.Valid {
		resp.ApprovedAt = &approvedAt.Time
	}
	if startedAt.Valid {
		resp.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		resp.CompletedAt = &completedAt.Time
	}
	if errorMsg.Valid {
		resp.ErrorMsg = &errorMsg.String
	}
	c.JSON(http.StatusOK, resp)
}

// ApproveUpdate handles POST /api/organizations/:id/approve-update.
// Sets approved_at=NOW(), status='approved', approved_by=userID.
func (h *UpdatesHandler) ApproveUpdate(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}

	// Verify access: global admin or org admin of that org.
	if !middleware.IsGlobalAdmin(c) {
		ctxOrgID, ok := middleware.GetOrganizationID(c)
		if !ok || ctxOrgID != orgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	}

	var body struct {
		ReleaseID int `json:"release_id" binding:"required"`
	}
	if bindErr := c.ShouldBindJSON(&body); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
		return
	}

	userID, _ := middleware.GetUserID(c)
	now := time.Now()

	res, err := h.db.ExecContext(c.Request.Context(), `
		UPDATE org_update_approvals
		SET status='approved', approved_at=$1, approved_by=$2
		WHERE org_id=$3 AND release_id=$4 AND status='pending'
	`, now, userID, orgID, body.ReleaseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "approval not found or already approved"})
		return
	}

	audit.Log(c, h.db, audit.Entry{
		Action:  "release.approve",
		Success: true,
		Details: map[string]interface{}{
			"org_id":     orgID,
			"release_id": body.ReleaseID,
		},
	})

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─── Edge agent endpoints ───────────────────────────────────────────────────

// EdgeUpdateCheck handles GET /api/edge/update-check.
// Query params: org_id, current_version.
// Only requires RequireAuth (not admin). Returns update info only when approved.
func (h *UpdatesHandler) EdgeUpdateCheck(c *gin.Context) {
	orgIDStr := c.Query("org_id")
	currentVersion := c.Query("current_version")

	orgID, err := strconv.Atoi(orgIDStr)
	if err != nil || orgID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org_id required"})
		return
	}

	type checkResp struct {
		UpdateAvailable bool    `json:"update_available"`
		ReleaseID       *int    `json:"release_id,omitempty"`
		Version         *string `json:"version,omitempty"`
		ArtifactURL     *string `json:"artifact_url,omitempty"`
		SHA256          *string `json:"sha256,omitempty"`
		ReleaseNotes    *string `json:"release_notes,omitempty"`
		Approved        bool    `json:"approved"`
	}

	var releaseID int
	var version, artifactURL, sha256, releaseNotes, status string

	err = h.db.QueryRowContext(c.Request.Context(), `
		SELECT r.id, r.version, r.artifact_url, r.sha256_checksum, r.release_notes, a.status
		FROM edge_releases r
		JOIN org_update_approvals a ON a.release_id = r.id AND a.org_id = $1
		WHERE a.status IN ('approved', 'updating')
		ORDER BY r.published_at DESC
		LIMIT 1
	`, orgID).Scan(&releaseID, &version, &artifactURL, &sha256, &releaseNotes, &status)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, checkResp{UpdateAvailable: false, Approved: false})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	// If current version matches the available release, no update needed.
	if currentVersion == version {
		c.JSON(http.StatusOK, checkResp{UpdateAvailable: false, Approved: true})
		return
	}

	approved := status == "approved" || status == "updating"
	c.JSON(http.StatusOK, checkResp{
		UpdateAvailable: true,
		ReleaseID:       &releaseID,
		Version:         &version,
		ArtifactURL:     &artifactURL,
		SHA256:          &sha256,
		ReleaseNotes:    &releaseNotes,
		Approved:        approved,
	})
}

// EdgeUpdateStatus handles POST /api/edge/update-status.
// Body: {org_id, release_id, status, error_msg?}
// Only requires RequireAuth.
func (h *UpdatesHandler) EdgeUpdateStatus(c *gin.Context) {
	var body struct {
		OrgID     int    `json:"org_id"     binding:"required"`
		ReleaseID int    `json:"release_id" binding:"required"`
		Status    string `json:"status"     binding:"required"`
		ErrorMsg  string `json:"error_msg"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validStatuses := map[string]bool{
		"updating": true, "success": true, "failed": true, "rolled_back": true,
	}
	if !validStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	now := time.Now()
	var err error

	switch body.Status {
	case "updating":
		_, err = h.db.ExecContext(c.Request.Context(), `
			UPDATE org_update_approvals
			SET status='updating', started_at=$1
			WHERE org_id=$2 AND release_id=$3
		`, now, body.OrgID, body.ReleaseID)
	case "success", "failed", "rolled_back":
		_, err = h.db.ExecContext(c.Request.Context(), `
			UPDATE org_update_approvals
			SET status=$1, completed_at=$2, error_msg=NULLIF($3,'')
			WHERE org_id=$4 AND release_id=$5
		`, body.Status, now, body.ErrorMsg, body.OrgID, body.ReleaseID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	// Log security event on failure or rollback.
	if body.Status == "failed" || body.Status == "rolled_back" {
		go func() {
			_, _ = h.db.ExecContext(context.Background(), `
				INSERT INTO security_events (org_id, event_type, severity, actor, resource, detail)
				VALUES ($1, $2, 'high', 'edge-agent', 'ota-update', jsonb_build_object(
					'release_id', $3::int,
					'status', $4,
					'error_msg', $5
				))
			`, body.OrgID, "ota_"+body.Status, body.ReleaseID, body.Status, body.ErrorMsg)
		}()
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
