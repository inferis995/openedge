package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/forecast"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// Machines (assets): a named group of one organization's tags, with an
// optional tag that says the machine is running. Their health comes from the
// members' forecasts (forecast.AssetHealth).

// AssetMember is one tag of an asset, with its forecast state.
type AssetMember struct {
	forecast.Member
	Unit        string `json:"unit,omitempty"`
	DataType    string `json:"data_type"`
	GatewayName string `json:"gateway_name"`
	Kind        string `json:"kind,omitempty"`
}

// Asset is one machine with its health.
type Asset struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	StateTagID  *int            `json:"state_tag_id"`
	StateAlias  string          `json:"state_alias,omitempty"`
	Members     []AssetMember   `json:"members"`
	Health      forecast.Health `json:"health"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// AssetInput is the body of create and update.
type AssetInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StateTagID  *int   `json:"state_tag_id"`
	TagIDs      []int  `json:"tag_ids"`
}

func orgFromContext(c *gin.Context) (int, bool) {
	org, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "select an organization first"})
		return 0, false
	}
	if !visible(c, org) {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return 0, false
	}
	return org, true
}

// loadAssets reads the organization's assets (one when id > 0) with members
// and health.
func (h *ForecastHandler) loadAssets(ctx context.Context, org, id int) ([]Asset, error) {
	q := `SELECT a.id, a.name, a.description, a.state_tag_id, COALESCE(st.alias, ''), a.updated_at
		FROM assets a LEFT JOIN tags st ON st.id = a.state_tag_id
		WHERE a.org_id = $1`
	args := []any{org}
	if id > 0 {
		q += ` AND a.id = $2`
		args = append(args, id)
	}
	rows, err := h.db.QueryContext(ctx, q+` ORDER BY a.name`, args...)
	if err != nil {
		return nil, err
	}
	var out []Asset
	index := map[int]int{}
	for rows.Next() {
		var a Asset
		var st sql.NullInt64
		if err = rows.Scan(&a.ID, &a.Name, &a.Description, &st, &a.StateAlias, &a.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if st.Valid {
			v := int(st.Int64)
			a.StateTagID = &v
		}
		a.Members = []AssetMember{}
		index[a.ID] = len(out)
		out = append(out, a)
	}
	_ = rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]int, 0, len(out))
	for i := range out {
		ids = append(ids, out[i].ID)
	}
	mrows, err := h.db.QueryContext(ctx, `
		SELECT at.asset_id, t.id, t.alias, COALESCE(t.eu_unit, ''), t.data_type, g.name,
		       COALESCE(f.status, 'pending'), COALESCE(f.kind, ''), f.risks, f.anomaly
		FROM asset_tags at
		JOIN tags t ON t.id = at.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		LEFT JOIN tag_forecasts f ON f.tag_id = t.id
		WHERE at.asset_id = ANY($1)
		ORDER BY t.alias`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = mrows.Close() }()
	for mrows.Next() {
		var (
			assetID        int
			m              AssetMember
			risks, anomaly []byte
		)
		if err := mrows.Scan(&assetID, &m.TagID, &m.Alias, &m.Unit, &m.DataType, &m.GatewayName,
			&m.Status, &m.Kind, &risks, &anomaly); err != nil {
			return nil, err
		}
		decodeAssessment(risks, anomaly, &m.Risks, &m.Anomaly)
		a := &out[index[assetID]]
		a.Members = append(a.Members, m)
	}
	now := time.Now()
	for i := range out {
		ms := make([]forecast.Member, len(out[i].Members))
		for j := range out[i].Members {
			ms[j] = out[i].Members[j].Member
		}
		out[i].Health = forecast.AssetHealth(ms, now)
	}
	return out, mrows.Err()
}

// ListAssets returns the organization's machines with their health, worst
// first.
func (h *ForecastHandler) ListAssets(c *gin.Context) {
	org, ok := orgFromContext(c)
	if !ok {
		return
	}
	assets, err := h.loadAssets(c.Request.Context(), org, 0)
	if err != nil {
		log.Printf("[ASSETS] list: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read machines"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"assets": assets})
}

// GetAsset returns one machine.
func (h *ForecastHandler) GetAsset(c *gin.Context) {
	org, ok := orgFromContext(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	assets, err := h.loadAssets(c.Request.Context(), org, id)
	if err != nil {
		log.Printf("[ASSETS] get: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the machine"})
		return
	}
	if len(assets) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "machine not found"})
		return
	}
	c.JSON(http.StatusOK, assets[0])
}

var errBadAsset = errors.New("invalid machine")

// checkAssetInput validates a machine: a name, and tags (state tag included)
// that all belong to the organization.
func (h *ForecastHandler) checkAssetInput(ctx context.Context, org int, in *AssetInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 120 {
		return errBadAsset
	}
	ids := append([]int(nil), in.TagIDs...)
	if in.StateTagID != nil {
		ids = append(ids, *in.StateTagID)
	}
	if len(ids) == 0 {
		return nil
	}
	var n int
	if err := h.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT t.id) FROM tags t
		JOIN gateways g ON g.id = t.gateway_id JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE t.id = ANY($1) AND s.org_id = $2`, pq.Array(ids), org).Scan(&n); err != nil {
		return err
	}
	distinct := map[int]bool{}
	for _, id := range ids {
		distinct[id] = true
	}
	if n != len(distinct) {
		return errBadAsset
	}
	return nil
}

func (h *ForecastHandler) saveAsset(c *gin.Context, id int) {
	org, ok := orgFromContext(c)
	if !ok {
		return
	}
	var in AssetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx := c.Request.Context()
	if err := h.checkAssetInput(ctx, org, &in); err != nil {
		if errors.Is(err, errBadAsset) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "a machine needs a name (up to 120 characters) and tags of this organization"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check the tags"})
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the machine"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	if id == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO assets (org_id, name, description, state_tag_id)
			VALUES ($1, $2, $3, $4) RETURNING id`, org, in.Name, in.Description, in.StateTagID).Scan(&id)
	} else {
		var res sql.Result
		res, err = tx.ExecContext(ctx, `UPDATE assets SET name = $1, description = $2, state_tag_id = $3, updated_at = NOW()
			WHERE id = $4 AND org_id = $5`, in.Name, in.Description, in.StateTagID, id, org)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				c.JSON(http.StatusNotFound, gin.H{"error": "machine not found"})
				return
			}
		}
	}
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "a machine with this name already exists"})
			return
		}
		log.Printf("[ASSETS] save: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the machine"})
		return
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM asset_tags WHERE asset_id = $1`, id); err == nil && len(in.TagIDs) > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO asset_tags (asset_id, tag_id)
			SELECT $1, unnest($2::int[]) ON CONFLICT DO NOTHING`, id, pq.Array(in.TagIDs))
	}
	if err == nil {
		// The members are forecast again with the machine's running state.
		_, err = tx.ExecContext(ctx, `UPDATE tag_forecasts SET next_run_at = NOW() WHERE tag_id = ANY($1)`, pq.Array(in.TagIDs))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		log.Printf("[ASSETS] save members: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the machine"})
		return
	}
	assets, err := h.loadAssets(ctx, org, id)
	if err != nil || len(assets) == 0 {
		c.JSON(http.StatusOK, gin.H{"id": id})
		return
	}
	c.JSON(http.StatusOK, assets[0])
}

// CreateAsset adds a machine (admin).
func (h *ForecastHandler) CreateAsset(c *gin.Context) { h.saveAsset(c, 0) }

// UpdateAsset changes a machine (admin): name, description, state tag, tags.
func (h *ForecastHandler) UpdateAsset(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	h.saveAsset(c, id)
}

// DeleteAsset removes a machine (admin); its tags are untouched.
func (h *ForecastHandler) DeleteAsset(c *gin.Context) {
	org, ok := orgFromContext(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	res, err := h.db.ExecContext(c.Request.Context(), `DELETE FROM assets WHERE id = $1 AND org_id = $2`, id, org)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete the machine"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "machine not found"})
		return
	}
	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

// AssetsFromGateways makes one machine per gateway of the organization that
// has none by that name yet, with the gateway's historized numeric and BOOL
// tags (admin). A starting point: on a data logger the gateway is the
// machine; on a PLC with several machines, split it afterwards.
func (h *ForecastHandler) AssetsFromGateways(c *gin.Context) {
	org, ok := orgFromContext(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	type gw struct {
		id         int
		name, site string
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT g.id, g.name, s.name FROM gateways g
		JOIN areas a ON a.id = g.area_id JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 ORDER BY s.name, g.name`, org)
	if err != nil {
		log.Printf("[ASSETS] from gateways: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create machines"})
		return
	}
	var gws []gw
	seen := map[string]int{}
	for rows.Next() {
		var g gw
		if rows.Scan(&g.id, &g.name, &g.site) == nil {
			gws = append(gws, g)
			seen[g.name]++
		}
	}
	_ = rows.Close()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create machines"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	created := 0
	for _, g := range gws {
		// Two gateways of one name in different sites stay two machines.
		name := g.name
		if seen[g.name] > 1 {
			name = g.name + " (" + g.site + ")"
		}
		var id int
		err := tx.QueryRowContext(ctx, `INSERT INTO assets (org_id, name, description)
			VALUES ($1, $2, $3) ON CONFLICT (org_id, name) DO NOTHING RETURNING id`,
			org, name, "Gateway "+g.name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue // a machine of that name exists: left as it is
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO asset_tags (asset_id, tag_id)
				SELECT $1, t.id FROM tags t
				WHERE t.gateway_id = $2 AND t.historize = TRUE
				  AND t.data_type IN ('INT', 'DINT', 'REAL', 'BOOL')
				ON CONFLICT DO NOTHING`, id, g.id)
		}
		if err != nil {
			log.Printf("[ASSETS] from gateways: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create machines"})
			return
		}
		created++
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create machines"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"created": created})
}
