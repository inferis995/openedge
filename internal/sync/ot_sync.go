package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"
)

// mapProtocol converts a gateway driver_type to a human-readable protocol name.
func mapProtocol(driverType string) string {
	switch strings.ToUpper(driverType) {
	case "MODBUS_TCP", "MODBUS":
		return "Modbus"
	case "OPC_UA", "OPCUA":
		return "OPC-UA"
	case "S7", "S7_ISO":
		return "S7/ISO-TCP"
	case "MQTT":
		return "MQTT"
	case "LORAWAN":
		return "LoRaWAN"
	case "OPC_DA", "OPCDA":
		return "OPC-DA"
	default:
		return driverType
	}
}

// computeAssetRiskScore calculates a risk score [0,10] for an OT asset.
func computeAssetRiskScore(driverType string, lastSeenAt *time.Time, agentVersion string, tlsEnabled bool, securityMode string) float64 {
	score := 3.0

	proto := strings.ToUpper(driverType)
	switch proto {
	case "MODBUS_TCP", "MODBUS", "S7", "S7_ISO", "OPC_DA", "OPCDA":
		score += 2.0
	case "OPC_UA", "OPCUA", "MQTT":
		score -= 1.0
	}

	if lastSeenAt != nil {
		age := time.Since(*lastSeenAt)
		if age > 48*time.Hour {
			score += 2.0
		} else if age > 24*time.Hour {
			score += 1.0
		}
	} else {
		// never seen — treat as offline >48h
		score += 2.0
	}

	if agentVersion == "" {
		score += 0.5
	}

	if tlsEnabled || (securityMode != "" && strings.ToLower(securityMode) != "none") {
		score -= 1.5
	}

	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}
	return score
}

// SyncGatewayAssets syncs gateways for a single org into ot_assets.
// Returns (created, updated, error).
func SyncGatewayAssets(ctx context.Context, db *sql.DB, orgID int) (int, int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT g.id, g.name, g.driver_type,
		       COALESCE(g.connection_config::text, '{}'),
		       g.last_seen_at, g.agent_version
		FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 AND g.enabled = true
	`, orgID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	created := 0
	updated := 0

	for rows.Next() {
		var (
			gatewayID    int
			name         string
			driverType   string
			configJSON   string
			lastSeenAt   *time.Time
			agentVersion sql.NullString
		)
		if err := rows.Scan(&gatewayID, &name, &driverType, &configJSON, &lastSeenAt, &agentVersion); err != nil {
			log.Printf("[OTSync] scan error: %v", err)
			continue
		}

		// Parse connection config for IP and TLS details
		var cfg map[string]interface{}
		_ = json.Unmarshal([]byte(configJSON), &cfg)

		ipAddress := ""
		if v, ok := cfg["host"]; ok {
			ipAddress = strings.TrimSpace(strings.Split(v.(string), ":")[0])
		}
		if ipAddress == "" {
			if v, ok := cfg["ip"]; ok {
				ipAddress = strings.TrimSpace(v.(string))
			}
		}

		tlsEnabled := false
		if v, ok := cfg["tls"]; ok {
			switch t := v.(type) {
			case bool:
				tlsEnabled = t
			}
		}
		if v, ok := cfg["tls_enabled"]; ok {
			switch t := v.(type) {
			case bool:
				tlsEnabled = t
			}
		}

		securityMode := ""
		if v, ok := cfg["security_mode"]; ok {
			securityMode, _ = v.(string)
		}

		agentVer := agentVersion.String

		protocol := mapProtocol(driverType)
		riskScore := computeAssetRiskScore(driverType, lastSeenAt, agentVer, tlsEnabled, securityMode)

		// Determine status
		status := "offline"
		if lastSeenAt != nil && time.Since(*lastSeenAt) < 5*time.Minute {
			status = "online"
		}

		// Build upsert — primary key conflict on (org_id, ip_address) when IP is known,
		// otherwise fall back to gateway_id uniqueness via a separate upsert.
		if ipAddress != "" {
			var existing int
			err := db.QueryRowContext(ctx,
				`SELECT id FROM ot_assets WHERE org_id=$1 AND ip_address=$2`, orgID, ipAddress,
			).Scan(&existing)

			if err == sql.ErrNoRows {
				_, err2 := db.ExecContext(ctx, `
					INSERT INTO ot_assets (org_id, name, ip_address, protocol, status,
					    risk_score, agent_version, last_seen_at, gateway_id)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				`, orgID, name, ipAddress, protocol, status, riskScore, agentVer, lastSeenAt, gatewayID)
				if err2 != nil {
					log.Printf("[OTSync] insert error: %v", err2)
				} else {
					created++
				}
			} else if err == nil {
				_, err2 := db.ExecContext(ctx, `
					UPDATE ot_assets
					SET name=$2, protocol=$3, status=$4, risk_score=$5,
					    agent_version=$6, last_seen_at=$7, gateway_id=$8, updated_at=now()
					WHERE id=$1
				`, existing, name, protocol, status, riskScore, agentVer, lastSeenAt, gatewayID)
				if err2 != nil {
					log.Printf("[OTSync] update error: %v", err2)
				} else {
					updated++
				}
			}
		} else {
			// No IP — use gateway_id as the identity key
			var existing int
			err := db.QueryRowContext(ctx,
				`SELECT id FROM ot_assets WHERE org_id=$1 AND gateway_id=$2`, orgID, gatewayID,
			).Scan(&existing)

			if err == sql.ErrNoRows {
				_, err2 := db.ExecContext(ctx, `
					INSERT INTO ot_assets (org_id, name, protocol, status,
					    risk_score, agent_version, last_seen_at, gateway_id)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
				`, orgID, name, protocol, status, riskScore, agentVer, lastSeenAt, gatewayID)
				if err2 != nil {
					log.Printf("[OTSync] insert (no-ip) error: %v", err2)
				} else {
					created++
				}
			} else if err == nil {
				_, err2 := db.ExecContext(ctx, `
					UPDATE ot_assets
					SET name=$2, protocol=$3, status=$4, risk_score=$5,
					    agent_version=$6, last_seen_at=$7, updated_at=now()
					WHERE id=$1
				`, existing, name, protocol, status, riskScore, agentVer, lastSeenAt)
				if err2 != nil {
					log.Printf("[OTSync] update (no-ip) error: %v", err2)
				} else {
					updated++
				}
			}
		}
	}
	return created, updated, rows.Err()
}

// SyncAllOrgs iterates over all organizations and syncs their gateway assets.
func SyncAllOrgs(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM organizations`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var orgID int
		if err := rows.Scan(&orgID); err != nil {
			continue
		}
		created, updated, err := SyncGatewayAssets(ctx, db, orgID)
		if err != nil {
			log.Printf("[OTSync] org %d sync error: %v", orgID, err)
			continue
		}
		if created+updated > 0 {
			log.Printf("[OTSync] org %d: created=%d updated=%d", orgID, created, updated)
		}
	}
	return rows.Err()
}

// StartAssetSyncWorker launches a background goroutine that syncs gateway assets
// for all organizations every hour, after an initial 2-minute delay.
func StartAssetSyncWorker(db *sql.DB) {
	go func() {
		time.Sleep(2 * time.Minute)
		log.Println("[OTSync] Asset sync worker started")
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		// Run immediately after initial delay
		if err := SyncAllOrgs(context.Background(), db); err != nil {
			log.Printf("[OTSync] initial sync error: %v", err)
		}

		for range ticker.C {
			if err := SyncAllOrgs(context.Background(), db); err != nil {
				log.Printf("[OTSync] periodic sync error: %v", err)
			}
		}
	}()
}
