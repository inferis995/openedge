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

	_ "github.com/lib/pq"
	"github.com/ralph/industrial-edge-middleware/internal/alarms"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	opcuaclient "github.com/ralph/industrial-edge-middleware/internal/opcua"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

// TagPayload is the standard payload published to MQTT
type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// GatewayConfig holds configuration
type GatewayConfig struct {
	Gateway  models.Gateway
	Tags     []models.Tag
	OrgName  string
	OrgID    int
	SiteName string
	AreaName string
	AuthMode string
	Username string
	Password string
	CertFile string
	KeyFile  string
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

// Driver manages the OPC UA driver lifecycle
type Driver struct {
	gatewayID      int
	database       *sql.DB
	mqttClient     *mqtt.Client
	opcuaClient    *opcuaclient.Client
	config         *GatewayConfig
	configMu       sync.RWMutex
	stopChan       chan struct{}
	wg             sync.WaitGroup
	previousValues map[int]interface{}
	previousMu     sync.RWMutex

	// Connection state tracking for DBIRTH/DDEATH
	isConnected  bool
	wasConnected bool
	connectionMu sync.RWMutex

	// Sparkplug B support
	sparkplugClient *sparkplug.SparkplugClient
	dualPublisher   *sparkplug.DualPublisher
	sparkplugMu     sync.RWMutex

	// Settings manager for publish mode and RBE
	settingsManager *settings.Manager

	// Previous qualities for RBE
	previousQualities map[int]int
	prevQualitiesMu   sync.RWMutex

	// Alarm Manager
	alarmManager *alarms.Manager
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[OPC-UA Driver] Starting...")

	// Get gateway ID from environment
	gatewayIDStr := os.Getenv("GATEWAY_ID")
	if gatewayIDStr == "" {
		log.Fatal("GATEWAY_ID environment variable is required")
	}
	var err error
	var gatewayID int
	gatewayID, err = strconv.Atoi(gatewayIDStr)
	if err != nil {
		log.Fatalf("Invalid GATEWAY_ID: %v", err)
	}

	// Connect to database using internal db package
	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "industrial_user"),
		Password: getEnv("DB_PASSWORD", "industrial_pass"),
		Database: getEnv("DB_NAME", "industrial_edge"),
	}

	var database *sql.DB
	err = retryWithBackoff("Database connection", func() error {
		var err error
		database, err = db.Connect(dbCfg)
		return err
	})
	if err != nil {
		log.Fatalf("Failed to connect to database after retries: %v", err)
	}
	defer database.Close()
	log.Println("[OPC-UA Driver] Connected to database")

	// Connect to MQTT broker using internal mqtt package
	mqttCfg := mqtt.Config{
		Host:          getEnv("MQTT_HOST", "mosquitto"),
		Port:          getEnvInt("MQTT_PORT", 1883),
		ClientID:      fmt.Sprintf("driver-opcua-%d", gatewayID),
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		LWTTopic:      fmt.Sprintf("sys/health/%d", gatewayID),
		LWTPayload:    "offline",
		LWTRetained:   true,
	}

	mqttClient := mqtt.NewClient(mqttCfg)
	err = retryWithBackoff("MQTT broker connection", func() error {
		return mqttClient.Connect()
	})
	if err != nil {
		log.Fatalf("Failed to connect to MQTT broker after retries: %v", err)
	}
	defer mqttClient.Disconnect(1000)
	log.Println("[OPC-UA Driver] Connected to MQTT broker")

	// NOTE: Health status will be published by setConnectionState() when PLC connection is established
	// This ensures the status reflects actual PLC connectivity, not just driver startup

	// Initialize settings manager
	settingsManager := settings.NewManager(database)

	// Create driver instance
	driver := &Driver{
		gatewayID:         gatewayID,
		database:          database,
		mqttClient:        mqttClient,
		stopChan:          make(chan struct{}),
		previousValues:    make(map[int]interface{}),
		previousQualities: make(map[int]int),
		settingsManager:   settingsManager,
		isConnected:       false,
		wasConnected:      false,
	}

	// Initialize Sparkplug B client (optional - for dual publishing)
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		log.Println("[OPC-UA Driver] Sparkplug B dual publishing enabled")
	}

	// Initialize settings manager
	if err := driver.settingsManager.Load(); err != nil {
		log.Printf("Warning: Failed to load settings: %v", err)
	}

	// Load initial configuration
	if err := driver.loadConfig(); err != nil {
		log.Fatalf("Failed to load initial config: %v", err)
	}

	// Subscribe to reload commands
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	mqttClient.Subscribe(reloadTopic, driver.handleReloadCommand)
	mqttClient.Subscribe("sys/command/reload/+", driver.handleReloadCommand)
	log.Printf("[OPC-UA Driver] Subscribed to reload topic: %s", reloadTopic)

	// Subscribe to settings-reload commands for hot-reload of publish mode
	mqttClient.Subscribe("sys/command/settings-reload", driver.handleSettingsReloadCommand)
	log.Printf("[OPC-UA Driver] Subscribed to settings-reload topic")

	// Subscribe to write commands
	writeTopic := fmt.Sprintf("sys/command/write/%d", gatewayID)
	mqttClient.Subscribe(writeTopic, driver.handleWriteCommand)
	log.Printf("[OPC-UA Driver] Subscribed to write topic: %s", writeTopic)

	// Subscribe to health events for auto-reload when gateway comes online
	healthTopic := "sys/health/+"
	mqttClient.Subscribe(healthTopic, driver.handleHealthMessage)
	log.Printf("[OPC-UA Driver] Subscribed to health topic: %s (auto-reload enabled)", healthTopic)

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

	// Start polling loop
	driver.wg.Add(1)
	go driver.pollLoop()

	// Start health reporting
	driver.wg.Add(1)
	go driver.healthLoop()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[OPC-UA Driver] Shutting down...")

	// Publish DDEATH before shutting down
	driver.publishDDEATH()

	close(driver.stopChan)
	driver.wg.Wait()

	if driver.opcuaClient != nil {
		driver.opcuaClient.Disconnect()
	}
	log.Println("[OPC-UA Driver] Shutdown complete")
}

func (d *Driver) handleReloadCommand(topic string, payload []byte) {
	log.Printf("[OPC-UA Driver] Received reload signal via %s", topic)
	if err := d.loadConfig(); err != nil {
		log.Printf("[OPC-UA Driver] Reload failed: %v", err)
	}
}

func (d *Driver) handleSettingsReloadCommand(topic string, payload []byte) {
	log.Printf("[OPC-UA Driver] Received settings-reload signal")
	if d.settingsManager != nil {
		if err := d.settingsManager.Load(); err != nil {
			log.Printf("[OPC-UA Driver] Failed to reload settings: %v", err)
		} else {
			cfg := d.settingsManager.Get()
			log.Printf("[OPC-UA Driver] Settings reloaded: publish_mode=%s", cfg.PublishMode)
		}
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
		log.Printf("[OPC-UA Driver] Gateway %d is ONLINE - auto-reloading config and tags", d.gatewayID)
		if err := d.loadConfig(); err != nil {
			log.Printf("[OPC-UA Driver] Auto-reload failed: %v", err)
		} else {
			log.Printf("[OPC-UA Driver] Auto-reload successful - %d tags loaded", len(d.config.Tags))
		}
	}
}

// WriteCommand represents a write command with full tag info
type WriteCommand struct {
	TagID    int         `json:"tag_id"`
	Code     string      `json:"code"`
	Value    interface{} `json:"value"`
	DataType string      `json:"data_type"`
}

func (d *Driver) handleWriteCommand(topic string, payload []byte) {
	log.Printf("[OPC-UA Driver] Received write command: %s", string(payload))

	var cmd WriteCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("[OPC-UA Driver] Failed to unmarshal write command: %v", err)
		return
	}

	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[OPC-UA Driver] Write failed: no config loaded")
		return
	}

	// Find the tag
	var targetTag *models.Tag
	for i := range cfg.Tags {
		if cfg.Tags[i].ID == cmd.TagID {
			targetTag = &cfg.Tags[i]
			break
		}
	}

	if targetTag == nil {
		log.Printf("[OPC-UA Driver] Write failed: tag %d not found", cmd.TagID)
		return
	}

	if d.opcuaClient == nil || !d.opcuaClient.IsConnected() {
		log.Printf("[OPC-UA Driver] Cannot write: OPC UA client not connected")
		return
	}

	if err := d.opcuaClient.WriteValue(targetTag.Code, cmd.Value, targetTag.DataType); err != nil {
		log.Printf("[OPC-UA Driver] Write error for tag %d (%s): %v", targetTag.ID, targetTag.Code, err)
		return
	}

	log.Printf("[OPC-UA Driver] Write success: tag %d (%s) = %v", targetTag.ID, targetTag.Code, cmd.Value)
}

// loadConfig loads gateway and tag configuration from the database
func (d *Driver) loadConfig() error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	// Load gateway
	query := `
		SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled,
		       o.id, o.name as org_name, s.name as site_name, a.name as area_name
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
		&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType,
		&connConfigBytes, &gateway.ScanRateMs, &gateway.Enabled,
		&orgID, &orgName, &siteName, &areaName,
	)
	if err != nil {
		return fmt.Errorf("failed to load gateway %d: %w", d.gatewayID, err)
	}

	// Parse connection config
	if err := json.Unmarshal(connConfigBytes, &gateway.ConnectionConfig); err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}

	// Load tags
	tagsQuery := `
		SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband
		FROM tags WHERE gateway_id = $1
		ORDER BY id
	`
	rows, err := d.database.Query(tagsQuery, d.gatewayID)
	if err != nil {
		return fmt.Errorf("failed to load tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.GatewayID, &t.Code, &t.Alias, &t.DataType, &t.Historize, &t.HistorizeDeadband); err != nil {
			return fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, t)
	}

	// Parse connection config to get endpoint and auth
	endpoint, _ := gateway.ConnectionConfig["endpoint"].(string)
	if endpoint == "" {
		return fmt.Errorf("missing 'endpoint' in connection_config for OPC_UA gateway %d", d.gatewayID)
	}

	authMode, _ := gateway.ConnectionConfig["auth_mode"].(string)
	username, _ := gateway.ConnectionConfig["username"].(string)
	password, _ := gateway.ConnectionConfig["password"].(string)
	certFile, _ := gateway.ConnectionConfig["cert_file"].(string)
	keyFile, _ := gateway.ConnectionConfig["key_file"].(string)

	if authMode == "" {
		authMode = "Anonymous"
	}

	// Create/update OPC UA client
	wasConnected := d.opcuaClient != nil && d.opcuaClient.IsConnected()

	if d.opcuaClient != nil {
		d.opcuaClient.Disconnect()
	}

	d.opcuaClient = opcuaclient.NewClient(opcuaclient.Config{
		Endpoint: endpoint,
		Timeout:  10 * time.Second,
		AuthMode: authMode,
		Username: username,
		Password: password,
		CertFile: certFile,
		KeyFile:  keyFile,
	})

	d.config = &GatewayConfig{
		Gateway:  gateway,
		Tags:     tags,
		OrgName:  orgName,
		OrgID:    orgID,
		SiteName: siteName,
		AreaName: areaName,
		AuthMode: authMode,
		Username: username,
		Password: password,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	// Initialize or update Sparkplug B client
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		d.initSparkplugClientLocked(slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name), orgID)
	}

	// Subscribe to legacy format write commands: cmd/{org}/{site}/{area}/{gateway}/+
	legacyWriteTopic := fmt.Sprintf("cmd/%s/%s/%s/%s/+",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name))
	d.mqttClient.Subscribe(legacyWriteTopic, d.handleLegacyWriteCommand)
	log.Printf("[OPC-UA Driver] Subscribed to legacy write topic: %s", legacyWriteTopic)

	// Subscribe to Sparkplug B DCMD (Device Command) if enabled
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		groupID := sparkplug.BuildGroupID(slugify(orgName), slugify(siteName))
		edgeNodeID := sparkplug.BuildEdgeNodeID(slugify(areaName), slugify(gateway.Name))
		dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", groupID, edgeNodeID)
		d.mqttClient.Subscribe(dcmdTopic, d.handleSparkplugDCMD)
		log.Printf("[OPC-UA Driver] Subscribed to Sparkplug B DCMD topic: %s", dcmdTopic)
	}

	log.Printf("[OPC-UA Driver] Config loaded: gateway=%s, endpoint=%s, auth=%s, tags=%d",
		gateway.Name, endpoint, authMode, len(tags))

	// Reset connection state on config reload
	d.wasConnected = wasConnected
	d.isConnected = false

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
		MQTTHost:     getEnv("MQTT_HOST", "mosquitto"),
		MQTTPort:     getEnvInt("MQTT_PORT", 1883),
		MQTTClientID: fmt.Sprintf("sparkplug-opcua-%d", d.gatewayID),
		GroupID:      groupID,
		EdgeNodeID:   edgeNodeID,
		EnableLegacy: true,
	}

	// Create sparkplug client with the internal mqtt wrapper
	d.sparkplugClient = sparkplug.NewClient(config, d.mqttClient)
	d.sparkplugClient.SetConnected(true)

	d.dualPublisher = sparkplug.NewDualPublisher(
		d.sparkplugClient,
		orgName,
		siteName,
		areaName,
		gatewayName,
		orgID,
	)

	log.Printf("[OPC-UA Driver] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

// getPublishMode returns the current publish mode from settings
func (d *Driver) getPublishMode() models.PublishMode {
	if d.settingsManager != nil {
		return d.settingsManager.Get().PublishMode
	}
	return models.PublishModeDual // default
}

// publishTagValue publishes a tag value to MQTT with respect to publish mode
func (d *Driver) publishTagValue(tag models.Tag, value interface{}, quality int) {
	timestamp := time.Now().UnixMilli()
	alias := tag.Alias
	if alias == "" {
		alias = fmt.Sprintf("tag_%d", tag.ID)
	}

	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	publishMode := d.getPublishMode()

	d.sparkplugMu.RLock()
	sparkplugClient := d.sparkplugClient
	d.sparkplugMu.RUnlock()

	switch publishMode {
	case models.PublishModeLegacyOnly:
		// Only legacy format
		d.publishLegacy(tag.ID, cfg.OrgID, alias, value, timestamp, quality, cfg)

	case models.PublishModeSparkplugOnly:
		// Only Sparkplug B format (Protobuf)
		if sparkplugClient != nil && sparkplugClient.IsConnected() {
			tagData := sparkplug.TagData{
				TagID:     tag.ID,
				DeviceID:  alias,
				Value:     value,
				DataType:  tag.DataType,
				Timestamp: timestamp,
				Quality:   quality,
				OrgID:     cfg.OrgID,
			}
			if err := sparkplugClient.PublishSingleTag(tagData); err != nil {
				log.Printf("[OPC-UA Driver] Sparkplug publish error for %s: %v", alias, err)
			}
		} else {
			log.Printf("[OPC-UA Driver] Sparkplug client not available, skipping publish for %s", alias)
		}

	default: // PublishModeDual
		// Both formats - publish legacy first, then Sparkplug B
		d.publishLegacy(tag.ID, cfg.OrgID, alias, value, timestamp, quality, cfg)

		// Publish Sparkplug B format
		if sparkplugClient != nil && sparkplugClient.IsConnected() {
			tagData := sparkplug.TagData{
				TagID:     tag.ID,
				DeviceID:  alias,
				Value:     value,
				DataType:  tag.DataType,
				Timestamp: timestamp,
				Quality:   quality,
				OrgID:     cfg.OrgID,
			}
			if err := sparkplugClient.PublishSingleTag(tagData); err != nil {
				log.Printf("[OPC-UA Driver] Sparkplug publish error for %s: %v", alias, err)
			}
		}
	}
}

// publishLegacy publishes a tag value in legacy JSON format only
func (d *Driver) publishLegacy(tagID int, orgID int, alias string, value interface{}, timestamp int64, quality int, cfg *GatewayConfig) {
	topic := fmt.Sprintf("data/%s/%s/%s/%s/%s",
		slugify(cfg.OrgName), slugify(cfg.SiteName), slugify(cfg.AreaName),
		slugify(cfg.Gateway.Name), slugify(alias))
	payload := TagPayload{
		TagID:     tagID,
		OrgID:     orgID,
		Value:     value,
		Timestamp: timestamp,
		Quality:   quality,
	}
	data, _ := json.Marshal(payload)
	d.mqttClient.PublishWithQoS(topic, string(data), 1, false)
}

// setConnectionState updates connection state and handles DBIRTH/DDEATH
func (d *Driver) setConnectionState(connected bool) {
	d.connectionMu.Lock()
	wasConnected := d.isConnected
	d.wasConnected = wasConnected
	d.isConnected = connected
	d.connectionMu.Unlock()

	d.handleConnectionStateChange(connected, wasConnected)
}

// handleConnectionStateChange sends DBIRTH/DDEATH based on connection state changes
func (d *Driver) handleConnectionStateChange(connected, wasConnected bool) {
	publishMode := d.getPublishMode()

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
		log.Printf("[OPC-UA Driver] Sending DBIRTH (device coming online)")
		var tagsData []sparkplug.TagData
		for _, t := range cfg.Tags {
			d.previousMu.RLock()
			val := d.previousValues[t.ID]
			d.previousMu.RUnlock()
			tagsData = append(tagsData, sparkplug.TagData{
				TagID:     t.ID,
				DeviceID:  t.Alias,
				Value:     val,
				DataType:  t.DataType,
				Timestamp: time.Now().UnixMilli(),
				Quality:   192, // GOOD
				OrgID:     cfg.OrgID,
			})
		}
		if err := spClient.PublishDBIRTH(slugify(cfg.Gateway.Name), tagsData); err != nil {
			log.Printf("[OPC-UA Driver] Failed to publish DBIRTH: %v", err)
		}
	} else if !connected && wasConnected {
		// Going offline - send DDEATH
		log.Printf("[OPC-UA Driver] Sending DDEATH (device going offline)")
		if err := spClient.PublishDDEATH(slugify(cfg.Gateway.Name)); err != nil {
			log.Printf("[OPC-UA Driver] Failed to publish DDEATH: %v", err)
		}
	}
}

// publishDDEATH sends DDEATH on shutdown
func (d *Driver) publishDDEATH() {
	publishMode := d.getPublishMode()

	// Only send death if using Sparkplug
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

	log.Printf("[OPC-UA Driver] Sending DDEATH (shutdown)")
	if err := spClient.PublishDDEATH(slugify(cfg.Gateway.Name)); err != nil {
		log.Printf("[OPC-UA Driver] Failed to publish DDEATH: %v", err)
	}
}

// pollLoop reads OPC UA node values at the configured scan rate
func (d *Driver) pollLoop() {
	defer d.wg.Done()

	log.Printf("[OPC-UA Driver] Starting poll loop for gateway %d", d.gatewayID)

	for {
		d.configMu.RLock()
		config := d.config
		d.configMu.RUnlock()

		if config == nil || !config.Gateway.Enabled {
			select {
			case <-d.stopChan:
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		scanRate := time.Duration(config.Gateway.ScanRateMs) * time.Millisecond
		if scanRate < 100*time.Millisecond {
			scanRate = 100 * time.Millisecond
		}

		// Read all tags info needed for publishing
		d.configMu.RLock()
		tags := config.Tags
		d.configMu.RUnlock()

		// Ensure connected
		if !d.opcuaClient.IsConnected() {
			log.Printf("[OPC-UA Driver] Not connected, attempting to connect to OPC UA server...")
			if err := d.opcuaClient.ConnectWithRetry(3, 5*time.Second); err != nil {
				log.Printf("[OPC-UA Driver] Connection failed after 3 retries: %v", err)
				d.setConnectionState(false)

				// Publish BAD quality for all tags (only if changed)
				for _, tag := range tags {
					d.previousMu.RLock()
					val, exists := d.previousValues[tag.ID]
					d.previousMu.RUnlock()
					if !exists {
						val = 0
					}
					if d.shouldPublish(tag.ID, val, 2) {
						d.publishTagValue(tag, val, 2) // BAD quality
						d.updateState(tag.ID, val, 2)
					}
				}

				select {
				case <-d.stopChan:
					return
				case <-time.After(10 * time.Second):
					continue
				}
			}
			log.Printf("[OPC-UA Driver] Successfully connected to OPC UA server")
			d.setConnectionState(true)
		}

		for _, tag := range tags {
			value, quality, err := d.opcuaClient.ReadValue(tag.Code)
			if err != nil {
				log.Printf("[OPC-UA Driver] Read error for tag %d (NodeID: %s): %v", tag.ID, tag.Code, err)
				// Publish bad quality if changed
				d.previousMu.RLock()
				val, exists := d.previousValues[tag.ID]
				d.previousMu.RUnlock()
				if !exists {
					val = 0
				}
				if d.shouldPublish(tag.ID, val, 2) {
					d.publishTagValue(tag, val, 2) // BAD quality
					d.updateState(tag.ID, val, 2)
				}
				continue
			}

			// Log successful read with quality info
			qualityStr := "GOOD"
			if quality != 0 {
				qualityStr = "BAD"
			}

			// Evaluate alarms via AlarmManager
			// Convert OPC-UA quality (0=GOOD, 1=BAD) to industrial-edge standard (192=GOOD, 0=BAD)
			alarmQuality := 192
			if quality != 0 {
				alarmQuality = 0 // BAD - will be skipped by alarm manager
			}
			if d.alarmManager != nil {
				d.alarmManager.EvaluateTag(tag.ID, tag.Alias, value, alarmQuality)
			}

			// Check RBE - only publish if value/quality changed
			if d.shouldPublish(tag.ID, value, quality) {
				log.Printf("[OPC-UA Driver] Tag %d (%s): value=%v, quality=%s - PUBLISHING",
					tag.ID, tag.Alias, value, qualityStr)
				d.publishTagValue(tag, value, quality)
				d.updateState(tag.ID, value, quality)
			}
		}

		select {
		case <-d.stopChan:
			return
		case <-time.After(scanRate):
		}
	}
}

// handleLegacyWriteCommand handles write commands in legacy MQTT format
// Topic format: cmd/{org}/{site}/{area}/{gateway}/{alias}
// Payload: {"value": <value>} or just <value>
func (d *Driver) handleLegacyWriteCommand(topic string, payload []byte) {
	log.Printf("[OPC-UA Driver] Received legacy write command: %s", topic)

	// Extract alias from topic
	parts := strings.Split(topic, "/")
	if len(parts) < 6 {
		log.Printf("[OPC-UA Driver] Invalid legacy write topic format: %s", topic)
		return
	}
	alias := parts[5]

	// Find tag by alias
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[OPC-UA Driver] No config loaded, cannot process write")
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
		log.Printf("[OPC-UA Driver] Tag with alias '%s' not found", alias)
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
			log.Printf("[OPC-UA Driver] Failed to parse value from payload: %v", err)
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
	d.handleWriteCommand("", cmdBytes)
}

// handleSparkplugDCMD handles Sparkplug B Device Commands
// Topic format: spBv1.0/{group}/DCMD/{node}/{device}
// Payload: Sparkplug B payload with metrics
func (d *Driver) handleSparkplugDCMD(topic string, payload []byte) {
	log.Printf("[OPC-UA Driver] Received Sparkplug B DCMD: %s", topic)

	// Parse Sparkplug B topic
	_, err := sparkplug.ParseTopic(topic)
	if err != nil {
		log.Printf("[OPC-UA Driver] Invalid Sparkplug DCMD topic: %v", err)
		return
	}

	// Try Protobuf first, then JSON
	var metrics []struct {
		Name      string
		Value     interface{}
		DataType  int
		Timestamp int64
	}

	// Check if Protobuf or JSON
	if len(payload) > 0 && payload[0] != 0x7B && payload[0] != 0x5B {
		// Protobuf - try to decode
		spPayload, err := sparkplug.DecodePayloadProtobuf(payload)
		if err != nil {
			log.Printf("[OPC-UA Driver] Failed to decode Sparkplug DCMD protobuf: %v", err)
			return
		}
		for _, m := range spPayload.Metrics {
			metrics = append(metrics, struct {
				Name      string
				Value     interface{}
				DataType  int
				Timestamp int64
			}{
				Name:      m.Name,
				Value:     m.Value,
				DataType:  int(m.DataType),
				Timestamp: m.Timestamp,
			})
		}
	} else {
		// JSON format
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
			log.Printf("[OPC-UA Driver] Failed to parse Sparkplug DCMD payload: %v", err)
			return
		}
		for _, m := range spPayload.Metrics {
			metrics = append(metrics, struct {
				Name      string
				Value     interface{}
				DataType  int
				Timestamp int64
			}{
				Name:      m.Name,
				Value:     m.Value,
				DataType:  m.DataType,
				Timestamp: m.Timestamp,
			})
		}
	}

	// Find config
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[OPC-UA Driver] No config loaded, cannot process DCMD")
		return
	}

	// Process each metric
	for _, metric := range metrics {
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
			log.Printf("[OPC-UA Driver] DCMD: Tag '%s' not found", metric.Name)
			continue
		}

		// Create WriteCommand and process
		writeCmd := WriteCommand{
			TagID:    targetTag.ID,
			Code:     targetTag.Code,
			Value:    metric.Value,
			DataType: targetTag.DataType,
		}

		log.Printf("[OPC-UA Driver] DCMD: Writing to tag %d (%s) = %v", targetTag.ID, targetTag.Alias, metric.Value)

		cmdBytes, _ := json.Marshal(writeCmd)
		d.handleWriteCommand("", cmdBytes)
	}
}

// healthLoop publishes health status periodically
func (d *Driver) healthLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			status := "offline"
			if d.opcuaClient != nil && d.opcuaClient.IsConnected() {
				status = "online"
			}

			topic := fmt.Sprintf("sys/health/%d", d.gatewayID)
			d.mqttClient.PublishWithQoS(topic, status, 1, true)
		}
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// slugify converts a string to a URL-friendly slug
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// publishDual publishes a tag value based on the configured publish mode
func (d *Driver) publishDual(tagID int, alias string, value interface{}, dataType string, quality int, timestamp int64) {
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
		d.publishLegacy(tagID, cfg.OrgID, alias, value, timestamp, quality, cfg)

	case models.PublishModeSparkplugOnly:
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
				log.Printf("[OPC-UA Driver] Sparkplug publish error for %s: %v", alias, err)
			}
		}

	default: // PublishModeDual
		if dualPublisher != nil {
			if err := dualPublisher.Publish(tagID, alias, value, dataType, quality, timestamp, d.mqttClient); err != nil {
				log.Printf("[OPC-UA Driver] Dual publish error for %s: %v", alias, err)
			}
		} else {
			d.publishLegacy(tagID, cfg.OrgID, alias, value, timestamp, quality, cfg)
		}
	}
}

// shouldPublish checks if the value should be published based on RBE settings
func (d *Driver) shouldPublish(tagID int, value interface{}, quality int) bool {
	// Get previous values for RBE comparison
	d.previousMu.RLock()
	oldValue := d.previousValues[tagID]
	d.previousMu.RUnlock()

	d.prevQualitiesMu.RLock()
	oldQuality := d.previousQualities[tagID]
	d.prevQualitiesMu.RUnlock()

	// Use settings manager for RBE logic
	if d.settingsManager != nil {
		return d.settingsManager.ShouldPublish(tagID, value, oldValue, quality, oldQuality)
	}

	// Fallback: publish only on value or quality change
	if oldValue == nil {
		return true
	}
	return !valuesEqual(oldValue, value) || oldQuality != quality
}

// updateState updates the previous value and quality for RBE
func (d *Driver) updateState(tagID int, value interface{}, quality int) {
	d.previousMu.Lock()
	d.previousValues[tagID] = value
	d.previousMu.Unlock()

	d.prevQualitiesMu.Lock()
	d.previousQualities[tagID] = quality
	d.prevQualitiesMu.Unlock()
}

// valuesEqual compares two values for equality
func valuesEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case int:
		bv, ok := b.(int)
		return ok && av == bv
	case int8:
		bv, ok := b.(int8)
		return ok && av == bv
	case int16:
		bv, ok := b.(int16)
		return ok && av == bv
	case int32:
		bv, ok := b.(int32)
		return ok && av == bv
	case int64:
		bv, ok := b.(int64)
		return ok && av == bv
	case uint:
		bv, ok := b.(uint)
		return ok && av == bv
	case uint8:
		bv, ok := b.(uint8)
		return ok && av == bv
	case uint16:
		bv, ok := b.(uint16)
		return ok && av == bv
	case uint32:
		bv, ok := b.(uint32)
		return ok && av == bv
	case uint64:
		bv, ok := b.(uint64)
		return ok && av == bv
	case float32:
		bv, ok := b.(float32)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}
