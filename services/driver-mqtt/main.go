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
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

// TagMapping maps a source MQTT topic to a system publish topic and tag metadata
type TagMapping struct {
	Tag          models.Tag
	SourceTopic  string // The PLC's MQTT topic (from tag.Code)
	PublishTopic string // The system topic: data/{org}/{site}/{area}/{gw}/{alias}
	OrgID        int
}

// GatewayConfig holds the loaded gateway configuration
type GatewayConfig struct {
	Gateway     models.Gateway
	Tags        []models.Tag
	TagMappings []TagMapping
	OrgName     string
	OrgID       int
	Site        string
	Area        string
}

// TagPayload is the standard system message format
type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// Driver is the main MQTT-to-MQTT bridge driver
type Driver struct {
	gatewayID      int
	database       *sql.DB
	mqttClient     *mqtt.Client // Internal broker (system)
	config         *GatewayConfig
	configMu       sync.RWMutex
	stopChan       chan struct{}
	reloadChan     chan struct{}
	isConnected    bool
	subscribedTags map[string]bool // Track subscribed source topics
	subMu          sync.Mutex

	// Source connectivity tracking
	lastMessageTime map[int]time.Time
	connectionLost  map[int]bool
	msgTimeMu       sync.Mutex

	// Sparkplug B support
	sparkplugClient *sparkplug.SparkplugClient
	dualPublisher   *sparkplug.DualPublisher
	sparkplugMu     sync.RWMutex

	// Alarm Manager
	alarmManager *alarms.Manager
}

// WriteCommand represents a write command received via MQTT
type WriteCommand struct {
	TagID    int         `json:"tag_id"`
	Code     string      `json:"code"`
	Value    interface{} `json:"value"`
	DataType string      `json:"data_type"`
}

// WriteResult is published to MQTT to confirm write success/failure
type WriteResult struct {
	TagID     int         `json:"tag_id"`
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	ReadBack  interface{} `json:"read_back,omitempty"`
	Timestamp int64       `json:"ts"`
}

func main() {
	log.Println("[DRIVER-MQTT] Starting driver-mqtt...")

	gatewayIDStr := os.Getenv("GATEWAY_ID")
	if gatewayIDStr == "" {
		log.Fatal("[DRIVER-MQTT] GATEWAY_ID environment variable is required")
	}
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		log.Fatalf("[DRIVER-MQTT] Invalid GATEWAY_ID: %v", err)
	}

	// Connect to database
	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "industrial_user"),
		Password: getEnv("DB_PASSWORD", "industrial_pass"),
		Database: getEnv("DB_NAME", "industrial_edge"),
	}

	database, err := db.Connect(dbCfg)
	if err != nil {
		log.Fatalf("[DRIVER-MQTT] Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("[DRIVER-MQTT] Connected to database")

	// Connect to internal MQTT broker (system broker)
	mqttCfg := mqtt.Config{
		Host:          getEnv("MQTT_HOST", "localhost"),
		Port:          getEnvInt("MQTT_PORT", 1883),
		ClientID:      fmt.Sprintf("driver-mqtt-%d", gatewayID),
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		LWTTopic:      fmt.Sprintf("sys/health/%d", gatewayID),
		LWTPayload:    "offline",
		LWTRetained:   true,
	}

	mqttClient := mqtt.NewClient(mqttCfg)
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("[DRIVER-MQTT] Failed to connect to MQTT broker: %v", err)
	}
	defer mqttClient.Disconnect(1000)
	log.Println("[DRIVER-MQTT] Connected to internal MQTT broker")

	// NOTE: Health status will be published by setConnectionState() when subscriptions are active
	// This ensures the status reflects actual driver readiness, not just startup

	driver := &Driver{
		gatewayID:       gatewayID,
		database:        database,
		mqttClient:      mqttClient,
		stopChan:        make(chan struct{}),
		reloadChan:      make(chan struct{}, 1),
		subscribedTags:  make(map[string]bool),
		lastMessageTime: make(map[int]time.Time),
		connectionLost:  make(map[int]bool),
	}

	// Initialize Sparkplug B client (optional - for dual publishing)
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		log.Println("[DRIVER-MQTT] Sparkplug B dual publishing enabled")
	}

	// Load initial configuration
	if err := driver.loadConfig(); err != nil {
		log.Fatalf("[DRIVER-MQTT] Failed to load config: %v", err)
	}

	// Subscribe to reload commands
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	mqttClient.Subscribe(reloadTopic, driver.handleReloadCommand)
	log.Printf("[DRIVER-MQTT] Subscribed to reload topic: %s", reloadTopic)

	// Subscribe to write commands
	writeTopic := fmt.Sprintf("cmd/write/%d", gatewayID)
	mqttClient.Subscribe(writeTopic, driver.handleWriteCommand)
	log.Printf("[DRIVER-MQTT] Subscribed to write topic: %s", writeTopic)

	// Subscribe to all source PLC topics
	driver.subscribeToSourceTopics()

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

	// Start the main loop
	go driver.run()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[DRIVER-MQTT] Shutting down...")
	close(driver.stopChan)

	// Publish offline status on shutdown
	healthTopic := fmt.Sprintf("sys/health/%d", gatewayID)
	mqttClient.PublishWithQoS(healthTopic, "offline", 1, true)
	log.Println("[DRIVER-MQTT] Shutdown complete")
}

// loadConfig reads the gateway and tag configuration from the database
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
		return fmt.Errorf("failed to load gateway: %w", err)
	}

	json.Unmarshal(connConfigBytes, &gateway.ConnectionConfig)

	// Load tags
	tagsQuery := `SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband FROM tags WHERE gateway_id = $1`
	rows, err := d.database.Query(tagsQuery, d.gatewayID)
	if err != nil {
		return fmt.Errorf("failed to load tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		rows.Scan(&t.ID, &t.GatewayID, &t.Code, &t.Alias, &t.DataType, &t.Historize, &t.HistorizeDeadband)
		tags = append(tags, t)
	}

	// Build topic prefix: data/{org}/{site}/{area}/{gateway}
	prefix := fmt.Sprintf("data/%s/%s/%s/%s",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name))

	// Create tag mappings: source topic → system publish topic
	var mappings []TagMapping
	for _, tag := range tags {
		sourceTopic := strings.TrimSpace(tag.Code) // tag.Code = PLC's MQTT topic
		if sourceTopic == "" {
			log.Printf("[DRIVER-MQTT] WARNING: Tag %d (%s) has empty code, skipping", tag.ID, tag.Alias)
			continue
		}

		publishTopic := fmt.Sprintf("%s/%s", prefix, slugify(tag.Alias))

		mappings = append(mappings, TagMapping{
			Tag:          tag,
			SourceTopic:  sourceTopic,
			PublishTopic: publishTopic,
			OrgID:        orgID,
		})
	}

	d.config = &GatewayConfig{
		Gateway:     gateway,
		Tags:        tags,
		TagMappings: mappings,
		OrgName:     slugify(orgName),
		OrgID:       orgID,
		Site:        slugify(siteName),
		Area:        slugify(areaName),
	}

	// Initialize or update Sparkplug B client
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		d.initSparkplugClientLocked(slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name), orgID)
	}

	// Subscribe to legacy format write commands: cmd/{org}/{site}/{area}/{gateway}/+
	legacyWriteTopic := fmt.Sprintf("cmd/%s/%s/%s/%s/+",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name))
	d.mqttClient.Subscribe(legacyWriteTopic, d.handleLegacyWriteCommand)
	log.Printf("[DRIVER-MQTT] Subscribed to legacy write topic: %s", legacyWriteTopic)

	// Subscribe to Sparkplug B DCMD (Device Command) if enabled
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		groupID := sparkplug.BuildGroupID(slugify(orgName), slugify(siteName))
		edgeNodeID := sparkplug.BuildEdgeNodeID(slugify(areaName), slugify(gateway.Name))
		dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", groupID, edgeNodeID)
		d.mqttClient.Subscribe(dcmdTopic, d.handleSparkplugDCMD)
		log.Printf("[DRIVER-MQTT] Subscribed to Sparkplug B DCMD topic: %s", dcmdTopic)
	}

	log.Printf("[DRIVER-MQTT] Config loaded: %d tags, %d mappings", len(tags), len(mappings))
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
		MQTTClientID: fmt.Sprintf("sparkplug-mqtt-%d", d.gatewayID),
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

	log.Printf("[DRIVER-MQTT] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

// publishDual publishes a tag value in both legacy and Sparkplug B formats
func (d *Driver) publishDual(tagID int, alias string, value interface{}, dataType string, quality int, timestamp int64) {
	d.sparkplugMu.RLock()
	dualPublisher := d.dualPublisher
	d.sparkplugMu.RUnlock()

	if dualPublisher != nil {
		// Use dual publisher
		if err := dualPublisher.Publish(tagID, alias, value, dataType, quality, timestamp, d.mqttClient); err != nil {
			log.Printf("[DRIVER-MQTT] Dual publish error for %s: %v", alias, err)
		}
	} else {
		// Legacy-only publish
		d.configMu.RLock()
		cfg := d.config
		d.configMu.RUnlock()

		if cfg != nil {
			topic := fmt.Sprintf("data/%s/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
			payload, _ := json.Marshal(TagPayload{tagID, cfg.OrgID, value, timestamp, quality})
			d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)
		}
	}
}

// subscribeToSourceTopics subscribes to all PLC source topics
func (d *Driver) subscribeToSourceTopics() {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	d.subMu.Lock()
	defer d.subMu.Unlock()

	// Unsubscribe from topics no longer in config
	newTopics := make(map[string]bool)
	for _, m := range cfg.TagMappings {
		newTopics[m.SourceTopic] = true
	}

	for topic := range d.subscribedTags {
		if !newTopics[topic] {
			d.mqttClient.Unsubscribe(topic)
			delete(d.subscribedTags, topic)
			log.Printf("[DRIVER-MQTT] Unsubscribed from removed topic: %s", topic)
		}
	}

	// Subscribe to new topics
	for _, mapping := range cfg.TagMappings {
		if d.subscribedTags[mapping.SourceTopic] {
			continue // Already subscribed
		}

		// Capture mapping in closure
		m := mapping
		err := d.mqttClient.Subscribe(m.SourceTopic, func(topic string, payload []byte) {
			d.handleSourceMessage(m, topic, payload)
		})
		if err != nil {
			log.Printf("[DRIVER-MQTT] ERROR: Failed to subscribe to %s: %v", m.SourceTopic, err)
			continue
		}

		d.subscribedTags[m.SourceTopic] = true
		log.Printf("[DRIVER-MQTT] Subscribed to PLC topic: %s → %s", m.SourceTopic, m.PublishTopic)
	}

	// Mark connection as online since we're listening
	if !d.isConnected && len(d.subscribedTags) > 0 {
		d.setConnectionState(true)
	}
}

// handleSourceMessage processes a message received from a PLC source topic
func (d *Driver) handleSourceMessage(mapping TagMapping, topic string, payload []byte) {
	// Parse the incoming value based on data type
	value, err := parseIncomingValue(payload, mapping.Tag.DataType)
	if err != nil {
		log.Printf("[DRIVER-MQTT] ERROR parsing value from %s: %v (raw: %s)", topic, err, string(payload))
		return
	}

	d.msgTimeMu.Lock()
	d.lastMessageTime[mapping.Tag.ID] = time.Now()
	if d.connectionLost[mapping.Tag.ID] {
		log.Printf("[DRIVER-MQTT] Source connection restored for tag %s", mapping.Tag.Alias)
		d.connectionLost[mapping.Tag.ID] = false
	}
	d.msgTimeMu.Unlock()

	timestamp := time.Now().UnixMilli()

	// Evaluate alarms via AlarmManager (192 = GOOD quality)
	if d.alarmManager != nil {
		d.alarmManager.EvaluateTag(mapping.Tag.ID, mapping.Tag.Alias, value, 192)
	}

	// Use dual publishing (legacy + Sparkplug B)
	d.publishDual(mapping.Tag.ID, mapping.Tag.Alias, value, mapping.Tag.DataType, 0, timestamp)

	log.Printf("[DRIVER-MQTT] BRIDGED: %s → %s = %v", topic, mapping.Tag.Alias, value)
}

// parseIncomingValue parses the raw MQTT payload into a typed value
func parseIncomingValue(payload []byte, dataType string) (interface{}, error) {
	raw := strings.TrimSpace(string(payload))
	if raw == "" {
		return nil, fmt.Errorf("empty payload")
	}

	// Try to parse as JSON first (e.g. {"value": 23.5} or just 23.5)
	var jsonVal interface{}
	if err := json.Unmarshal(payload, &jsonVal); err == nil {
		// If it's a map, try to extract "value" or "v" field
		if m, ok := jsonVal.(map[string]interface{}); ok {
			if v, exists := m["value"]; exists {
				return convertValue(v, dataType)
			}
			if v, exists := m["v"]; exists {
				return convertValue(v, dataType)
			}
			if v, exists := m["val"]; exists {
				return convertValue(v, dataType)
			}
			return nil, fmt.Errorf("JSON object has no 'value', 'v', or 'val' field")
		}
		// It's a raw JSON value (number, string, bool)
		return convertValue(jsonVal, dataType)
	}

	// Not JSON, treat as raw string value
	return convertStringValue(raw, dataType)
}

// convertValue converts a parsed JSON value to the expected data type
func convertValue(v interface{}, dataType string) (interface{}, error) {
	switch strings.ToUpper(dataType) {
	case "BOOL":
		switch val := v.(type) {
		case bool:
			return val, nil
		case float64:
			return val != 0, nil
		case string:
			return strings.ToLower(val) == "true" || val == "1", nil
		}
		return false, nil

	case "INT", "UINT", "DINT":
		switch val := v.(type) {
		case float64:
			return val, nil
		case string:
			f, err := strconv.ParseFloat(val, 64)
			return f, err
		}
		return nil, fmt.Errorf("cannot convert %T to %s", v, dataType)

	case "REAL":
		switch val := v.(type) {
		case float64:
			return val, nil
		case string:
			f, err := strconv.ParseFloat(val, 64)
			return f, err
		}
		return nil, fmt.Errorf("cannot convert %T to %s", v, dataType)

	case "STRING":
		return fmt.Sprintf("%v", v), nil

	default:
		// Default: try to return as-is
		return v, nil
	}
}

// convertStringValue converts a raw string value to the expected data type
func convertStringValue(raw string, dataType string) (interface{}, error) {
	switch strings.ToUpper(dataType) {
	case "BOOL":
		lower := strings.ToLower(raw)
		return lower == "true" || lower == "1" || lower == "on" || lower == "yes", nil

	case "INT", "UINT", "DINT":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse '%s' as number: %w", raw, err)
		}
		return f, nil

	case "REAL":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse '%s' as float: %w", raw, err)
		}
		return f, nil

	case "STRING":
		return raw, nil

	default:
		// Try as number, fallback to string
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f, nil
		}
		return raw, nil
	}
}

// handleReloadCommand handles reload commands from the system
func (d *Driver) handleReloadCommand(topic string, payload []byte) {
	log.Printf("[DRIVER-MQTT] Reload command received: %s", string(payload))
	select {
	case d.reloadChan <- struct{}{}:
	default:
	}
}

// handleWriteCommand handles write commands for MQTT-based PLCs
// For MQTT PLCs, "writing" means publishing a value to the PLC's command topic
func (d *Driver) handleWriteCommand(topic string, payload []byte) {
	log.Printf("[DRIVER-MQTT] Received write command: %s", string(payload))

	var cmd WriteCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("[DRIVER-MQTT] Failed to unmarshal write command: %v", err)
		d.publishWriteResult(0, false, fmt.Sprintf("Invalid command: %v", err), nil)
		return
	}

	// The "code" field in MQTT driver is the PLC's topic to publish to
	// For writing, we publish to a write topic (convention: {source_topic}/set or cmd/{source_topic})
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		d.publishWriteResult(cmd.TagID, false, "Driver not configured", nil)
		return
	}

	// Find the tag mapping
	var targetMapping *TagMapping
	for i, m := range cfg.TagMappings {
		if m.Tag.ID == cmd.TagID {
			targetMapping = &cfg.TagMappings[i]
			break
		}
	}

	if targetMapping == nil {
		d.publishWriteResult(cmd.TagID, false, fmt.Sprintf("Tag %d not found in config", cmd.TagID), nil)
		return
	}

	// Build the write topic: convention is {source_topic}/set
	// Many MQTT-enabled PLCs use this convention (e.g. Wago, Tasmota)
	writeTopic := targetMapping.SourceTopic + "/set"

	// If connection_config has a write_topic_suffix override, use that
	if cfg.Gateway.ConnectionConfig != nil {
		if suffix, ok := cfg.Gateway.ConnectionConfig["write_topic_suffix"].(string); ok && suffix != "" {
			writeTopic = targetMapping.SourceTopic + suffix
		}
	}

	// Format the write value
	writePayload := formatWriteValue(cmd.Value, cmd.DataType)

	// Publish the write command to the PLC's write topic
	if err := d.mqttClient.PublishWithQoS(writeTopic, writePayload, 1, false); err != nil {
		log.Printf("[DRIVER-MQTT] Write failed to %s: %v", writeTopic, err)
		d.publishWriteResult(cmd.TagID, false, fmt.Sprintf("Publish failed: %v", err), nil)
		return
	}

	log.Printf("[DRIVER-MQTT] Write sent to %s: %s", writeTopic, writePayload)
	d.publishWriteResult(cmd.TagID, true, "Write command sent to PLC topic", cmd.Value)
}

// formatWriteValue converts the value to a string suitable for MQTT publishing
func formatWriteValue(value interface{}, dataType string) string {
	switch strings.ToUpper(dataType) {
	case "BOOL":
		switch v := value.(type) {
		case bool:
			if v {
				return "true"
			}
			return "false"
		case float64:
			if v != 0 {
				return "true"
			}
			return "false"
		default:
			return fmt.Sprintf("%v", value)
		}
	default:
		return fmt.Sprintf("%v", value)
	}
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

	payload, err := json.Marshal(result)
	if err != nil {
		log.Printf("[DRIVER-MQTT] Failed to marshal write result: %v", err)
		return
	}

	resultTopic := fmt.Sprintf("cmd/write/result/%d", d.gatewayID)
	d.mqttClient.PublishWithQoS(resultTopic, string(payload), 1, false)

	status := "✓"
	if !success {
		status = "✗"
	}
	log.Printf("[DRIVER-MQTT] Write result: tag=%d %s %s", tagID, status, message)
}

// handleLegacyWriteCommand handles write commands in legacy MQTT format
// Topic format: cmd/{org}/{site}/{area}/{gateway}/{alias}
// Payload: {"value": <value>} or just <value>
func (d *Driver) handleLegacyWriteCommand(topic string, payload []byte) {
	log.Printf("[DRIVER-MQTT] Received legacy write command: %s", topic)

	// Extract alias from topic
	parts := strings.Split(topic, "/")
	if len(parts) < 6 {
		log.Printf("[DRIVER-MQTT] Invalid legacy write topic format: %s", topic)
		return
	}
	alias := parts[5]

	// Find tag by alias
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER-MQTT] No config loaded, cannot process write")
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
		log.Printf("[DRIVER-MQTT] Tag with alias '%s' not found", alias)
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
			log.Printf("[DRIVER-MQTT] Failed to parse value from payload: %v", err)
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
	log.Printf("[DRIVER-MQTT] Received Sparkplug B DCMD: %s", topic)

	// Parse Sparkplug B topic
	_, err := sparkplug.ParseTopic(topic)
	if err != nil {
		log.Printf("[DRIVER-MQTT] Invalid Sparkplug DCMD topic: %v", err)
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
		log.Printf("[DRIVER-MQTT] Failed to parse Sparkplug DCMD payload: %v", err)
		return
	}

	// Find config
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER-MQTT] No config loaded, cannot process DCMD")
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
			log.Printf("[DRIVER-MQTT] DCMD: Tag '%s' not found", metric.Name)
			continue
		}

		// Create WriteCommand and process
		writeCmd := WriteCommand{
			TagID:    targetTag.ID,
			Code:     targetTag.Code,
			Value:    metric.Value,
			DataType: targetTag.DataType,
		}

		log.Printf("[DRIVER-MQTT] DCMD: Writing to tag %d (%s) = %v", targetTag.ID, targetTag.Alias, metric.Value)

		cmdBytes, _ := json.Marshal(writeCmd)
		d.handleWriteCommand(fmt.Sprintf("cmd/write/%d", d.gatewayID), cmdBytes)
	}
}

// run is the main loop that handles config reloads and periodic health checks
func (d *Driver) run() {
	healthTicker := time.NewTicker(30 * time.Second)
	refreshTicker := time.NewTicker(5 * time.Minute)

	log.Println("[DRIVER-MQTT] Main loop started (event-driven, no polling)")

	for {
		select {
		case <-d.stopChan:
			healthTicker.Stop()
			refreshTicker.Stop()
			return

		case <-d.reloadChan:
			log.Println("[DRIVER-MQTT] Reloading config...")
			if err := d.loadConfig(); err != nil {
				log.Printf("[DRIVER-MQTT] Config reload failed: %v", err)
			} else {
				// Re-subscribe to source topics (handles additions/removals)
				d.subscribeToSourceTopics()
				log.Println("[DRIVER-MQTT] Config reloaded and subscriptions updated")
			}

		case <-refreshTicker.C:
			log.Println("[DRIVER-MQTT] Periodic config refresh...")
			if err := d.loadConfig(); err == nil {
				d.subscribeToSourceTopics()
			}

		case <-healthTicker.C:
			// Periodic health ping
			d.configMu.RLock()
			connected := d.isConnected
			d.configMu.RUnlock()

			if connected {
				healthTopic := fmt.Sprintf("sys/health/%d", d.gatewayID)
				d.mqttClient.PublishWithQoS(healthTopic, "online", 1, true)
			}

			// Check for source topic timeouts
			d.checkSourceTimeouts()
		}
	}
}

// setConnectionState updates the connection state and publishes health status
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
	d.mqttClient.PublishWithQoS(topic, status, 1, true)
	log.Printf("[DRIVER-MQTT] Health status changed to: %s", status)
}

// checkSourceTimeouts verifies if any tags have stopped receiving data
func (d *Driver) checkSourceTimeouts() {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil || !cfg.Gateway.Enabled {
		return
	}

	// Use 3x scan rate or 60s minimum as timeout
	timeoutMs := cfg.Gateway.ScanRateMs * 3
	if timeoutMs < 60000 {
		timeoutMs = 60000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	now := time.Now()
	timestamp := now.UnixMilli()

	d.msgTimeMu.Lock()
	defer d.msgTimeMu.Unlock()

	for _, tag := range cfg.Tags {
		lastTime, exists := d.lastMessageTime[tag.ID]
		// If we haven't heard from the tag for longer than the timeout, mark as connection lost
		if exists && now.Sub(lastTime) > timeout {
			if !d.connectionLost[tag.ID] {
				log.Printf("[DRIVER-MQTT] Source timeout for tag %s (ID:%d). No data for %v", tag.Alias, tag.ID, timeout)
				d.connectionLost[tag.ID] = true
				// Publish with BAD quality
				d.publishDual(tag.ID, tag.Alias, 0, tag.DataType, 2, timestamp)
			}
		}
	}
}

// Utility functions

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
