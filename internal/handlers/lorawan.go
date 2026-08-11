package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// LoRaWANHandler provides REST endpoints for LoRaWAN device management:
//   - GET  /api/gateways/:id/lorawan/devices          list auto-discovered devices
//   - POST /api/gateways/:id/lorawan/devices/import   bulk-create tags from fields
//   - POST /api/gateways/:id/lorawan/downlink         send downlink command to device
type LoRaWANHandler struct {
	db         *sql.DB
	mqttClient MQTTClient
}

func NewLoRaWANHandler(db *sql.DB, mqttClient MQTTClient) *LoRaWANHandler {
	return &LoRaWANHandler{db: db, mqttClient: mqttClient}
}

// ─── Device discovery ─────────────────────────────────────────────────────────

type LoRaWANDevice struct {
	ID              int                    `json:"id"`
	DeviceID        string                 `json:"device_id"`
	DevEUI          string                 `json:"dev_eui"`
	LastSeen        time.Time              `json:"last_seen"`
	LastRSSI        *float64               `json:"last_rssi"`
	LastSNR         *float64               `json:"last_snr"`
	LastFPort       *int                   `json:"last_f_port"`
	AvailableFields map[string]interface{} `json:"available_fields"`
	RawPayload      map[string]interface{} `json:"raw_payload,omitempty"`
	UplinkCount     int64                  `json:"uplink_count"`
	ExistingTags    []string               `json:"existing_tags"` // codes already imported
}

// ListDevices returns all devices discovered by the LoRaWAN driver for a gateway.
// GET /api/gateways/:id/lorawan/devices
func (h *LoRaWANHandler) ListDevices(c *gin.Context) {
	gwID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gateway id"})
		return
	}
	orgID, _ := middleware.GetOrganizationID(c)

	// Verify the gateway belongs to the org
	if !h.gatewayBelongsToOrg(gwID, orgID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "gateway not found"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT id, device_id, dev_eui, last_seen, last_rssi, last_snr,
		       last_f_port, available_fields, raw_payload, uplink_count
		FROM lorawan_devices
		WHERE gateway_id = $1
		ORDER BY last_seen DESC
	`, gwID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	// Also fetch existing tag codes for this gateway to mark which fields are already imported
	existingCodes := h.existingTagCodes(gwID)

	var devices []LoRaWANDevice
	for rows.Next() {
		var d LoRaWANDevice
		var afJSON, rawJSON []byte
		var rawNull sql.NullString
		if err := rows.Scan(&d.ID, &d.DeviceID, &d.DevEUI, &d.LastSeen,
			&d.LastRSSI, &d.LastSNR, &d.LastFPort,
			&afJSON, &rawNull, &d.UplinkCount); err != nil {
			log.Printf("[LORAWAN] device scan error: %v", err)
			continue
		}
		if err := json.Unmarshal(afJSON, &d.AvailableFields); err != nil {
			d.AvailableFields = map[string]interface{}{}
		}
		if rawNull.Valid {
			rawJSON = []byte(rawNull.String)
			_ = json.Unmarshal(rawJSON, &d.RawPayload)
		}

		// Mark which field codes are already imported as tags
		for field := range d.AvailableFields {
			code := fmt.Sprintf("%s/%s", d.DeviceID, field)
			if existingCodes[code] {
				d.ExistingTags = append(d.ExistingTags, code)
			}
		}
		devices = append(devices, d)
	}
	if devices == nil {
		devices = []LoRaWANDevice{}
	}
	c.JSON(http.StatusOK, devices)
}

// ─── Tag auto-import ──────────────────────────────────────────────────────────

type ImportTagsRequest struct {
	Devices []ImportDevice `json:"devices"`
}

type ImportDevice struct {
	DeviceID string        `json:"device_id"`
	Fields   []ImportField `json:"fields"`
}

type ImportField struct {
	Name      string `json:"name"`      // field name in payload, e.g. "temperature"
	Alias     string `json:"alias"`     // human-readable tag name
	DataType  string `json:"data_type"` // REAL | INT | BOOL | STRING
	Historize bool   `json:"historize"`
}

// ImportTags auto-creates tags for selected device fields.
// POST /api/gateways/:id/lorawan/devices/import
func (h *LoRaWANHandler) ImportTags(c *gin.Context) {
	gwID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gateway id"})
		return
	}
	orgID, _ := middleware.GetOrganizationID(c)

	if !h.gatewayBelongsToOrg(gwID, orgID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "gateway not found"})
		return
	}

	var req ImportTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Fetch current max display_order for the gateway
	var maxOrder int
	_ = h.db.QueryRowContext(c.Request.Context(),
		`SELECT COALESCE(MAX(display_order),0) FROM tags WHERE gateway_id=$1`, gwID).Scan(&maxOrder)

	created := 0
	skipped := 0
	existingCodes := h.existingTagCodes(gwID)

	for _, dev := range req.Devices {
		for _, f := range dev.Fields {
			code := fmt.Sprintf("%s/%s", dev.DeviceID, f.Name)
			if existingCodes[code] {
				skipped++
				continue
			}

			alias := f.Alias
			if alias == "" {
				alias = fmt.Sprintf("%s %s", dev.DeviceID, f.Name)
			}
			dt := strings.ToUpper(f.DataType)
			if dt == "" {
				dt = "REAL"
			}
			maxOrder++

			_, err := h.db.ExecContext(c.Request.Context(), `
				INSERT INTO tags (gateway_id, code, alias, data_type, historize, display_order, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, now(), now())
				ON CONFLICT (gateway_id, code) DO NOTHING
			`, gwID, code, alias, dt, f.Historize, maxOrder)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create tag %s: %v", code, err)})
				return
			}
			created++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"created": created,
		"skipped": skipped,
		"message": fmt.Sprintf("%d tag(s) created, %d already existed", created, skipped),
	})
}

// ─── Downlink ─────────────────────────────────────────────────────────────────

type DownlinkRequest struct {
	DeviceID   string `json:"device_id" binding:"required"`
	DevEUI     string `json:"dev_eui"`
	FPort      int    `json:"f_port"`
	PayloadHex string `json:"payload_hex" binding:"required"`
	Confirmed  bool   `json:"confirmed"`
}

// Downlink sends a downlink command to a LoRaWAN device via the driver.
// The driver subscribes to sys/lorawan/down/{gateway_id} and forwards to LNS.
// POST /api/gateways/:id/lorawan/downlink
func (h *LoRaWANHandler) Downlink(c *gin.Context) {
	gwID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gateway id"})
		return
	}
	orgID, _ := middleware.GetOrganizationID(c)

	if !h.gatewayBelongsToOrg(gwID, orgID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "gateway not found"})
		return
	}

	var req DownlinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.FPort == 0 {
		req.FPort = 1
	}

	// If dev_eui not provided, look it up from lorawan_devices
	if req.DevEUI == "" {
		if err := h.db.QueryRowContext(c.Request.Context(),
			`SELECT dev_eui FROM lorawan_devices WHERE gateway_id=$1 AND device_id=$2`,
			gwID, req.DeviceID).Scan(&req.DevEUI); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "device not found or dev_eui unknown"})
			return
		}
	}

	payload := map[string]interface{}{
		"device_id":   req.DeviceID,
		"dev_eui":     req.DevEUI,
		"f_port":      req.FPort,
		"payload_hex": strings.ToLower(strings.ReplaceAll(req.PayloadHex, " ", "")),
		"confirmed":   req.Confirmed,
	}
	topic := fmt.Sprintf("sys/lorawan/down/%d", gwID)
	if err := h.mqttClient.Publish(topic, payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MQTT publish: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   fmt.Sprintf("Downlink queued for device %s on fport %d", req.DeviceID, req.FPort),
		"device_id": req.DeviceID,
		"f_port":    req.FPort,
		"confirmed": req.Confirmed,
		"bytes":     len(req.PayloadHex) / 2,
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (h *LoRaWANHandler) gatewayBelongsToOrg(gwID, orgID int) bool {
	if orgID == 0 {
		return true // global admin
	}
	var count int
	err := h.db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE g.id = $1 AND s.org_id = $2
	`, gwID, orgID).Scan(&count)
	return err == nil && count > 0
}

func (h *LoRaWANHandler) existingTagCodes(gwID int) map[string]bool {
	codes := map[string]bool{}
	rows, err := h.db.QueryContext(context.Background(),
		`SELECT code FROM tags WHERE gateway_id=$1`, gwID)
	if err != nil {
		return codes
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if rows.Scan(&code) == nil {
			codes[code] = true
		}
	}
	return codes
}
