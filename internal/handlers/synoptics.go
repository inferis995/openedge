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
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

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

// synopticRequest is the create/update body. Layout is optional on update:
// when omitted (or null) the stored drawing is left untouched.
type synopticRequest struct {
	SiteID          *int            `json:"site_id"`
	AreaID          *int            `json:"area_id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	BackgroundColor string          `json:"background_color"`
	CanvasW         int             `json:"canvas_w"`
	CanvasH         int             `json:"canvas_h"`
	Layout          json.RawMessage `json:"layout"`

	// ExpectedUpdatedAt carries the updated_at the client loaded. When present
	// the save is refused if the row has moved on since — see Update.
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
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
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list synoptics"})
		return
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
	req, ok := h.bindRequest(c, orgID)
	if !ok {
		return
	}
	if len(req.Layout) == 0 {
		req.Layout = json.RawMessage("[]")
	}

	var createdBy interface{}
	if uid, ok := middleware.GetUserID(c); ok {
		createdBy = uid
	}

	var id int
	err := h.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO synoptics (org_id, site_id, area_id, name, description, background_color, canvas_w, canvas_h, layout, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		orgID, req.SiteID, req.AreaID, req.Name, req.Description, req.BackgroundColor, req.CanvasW, req.CanvasH, []byte(req.Layout), createdBy).
		Scan(&id)
	if err != nil {
		writeSynopticDBError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// Update overwrites a synoptic (metadata + layout) for the caller's org.
//
// The response carries the row's new updated_at. The editor sends it back as
// expected_updated_at on its next save; without it, a second save from the
// same editor was always refused as a conflict with nobody else involved.
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
	req, ok := h.bindRequest(c, orgID)
	if !ok {
		return
	}
	// A body without a layout leaves the drawing as it is. It used to be
	// replaced by an empty one: a rename from a script erased the page, and
	// nothing versions synoptics.
	var layout interface{}
	if len(req.Layout) > 0 {
		layout = []byte(req.Layout)
	}

	// Refuse a save built on a stale copy.
	//
	// A synoptic is hours of work laid out by hand. Two engineers with the same
	// screen open used to both save successfully, and the second silently threw
	// away everything the first had done — no error, no warning, and no way
	// back, since nothing versions these. The client echoes the updated_at it
	// loaded; if the row has moved on since, the save is refused and the
	// operator is told to reload rather than losing the other person's work.
	//
	// Clients that send no expected_updated_at keep the previous behavior, so
	// an older UI build against a newer API still saves.
	var updatedAt time.Time
	err = h.db.QueryRowContext(c.Request.Context(), `
		UPDATE synoptics
		SET name = $1, description = $2, background_color = $3,
		    canvas_w = $4, canvas_h = $5, layout = COALESCE($6::jsonb, layout),
		    site_id = $7, area_id = $8, updated_at = NOW()
		WHERE id = $9 AND org_id = $10 AND ($11::timestamptz IS NULL OR updated_at = $11)
		RETURNING updated_at`,
		req.Name, req.Description, req.BackgroundColor, req.CanvasW, req.CanvasH,
		layout, req.SiteID, req.AreaID, id, orgID, req.ExpectedUpdatedAt).Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Nothing matched. Separate the two reasons, because "not found" sent
		// to somebody who is looking at the drawing is a baffling message.
		if req.ExpectedUpdatedAt != nil {
			var exists bool
			_ = h.db.QueryRowContext(c.Request.Context(),
				`SELECT EXISTS (SELECT 1 FROM synoptics WHERE id = $1 AND org_id = $2)`,
				id, orgID).Scan(&exists)
			if exists {
				c.JSON(http.StatusConflict, gin.H{
					"error": "this synoptic was modified by somebody else since you opened it — " +
						"reload before saving, or your changes will overwrite theirs",
				})
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "synoptic not found"})
		return
	}
	if err != nil {
		writeSynopticDBError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "updated", "updated_at": updatedAt})
}

// Limits on what a synoptic may hold. A page is drawn by hand; these are far
// above any real one and stop a request from storing what a browser cannot
// draw (a 2-billion-pixel canvas) or the server cannot hold.
const (
	synopticMaxBody   = 8 << 20 // images can be embedded as data URIs
	synopticMaxName   = 200
	synopticMinCanvas = 100
	synopticMaxCanvas = 10000
)

var synopticColor = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

// bindRequest reads and validates a create/update body. On failure it has
// already answered the request.
func (h *SynopticsHandler) bindRequest(c *gin.Context, orgID int) (synopticRequest, bool) {
	var req synopticRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, synopticMaxBody)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid synoptic: " + err.Error()})
		return req, false
	}
	bad := func(msg string) (synopticRequest, bool) {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return req, false
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return bad("name is required")
	}
	if utf8.RuneCountInString(req.Name) > synopticMaxName {
		return bad(fmt.Sprintf("name is longer than %d characters", synopticMaxName))
	}
	if req.BackgroundColor == "" {
		req.BackgroundColor = "#0f172a"
	} else if !synopticColor.MatchString(req.BackgroundColor) {
		return bad("background_color must be a hex color such as #0f172a")
	}
	if req.CanvasW <= 0 {
		req.CanvasW = 1280
	}
	if req.CanvasH <= 0 {
		req.CanvasH = 720
	}
	if req.CanvasW < synopticMinCanvas || req.CanvasW > synopticMaxCanvas ||
		req.CanvasH < synopticMinCanvas || req.CanvasH > synopticMaxCanvas {
		return bad(fmt.Sprintf("canvas size must be between %d and %d pixels", synopticMinCanvas, synopticMaxCanvas))
	}
	// The layout is opaque here, but it must be a list: an object or a string
	// stored as the layout broke the page for every operator who opened it.
	// JSON null counts as absent.
	if t := bytes.TrimSpace(req.Layout); len(t) == 0 || bytes.Equal(t, []byte("null")) {
		req.Layout = nil
	} else if t[0] != '[' {
		return bad("layout must be a JSON array of widgets")
	}

	// Site and area must be the caller's own. A foreign key only proves the
	// row exists: another organization's site was accepted, which told the
	// caller which ids exist and tied this page to a site they do not own.
	ctx := c.Request.Context()
	if req.SiteID != nil {
		var mine bool
		if err := h.db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM sites WHERE id = $1 AND org_id = $2)`, *req.SiteID, orgID).Scan(&mine); err != nil || !mine {
			return bad("site_id is not a site of this organization")
		}
	}
	if req.AreaID != nil {
		var mine bool
		if err := h.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM areas a JOIN sites s ON s.id = a.site_id
			WHERE a.id = $1 AND s.org_id = $2 AND ($3::int IS NULL OR a.site_id = $3))`,
			*req.AreaID, orgID, req.SiteID).Scan(&mine); err != nil || !mine {
			return bad("area_id is not an area of this organization (or not of the chosen site)")
		}
	}
	return req, true
}

// writeSynopticDBError answers a failed write. A duplicate name is the
// operator's to fix, not a server fault.
func writeSynopticDBError(c *gin.Context, err error) {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23505":
			c.JSON(http.StatusConflict, gin.H{"error": "a synoptic with this name already exists"})
			return
		case "23503":
			c.JSON(http.StatusBadRequest, gin.H{"error": "the site or area no longer exists"})
			return
		}
	}
	log.Printf("[synoptics] write failed: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the synoptic"})
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
