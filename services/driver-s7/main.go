package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/alarms"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/s7"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

// GatewayConfig holds the loaded gateway configuration
type GatewayConfig struct {
	Gateway models.Gateway
	Tags    []models.Tag
	OrgID   int
	OrgName string
	Site    string
	Area    string
}

// TagPayload represents the MQTT message payload for tag values
type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// Driver manages the S7 driver lifecycle
type Driver struct {
	gatewayID  int
	database   *sql.DB
	mqttClient *mqtt.Client
	s7Client   *s7.Client
	config     *GatewayConfig
	configMu   sync.RWMutex
	stopChan   chan struct{}
	reloadChan chan struct{}

	// Alarm Manager
	alarmManager *alarms.Manager
	// Report by Exception: store previous values for change detection
	previousValues map[int]interface{}
	prevValuesMu   sync.RWMutex

	// Write Cooldown: prevent read-after-write flickering
	writeCooldowns map[int]time.Time
	cooldownMu     sync.RWMutex

	// Settings and RBE
	settingsManager   *settings.Manager
	previousQualities map[int]int

	// Sparkplug B support
	sparkplugClient *sparkplug.SparkplugClient
	dualPublisher   *sparkplug.DualPublisher
	sparkplugMu     sync.RWMutex
}

func main() {
	log.Println("Starting driver-s7...")

	// Get gateway ID from environment
	gatewayIDStr := getEnv("GATEWAY_ID", "")
	if gatewayIDStr == "" {
		log.Fatal("GATEWAY_ID environment variable is required")
	}
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		log.Fatalf("Invalid GATEWAY_ID: %v", err)
	}

	// Connect to PostgreSQL
	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		Database: getEnv("DB_NAME", "industrial_edge"),
	}

	database, err := db.Connect(dbCfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("Connected to PostgreSQL")

	// Create MQTT client with LWT for health monitoring
	mqttCfg := mqtt.Config{
		Host:          getEnv("MQTT_HOST", "localhost"),
		Port:          getEnvInt("MQTT_PORT", 1883),
		ClientID:      fmt.Sprintf("driver-s7-%d", gatewayID),
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		LWTTopic:      fmt.Sprintf("sys/health/%d", gatewayID),
		LWTPayload:    "offline",
		LWTRetained:   true,
	}

	mqttClient := mqtt.NewClient(mqttCfg)
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", err)
	}
	defer mqttClient.Disconnect(1000)
	log.Println("Connected to MQTT broker")

	// NOTE: Health status will be published by setConnectionState() when PLC connection is established
	// This ensures the status reflects actual PLC connectivity, not just driver startup

	// Create driver instance
	driver := &Driver{
		gatewayID:      gatewayID,
		database:       database,
		mqttClient:     mqttClient,
		stopChan:       make(chan struct{}),
		reloadChan:     make(chan struct{}, 1),
		previousValues: make(map[int]interface{}),
		writeCooldowns: make(map[int]time.Time),
	}

	// Initialize variables
	driver.previousQualities = make(map[int]int)

	// Initialize settings manager
	settingsManager := settings.NewManager(database)
	if err := settingsManager.Load(); err != nil {
		log.Printf("Warning: Failed to load settings: %v", err)
	}
	driver.settingsManager = settingsManager

	// Initialize Alarm Manager
	driver.alarmManager = alarms.NewManager(database, mqttClient, gatewayID)

	driver.alarmManager.OnAlarmEvent = func(tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
		driver.publishDual(
			tagID,
			alias+"_Alarm",
			status == "ACTIVE",
			"BOOL",
			192,
			time.Now().UnixMilli(),
		)
	}

	go driver.alarmManager.StartTicker(context.Background())

	// Subscribe to settings reload command
	settingsReloadTopic := fmt.Sprintf("sys/command/settings-reload/%d", gatewayID)
	if err := mqttClient.Subscribe(settingsReloadTopic, driver.handleSettingsReloadCommand); err != nil {
		log.Fatalf("Failed to subscribe to settings reload topic: %v", err)
	}
	log.Printf("Subscribed to settings reload topic: %s", settingsReloadTopic)

	// Initialize Sparkplug B client (optional - for dual publishing)
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		log.Println("[DRIVER-S7] Sparkplug B dual publishing enabled")
	}

	// Load initial configuration
	if err := driver.loadConfig(); err != nil {
		log.Fatalf("Failed to load gateway configuration: %v", err)
	}

	// Subscribe to reload command
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	if err := mqttClient.Subscribe(reloadTopic, driver.handleReloadCommand); err != nil {
		log.Fatalf("Failed to subscribe to reload topic: %v", err)
	}
	log.Printf("Subscribed to reload topic: %s", reloadTopic)

	// Also subscribe to wildcard reload
	if err := mqttClient.Subscribe("sys/command/reload/+", driver.handleReloadCommand); err != nil {
		log.Printf("Warning: Failed to subscribe to wildcard reload topic: %v", err)
	}

	// Subscribe to write commands
	writeTopic := fmt.Sprintf("cmd/write/%d", gatewayID)
	if err := mqttClient.Subscribe(writeTopic, driver.handleWriteCommand); err != nil {
		log.Fatalf("Failed to subscribe to write topic: %v", err)
	}
	log.Printf("Subscribed to write topic: %s", writeTopic)

	// Start the driver loop
	go driver.run()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down driver-s7...")
	close(driver.stopChan)
}

// loadConfig loads the gateway configuration from PostgreSQL
func (d *Driver) loadConfig() error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	// Load gateway with hierarchy info
	query := `
		SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled,
		       o.id as org_id, o.name as org_name, s.name as site_name, a.name as area_name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`

	var gateway models.Gateway
	var connConfigBytes []byte
	var orgID int
	var orgName, siteName, areaName string

	err := d.database.QueryRow(query, d.gatewayID).Scan(
		&gateway.ID,
		&gateway.AreaID,
		&gateway.Name,
		&gateway.DriverType,
		&connConfigBytes,
		&gateway.ScanRateMs,
		&gateway.Enabled,
		&orgID,
		&orgName,
		&siteName,
		&areaName,
	)
	if err != nil {
		return fmt.Errorf("failed to load gateway: %w", err)
	}

	// Parse connection config
	if err := json.Unmarshal(connConfigBytes, &gateway.ConnectionConfig); err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}

	// Verify it's an S7 gateway
	if gateway.DriverType != "S7" {
		return fmt.Errorf("gateway %d is not an S7 gateway (type: %s)", d.gatewayID, gateway.DriverType)
	}

	// Load tags for this gateway
	tagsQuery := `
		SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband
		FROM tags
		WHERE gateway_id = $1
	`

	rows, err := d.database.Query(tagsQuery, d.gatewayID)
	if err != nil {
		return fmt.Errorf("failed to load tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		err := rows.Scan(
			&tag.ID,
			&tag.GatewayID,
			&tag.Code,
			&tag.Alias,
			&tag.DataType,
			&tag.Historize,
			&tag.HistorizeDeadband,
		)
		if err != nil {
			return fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}

	d.config = &GatewayConfig{
		Gateway: gateway,
		Tags:    tags,
		OrgID:   orgID,
		OrgName: slugify(orgName),
		Site:    slugify(siteName),
		Area:    slugify(areaName),
	}

	// Initialize or update Sparkplug B client
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		d.initSparkplugClientLocked(slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name), orgID)
	}

	// Subscribe to legacy format write commands: cmd/{org}/{site}/{area}/{gateway}/+
	legacyWriteTopic := fmt.Sprintf("cmd/%s/%s/%s/%s/+",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name))
	d.mqttClient.Subscribe(legacyWriteTopic, d.handleLegacyWriteCommand)
	log.Printf("[DRIVER-S7] Subscribed to legacy write topic: %s", legacyWriteTopic)

	// Subscribe to Sparkplug B DCMD (Device Command) if enabled
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		groupID := sparkplug.BuildGroupID(slugify(orgName), slugify(siteName))
		edgeNodeID := sparkplug.BuildEdgeNodeID(slugify(areaName), slugify(gateway.Name))
		dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", groupID, edgeNodeID)
		d.mqttClient.Subscribe(dcmdTopic, d.handleSparkplugDCMD)
		log.Printf("[DRIVER-S7] Subscribed to Sparkplug B DCMD topic: %s", dcmdTopic)
	}

	log.Printf("Loaded gateway config: %s (ID: %d) with %d tags", gateway.Name, gateway.ID, len(tags))
	log.Printf("Topic prefix: data/%s/%s/%s/%s", d.config.OrgName, d.config.Site, d.config.Area, slugify(gateway.Name))

	return nil
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
		MQTTClientID: fmt.Sprintf("sparkplug-s7-%d", d.gatewayID),
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

	log.Printf("[DRIVER-S7] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

// publishDual publishes a tag value in both legacy and Sparkplug B formats
func (d *Driver) publishDual(tagID int, alias string, value interface{}, dataType string, quality int, timestamp int64) {
	// Get publish mode from settings manager
	publishMode := models.PublishModeDual // default
	if d.settingsManager != nil {
		publishMode = d.settingsManager.Get().PublishMode
	}

	d.sparkplugMu.RLock()
	dualPublisher := d.dualPublisher
	d.sparkplugMu.RUnlock()

	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	// Legacy publish func
	publishLegacy := func() {
		topic := fmt.Sprintf("data/%s/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
		payload, _ := json.Marshal(TagPayload{tagID, cfg.OrgID, value, timestamp, quality})
		d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)
	}

	switch publishMode {
	case models.PublishModeLegacyOnly:
		publishLegacy()

	case models.PublishModeSparkplugOnly:
		if dualPublisher != nil {
			d.sparkplugMu.RLock()
			spClient := d.sparkplugClient
			d.sparkplugMu.RUnlock()
			if spClient != nil && spClient.IsConnected() {
				tagData := sparkplug.TagData{
					TagID:     tagID,
					DeviceID:  alias,
					Value:     value,
					DataType:  dataType,
					Timestamp: timestamp,
					Quality:   quality,
					OrgID:     cfg.OrgID,
				}
				if err := spClient.PublishSingleTag(tagData); err != nil {
					log.Printf("[DRIVER-S7] Sparkplug publish error for %s: %v", alias, err)
				}
			}
		}

	default: // PublishModeDual
		if dualPublisher != nil {
			if err := dualPublisher.Publish(tagID, alias, value, dataType, quality, timestamp, d.mqttClient); err != nil {
				log.Printf("[DRIVER-S7] Dual publish error for %s: %v", alias, err)
			}
		} else {
			publishLegacy()
		}
	}
}

// handleSettingsReloadCommand handles settings-reload MQTT commands
func (d *Driver) handleSettingsReloadCommand(topic string, payload []byte) {
	if d.settingsManager != nil {
		log.Printf("Received settings reload command")
		if err := d.settingsManager.Load(); err != nil {
			log.Printf("Failed to reload settings: %v", err)
		} else {
			log.Printf("Successfully reloaded settings")
		}
	}
}

// handleReloadCommand handles the reload command from MQTT
func (d *Driver) handleReloadCommand(topic string, payload []byte) {
	// Extract gateway ID from topic (sys/command/reload/{gateway_id})
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		return
	}

	targetID, err := strconv.Atoi(parts[3])
	if err != nil {
		return
	}

	// Only reload if this command is for this gateway
	if targetID != d.gatewayID {
		return
	}

	log.Printf("Received reload command for gateway %d", d.gatewayID)

	// Signal reload (non-blocking)
	select {
	case d.reloadChan <- struct{}{}:
	default:
		// Already a reload pending
	}
}

// WriteCommand represents a write command from MQTT
type WriteCommand struct {
	TagID    int         `json:"tag_id"`
	Code     string      `json:"code"`
	Value    interface{} `json:"value"`
	DataType string      `json:"data_type"`
}

// handleWriteCommand handles write commands from MQTT
func (d *Driver) handleWriteCommand(topic string, payload []byte) {
	log.Printf("Received write command: %s", string(payload))

	var cmd WriteCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("Failed to unmarshal write command: %v", err)
		return
	}

	// EXECUTE WRITE TO PLC
	d.configMu.RLock()
	client := d.s7Client
	config := d.config
	d.configMu.RUnlock()

	if client == nil || !client.IsConnected() {
		log.Printf("Cannot write: S7 client not connected")
		return
	}

	// Convert DataType string to s7.DataType
	dataType := s7.DataType(cmd.DataType)

	// Perform Write
	if err := client.WriteTag(cmd.Code, dataType, cmd.Value); err != nil {
		log.Printf("Write failed for tag %d (%s): %v", cmd.TagID, cmd.Code, err)
		return
	}

	log.Printf("Write successful to %s (Value: %v)", cmd.Code, cmd.Value)

	// OPTIMISTIC UPDATE & COOLDOWN
	// 1. Update local cache immediately so we don't wait for next poll
	d.updatePreviousValue(cmd.TagID, cmd.Value)

	// 2. Set Cooldown to ignore read values for a short time
	// This prevents the "flicker" where the next poll might read the OLD value
	// from the PLC before the write has processed.
	d.cooldownMu.Lock()
	d.writeCooldowns[cmd.TagID] = time.Now().Add(2 * time.Second) // 2s cooldown
	d.cooldownMu.Unlock()

	// 3. Publish the new value back to MQTT immediately (feedback)
	if config != nil {
		topicPrefix := fmt.Sprintf("data/%s/%s/%s/%s",
			config.OrgName,
			config.Site,
			config.Area,
			slugify(config.Gateway.Name),
		)

		// Find tag alias for topic
		var tagAlias string
		for _, t := range config.Tags {
			if t.ID == cmd.TagID {
				tagAlias = t.Alias
				break
			}
		}

		if tagAlias != "" {
			// Publish success feedback
			tag := models.Tag{Alias: tagAlias, ID: cmd.TagID} // Minimal tag struct for publish
			// Use 0 for "Good" quality on successful write
			d.publishTagValue(topicPrefix, tag, cmd.Value, time.Now().UnixMilli(), 0, config.OrgID)
		}
	}
}

// handleLegacyWriteCommand handles write commands in legacy MQTT format
// Topic format: cmd/{org}/{site}/{area}/{gateway}/{alias}
// Payload: {"value": <value>} or just <value>
func (d *Driver) handleLegacyWriteCommand(topic string, payload []byte) {
	log.Printf("[DRIVER-S7] Received legacy write command: %s", topic)

	// Extract alias from topic
	parts := strings.Split(topic, "/")
	if len(parts) < 6 {
		log.Printf("[DRIVER-S7] Invalid legacy write topic format: %s", topic)
		return
	}
	alias := parts[5]

	// Find tag by alias
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER-S7] No config loaded, cannot process write")
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
		log.Printf("[DRIVER-S7] Tag with alias '%s' not found", alias)
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
			log.Printf("[DRIVER-S7] Failed to parse value from payload: %v", err)
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
	log.Printf("[DRIVER-S7] Received Sparkplug B DCMD: %s", topic)

	// Parse Sparkplug B topic
	_, err := sparkplug.ParseTopic(topic)
	if err != nil {
		log.Printf("[DRIVER-S7] Invalid Sparkplug DCMD topic: %v", err)
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
		log.Printf("[DRIVER-S7] Failed to parse Sparkplug DCMD payload: %v", err)
		return
	}

	// Find config
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER-S7] No config loaded, cannot process DCMD")
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
			log.Printf("[DRIVER-S7] DCMD: Tag '%s' not found", metric.Name)
			continue
		}

		// Create WriteCommand and process
		writeCmd := WriteCommand{
			TagID:    targetTag.ID,
			Code:     targetTag.Code,
			Value:    metric.Value,
			DataType: targetTag.DataType,
		}

		log.Printf("[DRIVER-S7] DCMD: Writing to tag %d (%s) = %v", targetTag.ID, targetTag.Alias, metric.Value)

		cmdBytes, _ := json.Marshal(writeCmd)
		d.handleWriteCommand(fmt.Sprintf("cmd/write/%d", d.gatewayID), cmdBytes)
	}
}

// run is the main driver loop
func (d *Driver) run() {
	d.configMu.RLock()
	scanRate := time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond
	d.configMu.RUnlock()

	// Connect to S7 PLC
	if err := d.connectS7(); err != nil {
		log.Printf("Initial S7 connection failed: %v (will retry on poll)", err)
	}

	ticker := time.NewTicker(scanRate)
	defer ticker.Stop()

	log.Printf("Starting S7 polling loop with scan rate: %v", scanRate)

	for {
		select {
		case <-d.stopChan:
			log.Println("Stopping S7 polling loop")
			if d.s7Client != nil {
				d.s7Client.Disconnect()
			}
			return

		case <-d.reloadChan:
			log.Println("Reloading configuration...")
			if err := d.loadConfig(); err != nil {
				log.Printf("Failed to reload configuration: %v", err)
			} else {
				// Reconnect S7 client with potentially new config
				if d.s7Client != nil {
					d.s7Client.Disconnect()
				}
				if err := d.connectS7(); err != nil {
					log.Printf("Failed to reconnect S7 after reload: %v", err)
				}
				// Update ticker with new scan rate
				d.configMu.RLock()
				newScanRate := time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond
				d.configMu.RUnlock()
				ticker.Reset(newScanRate)
				log.Printf("Configuration reloaded, scan rate: %v", newScanRate)
			}

		case <-ticker.C:
			d.poll()
		}
	}
}

// connectS7 establishes connection to the S7 PLC
func (d *Driver) connectS7() error {
	d.configMu.RLock()
	connConfig := d.config.Gateway.ConnectionConfig
	d.configMu.RUnlock()

	client, err := s7.NewClientFromConfig(connConfig)
	if err != nil {
		return fmt.Errorf("failed to create S7 client: %w", err)
	}

	if err := client.ConnectWithRetry(); err != nil {
		return fmt.Errorf("failed to connect to S7 PLC: %w", err)
	}

	d.s7Client = client
	log.Println("Connected to S7 PLC")
	return nil
}

// poll reads data from the S7 PLC and publishes to MQTT
func (d *Driver) poll() {
	d.configMu.RLock()
	config := d.config
	d.configMu.RUnlock()

	if config == nil || !config.Gateway.Enabled {
		return
	}

	// Build topic prefix: data/{org}/{site}/{area}/{gateway}
	topicPrefix := fmt.Sprintf("data/%s/%s/%s/%s",
		config.OrgName,
		config.Site,
		config.Area,
		slugify(config.Gateway.Name),
	)

	timestamp := time.Now().UnixMilli()

	// Ensure S7 connection is established
	if d.s7Client == nil || !d.s7Client.IsConnected() {
		log.Printf("S7 not connected, attempting reconnection...")
		if err := d.connectS7(); err != nil {
			log.Printf("S7 reconnection failed: %v", err)

			// Publish BAD quality for all tags
			for _, tag := range config.Tags {
				d.prevValuesMu.RLock()
				val, exists := d.previousValues[tag.ID]
				d.prevValuesMu.RUnlock()
				if !exists {
					val = 0
				}
				if d.shouldPublish(tag.ID, val, 2) {
					d.publishTagValue(topicPrefix, tag, val, timestamp, 2, config.OrgID)
					d.updatePreviousValue(tag.ID, val)
					d.updateQuality(tag.ID, 2)
				}
			}
			return
		}
	}

	// Read and publish each tag
	for _, tag := range config.Tags {
		// Read value from PLC
		dataType := s7.DataType(tag.DataType)
		result := d.s7Client.ReadTag(tag.Code, dataType)

		// Check for read error
		if result.Error != nil {
			log.Printf("Error reading tag %s (ID:%d): %v", tag.Alias, tag.ID, result.Error)

			// Publish with bad quality
			d.prevValuesMu.RLock()
			val, exists := d.previousValues[tag.ID]
			d.prevValuesMu.RUnlock()
			if !exists {
				val = 0
			}
			if d.shouldPublish(tag.ID, val, 2) {
				d.publishTagValue(topicPrefix, tag, val, timestamp, 2, config.OrgID)
				d.updatePreviousValue(tag.ID, val)
				d.updateQuality(tag.ID, 2)
			}
			continue
		}

		// COOLDOWN CHECK
		d.cooldownMu.RLock()
		cooldownUntil, hasCooldown := d.writeCooldowns[tag.ID]
		d.cooldownMu.RUnlock()

		if hasCooldown {
			if time.Now().Before(cooldownUntil) {
				// We are in cooldown. Check if PLC value matches our optimistic value.
				d.prevValuesMu.RLock()
				optValue, exists := d.previousValues[tag.ID]
				d.prevValuesMu.RUnlock()

				if exists && valuesEqual(result.Value, optValue) {
					// PLC has caught up! Clear cooldown early.
					d.cooldownMu.Lock()
					delete(d.writeCooldowns, tag.ID)
					d.cooldownMu.Unlock()
					log.Printf("Write confirmed for tag %s (PLC matches optimistic value)", tag.Alias)
				} else {
					// PLC still shows old value (or different). Ignore to prevent flicker.
					continue
				}
			} else {
				// Cooldown expired. Clean up.
				d.cooldownMu.Lock()
				delete(d.writeCooldowns, tag.ID)
				d.cooldownMu.Unlock()
			}
		}

		// Evaluate alarms via AlarmManager
		if d.alarmManager != nil {
			d.alarmManager.EvaluateTag(tag.ID, tag.Alias, result.Value, result.Quality)
		}

		// Publish to MQTT only if value changed (Report by Exception)
		if d.shouldPublish(tag.ID, result.Value, result.Quality) {
			d.updatePreviousValue(tag.ID, result.Value)
			d.updateQuality(tag.ID, result.Quality)
			d.publishTagValue(topicPrefix, tag, result.Value, timestamp, result.Quality, config.OrgID)
		}
	}
}

// publishTagValue publishes a tag value to MQTT (with Sparkplug B dual support)
func (d *Driver) publishTagValue(topicPrefix string, tag models.Tag, value interface{}, timestamp int64, quality int, orgID int) {
	// Use dual publishing if available
	d.publishDual(tag.ID, tag.Alias, value, tag.DataType, quality, timestamp)
}

// hasValueChanged checks if a value has changed from its previous value (Report by Exception)
func (d *Driver) hasValueChanged(tagID int, newValue interface{}) bool {
	d.prevValuesMu.RLock()
	prevValue, exists := d.previousValues[tagID]
	d.prevValuesMu.RUnlock()

	// If no previous value exists, it's always considered "changed"
	if !exists {
		return true
	}

	// Compare values based on type
	return !valuesEqual(prevValue, newValue)
}

func (d *Driver) updatePreviousValue(tagID int, value interface{}) {
	d.prevValuesMu.Lock()
	d.previousValues[tagID] = value
	d.prevValuesMu.Unlock()
}

func (d *Driver) updateQuality(tagID int, quality int) {
	d.prevValuesMu.Lock()
	if d.previousQualities == nil {
		d.previousQualities = make(map[int]int)
	}
	d.previousQualities[tagID] = quality
	d.prevValuesMu.Unlock()
}

// shouldPublish checks if the value should be published based on RBE settings
func (d *Driver) shouldPublish(id int, val interface{}, quality int) bool {
	d.prevValuesMu.RLock()
	oldVal := d.previousValues[id]
	oldQuality := d.previousQualities[id]
	d.prevValuesMu.RUnlock()

	if d.settingsManager != nil {
		return d.settingsManager.ShouldPublish(id, val, oldVal, quality, oldQuality)
	}

	return true // Fallback to always publish if no settings manager
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
	case int32:
		bv, ok := b.(int32)
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

// slugify converts a string to a URL-friendly slug
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intVal int
		if _, err := fmt.Sscanf(value, "%d", &intVal); err == nil {
			return intVal
		}
	}
	return defaultValue
}
