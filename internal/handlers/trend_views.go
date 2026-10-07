package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// TrendViewsHandler keeps named trend views per organization: the pens,
// window, aggregation, comparison and axes of a trend, as the query string
// the page already keeps in its address. Anyone in the organization can save
// and open them; changing or deleting one is for its author or an admin.
type TrendViewsHandler struct{ db *sql.DB }

func NewTrendViewsHandler(db *sql.DB) *TrendViewsHandler { return &TrendViewsHandler{db: db} }

type TrendView struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	CreatedBy *int      `json:"created_by,omitempty"`
	Author    string    `json:"author,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

const maxTrendViewQuery = 4096

// trendViewKeys are the parameters a trend view may carry (see the web UI's
// lib/trendView.ts). Anything else is refused rather than stored.
var trendViewKeys = map[string]bool{
	"pens": true, "range": true, "from": true, "to": true,
	"agg": true, "live": true, "cmp": true, "y": true,
}

// validTrendViewQuery checks a saved view is a trend view: a query string of
// known parameters, each once, within a bound, naming at least one pen.
func validTrendViewQuery(q string) bool {
	if q == "" || len(q) > maxTrendViewQuery {
		return false
	}
	vals, err := url.ParseQuery(q)
	if err != nil || vals.Get("pens") == "" {
		return false
	}
	for k, v := range vals {
		if !trendViewKeys[k] || len(v) != 1 {
			return false
		}
	}
	return true
}

func callerIsAdmin(c *gin.Context) bool {
	raw, _ := c.Get(middleware.UserKey)
	claims, ok := raw.(jwt.MapClaims)
	if !ok {
		return false
	}
	role, _ := claims["role"].(string)
	return role == "admin"
}

// List handles GET /api/trend-views.
func (h *TrendViewsHandler) List(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT v.id, v.name, v.query, v.created_by, COALESCE(u.full_name, u.username, ''), v.updated_at
		FROM trend_views v LEFT JOIN users u ON u.id = v.created_by
		WHERE v.org_id = $1 ORDER BY lower(v.name)`, orgID)
	if err != nil {
		log.Printf("[trend-views] list: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list trend views"})
		return
	}
	defer func() { _ = rows.Close() }()
	out := []TrendView{}
	for rows.Next() {
		var v TrendView
		var by sql.NullInt64
		if err := rows.Scan(&v.ID, &v.Name, &v.Query, &by, &v.Author, &v.UpdatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read trend views"})
			return
		}
		if by.Valid {
			id := int(by.Int64)
			v.CreatedBy = &id
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read trend views"})
		return
	}
	c.JSON(http.StatusOK, out)
}

// Save handles POST /api/trend-views: creates a view, or replaces the one
// with the same name when the caller wrote it or is an admin.
func (h *TrendViewsHandler) Save(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	var req struct {
		Name  string `json:"name"`
		Query string `json:"query"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and query are required"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the name must be 1 to 100 characters"})
		return
	}
	if !validTrendViewQuery(req.Query) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not a trend view"})
		return
	}
	userID, _ := middleware.GetUserID(c)
	ctx := c.Request.Context()

	var existing int
	var author sql.NullInt64
	err := h.db.QueryRowContext(ctx, `SELECT id, created_by FROM trend_views WHERE org_id = $1 AND name = $2`,
		orgID, req.Name).Scan(&existing, &author)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var id int
		if err := h.db.QueryRowContext(ctx, `
			INSERT INTO trend_views (org_id, name, query, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
			orgID, req.Name, req.Query, nullableUser(userID)).Scan(&id); err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23505" {
				c.JSON(http.StatusConflict, gin.H{"error": "a view with this name was just saved by someone else"})
				return
			}
			log.Printf("[trend-views] insert: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the view"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": id})
	case err != nil:
		log.Printf("[trend-views] lookup: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the view"})
	default:
		if !callerIsAdmin(c) && (!author.Valid || int(author.Int64) != userID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "another user's view: save it under another name"})
			return
		}
		if _, err := h.db.ExecContext(ctx, `UPDATE trend_views SET query = $1, updated_at = NOW() WHERE id = $2 AND org_id = $3`,
			req.Query, existing, orgID); err != nil {
			log.Printf("[trend-views] update: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the view"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": existing})
	}
}

// Delete handles DELETE /api/trend-views/:id (its author or an admin).
func (h *TrendViewsHandler) Delete(c *gin.Context) {
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
	var author sql.NullInt64
	err = h.db.QueryRowContext(c.Request.Context(), `SELECT created_by FROM trend_views WHERE id = $1 AND org_id = $2`,
		id, orgID).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "view not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete the view"})
		return
	}
	userID, _ := middleware.GetUserID(c)
	if !callerIsAdmin(c) && (!author.Valid || int(author.Int64) != userID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only its author or an admin can delete this view"})
		return
	}
	if _, err := h.db.ExecContext(c.Request.Context(), `DELETE FROM trend_views WHERE id = $1 AND org_id = $2`, id, orgID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete the view"})
		return
	}
	c.Status(http.StatusNoContent)
}

func nullableUser(id int) interface{} {
	if id <= 0 {
		return nil
	}
	return id
}
