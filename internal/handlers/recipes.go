// Package handlers — recipes.
//
// A recipe is a named (org-scoped) set of (tag, value) pairs. The
// "load" operation publishes a write command per pair, all under the
// same recipe_run row that the audit page can later show. Designed to
// be safe in multi-tenant: every tag in a recipe must belong to the
// caller's org, and the LOAD verifies that again at exec time (not just
// at create time) so a tag that has been moved to another org since the
// recipe was saved is never silently written.
package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/audit"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
)

// RecipesHandler owns the /api/recipes CRUD + load endpoints.
type RecipesHandler struct {
	db         *sql.DB
	mqttClient *mqtt.Client
}

func NewRecipesHandler(db *sql.DB, m *mqtt.Client) *RecipesHandler {
	return &RecipesHandler{db: db, mqttClient: m}
}

// Recipe is the wire shape returned by the list/get endpoints. Values
// are joined in on Get; List returns the count for the index page.
type Recipe struct {
	ID          int            `json:"id"`
	OrgID       int            `json:"org_id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	CreatedBy   *int           `json:"created_by,omitempty"`
	ValueCount  int            `json:"value_count"`
	Values      []RecipeValue  `json:"values,omitempty"`
}

type RecipeValue struct {
	TagID     int    `json:"tag_id"`
	TagAlias  string `json:"tag_alias,omitempty"`
	TagCode   string `json:"tag_code,omitempty"`
	GatewayID int    `json:"gateway_id,omitempty"`
	DataType  string `json:"data_type,omitempty"`
	Value     string `json:"value"`
}

type RecipeRun struct {
	ID                int64           `json:"id"`
	RecipeID          int             `json:"recipe_id"`
	TriggeredBy       *int            `json:"triggered_by,omitempty"`
	TriggeredUsername string          `json:"triggered_username,omitempty"`
	TriggeredAt       time.Time       `json:"triggered_at"`
	Status            string          `json:"status"`
	Results           json.RawMessage `json:"results"`
}

// CreateRecipeRequest is the body for POST /api/recipes and the
// "values" replace endpoint. An empty Values list is allowed at create
// time (an empty recipe is a valid placeholder) but the LOAD endpoint
// will refuse to fire it.
type CreateRecipeRequest struct {
	Name        string              `json:"name" binding:"required"`
	Description string              `json:"description"`
	Values      []recipeValueInput  `json:"values"`
}

type recipeValueInput struct {
	TagID int    `json:"tag_id" binding:"required"`
	Value string `json:"value" binding:"required"`
}

// List returns the org's recipes (lightweight — no value rows).
func (h *RecipesHandler) List(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	rows, err := h.db.Query(`
		SELECT r.id, r.org_id, r.name, COALESCE(r.description,''), r.created_at, r.updated_at, r.created_by,
		       (SELECT COUNT(*) FROM recipe_values v WHERE v.recipe_id = r.id)
		FROM recipes r WHERE r.org_id = $1 ORDER BY r.name`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list recipes"})
		return
	}
	defer rows.Close()
	out := []Recipe{}
	for rows.Next() {
		var r Recipe
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.ValueCount); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan recipe"})
			return
		}
		out = append(out, r)
	}
	c.JSON(http.StatusOK, out)
}

// Get returns one recipe with its full value list (joined with tag
// metadata for the UI).
func (h *RecipesHandler) Get(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipe id"})
		return
	}
	var r Recipe
	err = h.db.QueryRow(`
		SELECT id, org_id, name, COALESCE(description,''), created_at, updated_at, created_by
		FROM recipes WHERE id = $1 AND org_id = $2`, id, orgID).
		Scan(&r.ID, &r.OrgID, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt, &r.CreatedBy)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Recipe not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load recipe"})
		return
	}
	values, err := h.loadValues(r.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load values"})
		return
	}
	r.Values = values
	r.ValueCount = len(values)
	c.JSON(http.StatusOK, r)
}

// Create inserts the recipe + its values in a single transaction so a
// half-saved recipe is impossible. The actor + values are validated
// against the caller's org boundary.
func (h *RecipesHandler) Create(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	var req CreateRecipeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.assertTagsBelongToOrg(req.Values, orgID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	actorID, _ := recipesActor(c)

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start tx"})
		return
	}
	defer func() { _ = tx.Rollback() }()

	var recipeID int
	err = tx.QueryRow(`
		INSERT INTO recipes (org_id, name, description, created_by)
		VALUES ($1, $2, NULLIF($3,''), $4) RETURNING id`,
		orgID, req.Name, req.Description, nullableID(actorID)).Scan(&recipeID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to create (duplicate name?)"})
		return
	}
	for _, v := range req.Values {
		if _, err := tx.Exec(`
			INSERT INTO recipe_values (recipe_id, tag_id, value) VALUES ($1, $2, $3)`,
			recipeID, v.TagID, v.Value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to insert value for tag %d", v.TagID)})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": recipeID})
}

// Update replaces name / description / values atomically. We always
// drop+reinsert values (instead of computing a diff) — keeps the
// handler simple and matches what the UI sends anyway (the operator
// edits the whole table, not individual rows).
func (h *RecipesHandler) Update(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipe id"})
		return
	}
	var req CreateRecipeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.assertTagsBelongToOrg(req.Values, orgID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start tx"})
		return
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`
		UPDATE recipes SET name = $1, description = NULLIF($2,''), updated_at = NOW()
		WHERE id = $3 AND org_id = $4`,
		req.Name, req.Description, id, orgID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to update (duplicate name?)"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Recipe not found"})
		return
	}
	if _, err := tx.Exec(`DELETE FROM recipe_values WHERE recipe_id = $1`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear values"})
		return
	}
	for _, v := range req.Values {
		if _, err := tx.Exec(`
			INSERT INTO recipe_values (recipe_id, tag_id, value) VALUES ($1, $2, $3)`,
			id, v.TagID, v.Value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to insert value for tag %d", v.TagID)})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Recipe updated"})
}

// Delete removes a recipe (and its values via ON DELETE CASCADE). Past
// runs are preserved — the audit trail outlives the recipe.
func (h *RecipesHandler) Delete(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipe id"})
		return
	}
	res, err := h.db.Exec(`DELETE FROM recipes WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Recipe not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// Load executes the recipe: re-validates every tag belongs to the
// caller's org, publishes one MQTT write per (tag, value), and records
// a recipe_runs row with per-tag status. Fire-and-forget at the MQTT
// layer — "success" here means "publish succeeded", not "PLC confirmed".
// Driver ACKs would arrive later on sys/write-ack/* (not yet implemented).
func (h *RecipesHandler) Load(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	if h.mqttClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MQTT client not available"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipe id"})
		return
	}

	// Verify ownership and pull values + tag metadata.
	var owns int
	err = h.db.QueryRow(`SELECT 1 FROM recipes WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&owns)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Recipe not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load recipe"})
		return
	}
	values, err := h.loadValues(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load values"})
		return
	}
	if len(values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Recipe has no values to load"})
		return
	}
	// Belt-and-suspenders: re-check ownership of every tag. A tag that
	// was moved to another org since the recipe was created must NOT be
	// silently written.
	tagInputs := make([]recipeValueInput, 0, len(values))
	for _, v := range values {
		tagInputs = append(tagInputs, recipeValueInput{TagID: v.TagID, Value: v.Value})
	}
	if err := h.assertTagsBelongToOrg(tagInputs, orgID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	actorID, actorName := recipesActor(c)

	// Create the run row up-front so we have an id to embed in audit logs.
	var runID int64
	err = h.db.QueryRow(`
		INSERT INTO recipe_runs (recipe_id, org_id, triggered_by, triggered_username, status)
		VALUES ($1, $2, $3, NULLIF($4,''), 'pending') RETURNING id`,
		id, orgID, nullableID(actorID), actorName).Scan(&runID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record run"})
		return
	}

	type result struct {
		TagID    int    `json:"tag_id"`
		TagAlias string `json:"tag_alias"`
		OK       bool   `json:"ok"`
		Error    string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(values))
	failures := 0

	for _, v := range values {
		cmd := struct {
			TagID    int    `json:"tag_id"`
			Code     string `json:"code"`
			Value    string `json:"value"`
			DataType string `json:"data_type"`
			RunID    int64  `json:"recipe_run_id"` // lets the driver's ACK trace back to the run
		}{
			TagID:    v.TagID,
			Code:     v.TagCode,
			Value:    v.Value,
			DataType: v.DataType,
			RunID:    runID,
		}
		payload, _ := json.Marshal(cmd)
		topic := fmt.Sprintf("cmd/write/%d", v.GatewayID)
		r := result{TagID: v.TagID, TagAlias: v.TagAlias, OK: true}
		if err := h.mqttClient.Publish(topic, string(payload)); err != nil {
			r.OK = false
			r.Error = err.Error()
			failures++
		}
		results = append(results, r)
	}

	status := "success"
	if failures == len(results) {
		status = "failed"
	} else if failures > 0 {
		status = "partial"
	}
	resultsJSON, _ := json.Marshal(results)
	if _, err := h.db.Exec(`UPDATE recipe_runs SET status = $1, results = $2 WHERE id = $3`,
		status, resultsJSON, runID); err != nil {
		// Non-fatal: the writes already went out, we just can't update
		// the row. Logged centrally; operator sees the partial UI.
		fmt.Printf("[RECIPES] failed to finalise run %d: %v\n", runID, err)
	}

	// Cross-recipe audit row — the recipe_runs table is the detailed
	// per-tag log, audit_logs is the unified "what did this operator do
	// across the system?" view that compliance auditors look at.
	audit.Log(c, h.db, audit.Entry{
		Action:  "recipe.load",
		Success: failures == 0,
		Details: map[string]interface{}{
			"recipe_id": id,
			"run_id":    runID,
			"written":   len(results) - failures,
			"failed":    failures,
		},
	})

	c.JSON(http.StatusOK, gin.H{
		"run_id":   runID,
		"status":   status,
		"written":  len(results) - failures,
		"failed":   failures,
		"results":  results,
	})
}

// Runs returns the recent runs for a recipe (newest first).
func (h *RecipesHandler) Runs(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipe id"})
		return
	}
	limit := 50
	if l, _ := strconv.Atoi(c.Query("limit")); l > 0 && l <= 500 {
		limit = l
	}
	rows, err := h.db.Query(`
		SELECT id, recipe_id, triggered_by, COALESCE(triggered_username,''), triggered_at, status, results
		FROM recipe_runs WHERE recipe_id = $1 AND org_id = $2
		ORDER BY triggered_at DESC LIMIT $3`, id, orgID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load runs"})
		return
	}
	defer rows.Close()
	out := []RecipeRun{}
	for rows.Next() {
		var r RecipeRun
		var results []byte
		if err := rows.Scan(&r.ID, &r.RecipeID, &r.TriggeredBy, &r.TriggeredUsername, &r.TriggeredAt, &r.Status, &results); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan run"})
			return
		}
		r.Results = json.RawMessage(results)
		out = append(out, r)
	}
	c.JSON(http.StatusOK, out)
}

// loadValues joins recipe_values with tags + gateways so the LOAD endpoint
// has everything it needs (alias, data_type, gateway_id) without extra
// per-row roundtrips.
func (h *RecipesHandler) loadValues(recipeID int) ([]RecipeValue, error) {
	rows, err := h.db.Query(`
		SELECT v.tag_id, t.alias, t.code, t.gateway_id, t.data_type, v.value
		FROM recipe_values v JOIN tags t ON t.id = v.tag_id
		WHERE v.recipe_id = $1
		ORDER BY t.alias`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecipeValue{}
	for rows.Next() {
		var rv RecipeValue
		if err := rows.Scan(&rv.TagID, &rv.TagAlias, &rv.TagCode, &rv.GatewayID, &rv.DataType, &rv.Value); err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	return out, nil
}

// assertTagsBelongToOrg verifies every tag in `vals` belongs to `orgID`
// in a single query, so a recipe can't carry tags from another tenant.
// Empty input is OK (an empty recipe is a valid draft).
func (h *RecipesHandler) assertTagsBelongToOrg(vals []recipeValueInput, orgID int) error {
	if len(vals) == 0 {
		return nil
	}
	ids := make([]string, len(vals))
	for i, v := range vals {
		ids[i] = strconv.Itoa(v.TagID)
	}
	q := fmt.Sprintf(`
		SELECT COUNT(*) FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id = ANY('{%s}'::int[]) AND s.org_id = $1`, joinStr(ids, ","))
	var count int
	if err := h.db.QueryRow(q, orgID).Scan(&count); err != nil {
		return fmt.Errorf("validate tags: %w", err)
	}
	if count != len(vals) {
		return fmt.Errorf("one or more tags do not belong to this organization")
	}
	return nil
}

// recipesActor pulls (user_id, username) from JWT claims. Returns
// zero/empty when no auth context (shouldn't happen on the protected
// routes but keeps the helper safe).
func recipesActor(c *gin.Context) (int, string) {
	v, ok := c.Get(middleware.UserKey)
	if !ok {
		return 0, ""
	}
	claims, ok := v.(jwt.MapClaims)
	if !ok {
		return 0, ""
	}
	id := 0
	switch n := claims["user_id"].(type) {
	case float64:
		id = int(n)
	case int:
		id = n
	case int64:
		id = int(n)
	}
	name, _ := claims["username"].(string)
	return id, name
}

func nullableID(id int) interface{} {
	if id == 0 {
		return nil
	}
	return id
}

// joinStr is strings.Join with the comma already chosen — kept inline so
// the package import list doesn't change for one trivial helper.
func joinStr(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}
