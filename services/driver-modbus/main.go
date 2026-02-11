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
	OrgID   int
	Site    string
	Area    string
}

type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

type Driver struct {
	gatewayID         int
	database          *sql.DB
	mqttClient        *mqtt.Client
	modbusClient      *modbus.Client
	config            *GatewayConfig
	configMu          sync.RWMutex
	stopChan          chan struct{}
	reloadChan        chan struct{}
	previousValues    map[int]interface{}
	previousQualities map[int]int
	prevValuesMu      sync.RWMutex
	isConnected       bool

	// Write Cooldown
	writeCooldowns map[int]time.Time
	cooldownMu     sync.RWMutex
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
		gatewayID:         gatewayID,
		database:          database,
		mqttClient:        mqttClient,
		stopChan:          make(chan struct{}),
		reloadChan:        make(chan struct{}, 1),
		previousValues:    make(map[int]interface{}),
		previousQualities: make(map[int]int),
		writeCooldowns:    make(map[int]time.Time),
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

	// Subscribe to write commands
	writeTopic := fmt.Sprintf("cmd/write/%d", gatewayID)
	mqttClient.Subscribe(writeTopic, driver.handleWriteCommand)
	log.Printf("[DRIVER] Subscribed to write topic: %s", writeTopic)

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

type WriteCommand struct {
	TagID    int         `json:"tag_id"`
	Code     string      `json:"code"`
	Value    interface{} `json:"value"`
	DataType string      `json:"data_type"`
}

func (d *Driver) handleWriteCommand(topic string, payload []byte) {
	log.Printf("[DRIVER] Received write command: %s", string(payload))

	var cmd WriteCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("[DRIVER] Failed to unmarshal write command: %v", err)
		return
	}

	// 1. Parse Address
	d.configMu.RLock()
	zeroBased := d.config.Gateway.ZeroBased
	d.configMu.RUnlock()
	addr, err := modbus.ParseAddress(cmd.Code, zeroBased)
	if err != nil {
		log.Printf("[DRIVER] Invalid address code '%s': %v", cmd.Code, err)
		return
	}

	// 2. Convert Value based on DataType
	var val uint16
	var val32 uint32
	var is32Bit bool

	switch cmd.DataType {
	case "BOOL":
		// For BOOL, we need to read the register first if it's packed in a register (Holding/Input)
		// But if it's Coil, we can write directly.
		if addr.Type == "coil" {
			boolVal := false
			if v, ok := cmd.Value.(bool); ok {
				boolVal = v
			} else if v, ok := cmd.Value.(float64); ok { // JSON numbers are float64
				boolVal = v != 0
			}
			if boolVal {
				val = 0xFF00
			} else {
				val = 0x0000
			}
		} else {
			// Masking write for BOOL in Register not supported yet in simple implementation
			log.Printf("[DRIVER] Write to BOOL in Register not supported yet")
			return
		}
	case "INT":
		if v, ok := cmd.Value.(float64); ok {
			val = uint16(int16(v))
		}
	case "UINT":
		if v, ok := cmd.Value.(float64); ok {
			val = uint16(v)
		}
	case "DINT", "REAL":
		is32Bit = true
		if cmd.DataType == "REAL" {
			if v, ok := cmd.Value.(float64); ok {
				val32 = math.Float32bits(float32(v))
			}
		} else {
			if v, ok := cmd.Value.(float64); ok {
				val32 = uint32(int32(v))
			}
		}
	default:
		log.Printf("[DRIVER] Unsupported write data type: %s", cmd.DataType)
		return
	}

	// 3. Execute Write
	if d.modbusClient == nil || !d.modbusClient.IsConnected() {
		log.Printf("[DRIVER] Cannot write: Modbus client not connected")
		return
	}

	var writeErr error
	if addr.Type == "coil" {
		writeErr = d.modbusClient.WriteSingleCoil(addr.Offset, val)
	} else if addr.Type == "holding" {
		if is32Bit {
			// Write 2 registers
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, val32)
			writeErr = d.modbusClient.WriteMultipleRegisters(addr.Offset, 2, b)
		} else {
			writeErr = d.modbusClient.WriteSingleRegister(addr.Offset, val)
		}
	} else {
		log.Printf("[DRIVER] Cannot write to %s (Read-only)", addr.Type)
		return
	}

	if writeErr != nil {
		log.Printf("[DRIVER] Write failed: %v", writeErr)
	} else {
		log.Printf("[DRIVER] Write successful to %s (Value: %v)", cmd.Code, cmd.Value)
		// Optimistic update of local state
		d.updateState(cmd.TagID, cmd.Value, 0)

		// Set Cooldown
		d.cooldownMu.Lock()
		d.writeCooldowns[cmd.TagID] = time.Now().Add(2 * time.Second)
		d.cooldownMu.Unlock()

		// Force publish feedback immediately
		// We need to find the alias for the tag to build the topic
		d.configMu.RLock()
		cfg := d.config
		d.configMu.RUnlock()

		if cfg != nil {
			var alias string
			for _, t := range cfg.Tags {
				if t.ID == cmd.TagID {
					alias = t.Alias
					break
				}
			}

			if alias != "" {
				topic := fmt.Sprintf("data/%s/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
				payload, _ := json.Marshal(TagPayload{cmd.TagID, cfg.OrgID, cmd.Value, time.Now().UnixMilli(), 0})
				d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)
			}
		}
	}
}

func (d *Driver) loadConfig() error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	query := `
		SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled, g.zero_based,
		       o.name as org_name, o.id as org_id, s.name as site_name, a.name as area_name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`

	var gateway models.Gateway
	var connConfigBytes []byte
	var orgName, siteName, areaName string
	var orgID int

	err := d.database.QueryRow(query, d.gatewayID).Scan(
		&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &connConfigBytes,
		&gateway.ScanRateMs, &gateway.Enabled, &gateway.ZeroBased, &orgName, &orgID, &siteName, &areaName,
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
		OrgID:   orgID,
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
		addr, err := modbus.ParseAddress(t.Code, zeroBased)
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

	log.Printf("[DEBUG] createBlocks: Processing %d tags", len(parsed))

	var blocks []Block
	if len(parsed) == 0 {
		return blocks
	}

	// Helper to get size in registers
	getSize := func(dtype string) uint16 {
		switch dtype {
		case "REAL", "FLOAT32", "DINT", "DWORD", "UDINT":
			return 2
		case "LREAL", "LINT", "INT64", "UINT64":
			return 4
		default:
			return 1
		}
	}

	curr := Block{
		DataType:     parsed[0].Addr.Type,
		StartAddress: parsed[0].Addr.Offset,
		Tags:         []TagInBlock{{parsed[0].Tag, parsed[0].Addr.Offset, parsed[0].Addr.BitOffset}},
	}
	lastEnd := parsed[0].Addr.Offset + getSize(parsed[0].Tag.DataType)

	for i := 1; i < len(parsed); i++ {
		p := parsed[i]
		size := getSize(p.Tag.DataType)

		if p.Tag.DataType == "DINT" {
			log.Printf("[DEBUG] Processing DINT: %s (Off %d). CurrBlock: Type=%s, LastEnd=%d", p.Tag.Alias, p.Addr.Offset, curr.DataType, lastEnd)
		}

		// Check continuity and block size limits
		// Allow small gaps (<= 5 registers) to be filled to reduce request count
		if p.Addr.Type == curr.DataType && p.Addr.Offset-lastEnd <= 5 && (p.Addr.Offset+size-curr.StartAddress) <= 120 {
			curr.Tags = append(curr.Tags, TagInBlock{p.Tag, p.Addr.Offset, p.Addr.BitOffset})

			// Extend block end if needed
			end := p.Addr.Offset + size
			if end > lastEnd {
				lastEnd = end
			}
		} else {
			log.Printf("[DEBUG] Block Closed. Count=%d. New Block for %s (Off %d)", lastEnd-curr.StartAddress, p.Tag.Alias, p.Addr.Offset)
			curr.Count = lastEnd - curr.StartAddress
			blocks = append(blocks, curr)
			curr = Block{
				DataType:     p.Addr.Type,
				StartAddress: p.Addr.Offset,
				Tags:         []TagInBlock{{p.Tag, p.Addr.Offset, p.Addr.BitOffset}},
			}
			lastEnd = p.Addr.Offset + size
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

	prefix := fmt.Sprintf("data/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name))
	ts := time.Now().UnixMilli()

	if d.modbusClient == nil || !d.modbusClient.IsConnected() {
		log.Println("[DRIVER] Connecting to Modbus...")
		client, err := modbus.NewClientFromConfig(cfg.Gateway.ConnectionConfig)
		if err != nil {
			log.Printf("[DRIVER] Configuration error: %v", err)
			return
		}
		if err := client.ConnectWithRetry(3, 1*time.Second); err != nil {
			log.Printf("[DRIVER] Connection failed: %v", err)
			d.setConnectionState(false)
			// Connection failed, so all tags are BAD
			d.publishBadQualityForBlocks(cfg.Blocks, prefix, ts)
			return
		}
		d.modbusClient = client
		log.Println("[DRIVER] Connected.")
		d.setConnectionState(true)
	}

	for _, b := range cfg.Blocks {
		d.readBlock(b, prefix, ts)
	}
}

func (d *Driver) publishBadQualityForBlocks(blocks []Block, prefix string, ts int64) {
	for _, b := range blocks {
		d.configMu.RLock()
		orgID := d.config.OrgID
		d.configMu.RUnlock()

		for _, tib := range b.Tags {
			topic := fmt.Sprintf("%s/%s", prefix, slugify(tib.Tag.Alias))

			// Get last known value or default
			var val interface{} = 0
			d.prevValuesMu.RLock()
			if v, ok := d.previousValues[tib.Tag.ID]; ok {
				val = v
			}
			d.prevValuesMu.RUnlock()

			// Quality 2 = BAD
			payload, _ := json.Marshal(TagPayload{tib.Tag.ID, orgID, val, ts, 2})
			d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)

			// Update quality state to BAD so we detect recovery later
			d.updateState(tib.Tag.ID, val, 2)
		}
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

		// Force disconnect to trigger reconnection on next poll (Fixes broken pipe loop)
		if d.modbusClient != nil {
			d.modbusClient.Disconnect()
		}
		d.setConnectionState(false)

		// Publish BAD quality for all tags in this block
		d.configMu.RLock()
		orgID := d.config.OrgID
		d.configMu.RUnlock()

		for _, tib := range b.Tags {
			topic := fmt.Sprintf("%s/%s", prefix, slugify(tib.Tag.Alias))

			// Get last known value or default
			var val interface{} = 0
			d.prevValuesMu.RLock()
			if v, ok := d.previousValues[tib.Tag.ID]; ok {
				val = v
			}
			d.prevValuesMu.RUnlock()

			// Quality 2 = BAD
			payload, _ := json.Marshal(TagPayload{tib.Tag.ID, orgID, val, ts, 2})
			d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)

			// Update quality state to BAD so we detect recovery later
			log.Printf("[DEBUG] ID %d: Setting Quality BAD (2). Previous: %v", tib.Tag.ID, val)
			d.updateState(tib.Tag.ID, val, 2)
		}
		return
	}

	for _, tib := range b.Tags {
		off := tib.Offset - b.StartAddress
		val, err := parseValue(data, off, tib.Tag.DataType, tib.BitOffset, b.DataType)
		if err != nil {
			// Log errors for DINT tags to debug the issue
			if tib.Tag.DataType == "DINT" {
				byteOff := int(off) * 2
				log.Printf("[DEBUG] DINT ERROR: %s (TagOffset=%d, BlockStart=%d, RelOff=%d, ByteOff=%d, DataLen=%d): %v",
					tib.Tag.Alias, tib.Offset, b.StartAddress, off, byteOff, len(data), err)
			}
			continue
		}

		// COOLDOWN CHECK
		d.cooldownMu.RLock()
		cooldownUntil, hasCooldown := d.writeCooldowns[tib.Tag.ID]
		d.cooldownMu.RUnlock()

		if hasCooldown {
			if time.Now().Before(cooldownUntil) {
				// We are in cooldown. Check if PLC value matches our optimistic value.
				d.prevValuesMu.RLock()
				optValue, exists := d.previousValues[tib.Tag.ID]
				d.prevValuesMu.RUnlock()

				if exists && valuesEqual(val, optValue) {
					// PLC has caught up! Clear cooldown early.
					d.cooldownMu.Lock()
					delete(d.writeCooldowns, tib.Tag.ID)
					d.cooldownMu.Unlock()
					log.Printf("[DRIVER] Write confirmed for tag %s", tib.Tag.Alias)
				} else {
					// PLC still shows old value. Ignore.
					continue
				}
			} else {
				// Cooldown expired
				d.cooldownMu.Lock()
				delete(d.writeCooldowns, tib.Tag.ID)
				d.cooldownMu.Unlock()
			}
		}

		if tib.Tag.Alias == "HMI_CFG_HBeg_1" {
			byteOff := int(off) * 2
			if byteOff+4 <= len(data) {
				raw := data[byteOff : byteOff+4]
				log.Printf("[DEBUG] %s (Offset %d): Raw Bytes = % X, Parsed = %v", tib.Tag.Alias, tib.Offset, raw, val)
			}
		}

		if d.shouldPublish(tib.Tag.ID, val, 0) {
			topic := fmt.Sprintf("%s/%s", prefix, slugify(tib.Tag.Alias))
			d.configMu.RLock()
			orgID := d.config.OrgID
			d.configMu.RUnlock()
			payload, _ := json.Marshal(TagPayload{tib.Tag.ID, orgID, val, ts, 0})
			d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)
			d.updateState(tib.Tag.ID, val, 0)
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
	case "DINT":
		if byteOff+4 > len(data) {
			return nil, fmt.Errorf("out of bounds")
		}
		return int32(binary.BigEndian.Uint32(data[byteOff:])), nil
	}
	return nil, fmt.Errorf("unsupported")
}

func (d *Driver) shouldPublish(id int, val interface{}, quality int) bool {
	// CYCLIC REPORTING: Always publish on every scan cycle (user requested)
	// Original RBE logic disabled to show continuous data flow in UI
	return true
}

func (d *Driver) updateState(id int, val interface{}, quality int) {
	d.prevValuesMu.Lock()
	defer d.prevValuesMu.Unlock()
	d.previousValues[id] = val
	d.previousQualities[id] = quality
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
func (d *Driver) setConnectionState(connected bool) {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	if d.isConnected == connected {
		return
	}

	d.isConnected = connected
	status := "offline"
	if connected {
		status = "online"
	}

	topic := fmt.Sprintf("sys/health/%d", d.gatewayID)
	// Retain=true so Last Known Status is available
	d.mqttClient.PublishWithQoS(topic, status, 1, true)
	log.Printf("[DRIVER] Health status changed to: %s", status)
}

// valuesEqual compares two values for equality
func valuesEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Handle different types
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case int16:
		bv, ok := b.(int16)
		return ok && av == bv
	case uint16:
		bv, ok := b.(uint16)
		return ok && av == bv
	case int32:
		bv, ok := b.(int32)
		return ok && av == bv
	case uint32:
		bv, ok := b.(uint32)
		return ok && av == bv
	case float32:
		bv, ok := b.(float32)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case int:
		bv, ok := b.(int)
		return ok && av == bv
	default:
		// Fallback to direct comparison
		return a == b
	}
}
