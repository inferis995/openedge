package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// EdgeConfigHandler serves the configuration pull endpoint used by edge managers.
// Authentication is via API key (X-API-Key), not a user JWT.
type EdgeConfigHandler struct {
	db *sql.DB
}

func NewEdgeConfigHandler(db *sql.DB) *EdgeConfigHandler {
	return &EdgeConfigHandler{db: db}
}

// The payload types live in internal/edgesync, which the box's agent imports
// too. One definition, so a field added to what is served cannot go missing
// from what is consumed.

// Get returns the edge configuration for the org identified by the API key.
// GET /api/edge/config.
func (h *EdgeConfigHandler) Get(c *gin.Context) {
	orgIDRaw, ok := c.Get(middleware.APIKeyContextKey)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing org context"})
		return
	}
	orgID := orgIDRaw.(int)

	// Org info + MQTT credentials in one query
	var orgName, mqttUser, mqttPass string
	err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT o.name, COALESCE(mc.username, ''), COALESCE(mc.password, '')
		 FROM organizations o
		 LEFT JOIN org_mqtt_credentials mc ON mc.org_id = o.id
		 WHERE o.id = $1`,
		orgID,
	).Scan(&orgName, &mqttUser, &mqttPass)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load org config"})
		return
	}

	cfg := edgesync.Config{
		OrgID:   orgID,
		OrgName: orgName,
		MQTT: edgesync.MQTTCreds{
			Host:     mqttPublicHost(),
			Port:     mqttPublicPort(),
			Username: mqttUser,
			Password: mqttPass,
		},
		Sites:    []edgesync.Site{},
		Areas:    []edgesync.Area{},
		Gateways: []models.Gateway{},
		Tags:     []models.Tag{},
		Alarms:   []models.AlarmDefinition{},
	}

	// The whole tree, not just the gateways. The drivers join gateways to
	// areas, sites and organizations to know where they are, and read their
	// tags and alarm rules from the same database — a box that received only
	// the gateways would start containers that immediately fail their first
	// query.
	if err := h.loadTree(c.Request.Context(), orgID, &cfg); err != nil {
		log.Printf("[EDGE-CONFIG] building the configuration for org %d: %v", orgID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load edge configuration"})
		return
	}

	// Narrow it to the box that is asking. With one box this changes nothing;
	// with two it is what stops both of them polling every PLC of the
	// organization, including the ones on the other site.
	agentID, _ := c.Get(middleware.APIKeyAgentContextKey)
	id, _ := agentID.(int)

	var agentsInOrg int
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT COUNT(*) FROM edge_agents WHERE org_id = $1`, orgID).Scan(&agentsInOrg); err != nil {
		// Counting is what decides whether the assignment applies at all.
		// Guessing would either strand a lone box or let two fight, so the
		// request fails instead.
		log.Printf("[EDGE-CONFIG] counting the boxes of org %d: %v", orgID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load edge configuration"})
		return
	}

	cfg.AgentID = id
	cfg.FilterTree(id, agentsInOrg)
	if cfg.Unassigned > 0 {
		log.Printf("[EDGE-CONFIG] org %d has %d gateway(s) assigned to no box; nobody is polling them",
			orgID, cfg.Unassigned)
	}

	c.JSON(http.StatusOK, cfg)
}

func mqttPublicHost() string {
	if v := os.Getenv("MQTT_PUBLIC_HOST"); v != "" {
		return v
	}
	if v := os.Getenv("PUBLIC_HOST"); v != "" {
		return v
	}
	return "localhost"
}

func mqttPublicPort() int {
	if v := os.Getenv("MQTT_PUBLIC_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 1883
}

// loadTree fills in everything below the organization.
//
// Five queries rather than one join: joined into a single result the gateways
// would repeat once per tag and the tags once per alarm, and the payload for a
// plant of any size would be mostly duplication.
func (h *EdgeConfigHandler) loadTree(ctx context.Context, orgID int, cfg *edgesync.Config) error {
	sites, err := h.db.QueryContext(ctx,
		`SELECT id, org_id, name FROM sites WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return fmt.Errorf("sites: %w", err)
	}
	defer func() { _ = sites.Close() }()
	for sites.Next() {
		var s edgesync.Site
		if scanErr := sites.Scan(&s.ID, &s.OrgID, &s.Name); scanErr != nil {
			return fmt.Errorf("reading a site: %w", scanErr)
		}
		cfg.Sites = append(cfg.Sites, s)
	}
	if rowsErr := sites.Err(); rowsErr != nil {
		return rowsErr
	}

	areas, err := h.db.QueryContext(ctx, `
		SELECT a.id, a.site_id, a.name
		FROM areas a JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 ORDER BY a.id`, orgID)
	if err != nil {
		return fmt.Errorf("areas: %w", err)
	}
	defer func() { _ = areas.Close() }()
	for areas.Next() {
		var a edgesync.Area
		if scanErr := areas.Scan(&a.ID, &a.SiteID, &a.Name); scanErr != nil {
			return fmt.Errorf("reading an area: %w", scanErr)
		}
		cfg.Areas = append(cfg.Areas, a)
	}
	if rowsErr := areas.Err(); rowsErr != nil {
		return rowsErr
	}

	gateways, err := h.db.QueryContext(ctx, `
		SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config,
		       g.scan_rate_ms, g.enabled, COALESCE(g.zero_based, true), g.edge_agent_id
		FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 ORDER BY g.id`, orgID)
	if err != nil {
		return fmt.Errorf("gateways: %w", err)
	}
	defer func() { _ = gateways.Close() }()
	for gateways.Next() {
		var g models.Gateway
		if scanErr := gateways.Scan(&g.ID, &g.AreaID, &g.Name, &g.DriverType,
			&g.ConnectionConfig, &g.ScanRateMs, &g.Enabled, &g.ZeroBased,
			&g.EdgeAgentID); scanErr != nil {
			return fmt.Errorf("reading a gateway: %w", scanErr)
		}
		cfg.Gateways = append(cfg.Gateways, g)
	}
	if rowsErr := gateways.Err(); rowsErr != nil {
		return rowsErr
	}

	tags, err := h.db.QueryContext(ctx, `
		SELECT t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize,
		       COALESCE(t.historize_deadband, 0), COALESCE(t.sort_order, 0), t.json_path,
		       COALESCE(t.scaling_enabled, false), COALESCE(t.scaling_raw_min, 0),
		       COALESCE(t.scaling_raw_max, 0), COALESCE(t.scaling_eu_min, 0),
		       COALESCE(t.scaling_eu_max, 0), COALESCE(t.scaling_clamp, false),
		       COALESCE(t.eu_unit, ''), COALESCE(t.eu_decimals, 0), COALESCE(t.invert, false)
		FROM tags t
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 ORDER BY t.id`, orgID)
	if err != nil {
		return fmt.Errorf("tags: %w", err)
	}
	defer func() { _ = tags.Close() }()
	for tags.Next() {
		var t models.Tag
		if scanErr := tags.Scan(&t.ID, &t.GatewayID, &t.Code, &t.Alias, &t.DataType,
			&t.Historize, &t.HistorizeDeadband, &t.SortOrder, &t.JsonPath,
			&t.ScalingEnabled, &t.ScalingRawMin, &t.ScalingRawMax,
			&t.ScalingEuMin, &t.ScalingEuMax, &t.ScalingClamp,
			&t.EuUnit, &t.EuDecimals, &t.Invert); scanErr != nil {
			return fmt.Errorf("reading a tag: %w", scanErr)
		}
		cfg.Tags = append(cfg.Tags, t)
	}
	if rowsErr := tags.Err(); rowsErr != nil {
		return rowsErr
	}

	alarms, err := h.db.QueryContext(ctx, `
		SELECT ad.id, ad.tag_id, ad.alarm_type, ad.threshold, COALESCE(ad.deadband, 0),
		       COALESCE(ad.delay_seconds, 0), COALESCE(ad.severity, 'warning'),
		       COALESCE(ad.message, ''), COALESCE(ad.enabled, true)
		FROM alarm_definitions ad
		JOIN tags t ON t.id = ad.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 ORDER BY ad.id`, orgID)
	if err != nil {
		return fmt.Errorf("alarm definitions: %w", err)
	}
	defer func() { _ = alarms.Close() }()
	for alarms.Next() {
		var a models.AlarmDefinition
		if scanErr := alarms.Scan(&a.ID, &a.TagID, &a.AlarmType, &a.Threshold, &a.Deadband,
			&a.DelaySeconds, &a.Severity, &a.Message, &a.Enabled); scanErr != nil {
			return fmt.Errorf("reading an alarm definition: %w", scanErr)
		}
		cfg.Alarms = append(cfg.Alarms, a)
	}
	return alarms.Err()
}
