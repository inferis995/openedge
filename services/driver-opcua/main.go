package main

import (
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

	mqtt "github.com/eclipse/paho.mqtt.golang"
	_ "github.com/lib/pq"
	opcuaclient "github.com/ralph/industrial-edge-middleware/internal/opcua"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

// Models (same as other drivers)
type Gateway struct {
	ID               int
	AreaID           int
	Name             string
	DriverType       string
	ConnectionConfig json.RawMessage
	ScanRateMs       int
	Enabled          bool
}

type Tag struct {
	ID                int
	GatewayID         int
	Code              string // Node ID (e.g. "ns=2;s=Temperature")
	Alias             string
	DataType          string
	Historize         bool
	HistorizeDeadband float64
}

// TagPayload is the standard payload published to MQTT
type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// GatewayConfig adds auth configuration fields
type GatewayConfig struct {
	Gateway  Gateway
	Tags     []Tag
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

// Driver manages the OPC UA driver lifecycle
type Driver struct {
	gatewayID      int
	database       *sql.DB
	mqttClient     mqtt.Client
	opcuaClient    *opcuaclient.Client
	config         *GatewayConfig
	configMu       sync.RWMutex
	stopChan       chan struct{}
	wg             sync.WaitGroup
	previousValues map[int]interface{}
	previousMu     sync.RWMutex

	// Sparkplug B support
	sparkplugClient *sparkplug.SparkplugClient
	dualPublisher   *sparkplug.DualPublisher
	sparkplugMu     sync.RWMutex
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[OPC-UA Driver] Starting...")

	// Get gateway ID from environment
	gatewayIDStr := os.Getenv("GATEWAY_ID")
	if gatewayIDStr == "" {
		log.Fatal("GATEWAY_ID environment variable is required")
	}
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		log.Fatalf("Invalid GATEWAY_ID: %v", err)
	}

	// Connect to database
	dbHost := getEnv("DB_HOST", "postgres")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "industrial_user")
	dbPass := getEnv("DB_PASSWORD", "industrial_pass")
	dbName := getEnv("DB_NAME", "industrial_edge")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Verify database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Database ping failed: %v", err)
	}
	log.Println("[OPC-UA Driver] Connected to database")

	// Connect to MQTT broker
	mqttHost := getEnv("MQTT_HOST", "mosquitto")
	mqttPort := getEnv("MQTT_PORT", "1883")
	clientID := fmt.Sprintf("driver-opcua-%d", gatewayID)

	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", mqttHost, mqttPort)).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(30 * time.Second)

	mqttClient := mqtt.NewClient(opts)
	token := mqttClient.Connect()
	if token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", token.Error())
	}
	log.Printf("[OPC-UA Driver] Connected to MQTT broker at %s:%s", mqttHost, mqttPort)

	// Create driver instance
	driver := &Driver{
		gatewayID:      gatewayID,
		database:       db,
		mqttClient:     mqttClient,
		stopChan:       make(chan struct{}),
		previousValues: make(map[int]interface{}),
	}

	// Initialize Sparkplug B client (optional - for dual publishing)
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		log.Println("[OPC-UA Driver] Sparkplug B dual publishing enabled")
	}

	// Load initial configuration
	if err := driver.loadConfig(); err != nil {
		log.Fatalf("Failed to load initial config: %v", err)
	}

	// Subscribe to reload commands
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	mqttClient.Subscribe(reloadTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
		log.Printf("[OPC-UA Driver] Received reload command")
		if err := driver.loadConfig(); err != nil {
			log.Printf("[OPC-UA Driver] Reload failed: %v", err)
		}
	})

	// Subscribe to write commands
	writeTopic := fmt.Sprintf("sys/command/write/%d", gatewayID)
	mqttClient.Subscribe(writeTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
		driver.handleWriteCommand(msg.Payload())
	})

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
	close(driver.stopChan)
	driver.wg.Wait()

	if driver.opcuaClient != nil {
		driver.opcuaClient.Disconnect()
	}
	mqttClient.Disconnect(1000)
	log.Println("[OPC-UA Driver] Shutdown complete")
}

// loadConfig loads gateway and tag configuration from the database
func (d *Driver) loadConfig() error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	// Load gateway
	var gw Gateway
	err := d.database.QueryRow(`
		SELECT id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled
		FROM gateways WHERE id = $1
	`, d.gatewayID).Scan(
		&gw.ID, &gw.AreaID, &gw.Name, &gw.DriverType,
		&gw.ConnectionConfig, &gw.ScanRateMs, &gw.Enabled,
	)
	if err != nil {
		return fmt.Errorf("failed to load gateway %d: %w", d.gatewayID, err)
	}

	// Load organization/site/area names for MQTT topic and OrgID for TagPayload
	var orgID int
	var orgName, siteName, areaName string
	err = d.database.QueryRow(`
		SELECT o.id, o.name, s.name, a.name
		FROM areas a
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE a.id = $1
	`, gw.AreaID).Scan(&orgID, &orgName, &siteName, &areaName)
	if err != nil {
		log.Printf("[OPC-UA Driver] Warning: could not load org/site/area names/id: %v", err)
		orgName = "default"
		siteName = "default"
		areaName = "default"
	}

	// Load tags
	rows, err := d.database.Query(`
		SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband
		FROM tags WHERE gateway_id = $1
		ORDER BY id
	`, d.gatewayID)
	if err != nil {
		return fmt.Errorf("failed to load tags: %w", err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.GatewayID, &t.Code, &t.Alias, &t.DataType, &t.Historize, &t.HistorizeDeadband); err != nil {
			return fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, t)
	}

	// Parse connection config to get endpoint and auth
	var connConfig map[string]interface{}
	if err := json.Unmarshal(gw.ConnectionConfig, &connConfig); err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}

	endpoint, _ := connConfig["endpoint"].(string)
	if endpoint == "" {
		return fmt.Errorf("missing 'endpoint' in connection_config for OPC_UA gateway %d", d.gatewayID)
	}

	authMode, _ := connConfig["auth_mode"].(string)
	username, _ := connConfig["username"].(string)
	password, _ := connConfig["password"].(string)
	certFile, _ := connConfig["cert_file"].(string)
	keyFile, _ := connConfig["key_file"].(string)

	if authMode == "" {
		authMode = "Anonymous"
	}

	// Create/update OPC UA client
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
		Gateway:  gw,
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
		d.initSparkplugClientLocked(slugify(orgName), slugify(siteName), slugify(areaName), slugify(gw.Name), orgID)
	}

	// Subscribe to legacy format write commands: cmd/{org}/{site}/{area}/{gateway}/+
	legacyWriteTopic := fmt.Sprintf("cmd/%s/%s/%s/%s/+",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gw.Name))
	d.mqttClient.Subscribe(legacyWriteTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
		d.handleLegacyWriteCommand(msg.Topic(), msg.Payload())
	})
	log.Printf("[OPC-UA Driver] Subscribed to legacy write topic: %s", legacyWriteTopic)

	// Subscribe to Sparkplug B DCMD (Device Command) if enabled
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		groupID := sparkplug.BuildGroupID(slugify(orgName), slugify(siteName))
		edgeNodeID := sparkplug.BuildEdgeNodeID(slugify(areaName), slugify(gw.Name))
		dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", groupID, edgeNodeID)
		d.mqttClient.Subscribe(dcmdTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
			d.handleSparkplugDCMD(msg.Topic(), msg.Payload())
		})
		log.Printf("[OPC-UA Driver] Subscribed to Sparkplug B DCMD topic: %s", dcmdTopic)
	}

	log.Printf("[OPC-UA Driver] Config loaded: gateway=%s, endpoint=%s, auth=%s, tags=%d",
		gw.Name, endpoint, authMode, len(tags))

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

	if d.sparkplugClient == nil {
		// Create a wrapper for the paho MQTT client to use with Sparkplug
		// Since OPC UA driver uses paho directly, we need the wrapper
		d.sparkplugClient = sparkplug.NewClient(config, nil)
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

	log.Printf("[OPC-UA Driver] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

// publishDual publishes a tag value in both legacy and Sparkplug B formats
func (d *Driver) publishDual(tag Tag, value interface{}, quality int) {
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

	// 1. Always publish legacy format
	payload := TagPayload{
		TagID:     tag.ID,
		OrgID:     cfg.OrgID,
		Value:     value,
		Timestamp: timestamp,
		Quality:   quality,
	}
	data, err := json.Marshal(payload)
	if err == nil {
		dataTopic := fmt.Sprintf("data/%s/%s/%s/%s/%s",
			slugify(cfg.OrgName), slugify(cfg.SiteName), slugify(cfg.AreaName),
			slugify(cfg.Gateway.Name), slugify(alias))
		d.mqttClient.Publish(dataTopic, 1, false, data)
	}

	// 2. Publish Sparkplug B format if enabled (directly using paho client)
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		d.sparkplugMu.RLock()
		spClient := d.sparkplugClient
		d.sparkplugMu.RUnlock()

		if spClient != nil {
			// Build Sparkplug B topic and payload directly
			groupID := sparkplug.BuildGroupID(slugify(cfg.OrgName), slugify(cfg.SiteName))
			edgeNodeID := sparkplug.BuildEdgeNodeID(slugify(cfg.AreaName), slugify(cfg.Gateway.Name))
			spTopic := fmt.Sprintf("spBv1.0/%s/DDATA/%s/%s", groupID, edgeNodeID, slugify(alias))

			// Build Sparkplug B payload
			spPayload := sparkplug.CreateDDATAPayload(slugify(alias), []sparkplug.TagData{
				{
					TagID:     tag.ID,
					DeviceID:  slugify(alias),
					Value:     value,
					DataType:  tag.DataType,
					Timestamp: timestamp,
					Quality:   quality,
					OrgID:     cfg.OrgID,
				},
			})

			spData, err := json.Marshal(spPayload)
			if err == nil {
				d.mqttClient.Publish(spTopic, 0, false, spData)
			}
		}
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
		orgID := config.OrgID
		orgName := config.OrgName
		siteName := config.SiteName
		areaName := config.AreaName
		gwName := config.Gateway.Name
		d.configMu.RUnlock()

		// Ensure connected
		if !d.opcuaClient.IsConnected() {
			log.Printf("[OPC-UA Driver] Not connected, attempting to connect to OPC UA server...")
			if err := d.opcuaClient.ConnectWithRetry(3, 5*time.Second); err != nil {
				log.Printf("[OPC-UA Driver] Connection failed after 3 retries: %v", err)

				// Publish BAD quality for all tags (with updated timestamp)
				now := time.Now().UnixMilli()
				for _, tag := range tags {
					d.previousMu.RLock()
					val, exists := d.previousValues[tag.ID]
					d.previousMu.RUnlock()
					if !exists {
						val = 0
					}
					d.publishTagValue(tag, val, 2, orgID, orgName, siteName, areaName, gwName)
					log.Printf("[OPC-UA Driver] Published BAD quality for tag %d (%s) at timestamp %d", tag.ID, tag.Alias, now)

					// Update previous value
					d.previousMu.Lock()
					d.previousValues[tag.ID] = val
					d.previousMu.Unlock()
				}

				select {
				case <-d.stopChan:
					return
				case <-time.After(10 * time.Second):
					continue
				}
			}
			log.Printf("[OPC-UA Driver] Successfully connected to OPC UA server")
		}

		for _, tag := range tags {
			value, quality, err := d.opcuaClient.ReadValue(tag.Code)
			if err != nil {
				log.Printf("[OPC-UA Driver] Read error for tag %d (NodeID: %s): %v", tag.ID, tag.Code, err)
				// Publish bad quality
				d.previousMu.RLock()
				val, exists := d.previousValues[tag.ID]
				d.previousMu.RUnlock()
				if !exists {
					val = 0
				}
				d.publishTagValue(tag, val, 2, orgID, orgName, siteName, areaName, gwName)

				d.previousMu.Lock()
				d.previousValues[tag.ID] = val
				d.previousMu.Unlock()
				continue
			}

			// Log successful read with quality info
			qualityStr := "GOOD"
			if quality != 0 {
				qualityStr = "BAD"
			}

			// ALWAYS publish with updated timestamp (no Report by Exception)
			log.Printf("[OPC-UA Driver] Tag %d (%s): value=%v, quality=%s",
				tag.ID, tag.Alias, value, qualityStr)
			d.publishTagValue(tag, value, quality, orgID, orgName, siteName, areaName, gwName)

			d.previousMu.Lock()
			d.previousValues[tag.ID] = value
			d.previousMu.Unlock()
		}

		select {
		case <-d.stopChan:
			return
		case <-time.After(scanRate):
		}
	}
}

// publishTagValue publishes a tag value to MQTT (with Sparkplug B dual support)
func (d *Driver) publishTagValue(tag Tag, value interface{}, quality int, orgID int, orgName, siteName, areaName, gwName string) {
	// Use dual publishing
	d.publishDual(tag, value, quality)
}

// handleWriteCommand processes incoming write commands via MQTT
func (d *Driver) handleWriteCommand(payload []byte) {
	var cmd struct {
		TagID int         `json:"tag_id"`
		Value interface{} `json:"value"`
	}

	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("[OPC-UA Driver] Invalid write command: %v", err)
		return
	}

	d.configMu.RLock()
	config := d.config
	d.configMu.RUnlock()

	if config == nil {
		log.Printf("[OPC-UA Driver] Write failed: no config loaded")
		return
	}

	// Find the tag
	var targetTag *Tag
	for _, t := range config.Tags {
		if t.ID == cmd.TagID {
			tc := t
			targetTag = &tc
			break
		}
	}

	if targetTag == nil {
		log.Printf("[OPC-UA Driver] Write failed: tag %d not found", cmd.TagID)
		return
	}

	if err := d.opcuaClient.WriteValue(targetTag.Code, cmd.Value, targetTag.DataType); err != nil {
		log.Printf("[OPC-UA Driver] Write error for tag %d (%s): %v", targetTag.ID, targetTag.Code, err)
		return
	}

	log.Printf("[OPC-UA Driver] Write success: tag %d (%s) = %v", targetTag.ID, targetTag.Code, cmd.Value)
}

// WriteCommand represents a write command with full tag info
type WriteCommand struct {
	TagID    int         `json:"tag_id"`
	Code     string      `json:"code"`
	Value    interface{} `json:"value"`
	DataType string      `json:"data_type"`
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

	var targetTag *Tag
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
	d.handleWriteCommand(cmdBytes)
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
		log.Printf("[OPC-UA Driver] Failed to parse Sparkplug DCMD payload: %v", err)
		return
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
	for _, metric := range spPayload.Metrics {
		// Find tag by alias (metric name)
		var targetTag *Tag
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
		d.handleWriteCommand(cmdBytes)
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
			d.mqttClient.Publish(topic, 0, true, status)
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
