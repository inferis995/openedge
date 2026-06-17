// Package handlers — synoptics.
//
// A synoptic is an org-scoped SCADA mimic page: a canvas with a background
// color, fixed dimensions, and a JSON array of freely positioned widgets.
// Each widget can bind to a tag; at runtime the live value (delivered over
// the existing realtime WebSocket) drives the widget's visual state.
//
// The whole layout is persisted as JSONB in a single column, so the designer
// saves the entire page atomically — no per-widget sub-CRUD. The backend
// treats the layout as opaque: the widget schema lives in the frontend.
//
// Multi-tenant: every endpoint is scoped to the caller's organization via
// middleware.GetOrganizationID. Reads are open to any org member; writes are
// restricted to admins at the route level.
package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// SynopticsHandler owns the /api/synoptics endpoints.
type SynopticsHandler struct {
	db *sql.DB
}

func NewSynopticsHandler(db *sql.DB) *SynopticsHandler {
	return &SynopticsHandler{db: db}
}

// Synoptic is the wire shape for a mimic page. Layout is raw JSON (the widget
// array) so the backend never has to understand the widget schema.
type Synoptic struct {
	ID              int             `json:"id"`
	OrgID           int             `json:"org_id"`
	SiteID          *int            `json:"site_id"`
	AreaID          *int            `json:"area_id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	BackgroundColor string          `json:"background_color"`
	CanvasW         int             `json:"canvas_w"`
	CanvasH         int             `json:"canvas_h"`
	Layout          json.RawMessage `json:"layout"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// synopticRequest is the create/update body. Layout is optional on update of
// metadata-only edits; when omitted it is left untouched.
type synopticRequest struct {
	SiteID          *int            `json:"site_id"`
	AreaID          *int            `json:"area_id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	BackgroundColor string          `json:"background_color"`
	CanvasW         int             `json:"canvas_w"`
	CanvasH         int             `json:"canvas_h"`
	Layout          json.RawMessage `json:"layout"`
}

// List returns all synoptics for the caller's org (header fields + layout so
// the index can render thumbnails without a second round-trip).
func (h *SynopticsHandler) List(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT id, org_id, site_id, area_id, name, COALESCE(description,''), background_color,
		       canvas_w, canvas_h, layout, created_at, updated_at
		FROM synoptics WHERE org_id = $1 ORDER BY name`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("[synoptics] rows.Close: %v", cerr)
		}
	}()

	items := make([]Synoptic, 0)
	for rows.Next() {
		var s Synoptic
		if err := rows.Scan(&s.ID, &s.OrgID, &s.SiteID, &s.AreaID, &s.Name, &s.Description, &s.BackgroundColor,
			&s.CanvasW, &s.CanvasH, &s.Layout, &s.CreatedAt, &s.UpdatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		items = append(items, s)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// Get returns a single synoptic, scoped to the caller's org.
func (h *SynopticsHandler) Get(c *gin.Context) {
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

	var s Synoptic
	err = h.db.QueryRowContext(c.Request.Context(), `
		SELECT id, org_id, site_id, area_id, name, COALESCE(description,''), background_color,
		       canvas_w, canvas_h, layout, created_at, updated_at
		FROM synoptics WHERE id = $1 AND org_id = $2`, id, orgID).
		Scan(&s.ID, &s.OrgID, &s.SiteID, &s.AreaID, &s.Name, &s.Description, &s.BackgroundColor,
			&s.CanvasW, &s.CanvasH, &s.Layout, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "synoptic not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

// Create inserts a new synoptic for the caller's org.
func (h *SynopticsHandler) Create(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	var req synopticRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	applyDefaults(&req)

	var createdBy interface{}
	if uid, ok := middleware.GetUserID(c); ok {
		createdBy = uid
	}

	var id int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO synoptics (org_id, site_id, area_id, name, description, background_color, canvas_w, canvas_h, layout, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		orgID, req.SiteID, req.AreaID, req.Name, req.Description, req.BackgroundColor, req.CanvasW, req.CanvasH, req.Layout, createdBy).
		Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// Update overwrites a synoptic (metadata + layout) for the caller's org.
func (h *SynopticsHandler) Update(c *gin.Context) {
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
	var req synopticRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	applyDefaults(&req)

	res, err := h.db.ExecContext(c.Request.Context(), `
		UPDATE synoptics
		SET name = $1, description = $2, background_color = $3,
		    canvas_w = $4, canvas_h = $5, layout = $6, site_id = $7, area_id = $8, updated_at = NOW()
		WHERE id = $9 AND org_id = $10`,
		req.Name, req.Description, req.BackgroundColor, req.CanvasW, req.CanvasH, req.Layout, req.SiteID, req.AreaID, id, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "synoptic not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// Delete removes a synoptic, scoped to the caller's org.
func (h *SynopticsHandler) Delete(c *gin.Context) {
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
	res, err := h.db.ExecContext(c.Request.Context(),
		`DELETE FROM synoptics WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "synoptic not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// applyDefaults fills sensible defaults so a partial body still produces a
// valid row (empty layout, dark canvas, 1280x720).
func applyDefaults(req *synopticRequest) {
	if req.BackgroundColor == "" {
		req.BackgroundColor = "#0f172a"
	}
	if req.CanvasW <= 0 {
		req.CanvasW = 1280
	}
	if req.CanvasH <= 0 {
		req.CanvasH = 720
	}
	if len(req.Layout) == 0 {
		req.Layout = json.RawMessage("[]")
	}
}
