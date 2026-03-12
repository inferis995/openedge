package main

import (
	"context"
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

	"github.com/ralph/industrial-edge-middleware/internal/alarms"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/modbus"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
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
	isConnected       *bool // pointer to detect uninitialized state

	// Write Cooldown
	writeCooldowns map[int]time.Time
	cooldownMu     sync.RWMutex

	// Sparkplug B support
	sparkplugClient *sparkplug.SparkplugClient
	dualPublisher   *sparkplug.DualPublisher
	sparkplugMu     sync.RWMutex

	// Settings manager for publish mode and RBE
	settingsManager *settings.Manager

	// Alarm manager for evaluating threshold conditions
	alarmManager *alarms.Manager
}

const (
	maxRetries   = 30
	initialDelay = 2 * time.Second
	maxDelay     = 30 * time.Second
)

// retryWithBackoff attempts to execute a function with exponential backoff retry logic
func retryWithBackoff(operationName string, operation func() error) error {
	var lastErr error
	delay := initialDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[RETRY] %s - attempt %d/%d failed, retrying in %v...",
				operationName, attempt, maxRetries, delay)
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * 1.5)
			if delay > maxDelay {
				delay = maxDelay
			}
		}

		err := operation()
		if err == nil {
			if attempt > 0 {
				log.Printf("[RETRY] %s - succeeded on attempt %d", operationName, attempt+1)
			}
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries+1, lastErr)
}

func main() {
	log.Println("[DRIVER] Starting driver-modbus...")

	gatewayIDStr := getEnv("GATEWAY_ID", "")
	if gatewayIDStr == "" {
		log.Fatal("[DRIVER] GATEWAY_ID environment variable is required")
	}
	gatewayID, _ := strconv.Atoi(gatewayIDStr)

	var err error

	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		Database: getEnv("DB_NAME", "industrial_edge"),
	}

	var database *sql.DB
	err = retryWithBackoff("[DRIVER] Database connection", func() error {
		var err error
		database, err = db.Connect(dbCfg)
		return err
	})
	if err != nil {
		log.Fatalf("[DRIVER] Failed to connect to database after retries: %v", err)
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
	err = retryWithBackoff("[DRIVER] MQTT broker connection", func() error {
		return mqttClient.Connect()
	})
	if err != nil {
		log.Fatalf("[DRIVER] Failed to connect to MQTT broker after retries: %v", err)
	}
	defer mqttClient.Disconnect(1000)
	log.Println("[DRIVER] Connected to MQTT broker")

	// NOTE: Health status will be published by setConnectionState() when PLC connection is established
	// This ensures the status reflects actual PLC connectivity, not just driver startup

	driver := &Driver{
		gatewayID:         gatewayID,
		database:          database,
		mqttClient:        mqttClient,
		stopChan:          make(chan struct{}),
		reloadChan:        make(chan struct{}, 1),
		previousValues:    make(map[int]interface{}),
		previousQualities: make(map[int]int),
		writeCooldowns:    make(map[int]time.Time),
		settingsManager:   settings.NewManager(database),
		alarmManager:      alarms.NewManager(database, mqttClient, gatewayID),
	}

	driver.alarmManager.OnAlarmEvent = func(tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
		// Use publishDual to automatically route the alarm to Sparkplug B, Legacy, or both
		// based on the user's configured PublishMode.
		driver.publishDual(
			tagID,
			alias+"_Alarm",
			status == "ACTIVE",
			"BOOL",
			0, // Quality: 0 = GOOD in internal standard (for UI compatibility)
			time.Now().UnixMilli(),
		)
	}

	// Initialize settings
	if err := driver.settingsManager.Load(); err != nil {
		log.Printf("[DRIVER] Warning: Failed to load settings: %v", err)
	}

	// Initialize Sparkplug B client (optional - for dual publishing)
	// Enabled via environment variable: SPARKPLUG_ENABLED=true
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		log.Println("[DRIVER] Sparkplug B dual publishing enabled")
		// Sparkplug client will be initialized after config is loaded
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

	// Subscribe to settings-reload commands for hot-reload of publish mode
	mqttClient.Subscribe("sys/command/settings-reload", driver.handleSettingsReloadCommand)
	log.Printf("[DRIVER] Subscribed to settings-reload topic")

	// Subscribe to health events for auto-reload when gateway comes online
	healthTopic := "sys/health/+"
	if err := mqttClient.Subscribe(healthTopic, driver.handleHealthMessage); err != nil {
		log.Printf("[DRIVER] Failed to subscribe to health topic: %v", err)
	} else {
		log.Printf("[DRIVER] Subscribed to health topic: %s (auto-reload enabled)", healthTopic)
	}

	// Subscribe to write commands - Multiple formats for SCADA compatibility
	// Format 1: cmd/write/{gatewayID} (internal API format)
	writeTopic := fmt.Sprintf("cmd/write/%d", gatewayID)
	mqttClient.Subscribe(writeTopic, driver.handleWriteCommand)
	log.Printf("[DRIVER] Subscribed to write topic: %s", writeTopic)

	// Subscribe to legacy format writes (will be set after config load when we know org/site/area)
	// Format 2: cmd/{org}/{site}/{area}/{gateway}/{alias}
	// Format 3: spBv1.0/{group}/DCMD/{node}/{device} (Sparkplug B)

	// Start alarm ticker for delay-based alarm triggering
	go driver.alarmManager.StartTicker(context.Background())

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

// handleHealthMessage handles gateway health events for auto-reload
// Topic format: sys/health/{gateway_id}
// Payload: "online" or "offline"
func (d *Driver) handleHealthMessage(topic string, payload []byte) {
	// Parse topic: sys/health/{gateway_id}
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		return
	}

	// Extract gateway ID from topic
	gatewayIDStr := parts[2]
	healthGatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		return
	}

	// Only process health events for this driver's gateway
	if healthGatewayID != d.gatewayID {
		return
	}

	// Check if gateway is coming online
	status := strings.ToLower(strings.TrimSpace(string(payload)))
	if status == "online" {
		// Skip if this is our own health message (we just published it)
		if d.isConnected != nil && *d.isConnected {
			log.Printf("[DRIVER] Skipping own health message for gateway %d (status: %s)", d.gatewayID, status)
			return
		}
		log.Printf("[DRIVER] Gateway %d is ONLINE - signaling config reload", d.gatewayID)
		// Use reloadChan to avoid blocking the MQTT handler and ensure thread-safe reload
		select {
		case d.reloadChan <- struct{}{}:
		default:
		}
	}
}

// handleSettingsReloadCommand handles settings-reload MQTT commands
func (d *Driver) handleSettingsReloadCommand(topic string, payload []byte) {
	log.Printf("[DRIVER] Received settings-reload signal via %s", topic)
	if d.settingsManager != nil {
		if err := d.settingsManager.Load(); err != nil {
			log.Printf("[DRIVER] Failed to reload settings: %v", err)
		} else {
			cfg := d.settingsManager.Get()
			log.Printf("[DRIVER] Settings reloaded: publish_mode=%s", cfg.PublishMode)
		}
	}
	if d.alarmManager != nil {
		d.alarmManager.LoadDefinitions()
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
		d.publishWriteResult(cmd.TagID, false, "invalid command payload", nil)
		return
	}

	// 1. Parse Address
	d.configMu.RLock()
	zeroBased := d.config.Gateway.ZeroBased
	d.configMu.RUnlock()
	addr, err := modbus.ParseAddress(cmd.Code, zeroBased)
	if err != nil {
		log.Printf("[DRIVER] Invalid address code '%s': %v", cmd.Code, err)
		d.publishWriteResult(cmd.TagID, false, fmt.Sprintf("invalid address: %s", cmd.Code), nil)
		return
	}

	// 2. Check connection
	if d.modbusClient == nil || !d.modbusClient.IsConnected() {
		log.Printf("[DRIVER] Cannot write: Modbus client not connected")
		d.publishWriteResult(cmd.TagID, false, "Modbus not connected", nil)
		return
	}

	// 3. Convert value and execute write based on DataType
	var writeErr error

	switch cmd.DataType {
	case "BOOL":
		writeErr = d.writeBool(addr, cmd.Value)
	case "INT":
		if v, ok := cmd.Value.(float64); ok {
			val := uint16(int16(v))
			if addr.Type == "holding" {
				writeErr = d.modbusClient.WriteSingleRegister(addr.Offset, val)
			} else {
				writeErr = fmt.Errorf("cannot write INT to %s (read-only)", addr.Type)
			}
		} else {
			writeErr = fmt.Errorf("invalid value for INT: %v", cmd.Value)
		}
	case "UINT":
		if v, ok := cmd.Value.(float64); ok {
			val := uint16(v)
			if addr.Type == "holding" {
				writeErr = d.modbusClient.WriteSingleRegister(addr.Offset, val)
			} else {
				writeErr = fmt.Errorf("cannot write UINT to %s (read-only)", addr.Type)
			}
		} else {
			writeErr = fmt.Errorf("invalid value for UINT: %v", cmd.Value)
		}
	case "DINT":
		if v, ok := cmd.Value.(float64); ok {
			val32 := uint32(int32(v))
			if addr.Type == "holding" {
				b := make([]byte, 4)
				binary.BigEndian.PutUint32(b, val32)
				writeErr = d.modbusClient.WriteMultipleRegisters(addr.Offset, 2, b)
			} else {
				writeErr = fmt.Errorf("cannot write DINT to %s (read-only)", addr.Type)
			}
		} else {
			writeErr = fmt.Errorf("invalid value for DINT: %v", cmd.Value)
		}
	case "REAL":
		if v, ok := cmd.Value.(float64); ok {
			val32 := math.Float32bits(float32(v))
			if addr.Type == "holding" {
				b := make([]byte, 4)
				binary.BigEndian.PutUint32(b, val32)
				writeErr = d.modbusClient.WriteMultipleRegisters(addr.Offset, 2, b)
			} else {
				writeErr = fmt.Errorf("cannot write REAL to %s (read-only)", addr.Type)
			}
		} else {
			writeErr = fmt.Errorf("invalid value for REAL: %v", cmd.Value)
		}
	default:
		log.Printf("[DRIVER] Unsupported write data type: %s", cmd.DataType)
		d.publishWriteResult(cmd.TagID, false, fmt.Sprintf("unsupported data type: %s", cmd.DataType), nil)
		return
	}

	if writeErr != nil {
		log.Printf("[DRIVER] Write failed for tag %d: %v", cmd.TagID, writeErr)
		d.publishWriteResult(cmd.TagID, false, writeErr.Error(), nil)
		return
	}

	log.Printf("[DRIVER] Write successful to %s (Value: %v), performing read-back verification...", cmd.Code, cmd.Value)

	// 4. Read-back verification: read the value back from PLC and compare
	readBackValue, readBackErr := d.readBackValue(addr, cmd.DataType)
	if readBackErr != nil {
		log.Printf("[DRIVER] Read-back failed for tag %d: %v (write was successful)", cmd.TagID, readBackErr)
		// Write succeeded but read-back failed — still consider it a success with warning
		d.publishWriteResult(cmd.TagID, true, "write OK, read-back failed: "+readBackErr.Error(), cmd.Value)
	} else {
		verified := d.verifyReadBack(cmd.Value, readBackValue, cmd.DataType)
		if verified {
			log.Printf("[DRIVER] Read-back verified for tag %d: written=%v, read=%v ✓", cmd.TagID, cmd.Value, readBackValue)
			d.publishWriteResult(cmd.TagID, true, "verified", readBackValue)
		} else {
			log.Printf("[DRIVER] Read-back MISMATCH for tag %d: written=%v, read=%v ✗", cmd.TagID, cmd.Value, readBackValue)
			d.publishWriteResult(cmd.TagID, false, fmt.Sprintf("read-back mismatch: sent=%v, got=%v", cmd.Value, readBackValue), readBackValue)
		}
	}

	// 5. Update local state and publish feedback
	d.updateState(cmd.TagID, cmd.Value, 0)

	// Set Cooldown to prevent cyclic read from overwriting immediately
	d.cooldownMu.Lock()
	d.writeCooldowns[cmd.TagID] = time.Now().Add(2 * time.Second)
	d.cooldownMu.Unlock()

	// Force publish feedback immediately
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg != nil {
		var alias string
		var dataType string
		for _, t := range cfg.Tags {
			if t.ID == cmd.TagID {
				alias = t.Alias
				dataType = t.DataType
				break
			}
		}

		if alias != "" {
			d.publishDual(cmd.TagID, alias, cmd.Value, dataType, 0, time.Now().UnixMilli())
		}
	}
}

// writeBool handles writing BOOL values to both Coils and Holding Registers (bit masking)
func (d *Driver) writeBool(addr modbus.Address, value interface{}) error {
	boolVal := false
	if v, ok := value.(bool); ok {
		boolVal = v
	} else if v, ok := value.(float64); ok {
		boolVal = v != 0
	}

	if addr.Type == "coil" {
		// Direct coil write
		var coilVal uint16
		if boolVal {
			coilVal = 0xFF00
		}
		return d.modbusClient.WriteSingleCoil(addr.Offset, coilVal)
	}

	if addr.Type == "holding" {
		if addr.BitOffset == nil {
			return fmt.Errorf("BOOL write to holding register requires bit offset (e.g., 40001.0)")
		}
		// Read-Modify-Write: read current register, modify the specific bit, write back
		currentVal, err := d.modbusClient.ReadHoldingRegister(addr.Offset)
		if err != nil {
			return fmt.Errorf("read-modify-write: failed to read register %d: %w", addr.Offset, err)
		}

		bitMask := uint16(1 << uint(*addr.BitOffset))
		var newVal uint16
		if boolVal {
			newVal = currentVal | bitMask // Set bit
		} else {
			newVal = currentVal & ^bitMask // Clear bit
		}

		log.Printf("[DRIVER] BOOL write to HR%d.%d: register %d -> %d (bit %d = %v)",
			addr.Offset, *addr.BitOffset, currentVal, newVal, *addr.BitOffset, boolVal)

		return d.modbusClient.WriteSingleRegister(addr.Offset, newVal)
	}

	return fmt.Errorf("cannot write BOOL to %s (read-only)", addr.Type)
}

// readBackValue reads a value back from the PLC after a write for verification
func (d *Driver) readBackValue(addr modbus.Address, dataType string) (interface{}, error) {
	// Small delay to allow PLC to process the write
	time.Sleep(50 * time.Millisecond)

	switch dataType {
	case "BOOL":
		if addr.Type == "coil" {
			data, err := d.modbusClient.ReadCoils(addr.Offset, 1)
			if err != nil {
				return nil, err
			}
			if len(data) == 0 {
				return nil, fmt.Errorf("empty coil response")
			}
			return (data[0] & 0x01) == 1, nil
		}
		if addr.Type == "holding" && addr.BitOffset != nil {
			val, err := d.modbusClient.ReadHoldingRegister(addr.Offset)
			if err != nil {
				return nil, err
			}
			return (val>>uint(*addr.BitOffset))&1 == 1, nil
		}
		return nil, fmt.Errorf("unsupported BOOL address type for read-back")

	case "INT":
		val, err := d.modbusClient.ReadHoldingRegister(addr.Offset)
		if err != nil {
			return nil, err
		}
		return float64(int16(val)), nil

	case "UINT":
		val, err := d.modbusClient.ReadHoldingRegister(addr.Offset)
		if err != nil {
			return nil, err
		}
		return float64(val), nil

	case "DINT":
		data, err := d.modbusClient.ReadHoldingRegisters(addr.Offset, 2)
		if err != nil {
			return nil, err
		}
		if len(data) < 4 {
			return nil, fmt.Errorf("insufficient data for DINT read-back")
		}
		val := int32(binary.BigEndian.Uint32(data))
		return float64(val), nil

	case "REAL":
		data, err := d.modbusClient.ReadHoldingRegisters(addr.Offset, 2)
		if err != nil {
			return nil, err
		}
		if len(data) < 4 {
			return nil, fmt.Errorf("insufficient data for REAL read-back")
		}
		bits := binary.BigEndian.Uint32(data)
		val := math.Float32frombits(bits)
		return float64(val), nil

	default:
		return nil, fmt.Errorf("unsupported data type for read-back: %s", dataType)
	}
}

// verifyReadBack compares the written value with the read-back value
func (d *Driver) verifyReadBack(written, readBack interface{}, dataType string) bool {
	switch dataType {
	case "BOOL":
		// Compare booleans
		wBool := false
		if v, ok := written.(bool); ok {
			wBool = v
		} else if v, ok := written.(float64); ok {
			wBool = v != 0
		}
		rBool, ok := readBack.(bool)
		if !ok {
			return false
		}
		return wBool == rBool

	case "INT", "UINT", "DINT":
		// Compare as integers (within tolerance of 0)
		wFloat, ok1 := written.(float64)
		rFloat, ok2 := readBack.(float64)
		if !ok1 || !ok2 {
			return false
		}
		return int64(wFloat) == int64(rFloat)

	case "REAL":
		// Compare as float32 (with small epsilon for floating point precision)
		wFloat, ok1 := written.(float64)
		rFloat, ok2 := readBack.(float64)
		if !ok1 || !ok2 {
			return false
		}
		// Compare at float32 precision
		diff := math.Abs(wFloat - rFloat)
		return diff < 0.01 || (wFloat != 0 && diff/math.Abs(wFloat) < 0.001)

	default:
		return false
	}
}

// handleLegacyWriteCommand handles write commands in legacy MQTT format
// Topic format: cmd/{org}/{site}/{area}/{gateway}/{alias}
// Payload: {"value": <value>} or just <value>
func (d *Driver) handleLegacyWriteCommand(topic string, payload []byte) {
	log.Printf("[DRIVER] Received legacy write command: %s", topic)

	// Extract alias from topic
	parts := strings.Split(topic, "/")
	if len(parts) < 6 {
		log.Printf("[DRIVER] Invalid legacy write topic format: %s", topic)
		return
	}
	alias := parts[5]

	// Find tag by alias
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER] No config loaded, cannot process write")
		return
	}

	var targetTag *models.Tag
	for i := range cfg.Tags {
		if strings.EqualFold(cfg.Tags[i].Alias, alias) || strings.EqualFold(slugify(cfg.Tags[i].Alias), alias) {
			targetTag = &cfg.Tags[i]
			break
		}
	}

	if targetTag == nil {
		log.Printf("[DRIVER] Tag with alias '%s' not found", alias)
		return
	}

	// Parse value from payload
	var value interface{}
	var cmd WriteCommand

	// Try to parse as full command first
	if err := json.Unmarshal(payload, &cmd); err == nil && cmd.Value != nil {
		value = cmd.Value
	} else {
		// Try to parse as direct value
		if err := json.Unmarshal(payload, &value); err != nil {
			log.Printf("[DRIVER] Failed to parse value from payload: %v", err)
			return
		}
	}

	// Create WriteCommand and process it
	writeCmd := WriteCommand{
		TagID:    targetTag.ID,
		Code:     targetTag.Code,
		Value:    value,
		DataType: targetTag.DataType,
	}

	// Marshal and process through existing handler
	cmdBytes, _ := json.Marshal(writeCmd)
	d.handleWriteCommand(fmt.Sprintf("cmd/write/%d", d.gatewayID), cmdBytes)
}

// handleSparkplugDCMD handles Sparkplug B Device Commands
// Topic format: spBv1.0/{group}/DCMD/{node}/{device}
// Payload: Sparkplug B payload with metrics
func (d *Driver) handleSparkplugDCMD(topic string, payload []byte) {
	log.Printf("[DRIVER] Received Sparkplug B DCMD: %s", topic)

	// Parse Sparkplug B topic
	_, err := sparkplug.ParseTopic(topic)
	if err != nil {
		log.Printf("[DRIVER] Invalid Sparkplug DCMD topic: %v", err)
		return
	}

	// Parse Sparkplug B payload (simplified JSON format)
	var spPayload struct {
		Timestamp int64 `json:"timestamp"`
		Seq       int   `json:"seq"`
		Metrics   []struct {
			Name      string      `json:"name"`
			DataType  int         `json:"dataType"`
			Value     interface{} `json:"value"`
			Timestamp int64       `json:"timestamp"`
		} `json:"metrics"`
	}

	if err := json.Unmarshal(payload, &spPayload); err != nil {
		log.Printf("[DRIVER] Failed to parse Sparkplug DCMD payload: %v", err)
		return
	}

	// Find config
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER] No config loaded, cannot process DCMD")
		return
	}

	// Process each metric
	for _, metric := range spPayload.Metrics {
		// Find tag by alias (metric name)
		var targetTag *models.Tag
		for i := range cfg.Tags {
			alias := cfg.Tags[i].Alias
			// Match with underscores (Sparkplug format) or original alias
			if strings.EqualFold(alias, metric.Name) ||
				strings.EqualFold(strings.ReplaceAll(alias, "-", "_"), metric.Name) {
				targetTag = &cfg.Tags[i]
				break
			}
		}

		if targetTag == nil {
			log.Printf("[DRIVER] DCMD: Tag '%s' not found", metric.Name)
			continue
		}

		// Create WriteCommand and process
		writeCmd := WriteCommand{
			TagID:    targetTag.ID,
			Code:     targetTag.Code,
			Value:    metric.Value,
			DataType: targetTag.DataType,
		}

		log.Printf("[DRIVER] DCMD: Writing to tag %d (%s) = %v", targetTag.ID, targetTag.Alias, metric.Value)

		cmdBytes, _ := json.Marshal(writeCmd)
		d.handleWriteCommand(fmt.Sprintf("cmd/write/%d", d.gatewayID), cmdBytes)
	}
}

// WriteResult is published to MQTT to confirm write success/failure
type WriteResult struct {
	TagID     int         `json:"tag_id"`
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	ReadBack  interface{} `json:"read_back,omitempty"`
	Timestamp int64       `json:"ts"`
}

// publishWriteResult publishes the write result to MQTT for feedback
func (d *Driver) publishWriteResult(tagID int, success bool, message string, readBack interface{}) {
	result := WriteResult{
		TagID:     tagID,
		Success:   success,
		Message:   message,
		ReadBack:  readBack,
		Timestamp: time.Now().UnixMilli(),
	}

	payload, _ := json.Marshal(result)
	resultTopic := fmt.Sprintf("cmd/write/result/%d", d.gatewayID)
	d.mqttClient.PublishWithQoS(resultTopic, string(payload), 1, false)

	// Also publish in legacy format for external SCADA systems
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg != nil {
		// Find tag alias
		var alias string
		for _, t := range cfg.Tags {
			if t.ID == tagID {
				alias = t.Alias
				break
			}
		}

		if alias != "" {
			// Legacy format result: cmd/result/{org}/{site}/{area}/{gateway}/{alias}
			legacyResultTopic := fmt.Sprintf("cmd/result/%s/%s/%s/%s/%s",
				cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
			d.mqttClient.PublishWithQoS(legacyResultTopic, string(payload), 1, false)

			// Sparkplug B format result if enabled
			if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
				d.sparkplugMu.RLock()
				dualPublisher := d.dualPublisher
				d.sparkplugMu.RUnlock()

				if dualPublisher != nil {
					// Publish DCMD response
					groupID := sparkplug.BuildGroupID(cfg.OrgName, cfg.Site)
					edgeNodeID := sparkplug.BuildEdgeNodeID(cfg.Area, cfg.Gateway.Name)
					dcmdResultTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/%s/result", groupID, edgeNodeID, slugify(alias))
					d.mqttClient.PublishWithQoS(dcmdResultTopic, string(payload), 1, false)
				}
			}
		}
	}

	if success {
		log.Printf("[DRIVER] Write result: tag=%d ✓ %s", tagID, message)
	} else {
		log.Printf("[DRIVER] Write result: tag=%d ✗ %s", tagID, message)
	}
}

func (d *Driver) loadConfig() error {
	// Save old connection config to detect changes
	var oldConnConfig models.ConnectionConfig
	d.configMu.RLock()
	if d.config != nil {
		oldConnConfig = d.config.Gateway.ConnectionConfig
	}
	d.configMu.RUnlock()

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

	// Check if connection config changed - disconnect old client to force reconnection
	if connectionConfigChanged(oldConnConfig, gateway.ConnectionConfig) {
		log.Printf("[DRIVER] Connection config changed, disconnecting old client...")
		if d.modbusClient != nil {
			d.modbusClient.Disconnect()
			d.modbusClient = nil
		}
		d.setConnectionState(false)
	}

	// Initialize or update Sparkplug B client
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		d.initSparkplugClientLocked(slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name), orgID)
	}

	// Subscribe to legacy format write commands: cmd/{org}/{site}/{area}/{gateway}/+
	legacyWriteTopic := fmt.Sprintf("cmd/%s/%s/%s/%s/+",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name))
	d.mqttClient.Subscribe(legacyWriteTopic, d.handleLegacyWriteCommand)
	log.Printf("[DRIVER] Subscribed to legacy write topic: %s", legacyWriteTopic)

	// Subscribe to Sparkplug B DCMD (Device Command) if enabled
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		groupID := sparkplug.BuildGroupID(slugify(orgName), slugify(siteName))
		edgeNodeID := sparkplug.BuildEdgeNodeID(slugify(areaName), slugify(gateway.Name))
		dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", groupID, edgeNodeID)
		d.mqttClient.Subscribe(dcmdTopic, d.handleSparkplugDCMD)
		log.Printf("[DRIVER] Subscribed to Sparkplug B DCMD topic: %s", dcmdTopic)
	}

	log.Printf("[DRIVER] Config loaded: %d tags, %d blocks (Scan Rate: %dms)", len(tags), len(blocks), gateway.ScanRateMs)
	return nil
}

// connectionConfigChanged compares two connection configs to detect changes
func connectionConfigChanged(old, new models.ConnectionConfig) bool {
	// If old config was empty (first load), no change
	if len(old) == 0 {
		return false
	}

	// Compare key connection fields
	oldHost, _ := old["host"].(string)
	newHost, _ := new["host"].(string)
	if oldHost != newHost {
		return true
	}

	oldPort, _ := old["port"].(float64)
	newPort, _ := new["port"].(float64)
	if oldPort != newPort {
		return true
	}

	oldSlaveID, _ := old["slave_id"].(float64)
	newSlaveID, _ := new["slave_id"].(float64)
	if oldSlaveID != newSlaveID {
		return true
	}

	oldProtocol, _ := old["protocol"].(string)
	newProtocol, _ := new["protocol"].(string)
	if oldProtocol != newProtocol {
		return true
	}

	return false
}

// initSparkplugClient initializes the Sparkplug B client (called from main after config load)
func (d *Driver) initSparkplugClient() {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	d.sparkplugMu.Lock()
	defer d.sparkplugMu.Unlock()

	// Build Sparkplug B identifiers
	groupID := sparkplug.BuildGroupID(cfg.OrgName, cfg.Site)
	edgeNodeID := sparkplug.BuildEdgeNodeID(cfg.Area, cfg.Gateway.Name)

	config := sparkplug.Config{
		MQTTHost:     getEnv("MQTT_HOST", "localhost"),
		MQTTPort:     getEnvInt("MQTT_PORT", 1883),
		MQTTClientID: fmt.Sprintf("sparkplug-modbus-%d", d.gatewayID),
		GroupID:      groupID,
		EdgeNodeID:   edgeNodeID,
		EnableLegacy: true,
	}

	d.sparkplugClient = sparkplug.NewClient(config, d.mqttClient)
	d.sparkplugClient.SetConnected(true)

	// Create dual publisher
	d.dualPublisher = sparkplug.NewDualPublisher(
		d.sparkplugClient,
		cfg.OrgName,
		cfg.Site,
		cfg.Area,
		cfg.Gateway.Name,
		cfg.OrgID,
	)

	log.Printf("[DRIVER] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

// initSparkplugClientLocked initializes Sparkplug client while holding configMu lock
func (d *Driver) initSparkplugClientLocked(orgName, siteName, areaName, gatewayName string, orgID int) {
	d.sparkplugMu.Lock()
	defer d.sparkplugMu.Unlock()

	// Build Sparkplug B identifiers
	groupID := sparkplug.BuildGroupID(orgName, siteName)
	edgeNodeID := sparkplug.BuildEdgeNodeID(areaName, gatewayName)

	config := sparkplug.Config{
		MQTTHost:     getEnv("MQTT_HOST", "localhost"),
		MQTTPort:     getEnvInt("MQTT_PORT", 1883),
		MQTTClientID: fmt.Sprintf("sparkplug-modbus-%d", d.gatewayID),
		GroupID:      groupID,
		EdgeNodeID:   edgeNodeID,
		EnableLegacy: true,
	}

	if d.sparkplugClient == nil {
		d.sparkplugClient = sparkplug.NewClient(config, d.mqttClient)
		d.sparkplugClient.SetConnected(true)
	}

	// Create or update dual publisher
	d.dualPublisher = sparkplug.NewDualPublisher(
		d.sparkplugClient,
		orgName,
		siteName,
		areaName,
		gatewayName,
		orgID,
	)

	log.Printf("[DRIVER] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

// publishDual publishes a tag value based on the configured publish mode
func (d *Driver) publishDual(tagID int, alias string, value interface{}, dataType string, quality int, timestamp int64) {
	// Get publish mode from settings manager
	publishMode := models.PublishModeDual // default
	if d.settingsManager != nil {
		publishMode = d.settingsManager.Get().PublishMode
	}

	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	d.sparkplugMu.RLock()
	dualPublisher := d.dualPublisher
	sparkplugClient := d.sparkplugClient
	d.sparkplugMu.RUnlock()

	// Publish based on mode
	switch publishMode {
	case models.PublishModeLegacyOnly:
		// Only legacy format
		d.publishLegacy(tagID, alias, value, timestamp, quality, cfg)

	case models.PublishModeSparkplugOnly:
		// Only Sparkplug B format
		if sparkplugClient != nil && sparkplugClient.IsConnected() {
			tagData := sparkplug.TagData{
				TagID:     tagID,
				DeviceID:  alias,
				Value:     value,
				DataType:  dataType,
				Timestamp: timestamp,
				Quality:   quality,
				OrgID:     cfg.OrgID,
			}
			if err := sparkplugClient.PublishSingleTag(tagData); err != nil {
				log.Printf("[DRIVER] Sparkplug publish error for %s: %v", alias, err)
			}
		} else {
			log.Printf("[DRIVER] Sparkplug client not available, skipping publish for %s", alias)
		}

	default: // PublishModeDual
		// Both formats - use dual publisher if available
		if dualPublisher != nil {
			if err := dualPublisher.Publish(tagID, alias, value, dataType, quality, timestamp, d.mqttClient); err != nil {
				log.Printf("[DRIVER] Dual publish error for %s: %v", alias, err)
			}
		} else {
			// Fallback to legacy only
			d.publishLegacy(tagID, alias, value, timestamp, quality, cfg)
		}
	}
}

// publishLegacy publishes a tag value in legacy format only
func (d *Driver) publishLegacy(tagID int, alias string, value interface{}, timestamp int64, quality int, cfg *GatewayConfig) {
	topic := fmt.Sprintf("data/%s/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
	payload, _ := json.Marshal(TagPayload{tagID, cfg.OrgID, value, timestamp, quality})
	d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)
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
		for _, tib := range b.Tags {
			// Get last known value or default
			var val interface{} = 0
			d.prevValuesMu.RLock()
			if v, ok := d.previousValues[tib.Tag.ID]; ok {
				val = v
			}
			d.prevValuesMu.RUnlock()

			// Quality 2 = BAD - publish using dual publisher
			d.publishDual(tib.Tag.ID, tib.Tag.Alias, val, tib.Tag.DataType, 2, ts)

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

		// Publish BAD quality for all tags in this block using dual publisher
		for _, tib := range b.Tags {
			// Get last known value or default
			var val interface{} = 0
			d.prevValuesMu.RLock()
			if v, ok := d.previousValues[tib.Tag.ID]; ok {
				val = v
			}
			d.prevValuesMu.RUnlock()

			// Quality 2 = BAD - publish using dual publisher
			d.publishDual(tib.Tag.ID, tib.Tag.Alias, val, tib.Tag.DataType, 2, ts)

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

		// 1. Evaluate alarms via AlarmManager (always evaluates, even before RBE)
		if d.alarmManager != nil {
			d.alarmManager.EvaluateTag(tib.Tag.ID, tib.Tag.Alias, val, 192)
		}

		// 2. Report by Exception Logic (RBE)
		if d.shouldPublish(tib.Tag.ID, val, 0) {
			d.publishDual(tib.Tag.ID, tib.Tag.Alias, val, tib.Tag.DataType, 0, ts)
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
	// Get previous values for RBE comparison
	d.prevValuesMu.RLock()
	oldVal := d.previousValues[id]
	oldQuality := d.previousQualities[id]
	d.prevValuesMu.RUnlock()

	// Use settings manager for RBE logic
	if d.settingsManager != nil {
		return d.settingsManager.ShouldPublish(id, val, oldVal, quality, oldQuality)
	}

	// Fallback: always publish if settings manager not available
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

	// Always publish if this is the first state (isConnected is nil/unset)
	// or if the state has actually changed
	if d.isConnected != nil && *d.isConnected == connected {
		return
	}

	// Initialize isConnected if nil
	if d.isConnected == nil {
		d.isConnected = new(bool)
	}

	wasConnected := *d.isConnected
	*d.isConnected = connected
	status := "offline"
	if connected {
		status = "online"
	}

	// Publish health status
	topic := fmt.Sprintf("sys/health/%d", d.gatewayID)
	d.mqttClient.PublishWithQoS(topic, status, 1, true)
	log.Printf("[DRIVER] Health status changed to: %s (was: %v)", status, wasConnected)

	// Handle Sparkplug B birth/death messages
	publishMode := models.PublishModeDual
	if d.settingsManager != nil {
		publishMode = d.settingsManager.Get().PublishMode
	}

	// Only send death/birth if using Sparkplug (sparkplug_only or dual)
	if publishMode == models.PublishModeLegacyOnly {
		return
	}

	d.sparkplugMu.RLock()
	spClient := d.sparkplugClient
	cfg := d.config
	d.sparkplugMu.RUnlock()

	if spClient == nil || !spClient.IsConnected() || cfg == nil {
		return
	}

	if connected && !wasConnected {
		// Coming online - send DBIRTH with all current tag values
		log.Printf("[DRIVER] Sending DBIRTH for all tags (device coming online)")
		var tagsData []sparkplug.TagData
		for _, t := range cfg.Tags {
			tagsData = append(tagsData, sparkplug.TagData{
				TagID:     t.ID,
				DeviceID:  t.Alias,
				Value:     d.previousValues[t.ID],
				DataType:  t.DataType,
				Timestamp: time.Now().UnixMilli(),
				Quality:   192, // GOOD
				OrgID:     cfg.OrgID,
			})
		}
		if err := spClient.PublishDBIRTH(slugify(cfg.Gateway.Name), tagsData); err != nil {
			log.Printf("[DRIVER] Failed to publish DBIRTH: %v", err)
		}
	} else if !connected && wasConnected {
		// Going offline - send DDEATH
		log.Printf("[DRIVER] Sending DDEATH (device going offline)")
		if err := spClient.PublishDDEATH(slugify(cfg.Gateway.Name)); err != nil {
			log.Printf("[DRIVER] Failed to publish DDEATH: %v", err)
		}
	}
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
