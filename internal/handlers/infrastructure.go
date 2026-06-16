package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

type InfrastructureHandler struct{ db *sql.DB }

func NewInfrastructureHandler(db *sql.DB) *InfrastructureHandler {
	return &InfrastructureHandler{db: db}
}

type GatewayInfraEntry struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	DriverType   string     `json:"driver_type"`
	OrgID        int        `json:"org_id"`
	OrgName      string     `json:"org_name"`
	Host         string     `json:"host"`
	Port         int        `json:"port"`
	Enabled      bool       `json:"enabled"`
	Online       bool       `json:"online"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	LastSeenIP   *string    `json:"last_seen_ip"`
	AgentVersion *string    `json:"agent_version"`
	TagCount     int        `json:"tag_count"`
	TLSEnabled   bool       `json:"tls_enabled"`
	MQTTAuth     bool       `json:"mqtt_auth"`
}

type InfrastructureSummary struct {
	Total      int `json:"total"`
	Online     int `json:"online"`
	Offline    int `json:"offline"`
	TLSMissing int `json:"tls_missing"`
	MQTTAuthOK int `json:"mqtt_auth_ok"`
}

type InfrastructureResponse struct {
	Gateways []GatewayInfraEntry  `json:"gateways"`
	Summary  InfrastructureSummary `json:"summary"`
}

func (h *InfrastructureHandler) List(c *gin.Context) {
	isGlobal := middleware.IsGlobalAdmin(c)
	orgID, _ := c.Get("org_id")

	var orgIDPtr *int
	if !isGlobal {
		if oid, ok := orgID.(int); ok {
			orgIDPtr = &oid
		}
	}

	query := `
		SELECT g.id, g.name, g.driver_type, g.connection_config, g.enabled,
		       g.last_seen_at, g.last_seen_ip::text, g.agent_version,
		       COUNT(t.id) as tag_count,
		       o.id as org_id, o.name as org_name
		FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		JOIN organizations o ON o.id = s.org_id
		LEFT JOIN tags t ON t.gateway_id = g.id
		WHERE ($1::int IS NULL OR o.id = $1)
		GROUP BY g.id, o.id, o.name, g.last_seen_at, g.last_seen_ip, g.agent_version
		ORDER BY o.name, g.name`

	var queryArg interface{}
	if orgIDPtr != nil {
		queryArg = *orgIDPtr
	}

	rows, err := h.db.QueryContext(c.Request.Context(), query, queryArg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var gateways []GatewayInfraEntry
	for rows.Next() {
		var entry GatewayInfraEntry
		var connConfig []byte
		var lastSeenAt sql.NullTime
		var lastSeenIP sql.NullString
		var agentVersion sql.NullString

		if err := rows.Scan(
			&entry.ID, &entry.Name, &entry.DriverType, &connConfig, &entry.Enabled,
			&lastSeenAt, &lastSeenIP, &agentVersion,
			&entry.TagCount, &entry.OrgID, &entry.OrgName,
		); err != nil {
			continue
		}

		if lastSeenAt.Valid {
			entry.LastSeenAt = &lastSeenAt.Time
			entry.Online = time.Since(lastSeenAt.Time) < 5*time.Minute
		}
		if lastSeenIP.Valid {
			entry.LastSeenIP = &lastSeenIP.String
		}
		if agentVersion.Valid {
			entry.AgentVersion = &agentVersion.String
		}

		// Parse connection_config
		var cfg map[string]interface{}
		if len(connConfig) > 0 {
			if err := json.Unmarshal(connConfig, &cfg); err == nil {
				if h, ok := cfg["host"].(string); ok {
					entry.Host = h
				}
				if p, ok := cfg["port"].(float64); ok {
					entry.Port = int(p)
				}
			}
		}

		// Default port by driver
		if entry.Port == 0 {
			switch entry.DriverType {
			case "MODBUS":
				entry.Port = 502
			case "OPC_UA":
				entry.Port = 4840
			case "S7":
				entry.Port = 102
			case "MQTT":
				entry.Port = 1883
			}
		}

		// TLS detection
		switch entry.DriverType {
		case "LORAWAN":
			entry.TLSEnabled = true
		default:
			entry.TLSEnabled = entry.Port == 8883 || entry.Port == 4843
		}

		entry.MQTTAuth = true // always true since dynsec

		gateways = append(gateways, entry)
	}

	if gateways == nil {
		gateways = []GatewayInfraEntry{}
	}

	summary := InfrastructureSummary{
		Total: len(gateways),
	}
	for _, g := range gateways {
		if g.Online {
			summary.Online++
		} else {
			summary.Offline++
		}
		if !g.TLSEnabled {
			summary.TLSMissing++
		}
		if g.MQTTAuth {
			summary.MQTTAuthOK++
		}
	}

	c.JSON(http.StatusOK, InfrastructureResponse{
		Gateways: gateways,
		Summary:  summary,
	})
}
