package handlers

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
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

// EdgeConfig is the full configuration payload returned to the edge manager.
type EdgeConfig struct {
	OrgID    int           `json:"org_id"`
	OrgName  string        `json:"org_name"`
	MQTT     EdgeMQTTCreds `json:"mqtt"`
	Gateways []EdgeGateway `json:"gateways"`
}

// EdgeMQTTCreds contains the broker address and per-org credentials.
type EdgeMQTTCreds struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// EdgeGateway is a minimal gateway descriptor for the edge manager.
type EdgeGateway struct {
	ID               int                    `json:"id"`
	Name             string                 `json:"name"`
	DriverType       string                 `json:"driver_type"`
	ConnectionConfig models.ConnectionConfig `json:"connection_config"`
	ScanRateMs       int                    `json:"scan_rate_ms"`
	Enabled          bool                   `json:"enabled"`
}

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

	// Gateways belonging to this org
	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT g.id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled
		 FROM gateways g
		 JOIN areas a ON g.area_id = a.id
		 JOIN sites s ON a.site_id = s.id
		 WHERE s.org_id = $1
		 ORDER BY g.id`,
		orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load gateways"})
		return
	}
	defer rows.Close() //nolint:errcheck

	var gateways []EdgeGateway
	for rows.Next() {
		var gw EdgeGateway
		if err := rows.Scan(
			&gw.ID, &gw.Name, &gw.DriverType,
			&gw.ConnectionConfig, &gw.ScanRateMs, &gw.Enabled,
		); err != nil {
			continue
		}
		gateways = append(gateways, gw)
	}
	if gateways == nil {
		gateways = []EdgeGateway{}
	}

	cfg := EdgeConfig{
		OrgID:   orgID,
		OrgName: orgName,
		MQTT: EdgeMQTTCreds{
			Host:     mqttPublicHost(),
			Port:     mqttPublicPort(),
			Username: mqttUser,
			Password: mqttPass,
		},
		Gateways: gateways,
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
