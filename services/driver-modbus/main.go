package main

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/modbus"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
)

// TagInBlock stores a tag with its pre-computed offset within a block
type TagInBlock struct {
	Tag       models.Tag
	Offset    uint16 // Pre-computed 0-based offset
	BitOffset *int   // Optional bit offset
}

// Block represents a contiguous block of Modbus registers
type Block struct {
	StartAddress uint16
	Count        uint16
	DataType     string       // "holding", "input", "coil", "discrete"
	Tags         []TagInBlock // Tags with pre-computed offsets
}

type GatewayConfig struct {
	Gateway models.Gateway
	Tags    []models.Tag
	Blocks  []Block
	OrgName string
	Site    string
	Area    string
}

type TagPayload struct {
	TagID     int         `json:"tag_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

type Driver struct {
	gatewayID      int
	database       *sql.DB
	mqttClient     *mqtt.Client
	modbusClient   *modbus.Client
	config         *GatewayConfig
	configMu       sync.RWMutex
	stopChan       chan struct{}
	reloadChan     chan struct{}
	previousValues map[int]interface{}
	prevValuesMu   sync.RWMutex
}

func main() {
	log.Println("[DRIVER] Starting driver-modbus...")

	gatewayIDStr := getEnv("GATEWAY_ID", "")
	if gatewayIDStr == "" {
		log.Fatal("[DRIVER] GATEWAY_ID environment variable is required")
	}
	gatewayID, _ := strconv.Atoi(gatewayIDStr)

	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		Database: getEnv("DB_NAME", "industrial_edge"),
	}

	database, err := db.Connect(dbCfg)
	if err != nil {
		log.Fatalf("[DRIVER] Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("[DRIVER] Connected to PostgreSQL")

	mqttCfg := mqtt.Config{
		Host:          getEnv("MQTT_HOST", "localhost"),
		Port:          getEnvInt("MQTT_PORT", 1883),
		ClientID:      fmt.Sprintf("driver-modbus-%d", gatewayID),
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		LWTTopic:      fmt.Sprintf("sys/health/%d", gatewayID),
		LWTPayload:    "offline",
		LWTRetained:   true,
	}

	mqttClient := mqtt.NewClient(mqttCfg)
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("[DRIVER] Failed to connect to MQTT broker: %v", err)
	}
	defer mqttClient.Disconnect(1000)
	log.Println("[DRIVER] Connected to MQTT broker")

	// Publish online status (matches LWT topic for health tracking)
	healthTopic := fmt.Sprintf("sys/health/%d", gatewayID)
	mqttClient.PublishWithQoS(healthTopic, "online", 1, true)
	log.Printf("[DRIVER] Published online status to %s", healthTopic)

	driver := &Driver{
		gatewayID:      gatewayID,
		database:       database,
		mqttClient:     mqttClient,
		stopChan:       make(chan struct{}),
		reloadChan:     make(chan struct{}, 1),
		previousValues: make(map[int]interface{}),
	}

	// Load initial configuration
	if err := driver.loadConfig(); err != nil {
		log.Fatalf("[DRIVER] Failed to load config: %v", err)
	}

	// Subscribe to reload commands
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	mqttClient.Subscribe(reloadTopic, driver.handleReloadCommand)
	mqttClient.Subscribe("sys/command/reload/+", driver.handleReloadCommand)
	log.Printf("[DRIVER] Subscribed to reload topic: %s", reloadTopic)

	go driver.run()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[DRIVER] Shutting down...")
	close(driver.stopChan)
	if driver.modbusClient != nil {
		driver.modbusClient.Disconnect()
	}
}

func (d *Driver) handleReloadCommand(topic string, payload []byte) {
	log.Printf("[DRIVER] Received reload signal via %s", topic)
	select {
	case d.reloadChan <- struct{}{}:
	default:
	}
}

func (d *Driver) loadConfig() error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	query := `
		SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled, g.zero_based,
		       o.name as org_name, s.name as site_name, a.name as area_name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`

	var gateway models.Gateway
	var connConfigBytes []byte
	var orgName, siteName, areaName string

	err := d.database.QueryRow(query, d.gatewayID).Scan(
		&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &connConfigBytes,
		&gateway.ScanRateMs, &gateway.Enabled, &gateway.ZeroBased, &orgName, &siteName, &areaName,
	)
	if err != nil {
		return err
	}

	json.Unmarshal(connConfigBytes, &gateway.ConnectionConfig)

	tagsQuery := `SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband FROM tags WHERE gateway_id = $1`
	rows, err := d.database.Query(tagsQuery, d.gatewayID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		rows.Scan(&t.ID, &t.GatewayID, &t.Code, &t.Alias, &t.DataType, &t.Historize, &t.HistorizeDeadband)
		tags = append(tags, t)
	}

	blocks := d.createBlocks(tags, gateway.ZeroBased)

	d.config = &GatewayConfig{
		Gateway: gateway,
		Tags:    tags,
		Blocks:  blocks,
		OrgName: slugify(orgName),
		Site:    slugify(siteName),
		Area:    slugify(areaName),
	}

	log.Printf("[DRIVER] Config loaded: %d tags, %d blocks (Scan Rate: %dms)", len(tags), len(blocks), gateway.ScanRateMs)
	return nil
}

func (d *Driver) createBlocks(tags []models.Tag, zeroBased bool) []Block {
	if len(tags) == 0 {
		return nil
	}

	type TagWithAddr struct {
		Tag  models.Tag
		Addr modbus.Address
	}
	var parsed []TagWithAddr
	for _, t := range tags {
		addr, err := modbus.ParseAddress(t.Code)
		if err == nil {
			parsed = append(parsed, TagWithAddr{t, addr})
		}
	}

	sort.Slice(parsed, func(i, j int) bool {
		if parsed[i].Addr.Type != parsed[j].Addr.Type {
			return parsed[i].Addr.Type < parsed[j].Addr.Type
		}
		return parsed[i].Addr.Offset < parsed[j].Addr.Offset
	})

	var blocks []Block
	if len(parsed) == 0 {
		return blocks
	}

	curr := Block{
		DataType:     parsed[0].Addr.Type,
		StartAddress: parsed[0].Addr.Offset,
		Tags:         []TagInBlock{{parsed[0].Tag, parsed[0].Addr.Offset, parsed[0].Addr.BitOffset}},
	}
	lastEnd := parsed[0].Addr.Offset + 1

	for i := 1; i < len(parsed); i++ {
		p := parsed[i]
		if p.Addr.Type == curr.DataType && p.Addr.Offset-lastEnd <= 5 && (p.Addr.Offset-curr.StartAddress) < 100 {
			curr.Tags = append(curr.Tags, TagInBlock{p.Tag, p.Addr.Offset, p.Addr.BitOffset})
			lastEnd = p.Addr.Offset + 1
		} else {
			curr.Count = lastEnd - curr.StartAddress
			blocks = append(blocks, curr)
			curr = Block{
				DataType:     p.Addr.Type,
				StartAddress: p.Addr.Offset,
				Tags:         []TagInBlock{{p.Tag, p.Addr.Offset, p.Addr.BitOffset}},
			}
			lastEnd = p.Addr.Offset + 1
		}
	}
	curr.Count = lastEnd - curr.StartAddress
	blocks = append(blocks, curr)
	return blocks
}

func (d *Driver) run() {
	d.configMu.RLock()
	rate := time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond
	d.configMu.RUnlock()

	pollTicker := time.NewTicker(rate)
	refreshTicker := time.NewTicker(5 * time.Minute) // Fallback periodic refresh

	log.Printf("[DRIVER] Starting loop at %v", rate)

	for {
		select {
		case <-d.stopChan:
			return
		case <-d.reloadChan:
			log.Println("[DRIVER] Reloading config...")
			if err := d.loadConfig(); err == nil {
				d.configMu.RLock()
				newRate := time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond
				d.configMu.RUnlock()
				pollTicker.Reset(newRate)
			}
		case <-refreshTicker.C:
			log.Println("[DRIVER] Periodic config refresh...")
			d.loadConfig()
		case <-pollTicker.C:
			d.poll()
		}
	}
}

func (d *Driver) poll() {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil || !cfg.Gateway.Enabled {
		return
	}

	if d.modbusClient == nil || !d.modbusClient.IsConnected() {
		log.Println("[DRIVER] Connecting to Modbus...")
		client, err := modbus.NewClientFromConfig(cfg.Gateway.ConnectionConfig)
		if err != nil {
			log.Printf("[DRIVER] Connection failed: %v", err)
			return
		}
		if err := client.ConnectWithRetry(3, 1*time.Second); err != nil {
			log.Printf("[DRIVER] Connection failed: %v", err)
			return
		}
		d.modbusClient = client
		log.Println("[DRIVER] Connected.")
	}

	prefix := fmt.Sprintf("data/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name))
	ts := time.Now().UnixMilli()

	for _, b := range cfg.Blocks {
		d.readBlock(b, prefix, ts)
	}
}

func (d *Driver) readBlock(b Block, prefix string, ts int64) {
	var data []byte
	var err error

	switch b.DataType {
	case "holding":
		data, err = d.modbusClient.ReadHoldingRegisters(b.StartAddress, b.Count)
	case "input":
		data, err = d.modbusClient.ReadInputRegisters(b.StartAddress, b.Count)
	case "coil":
		data, err = d.modbusClient.ReadCoils(b.StartAddress, b.Count)
	case "discrete":
		data, err = d.modbusClient.ReadDiscreteInputs(b.StartAddress, b.Count)
	}

	if err != nil {
		log.Printf("[DRIVER] Read error (type=%s, addr=%d): %v", b.DataType, b.StartAddress, err)
		return
	}

	for _, tib := range b.Tags {
		off := tib.Offset - b.StartAddress
		val, err := parseValue(data, off, tib.Tag.DataType, tib.BitOffset, b.DataType)
		if err != nil {
			continue
		}

		if d.hasValueChanged(tib.Tag.ID, val) {
			topic := fmt.Sprintf("%s/%s", prefix, slugify(tib.Tag.Alias))
			payload, _ := json.Marshal(TagPayload{tib.Tag.ID, val, ts, 0})
			d.mqttClient.PublishWithQoS(topic, string(payload), 1, true)
			d.updatePreviousValue(tib.Tag.ID, val)
			log.Printf("[DRIVER] PUBLISHED: %s = %v", tib.Tag.Alias, val)
		}
	}
}

func parseValue(data []byte, off uint16, dtype string, boff *int, btype string) (interface{}, error) {
	if btype == "coil" || btype == "discrete" {
		// Bit based
		byteIdx := off / 8
		bitIdx := off % 8
		if int(byteIdx) >= len(data) {
			return nil, fmt.Errorf("out of bounds")
		}
		return (data[byteIdx] & (1 << bitIdx)) != 0, nil
	}

	// Register based (2 bytes per register)
	byteOff := int(off) * 2
	if byteOff+2 > len(data) {
		return nil, fmt.Errorf("out of bounds")
	}

	switch dtype {
	case "BOOL":
		if boff == nil {
			return nil, fmt.Errorf("no bit offset")
		}
		reg := binary.BigEndian.Uint16(data[byteOff:])
		return (reg >> *boff & 1) == 1, nil
	case "INT":
		return int16(binary.BigEndian.Uint16(data[byteOff:])), nil
	case "REAL":
		if byteOff+4 > len(data) {
			return nil, fmt.Errorf("out of bounds")
		}
		bits := binary.BigEndian.Uint32(data[byteOff:])
		return math.Float32frombits(bits), nil
	}
	return nil, fmt.Errorf("unsupported")
}

func (d *Driver) hasValueChanged(id int, val interface{}) bool {
	d.prevValuesMu.RLock()
	defer d.prevValuesMu.RUnlock()
	prev, ok := d.previousValues[id]
	if !ok {
		return true
	}
	return fmt.Sprintf("%v", prev) != fmt.Sprintf("%v", val)
}

func (d *Driver) updatePreviousValue(id int, val interface{}) {
	d.prevValuesMu.Lock()
	defer d.prevValuesMu.Unlock()
	d.previousValues[id] = val
}

func slugify(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "-")
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getEnvInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		i, _ := strconv.Atoi(v)
		return i
	}
	return d
}
