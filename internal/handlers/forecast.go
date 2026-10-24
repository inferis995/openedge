package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/forecast"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// ForecastHandler serves tag forecasts and the profiles that shape them.
type ForecastHandler struct {
	db     *sql.DB
	runner *forecast.Runner
}

// NewForecastHandler returns the handler. runner may be nil when forecasting
// is switched off; refresh then answers 503.
func NewForecastHandler(db *sql.DB, runner *forecast.Runner) *ForecastHandler {
	return &ForecastHandler{db: db, runner: runner}
}

// hierarchy is where an entity sits: the ids of the levels above it and its
// own (0 below it).
type hierarchy struct {
	org, site, area, gateway, tag int
}

func (h hierarchy) layers(p forecast.Profiles) []forecast.Settings {
	return p.Layers(h.org, h.site, h.area, h.gateway, h.tag)
}

var errForecastNotFound = errors.New("not found")

// locate finds the hierarchy of an entity of the given scope.
func (h *ForecastHandler) locate(ctx context.Context, scope forecast.Scope, id int) (hierarchy, error) {
	var x hierarchy
	var err error
	switch scope {
	case forecast.ScopeOrg:
		err = h.db.QueryRowContext(ctx, `SELECT id FROM organizations WHERE id = $1`, id).Scan(&x.org)
	case forecast.ScopeSite:
		err = h.db.QueryRowContext(ctx, `SELECT org_id, id FROM sites WHERE id = $1`, id).Scan(&x.org, &x.site)
	case forecast.ScopeArea:
		err = h.db.QueryRowContext(ctx, `
			SELECT s.org_id, s.id, a.id FROM areas a JOIN sites s ON s.id = a.site_id
			WHERE a.id = $1`, id).Scan(&x.org, &x.site, &x.area)
	case forecast.ScopeGateway:
		err = h.db.QueryRowContext(ctx, `
			SELECT s.org_id, s.id, a.id, g.id FROM gateways g
			JOIN areas a ON a.id = g.area_id JOIN sites s ON s.id = a.site_id
			WHERE g.id = $1`, id).Scan(&x.org, &x.site, &x.area, &x.gateway)
	case forecast.ScopeTag:
		err = h.db.QueryRowContext(ctx, `
			SELECT s.org_id, s.id, a.id, g.id, t.id FROM tags t
			JOIN gateways g ON g.id = t.gateway_id
			JOIN areas a ON a.id = g.area_id JOIN sites s ON s.id = a.site_id
			WHERE t.id = $1`, id).Scan(&x.org, &x.site, &x.area, &x.gateway, &x.tag)
	default:
		return x, errForecastNotFound
	}
	if errors.Is(err, sql.ErrNoRows) {
		return x, errForecastNotFound
	}
	return x, err
}

// visible: the caller may see this organization's data. A global admin sees
// every organization.
func visible(c *gin.Context, org int) bool {
	f := middleware.GetOrgFilterForQuery(c)
	return f == nil || *f == org
}

func (h *ForecastHandler) locateVisible(c *gin.Context, scope forecast.Scope, id int) (hierarchy, bool) {
	x, err := h.locate(c.Request.Context(), scope, id)
	if err == nil && !visible(c, x.org) {
		err = errForecastNotFound
	}
	switch {
	case errors.Is(err, errForecastNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": string(scope) + " not found"})
		return x, false
	case err != nil:
		log.Printf("[FORECAST] locate %s %d: %v", scope, id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return x, false
	}
	return x, true
}

// ForecastPoint is one step of a forecast.
type ForecastPoint struct {
	T   time.Time `json:"t"`
	Q10 *float64  `json:"q10"`
	Q50 *float64  `json:"q50"`
	Q90 *float64  `json:"q90"`
}

// TagForecastResponse is GET /api/tags/:id/forecast.
type TagForecastResponse struct {
	TagID int `json:"tag_id"`
	// Status: ok, unreliable, learning, disabled, error, or pending (never
	// forecast yet).
	Status      string             `json:"status"`
	Reason      string             `json:"reason,omitempty"`
	Kind        string             `json:"kind,omitempty"`
	Engine      string             `json:"engine,omitempty"`
	Model       string             `json:"model,omitempty"`
	StepSeconds int                `json:"step_seconds,omitempty"`
	GeneratedAt *time.Time         `json:"generated_at,omitempty"`
	Skill       *float64           `json:"skill,omitempty"`
	Coverage    *float64           `json:"coverage,omitempty"`
	Points      []ForecastPoint    `json:"points"`
	Settings    forecast.Effective `json:"settings"`
	// Risks are the tag's alarm rules the forecast may trip.
	Risks []forecast.Risk `json:"risks"`
	// Anomaly compares the values that arrived with the previous forecast.
	Anomaly *forecast.Anomaly `json:"anomaly,omitempty"`
	// StateTagID: forecast from the machine's running time only, as said by
	// this tag.
	StateTagID int `json:"state_tag_id,omitempty"`
}

func finitePtr(x float64) *float64 {
	if x != x { // NaN
		return nil
	}
	return &x
}

// GetTagForecast returns a tag's latest forecast.
func (h *ForecastHandler) GetTagForecast(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tag id"})
		return
	}
	x, ok := h.locateVisible(c, forecast.ScopeTag, id)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	profiles, err := forecast.LoadProfiles(ctx, h.db, []int{x.org})
	if err != nil {
		log.Printf("[FORECAST] profiles: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read forecast"})
		return
	}
	resp := TagForecastResponse{TagID: id, Status: "pending", Points: []ForecastPoint{},
		Settings: forecast.Merge(x.layers(profiles)...), Risks: []forecast.Risk{}}

	var (
		startAt         sql.NullTime
		generated       time.Time
		quantiles       []byte
		detailsRaw      []byte
		risksRaw        []byte
		anomalyRaw      []byte
		skill, coverage sql.NullFloat64
	)
	err = h.db.QueryRowContext(ctx, `
		SELECT status, reason, kind, engine, step_seconds, start_at, quantiles, skill, coverage,
		       details, generated_at, risks, anomaly
		FROM tag_forecasts WHERE tag_id = $1`, id).Scan(
		&resp.Status, &resp.Reason, &resp.Kind, &resp.Engine, &resp.StepSeconds, &startAt,
		&quantiles, &skill, &coverage, &detailsRaw, &generated, &risksRaw, &anomalyRaw)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusOK, resp)
		return
	}
	if err != nil {
		log.Printf("[FORECAST] read tag %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read forecast"})
		return
	}
	resp.GeneratedAt = &generated
	if skill.Valid {
		resp.Skill = &skill.Float64
	}
	if coverage.Valid {
		resp.Coverage = &coverage.Float64
	}
	var d struct {
		Model      string `json:"model"`
		StateTagID int    `json:"state_tag_id"`
	}
	_ = json.Unmarshal(detailsRaw, &d)
	resp.Model, resp.StateTagID = d.Model, d.StateTagID
	decodeAssessment(risksRaw, anomalyRaw, &resp.Risks, &resp.Anomaly)

	if startAt.Valid && len(quantiles) > 0 && resp.StepSeconds > 0 {
		var q forecast.Quantiles
		if err := json.Unmarshal(quantiles, &q); err == nil {
			step := time.Duration(resp.StepSeconds) * time.Second
			for i := range q.Q50 {
				p := ForecastPoint{T: startAt.Time.Add(time.Duration(i) * step), Q50: finitePtr(q.Q50[i])}
				if i < len(q.Q10) {
					p.Q10 = finitePtr(q.Q10[i])
				}
				if i < len(q.Q90) {
					p.Q90 = finitePtr(q.Q90[i])
				}
				resp.Points = append(resp.Points, p)
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}

// RefreshTagForecast forecasts a tag now and returns the result.
func (h *ForecastHandler) RefreshTagForecast(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tag id"})
		return
	}
	if _, ok := h.locateVisible(c, forecast.ScopeTag, id); !ok {
		return
	}
	if h.runner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "forecasting is switched off on this server"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	if err := h.runner.RunTags(ctx, []int{id}); err != nil {
		log.Printf("[FORECAST] refresh tag %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "forecast failed"})
		return
	}
	h.GetTagForecast(c)
}

// ProfileResponse is GET /api/forecast/profiles.
type ProfileResponse struct {
	Scope   forecast.Scope `json:"scope"`
	ScopeID int            `json:"scope_id"`
	// Settings is this level's own profile; empty fields inherit.
	Settings forecast.Settings `json:"settings"`
	// Inherited is what this level gets from the levels above it.
	Inherited forecast.Effective `json:"inherited"`
	// Effective is Inherited with Settings applied.
	Effective forecast.Effective `json:"effective"`
}

func scopeParams(scopeStr, idStr string) (forecast.Scope, int, bool) {
	scope := forecast.Scope(scopeStr)
	id, err := strconv.Atoi(idStr)
	return scope, id, scope.Valid() && err == nil && id > 0
}

func (h *ForecastHandler) profileResponse(ctx context.Context, scope forecast.Scope, id int, x hierarchy) (ProfileResponse, error) {
	profiles, err := forecast.LoadProfiles(ctx, h.db, []int{x.org})
	if err != nil {
		return ProfileResponse{}, err
	}
	layers := x.layers(profiles)
	own := profiles[scope][id]
	inherited := layers
	if _, has := profiles[scope][id]; has && len(layers) > 0 {
		inherited = layers[:len(layers)-1]
	}
	return ProfileResponse{
		Scope: scope, ScopeID: id, Settings: own,
		Inherited: forecast.Merge(inherited...), Effective: forecast.Merge(layers...),
	}, nil
}

// GetProfile returns one level's profile, what it inherits and the result.
func (h *ForecastHandler) GetProfile(c *gin.Context) {
	scope, id, ok := scopeParams(c.Query("scope"), c.Query("scope_id"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope (org, site, area, gateway, tag) and scope_id are required"})
		return
	}
	x, ok := h.locateVisible(c, scope, id)
	if !ok {
		return
	}
	resp, err := h.profileResponse(c.Request.Context(), scope, id, x)
	if err != nil {
		log.Printf("[FORECAST] profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read profile"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// PutProfileRequest is the body of PUT /api/forecast/profiles.
type PutProfileRequest struct {
	Scope    forecast.Scope    `json:"scope"`
	ScopeID  int               `json:"scope_id"`
	Settings forecast.Settings `json:"settings"`
}

// PutProfile sets one level's profile. Empty settings remove it, so the level
// inherits everything again. The tags under the level are forecast again at
// the runner's next pass.
func (h *ForecastHandler) PutProfile(c *gin.Context) {
	var req PutProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if !req.Scope.Valid() || req.ScopeID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope (org, site, area, gateway, tag) and scope_id are required"})
		return
	}
	if err := req.Settings.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	x, ok := h.locateVisible(c, req.Scope, req.ScopeID)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	empty := req.Settings == (forecast.Settings{})
	var err error
	if empty {
		_, err = h.db.ExecContext(ctx, `DELETE FROM forecast_profiles WHERE scope = $1 AND scope_id = $2`,
			string(req.Scope), req.ScopeID)
	} else {
		raw, _ := json.Marshal(req.Settings)
		var by any
		if uid, ok := middleware.GetUserID(c); ok {
			by = uid
		}
		_, err = h.db.ExecContext(ctx, `
			INSERT INTO forecast_profiles (org_id, scope, scope_id, settings, updated_by, updated_at)
			VALUES ($1, $2, $3, $4::jsonb, $5, NOW())
			ON CONFLICT (scope, scope_id) DO UPDATE SET
				settings = EXCLUDED.settings, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
			x.org, string(req.Scope), req.ScopeID, string(raw), by)
	}
	if err != nil {
		log.Printf("[FORECAST] save profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save profile"})
		return
	}
	if _, rerr := h.db.ExecContext(ctx, `UPDATE tag_forecasts SET next_run_at = NOW() WHERE tag_id IN (`+
		tagsUnder(req.Scope)+`)`, req.ScopeID); rerr != nil {
		log.Printf("[FORECAST] reschedule after profile change: %v", rerr)
	}
	resp, err := h.profileResponse(ctx, req.Scope, req.ScopeID, x)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read profile"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// tagsUnder is a query for the ids of the tags under an entity ($1).
func tagsUnder(scope forecast.Scope) string {
	base := `SELECT t.id FROM tags t JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id JOIN sites s ON s.id = a.site_id WHERE `
	switch scope {
	case forecast.ScopeOrg:
		return base + `s.org_id = $1`
	case forecast.ScopeSite:
		return base + `s.id = $1`
	case forecast.ScopeArea:
		return base + `a.id = $1`
	case forecast.ScopeGateway:
		return base + `g.id = $1`
	default:
		return base + `t.id = $1`
	}
}

// OverviewTag is one row of GET /api/forecast/overview.
type OverviewTag struct {
	TagID       int        `json:"tag_id"`
	Alias       string     `json:"alias"`
	Unit        string     `json:"unit,omitempty"`
	DataType    string     `json:"data_type"`
	GatewayID   int        `json:"gateway_id"`
	GatewayName string     `json:"gateway_name"`
	AreaID      int        `json:"area_id"`
	AreaName    string     `json:"area_name"`
	SiteID      int        `json:"site_id"`
	SiteName    string     `json:"site_name"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	Engine      string     `json:"engine,omitempty"`
	Skill       *float64   `json:"skill,omitempty"`
	Coverage    *float64   `json:"coverage,omitempty"`
	GeneratedAt *time.Time `json:"generated_at,omitempty"`
	// HorizonHours is the tag's effective horizon (its profiles merged).
	HorizonHours int `json:"horizon_hours"`
	// End is the forecast at the last step: where the tag is expected to be.
	End *ForecastPoint `json:"end,omitempty"`
	// Risks are the alarm rules the forecast may trip, soonest first.
	Risks   []forecast.Risk   `json:"risks,omitempty"`
	Anomaly *forecast.Anomaly `json:"anomaly,omitempty"`
}

// OverviewResponse is GET /api/forecast/overview.
type OverviewResponse struct {
	// Engines the server forecasts with, statistical first.
	Engines []string       `json:"engines"`
	Summary map[string]int `json:"summary"`
	Tags    []OverviewTag  `json:"tags"`
}

// Overview lists the organization's forecastable tags with the state of their
// forecast: every historized numeric or BOOL tag, forecast or not yet.
func (h *ForecastHandler) Overview(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "select an organization first"})
		return
	}
	if !visible(c, orgID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	ctx := c.Request.Context()
	profiles, err := forecast.LoadProfiles(ctx, h.db, []int{orgID})
	if err != nil {
		log.Printf("[FORECAST] overview profiles: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read forecasts"})
		return
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT t.id, t.alias, COALESCE(t.eu_unit, ''), t.data_type,
		       g.id, g.name, a.id, a.name, s.id, s.name,
		       COALESCE(f.status, 'pending'), COALESCE(f.reason, ''), COALESCE(f.kind, ''),
		       COALESCE(f.engine, ''), f.skill, f.coverage, f.generated_at,
		       f.start_at, COALESCE(f.step_seconds, 0), f.quantiles, f.risks, f.anomaly
		FROM tags t
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a    ON a.id = g.area_id
		JOIN sites s    ON s.id = a.site_id
		LEFT JOIN tag_forecasts f ON f.tag_id = t.id
		WHERE s.org_id = $1 AND t.historize = TRUE
		  AND t.data_type IN ('INT', 'DINT', 'REAL', 'BOOL')
		ORDER BY s.name, a.name, g.name, t.alias
		LIMIT 5000`, orgID)
	if err != nil {
		log.Printf("[FORECAST] overview: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read forecasts"})
		return
	}
	defer func() { _ = rows.Close() }()

	resp := OverviewResponse{Summary: map[string]int{}, Tags: []OverviewTag{}}
	if h.runner != nil {
		for _, e := range h.runner.Engines {
			resp.Engines = append(resp.Engines, e.Name())
		}
	}
	for rows.Next() {
		var (
			t               OverviewTag
			skill, coverage sql.NullFloat64
			generated       sql.NullTime
			startAt         sql.NullTime
			step            int
			quantiles       []byte
			risks, anomaly  []byte
		)
		if err := rows.Scan(&t.TagID, &t.Alias, &t.Unit, &t.DataType, &t.GatewayID, &t.GatewayName,
			&t.AreaID, &t.AreaName, &t.SiteID, &t.SiteName, &t.Status, &t.Reason, &t.Kind, &t.Engine,
			&skill, &coverage, &generated, &startAt, &step, &quantiles, &risks, &anomaly); err != nil {
			log.Printf("[FORECAST] overview row: %v", err)
			continue
		}
		if skill.Valid {
			t.Skill = &skill.Float64
		}
		if coverage.Valid {
			t.Coverage = &coverage.Float64
		}
		if generated.Valid {
			t.GeneratedAt = &generated.Time
		}
		t.HorizonHours = profiles.Effective(orgID, t.SiteID, t.AreaID, t.GatewayID, t.TagID).HorizonHours
		t.End = lastPoint(startAt, step, quantiles)
		decodeAssessment(risks, anomaly, &t.Risks, &t.Anomaly)
		if seriousRisk(t.Risks) {
			resp.Summary["at_risk"]++
		}
		if t.Anomaly != nil && t.Anomaly.Outside > 0 {
			resp.Summary["anomalous"]++
		}
		resp.Summary[t.Status]++
		resp.Tags = append(resp.Tags, t)
	}
	resp.Summary["total"] = len(resp.Tags)
	c.JSON(http.StatusOK, resp)
}

// lastPoint is the last step of a stored forecast that has a value.
func lastPoint(startAt sql.NullTime, step int, quantiles []byte) *ForecastPoint {
	if !startAt.Valid || step <= 0 || len(quantiles) == 0 {
		return nil
	}
	var q forecast.Quantiles
	if json.Unmarshal(quantiles, &q) != nil {
		return nil
	}
	for i := len(q.Q50) - 1; i >= 0; i-- {
		if q.Q50[i] != q.Q50[i] { // NaN
			continue
		}
		p := &ForecastPoint{T: startAt.Time.Add(time.Duration(i*step) * time.Second), Q50: finitePtr(q.Q50[i])}
		if i < len(q.Q10) {
			p.Q10 = finitePtr(q.Q10[i])
		}
		if i < len(q.Q90) {
			p.Q90 = finitePtr(q.Q90[i])
		}
		return p
	}
	return nil
}

// decodeAssessment reads the stored risks (soonest first) and anomaly.
func decodeAssessment(risksRaw, anomalyRaw []byte, risks *[]forecast.Risk, anomaly **forecast.Anomaly) {
	if len(risksRaw) > 0 {
		var rs []forecast.Risk
		if json.Unmarshal(risksRaw, &rs) == nil && len(rs) > 0 {
			sort.Slice(rs, func(i, j int) bool { return rs[i].At.Before(rs[j].At) })
			*risks = rs
		}
	}
	if len(anomalyRaw) > 0 {
		var a forecast.Anomaly
		if json.Unmarshal(anomalyRaw, &a) == nil {
			*anomaly = &a
		}
	}
}

// seriousRisk: a rule likely or almost certain to trip within the horizon.
func seriousRisk(rs []forecast.Risk) bool {
	for _, r := range rs {
		if r.Likelihood == forecast.Likely || r.Likelihood == forecast.AlmostCertain {
			return true
		}
	}
	return false
}
