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
package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
		FPort         int                    `json:"f_port"`
		DecodedPayload map[string]interface{} `json:"decoded_payload"`
		RxMetadata    []struct {
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
	Object map[string]interface{} `json:"object"`
	RxInfo []struct {
		RSSI float64 `json:"rssi"`
		SNR  float64 `json:"snr"`
	} `json:"rxInfo"`
}

// ─── Driver ───────────────────────────────────────────────────────────────────

type Driver struct {
	gatewayID  int
	gateway    GatewayRow
	cfg        ConnectionConfig
	tags       []TagRow
	lnsClient  paho.Client // subscribes to LNS MQTT
	sysClient  paho.Client // publishes to OpenEdge internal MQTT
	pubPrefix  string      // data/{org}/{site}/{area}/{gateway}
	stopChan   chan struct{}
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

	d := &Driver{gatewayID: gatewayID, stopChan: make(chan struct{})}
	if err = d.loadConfig(db); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Internal OpenEdge MQTT (for publishing data/#)
	d.sysClient, err = connectMQTT(
		getEnv("MQTT_HOST", "mosquitto"),
		getEnvInt("MQTT_PORT", 1883),
		fmt.Sprintf("driver-lorawan-%d", gatewayID),
		"", "", false,
		fmt.Sprintf("sys/health/%d", gatewayID),
		buildOfflineStatus(gatewayID),
	)
	if err != nil {
		return fmt.Errorf("internal MQTT connect: %w", err)
	}
	defer d.sysClient.Disconnect(500)

	d.publishHealth("starting")

	// LNS MQTT (TTN / ChirpStack)
	lnsClientID := fmt.Sprintf("openedge-lorawan-%d-%d", gatewayID, rand.Int31n(10000))
	d.lnsClient, err = connectMQTT(
		d.cfg.ServerHost,
		d.cfg.ServerPort,
		lnsClientID,
		d.cfg.Username,
		d.cfg.Password,
		d.cfg.TLSEnabled,
		"", "",
	)
	if err != nil {
		d.publishHealth("error")
		return fmt.Errorf("LNS MQTT connect: %w", err)
	}
	defer d.lnsClient.Disconnect(500)

	d.subscribe()
	d.publishHealth("online")
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
		if err := rows.Scan(&t.ID, &t.Code, &t.Alias, &t.DataType); err == nil {
			tags = append(tags, t)
		}
	}
	d.tags = tags
	return nil
}

// ─── MQTT subscriptions ───────────────────────────────────────────────────────

func (d *Driver) subscribe() {
	var topic string
	switch d.cfg.ServerType {
	case "chirpstack":
		topic = fmt.Sprintf("application/%s/device/+/event/up", d.cfg.ApplicationID)
	default: // ttn_v3
		topic = fmt.Sprintf("v3/%s/devices/+/up", d.cfg.ApplicationID)
	}
	log.Printf("Subscribing to LNS topic: %s", topic)
	if tok := d.lnsClient.Subscribe(topic, 1, d.handleMessage); tok.Wait() && tok.Error() != nil {
		log.Printf("Subscribe failed: %v", tok.Error())
	}
}

// handleMessage is called for every LNS uplink. It dispatches to the
// appropriate parser depending on server_type.
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
	if devID == "" {
		devID = strings.ToLower(up.EndDeviceIDs.DevEUI)
	}
	ts := up.ReceivedAt.UnixMilli()
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}

	// Build lookup: field → value from decoded_payload + special fields
	fields := make(map[string]interface{})
	for k, v := range up.UplinkMessage.DecodedPayload {
		fields[k] = v
	}
	if len(up.UplinkMessage.RxMetadata) > 0 {
		fields["rssi"] = up.UplinkMessage.RxMetadata[0].RSSI
		fields["snr"] = up.UplinkMessage.RxMetadata[0].SNR
	}
	fields["f_port"] = up.UplinkMessage.FPort

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
	if devID == "" {
		devID = strings.ToLower(up.DeviceInfo.DevEUI)
	}
	ts := up.Time.UnixMilli()
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}

	fields := make(map[string]interface{})
	for k, v := range up.Object {
		fields[k] = v
	}
	if len(up.RxInfo) > 0 {
		fields["rssi"] = up.RxInfo[0].RSSI
		fields["snr"] = up.RxInfo[0].SNR
	}
	fields["f_port"] = up.FPort

	d.publishFields(devID, fields, ts)
}

// ─── Tag matching & publishing ────────────────────────────────────────────────

// publishFields matches the received device fields against the tag list and
// publishes each matched value to OpenEdge data/# MQTT.
func (d *Driver) publishFields(deviceID string, fields map[string]interface{}, tsMs int64) {
	for _, tag := range d.tags {
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
		if tok := d.sysClient.Publish(topic, 1, false, payload); tok.Wait() && tok.Error() != nil {
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

func connectMQTT(host string, port int, clientID, user, pass string, useTLS bool, lwtTopic, lwtPayload string) (paho.Client, error) {
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
