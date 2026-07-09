// LoRaWAN Network Server bridge driver.
//
// Connects to a LoRaWAN Network Server (The Things Network v3 or ChirpStack)
// via their MQTT integration and bridges device uplinks into the OpenEdge
// standard data/# topic hierarchy.
//
// connection_config JSON:
//
//	{
//	  "server_host":     "eu1.cloud.thethings.network",
//	  "server_port":     1883,
//	  "tls_enabled":     false,
//	  "username":        "my-app@ttn",
//	  "password":        "NNSXS.XXXXXX",
//	  "application_id":  "my-app",
//	  "server_type":     "ttn_v3"   // or "chirpstack"
//	}
//
// Tag code format: "{device_id}/{field}"
//
//	Examples:
//	  sensor-01/temperature          → decoded_payload.temperature (TTN) or object.temperature (ChirpStack)
//	  sensor-01/humidity
//	  70B3D57ED005A200/rssi          → RSSI from rx metadata (special field)
//	  sensor-01/snr                  → SNR from rx metadata
//	  sensor-01/battery              → any field in decoded payload
//	  */temperature                  → wildcard: any device's temperature field
package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	_ "github.com/lib/pq"
)

// ─── Config ──────────────────────────────────────────────────────────────────

type ConnectionConfig struct {
	ServerHost    string `json:"server_host"`
	ServerPort    int    `json:"server_port"`
	TLSEnabled    bool   `json:"tls_enabled"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	ApplicationID string `json:"application_id"`
	ServerType    string `json:"server_type"` // "ttn_v3" or "chirpstack"
}

type GatewayRow struct {
	ID               int
	Name             string
	ConnectionConfig json.RawMessage
	OrgID            int
	OrgName          string
	SiteName         string
	AreaName         string
}

type TagRow struct {
	ID       int
	Code     string // device_id/field
	Alias    string
	DataType string
}

// TagPayload is the standard OpenEdge tag value message.
type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// ─── LNS message formats ──────────────────────────────────────────────────────

// TTN v3 uplink message envelope.
type TTNUplink struct {
	EndDeviceIDs struct {
		DeviceID string `json:"device_id"`
		DevEUI   string `json:"dev_eui"`
	} `json:"end_device_ids"`
	ReceivedAt    time.Time `json:"received_at"`
	UplinkMessage struct {
		FPort          int                    `json:"f_port"`
		FRMPayload     string                 `json:"frm_payload"` // base64 raw bytes
		DecodedPayload map[string]interface{} `json:"decoded_payload"`
		RxMetadata     []struct {
			RSSI float64 `json:"rssi"`
			SNR  float64 `json:"snr"`
		} `json:"rx_metadata"`
	} `json:"uplink_message"`
}

// ChirpStack v4 uplink message envelope.
type CSUplink struct {
	DeviceInfo struct {
		DeviceName string `json:"deviceName"`
		DevEUI     string `json:"devEui"`
	} `json:"deviceInfo"`
	Time   time.Time              `json:"time"`
	FPort  int                    `json:"fPort"`
	Data   string                 `json:"data"` // base64 raw bytes
	Object map[string]interface{} `json:"object"`
	RxInfo []struct {
		RSSI float64 `json:"rssi"`
		SNR  float64 `json:"snr"`
	} `json:"rxInfo"`
}

// downlinkCmd is the internal MQTT payload for sending a downlink command.
// Published on sys/lorawan/down/{gateway_id} by core-api.
type downlinkCmd struct {
	DeviceID   string `json:"device_id"`
	DevEUI     string `json:"dev_eui"`
	FPort      int    `json:"f_port"`
	PayloadHex string `json:"payload_hex"`
	Confirmed  bool   `json:"confirmed"`
}

// ─── Driver ───────────────────────────────────────────────────────────────────

type Driver struct {
	gatewayID int
	gateway   GatewayRow
	cfg       ConnectionConfig
	tags      []TagRow
	tagsMu    sync.RWMutex // guards tags (reload goroutine vs paho handlers)
	db        *sql.DB
	lnsClient paho.Client // subscribes to LNS MQTT
	sysClient paho.Client // publishes to OpenEdge internal MQTT
	pubPrefix string      // data/{org}/{site}/{area}/{gateway}
	stopChan  chan struct{}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[LORAWAN] ")
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	gatewayIDStr := os.Getenv("GATEWAY_ID")
	if gatewayIDStr == "" {
		return fmt.Errorf("GATEWAY_ID environment variable is required")
	}
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		return fmt.Errorf("invalid GATEWAY_ID: %w", err)
	}

	db, err := connectDB()
	if err != nil {
		return fmt.Errorf("DB connect: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Printf("DB close: %v", cerr)
		}
	}()

	d := &Driver{gatewayID: gatewayID, db: db, stopChan: make(chan struct{})}
	if err = d.loadConfig(db); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Internal OpenEdge MQTT (for publishing data/# and listening for downlink commands).
	// Subscriptions are (re)established by the OnConnect handler so they survive
	// auto-reconnects (broker drops them on a clean-session reconnect).
	d.sysClient, err = connectMQTT(
		getEnv("MQTT_HOST", "mosquitto"),
		getEnvInt("MQTT_PORT", 1883),
		fmt.Sprintf("driver-lorawan-%d", gatewayID),
		"", "", false,
		fmt.Sprintf("sys/health/%d", gatewayID),
		buildOfflineStatus(gatewayID),
		d.subscribeDownlink, // subscribe to downlink commands from core-api
	)
	if err != nil {
		return fmt.Errorf("internal MQTT connect: %w", err)
	}
	defer d.sysClient.Disconnect(500)

	d.publishHealth("starting")

	// LNS MQTT (TTN / ChirpStack) — uplink subscription runs in the OnConnect
	// handler so it is re-established after every reconnect; it also publishes
	// health "online"/"error" depending on whether the subscription succeeded.
	lnsClientID := fmt.Sprintf("openedge-lorawan-%d-%d", gatewayID, rand.Int31n(10000))
	d.lnsClient, err = connectMQTT(
		d.cfg.ServerHost,
		d.cfg.ServerPort,
		lnsClientID,
		d.cfg.Username,
		d.cfg.Password,
		d.cfg.TLSEnabled,
		"", "",
		d.subscribeUplinks,
	)
	if err != nil {
		d.publishHealth("error")
		return fmt.Errorf("LNS MQTT connect: %w", err)
	}
	defer d.lnsClient.Disconnect(500)

	log.Printf("Gateway %d (%s) running — type=%s host=%s app=%s tags=%d",
		gatewayID, d.gateway.Name, d.cfg.ServerType, d.cfg.ServerHost, d.cfg.ApplicationID, len(d.tags))

	// Reload tags periodically so new tags appear without restart.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-d.stopChan:
				return
			case <-t.C:
				if reloadErr := d.loadTags(db); reloadErr != nil {
					log.Printf("Tag reload failed: %v", reloadErr)
				}
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	close(d.stopChan)
	d.publishHealth("offline")
	log.Printf("Shutting down gateway %d", gatewayID)
	return nil
}

// ─── Configuration loading ────────────────────────────────────────────────────

func (d *Driver) loadConfig(db *sql.DB) error {
	row := db.QueryRowContext(context.Background(), `
		SELECT g.id, g.name, g.connection_config,
		       o.id AS org_id, o.name AS org_name,
		       COALESCE(s.name,'') AS site_name,
		       COALESCE(a.name,'') AS area_name
		FROM   gateways g
		JOIN   areas     a ON a.id = g.area_id
		JOIN   sites     s ON s.id = a.site_id
		JOIN   organizations o ON o.id = s.org_id
		WHERE  g.id = $1`, d.gatewayID)

	var raw json.RawMessage
	if err := row.Scan(&d.gateway.ID, &d.gateway.Name, &raw,
		&d.gateway.OrgID, &d.gateway.OrgName,
		&d.gateway.SiteName, &d.gateway.AreaName); err != nil {
		return fmt.Errorf("gateway not found (id=%d): %w", d.gatewayID, err)
	}
	if err := json.Unmarshal(raw, &d.cfg); err != nil {
		return fmt.Errorf("parse connection_config: %w", err)
	}
	if d.cfg.ServerPort == 0 {
		if d.cfg.TLSEnabled {
			d.cfg.ServerPort = 8883
		} else {
			d.cfg.ServerPort = 1883
		}
	}
	if d.cfg.ServerType == "" {
		d.cfg.ServerType = "ttn_v3"
	}
	d.pubPrefix = fmt.Sprintf("data/%s/%s/%s/%s",
		slugify(d.gateway.OrgName), slugify(d.gateway.SiteName),
		slugify(d.gateway.AreaName), slugify(d.gateway.Name))

	return d.loadTags(db)
}

func (d *Driver) loadTags(db *sql.DB) error {
	rows, err := db.QueryContext(context.Background(),
		`SELECT id, code, alias, data_type FROM tags WHERE gateway_id = $1`, d.gatewayID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	var tags []TagRow
	for rows.Next() {
		var t TagRow
		if err := rows.Scan(&t.ID, &t.Code, &t.Alias, &t.DataType); err != nil {
			log.Printf("loadTags: scan failed, skipping row: %v", err)
			continue
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		// Abort the swap — keep the previous (complete) tag list.
		return fmt.Errorf("tags iteration: %w", err)
	}
	d.tagsMu.Lock()
	d.tags = tags
	d.tagsMu.Unlock()
	return nil
}

// ─── MQTT subscriptions ───────────────────────────────────────────────────────

// subscribeUplinks subscribes to the LNS uplink topic. It runs as the LNS
// client's OnConnect handler, so the subscription is re-established after
// every reconnect. Health reflects the subscription state: without an active
// subscription the driver receives no uplinks, so report "error" until a
// (re)connect succeeds in subscribing.
func (d *Driver) subscribeUplinks(c paho.Client) {
	var topic string
	switch d.cfg.ServerType {
	case "chirpstack":
		topic = fmt.Sprintf("application/%s/device/+/event/up", d.cfg.ApplicationID)
	default: // ttn_v3
		topic = fmt.Sprintf("v3/%s/devices/+/up", d.cfg.ApplicationID)
	}
	log.Printf("Subscribing to LNS uplink topic: %s", topic)
	tok := c.Subscribe(topic, 1, d.handleMessage)
	if !tok.WaitTimeout(10 * time.Second) {
		log.Printf("Subscribe timed out: %s", topic)
		d.publishHealth("error")
		return
	}
	if tok.Error() != nil {
		log.Printf("Subscribe failed: %v", tok.Error())
		d.publishHealth("error")
		return
	}
	d.publishHealth("online")
}

// subscribeDownlink listens on the internal MQTT bus for downlink commands
// published by core-api and forwards them to the LNS. It runs as the internal
// client's OnConnect handler, so the subscription is re-established after
// every reconnect.
func (d *Driver) subscribeDownlink(c paho.Client) {
	topic := fmt.Sprintf("sys/lorawan/down/%d", d.gatewayID)
	tok := c.Subscribe(topic, 1, func(_ paho.Client, msg paho.Message) {
		var cmd downlinkCmd
		if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
			log.Printf("Downlink parse error: %v", err)
			return
		}
		d.forwardDownlink(cmd)
	})
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		log.Printf("Downlink subscribe failed on %s: %v", topic, tok.Error())
		return
	}
	log.Printf("Subscribed to downlink commands on %s", topic)
}

// handleMessage dispatches incoming LNS uplinks to the correct parser.
func (d *Driver) handleMessage(_ paho.Client, msg paho.Message) {
	switch d.cfg.ServerType {
	case "chirpstack":
		d.handleCSMessage(msg.Payload())
	default:
		d.handleTTNMessage(msg.Payload())
	}
}

// ─── TTN v3 parser ────────────────────────────────────────────────────────────

func (d *Driver) handleTTNMessage(raw []byte) {
	var up TTNUplink
	if err := json.Unmarshal(raw, &up); err != nil {
		log.Printf("TTN parse error: %v", err)
		return
	}
	devID := up.EndDeviceIDs.DeviceID
	devEUI := strings.ToLower(up.EndDeviceIDs.DevEUI)
	if devID == "" {
		devID = devEUI
	}
	ts := time.Now().UnixMilli()
	if !up.ReceivedAt.IsZero() {
		ts = up.ReceivedAt.UnixMilli()
	}

	fields := make(map[string]interface{})
	for k, v := range up.UplinkMessage.DecodedPayload {
		fields[k] = v
	}
	var rssi, snr float64
	if len(up.UplinkMessage.RxMetadata) > 0 {
		rssi = up.UplinkMessage.RxMetadata[0].RSSI
		snr = up.UplinkMessage.RxMetadata[0].SNR
	}
	fields["rssi"] = rssi
	fields["snr"] = snr
	fields["f_port"] = up.UplinkMessage.FPort

	// Store raw bytes as hex for debugging
	if up.UplinkMessage.FRMPayload != "" {
		if b, err := base64.StdEncoding.DecodeString(up.UplinkMessage.FRMPayload); err == nil {
			fields["_raw_hex"] = hex.EncodeToString(b)
		}
	}

	d.upsertDevice(devID, devEUI, rssi, snr, up.UplinkMessage.FPort, fields, raw)
	d.publishFields(devID, fields, ts)
}

// ─── ChirpStack v4 parser ─────────────────────────────────────────────────────

func (d *Driver) handleCSMessage(raw []byte) {
	var up CSUplink
	if err := json.Unmarshal(raw, &up); err != nil {
		log.Printf("ChirpStack parse error: %v", err)
		return
	}
	devID := up.DeviceInfo.DeviceName
	devEUI := strings.ToLower(up.DeviceInfo.DevEUI)
	if devID == "" {
		devID = devEUI
	}
	ts := time.Now().UnixMilli()
	if !up.Time.IsZero() {
		ts = up.Time.UnixMilli()
	}

	fields := make(map[string]interface{})
	for k, v := range up.Object {
		fields[k] = v
	}
	var rssi, snr float64
	if len(up.RxInfo) > 0 {
		rssi = up.RxInfo[0].RSSI
		snr = up.RxInfo[0].SNR
	}
	fields["rssi"] = rssi
	fields["snr"] = snr
	fields["f_port"] = up.FPort

	if up.Data != "" {
		if b, err := base64.StdEncoding.DecodeString(up.Data); err == nil {
			fields["_raw_hex"] = hex.EncodeToString(b)
		}
	}

	d.upsertDevice(devID, devEUI, rssi, snr, up.FPort, fields, raw)
	d.publishFields(devID, fields, ts)
}

// ─── Device auto-discovery ────────────────────────────────────────────────────

// upsertDevice records a discovered LoRaWAN device in the database so the
// core-api can expose it to the UI for tag auto-import.
func (d *Driver) upsertDevice(deviceID, devEUI string, rssi, snr float64, fPort int, fields map[string]interface{}, rawMsg []byte) {
	// Build available_fields: field_name → example value (string for display)
	af := make(map[string]interface{})
	for k, v := range fields {
		if strings.HasPrefix(k, "_") {
			continue // skip internal fields like _raw_hex in the field list
		}
		af[k] = fmt.Sprintf("%v", v)
	}
	afJSON, _ := json.Marshal(af)

	// Store a compact version of the raw payload for debugging
	var rawJSON []byte
	var compact map[string]interface{}
	if json.Unmarshal(rawMsg, &compact) == nil {
		rawJSON, _ = json.Marshal(compact)
	}

	// Bounded timeout: this runs inside a paho message handler, so a hung DB
	// call would stall the client's ordered message dispatch.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO lorawan_devices
		    (gateway_id, device_id, dev_eui, last_seen, last_rssi, last_snr,
		     last_f_port, available_fields, raw_payload, uplink_count)
		VALUES ($1, $2, $3, now(), $4, $5, $6, $7, $8, 1)
		ON CONFLICT (gateway_id, device_id) DO UPDATE SET
		    dev_eui          = EXCLUDED.dev_eui,
		    last_seen        = now(),
		    last_rssi        = EXCLUDED.last_rssi,
		    last_snr         = EXCLUDED.last_snr,
		    last_f_port      = EXCLUDED.last_f_port,
		    available_fields = EXCLUDED.available_fields,
		    raw_payload      = EXCLUDED.raw_payload,
		    uplink_count     = lorawan_devices.uplink_count + 1
	`, d.gatewayID, deviceID, devEUI, rssi, snr, fPort, afJSON, rawJSON)
	if err != nil {
		log.Printf("upsertDevice %s: %v", deviceID, err)
	}
}

// ─── Tag matching & publishing ────────────────────────────────────────────────

// publishFields matches the received device fields against the tag list and
// publishes each matched value to OpenEdge data/# MQTT.
func (d *Driver) publishFields(deviceID string, fields map[string]interface{}, tsMs int64) {
	d.tagsMu.RLock()
	tags := d.tags
	d.tagsMu.RUnlock()
	for _, tag := range tags {
		// Tag code format: "device_id/field" — e.g. "sensor-01/temperature"
		parts := strings.SplitN(tag.Code, "/", 2)
		if len(parts) != 2 {
			continue
		}
		tagDevice, tagField := parts[0], parts[1]
		// Match by device_id (case-insensitive) or wildcard "*"
		if tagDevice != "*" && !strings.EqualFold(tagDevice, deviceID) {
			continue
		}
		rawVal, ok := fields[tagField]
		if !ok {
			continue
		}
		value := coerceValue(rawVal, tag.DataType)
		topic := fmt.Sprintf("%s/%s", d.pubPrefix, slugify(tag.Alias))
		payload, _ := json.Marshal(TagPayload{
			TagID: tag.ID, OrgID: d.gateway.OrgID,
			Value: value, Timestamp: tsMs, Quality: 0,
		})
		tok := d.sysClient.Publish(topic, 1, false, payload)
		if !tok.WaitTimeout(10 * time.Second) {
			log.Printf("Publish timeout (tag %d)", tag.ID)
		} else if tok.Error() != nil {
			log.Printf("Publish error (tag %d): %v", tag.ID, tok.Error())
		} else {
			log.Printf("tag %d [%s] = %v", tag.ID, tag.Alias, value)
		}
	}
}

// coerceValue converts a decoded JSON value to the tag's declared data type.
func coerceValue(raw interface{}, dataType string) interface{} {
	switch strings.ToUpper(dataType) {
	case "BOOL":
		switch v := raw.(type) {
		case bool:
			return v
		case float64:
			return v != 0
		}
	case "INT", "DINT":
		if f, ok := raw.(float64); ok {
			return int64(f)
		}
	case "REAL":
		if f, ok := raw.(float64); ok {
			return f
		}
	case "STRING":
		return fmt.Sprintf("%v", raw)
	}
	return raw
}

// ─── Downlink ────────────────────────────────────────────────────────────────

// forwardDownlink forwards a downlink command to the LNS via its MQTT interface.
func (d *Driver) forwardDownlink(cmd downlinkCmd) {
	if cmd.FPort == 0 {
		cmd.FPort = 1
	}
	rawBytes, err := hex.DecodeString(strings.ReplaceAll(cmd.PayloadHex, " ", ""))
	if err != nil {
		log.Printf("Downlink: invalid hex payload %q: %v", cmd.PayloadHex, err)
		return
	}
	b64 := base64.StdEncoding.EncodeToString(rawBytes)

	var topic string
	var payload []byte

	switch d.cfg.ServerType {
	case "chirpstack":
		// ChirpStack v4 downlink
		topic = fmt.Sprintf("application/%s/device/%s/command/down", d.cfg.ApplicationID, cmd.DevEUI)
		type csDownlink struct {
			DevEUI    string `json:"devEui"`
			Confirmed bool   `json:"confirmed"`
			FPort     int    `json:"fPort"`
			Data      string `json:"data"`
		}
		payload, _ = json.Marshal(csDownlink{
			DevEUI:    cmd.DevEUI,
			Confirmed: cmd.Confirmed,
			FPort:     cmd.FPort,
			Data:      b64,
		})
	default: // ttn_v3
		// TTN v3 downlink — push to queue
		topic = fmt.Sprintf("v3/%s/devices/%s/down/push", d.cfg.ApplicationID, cmd.DeviceID)
		type ttnDownlink struct {
			Downlinks []struct {
				FRMPayload string `json:"frm_payload"`
				FPort      int    `json:"f_port"`
				Priority   string `json:"priority"`
				Confirmed  bool   `json:"confirmed"`
			} `json:"downlinks"`
		}
		dl := ttnDownlink{}
		dl.Downlinks = append(dl.Downlinks, struct {
			FRMPayload string `json:"frm_payload"`
			FPort      int    `json:"f_port"`
			Priority   string `json:"priority"`
			Confirmed  bool   `json:"confirmed"`
		}{FRMPayload: b64, FPort: cmd.FPort, Priority: "NORMAL", Confirmed: cmd.Confirmed})
		payload, _ = json.Marshal(dl)
	}

	tok := d.lnsClient.Publish(topic, 1, false, payload)
	if !tok.WaitTimeout(10 * time.Second) {
		log.Printf("Downlink publish timeout for %s", cmd.DeviceID)
	} else if tok.Error() != nil {
		log.Printf("Downlink publish error: %v", tok.Error())
	} else {
		log.Printf("Downlink sent to %s (fport=%d len=%d bytes confirmed=%v)", cmd.DeviceID, cmd.FPort, len(rawBytes), cmd.Confirmed)
	}
}

// ─── Health ───────────────────────────────────────────────────────────────────

func (d *Driver) publishHealth(status string) {
	type Health struct {
		GatewayID  int    `json:"gateway_id"`
		Status     string `json:"status"`
		DriverType string `json:"driver_type"`
		Timestamp  int64  `json:"timestamp"`
	}
	payload, _ := json.Marshal(Health{
		GatewayID:  d.gatewayID,
		Status:     status,
		DriverType: "LORAWAN",
		Timestamp:  time.Now().UnixMilli(),
	})
	d.sysClient.Publish(fmt.Sprintf("sys/health/%d", d.gatewayID), 1, true, payload)
}

func buildOfflineStatus(gatewayID int) string {
	type Health struct {
		GatewayID  int    `json:"gateway_id"`
		Status     string `json:"status"`
		DriverType string `json:"driver_type"`
		Timestamp  int64  `json:"timestamp"`
	}
	b, _ := json.Marshal(Health{
		GatewayID:  gatewayID,
		Status:     "offline",
		DriverType: "LORAWAN",
		Timestamp:  time.Now().UnixMilli(),
	})
	return string(b)
}

// ─── MQTT helpers ─────────────────────────────────────────────────────────────

func connectMQTT(host string, port int, clientID, user, pass string, useTLS bool, lwtTopic, lwtPayload string, onConnect paho.OnConnectHandler) (paho.Client, error) {
	scheme := "tcp"
	if useTLS {
		scheme = "tls"
	}
	broker := fmt.Sprintf("%s://%s:%d", scheme, host, port)

	opts := paho.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetKeepAlive(60 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second)

	if user != "" {
		opts.SetUsername(user)
	}
	if pass != "" {
		opts.SetPassword(pass)
	}
	if useTLS {
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	if lwtTopic != "" && lwtPayload != "" {
		opts.SetWill(lwtTopic, lwtPayload, 1, true)
	}
	if onConnect != nil {
		// Runs on the initial connect and after every auto-reconnect, so
		// subscriptions are re-established when the broker drops them.
		opts.SetOnConnectHandler(onConnect)
	}

	client := paho.NewClient(opts)
	tok := client.Connect()
	tok.Wait()
	if tok.Error() != nil {
		return nil, fmt.Errorf("connect %s: %w", broker, tok.Error())
	}
	log.Printf("MQTT connected: %s (client=%s)", broker, clientID)
	return client, nil
}

// ─── DB ───────────────────────────────────────────────────────────────────────

func connectDB() (*sql.DB, error) {
	host := getEnv("DB_HOST", "postgres")
	port := getEnvInt("DB_PORT", 5432)
	user := getEnv("DB_USER", "industrial_user")
	pass := getEnv("DB_PASSWORD", "industrial_pass")
	dbname := getEnv("DB_NAME", "industrial_db")
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, dbname)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 10; i++ {
		if err = db.PingContext(context.Background()); err == nil {
			return db, nil
		}
		log.Printf("DB not ready (attempt %d/10): %v", i+1, err)
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("DB unreachable: %w", err)
}

// ─── Utilities ────────────────────────────────────────────────────────────────

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '/' || r == '\\' {
			b.WriteRune('-')
		}
	}
	return b.String()
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
