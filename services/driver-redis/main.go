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
	"github.com/ralph/industrial-edge-middleware/internal/settings"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
	"github.com/redis/go-redis/v9"
)

// maxPollFailures is the number of consecutive poll failures after which the
// driver reports health "error" and publishes BAD quality for all tags.
const maxPollFailures = 3

// TagPayload represents the MQTT message payload for tag values (legacy format)
type TagPayload struct {
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// GatewayConfig holds the loaded gateway configuration
type GatewayConfig struct {
	Gateway models.Gateway
	Tags    []models.Tag
	OrgName string
	Site    string
	Area    string
	OrgID   int
}

// Driver manages the Redis driver lifecycle
type Driver struct {
	gatewayID   int
	database    *sql.DB
	mqttClient  *mqtt.Client
	redisClient *redis.Client
	config      *GatewayConfig
	configMu    sync.RWMutex
	stopChan    chan struct{}
	reloadChan  chan struct{}

	// Report by Exception
	previousValues map[int]interface{}
	prevValuesMu   sync.RWMutex

	// Connection state tracking for DBIRTH/DDEATH
	isConnected  bool
	wasConnected bool
	connectionMu sync.RWMutex

	// Consecutive poll failure tracking (Redis outage detection)
	pollFailMu   sync.Mutex
	pollFailures int
	badPublished bool // BAD quality already published for this outage

	// Sparkplug B support
	sparkplugClient *sparkplug.SparkplugClient
	dualPublisher   *sparkplug.DualPublisher
	sparkplugMu     sync.RWMutex

	// Settings manager for publish mode and RBE
	settingsManager *settings.Manager

	// Alarm Manager
	alarmManager *alarms.Manager
}

func main() {
	log.Println("[DRIVER-REDIS] Starting...")

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
		Host:     getEnv("DB_HOST", "postgres"),
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
	log.Println("[DRIVER-REDIS] Connected to PostgreSQL")

	// Connect to MQTT
	mqttCfg := mqtt.Config{
		Host:          getEnv("MQTT_HOST", "localhost"),
		Port:          getEnvInt("MQTT_PORT", 1883),
		ClientID:      fmt.Sprintf("driver-redis-%d", gatewayID),
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
	log.Println("[DRIVER-REDIS] Connected to MQTT broker")

	// NOTE: Health status will be published by setConnectionState() when Redis connection is established
	// This ensures the status reflects actual Redis connectivity, not just driver startup

	// Initialize settings manager
	settingsManager := settings.NewManager(database)

	driver := &Driver{
		gatewayID:       gatewayID,
		database:        database,
		mqttClient:      mqttClient,
		stopChan:        make(chan struct{}),
		reloadChan:      make(chan struct{}, 1),
		previousValues:  make(map[int]interface{}),
		settingsManager: settingsManager,
		wasConnected:    false,
		isConnected:     false,
	}

	if err := driver.settingsManager.Load(); err != nil {
		log.Printf("[DRIVER-REDIS] Warning: Failed to load settings: %v", err)
	}

	if err := driver.loadConfig(); err != nil {
		log.Fatalf("Failed to load gateway configuration: %v", err)
	}

	// Initialize Sparkplug B client if enabled
	if getEnv("SPARKPLUG_ENABLED", "true") == "true" {
		driver.initSparkplugClient()
	}

	// Subscribe to reload command
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	if err := mqttClient.Subscribe(reloadTopic, driver.handleReloadCommand); err != nil {
		log.Fatalf("Failed to subscribe to reload topic: %v", err)
	}

	// Initialize Alarm Manager
	driver.alarmManager = alarms.NewManager(database, mqttClient, gatewayID)

	driver.alarmManager.OnAlarmEvent = func(eventID int, tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
		// Publish on sys/alarms: this is the ONLY route to core-api's
		// handleAlarmEvent and therefore to the notification dispatcher.
		//
		// The previous publishDual(tagID, alias+"_Alarm", status == "ACTIVE", ...)
		// was dropped on purpose: publishDual emits a data/ payload keyed by
		// tag_id, so core-api's handleDataUpdate overwrote realtime:{tagID} and
		// tag_shadow:{tagID} with true/false — the measured value was replaced
		// by the alarm boolean on the HMI until the next poll. The alarm state
		// now travels on its own topic, where it cannot be mistaken for it.
		driver.publishAlarmEvent(eventID, tagID, alias, def, val, status)
	}

	go driver.alarmManager.StartTicker(context.Background())

	go driver.run()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[DRIVER-REDIS] Shutting down...")

	// Publish DDEATH before shutting down
	driver.publishDDEATH()

	close(driver.stopChan)

	if driver.redisClient != nil {
		if err := driver.redisClient.Close(); err != nil {
			log.Printf("[DRIVER-REDIS] Error closing Redis client: %v", err)
		}
	}
}

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

	if gateway.DriverType != "REDIS" && gateway.DriverType != "GENERIC" {
		log.Printf("[DRIVER-REDIS] Warning: Driver type is %s, expected REDIS", gateway.DriverType)
	}

	// Load tags
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
		OrgName: slugify(orgName),
		Site:    slugify(siteName),
		Area:    slugify(areaName),
		OrgID:   orgID,
	}

	// Reconnect/Update Redis connection if needed
	if d.redisClient != nil {
		_ = d.redisClient.Close()
	}
	if err := d.connectRedis(gateway.ConnectionConfig); err != nil {
		log.Printf("[DRIVER-REDIS] Error connecting to Redis: %v", err)
		d.setConnectionState(false)
	} else {
		d.setConnectionState(true)
	}

	log.Printf("[DRIVER-REDIS] Loaded config: %d tags", len(tags))
	return nil
}

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
		MQTTClientID: fmt.Sprintf("sparkplug-redis-%d", d.gatewayID),
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

	log.Printf("[DRIVER-REDIS] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

func (d *Driver) connectRedis(config map[string]interface{}) error {
	host, _ := config["host"].(string)
	if host == "" {
		host = "localhost"
	}
	portVal := config["port"]
	var port int
	switch v := portVal.(type) {
	case float64:
		port = int(v)
	case string:
		port, _ = strconv.Atoi(v)
	default:
		port = 6379
	}

	dbIdxVal := config["db"]
	var dbIdx int
	switch v := dbIdxVal.(type) {
	case float64:
		dbIdx = int(v)
	case int:
		dbIdx = v
	default:
		dbIdx = 0
	}

	password, _ := config["password"].(string)

	addr := fmt.Sprintf("%s:%d", host, port)
	d.redisClient = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       dbIdx,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return d.redisClient.Ping(ctx).Err()
}

func (d *Driver) setConnectionState(connected bool) {
	d.connectionMu.Lock()
	wasConnected := d.isConnected
	d.wasConnected = wasConnected
	d.isConnected = connected
	d.connectionMu.Unlock()

	// Handle DBIRTH/DDEATH based on connection state change
	d.handleConnectionStateChange(connected, wasConnected)
}

func (d *Driver) handleConnectionStateChange(connected, wasConnected bool) {
	d.sparkplugMu.RLock()
	spClient := d.sparkplugClient
	cfg := d.config
	d.sparkplugMu.RUnlock()

	if spClient == nil || !spClient.IsConnected() || cfg == nil {
		return
	}

	if connected && !wasConnected {
		// Coming online - send DBIRTH with all current tag values
		log.Printf("[DRIVER-REDIS] Sending DBIRTH (device coming online)")
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
			log.Printf("[DRIVER-REDIS] Failed to publish DBIRTH: %v", err)
		}
	} else if !connected && wasConnected {
		// Going offline - send DDEATH
		log.Printf("[DRIVER-REDIS] Sending DDEATH (device going offline)")
		if err := spClient.PublishDDEATH(slugify(cfg.Gateway.Name)); err != nil {
			log.Printf("[DRIVER-REDIS] Failed to publish DDEATH: %v", err)
		}
	}
}

func (d *Driver) publishDDEATH() {
	d.sparkplugMu.RLock()
	spClient := d.sparkplugClient
	cfg := d.config
	d.sparkplugMu.RUnlock()

	if spClient == nil || !spClient.IsConnected() || cfg == nil {
		return
	}

	log.Printf("[DRIVER-REDIS] Sending DDEATH (shutdown)")
	if err := spClient.PublishDDEATH(slugify(cfg.Gateway.Name)); err != nil {
		log.Printf("[DRIVER-REDIS] Failed to publish DDEATH: %v", err)
	}
}

func (d *Driver) run() {
	d.configMu.RLock()
	scanRate := scanRateDuration(d.config.Gateway.ScanRateMs)
	d.configMu.RUnlock()

	ticker := time.NewTicker(scanRate)
	defer ticker.Stop()

	healthTicker := time.NewTicker(30 * time.Second)
	defer healthTicker.Stop()

	// Initial connection check
	d.checkConnection()

	log.Printf("[DRIVER-REDIS] Starting polling loop with rate: %v", scanRate)

	for {
		select {
		case <-d.stopChan:
			return
		case <-d.reloadChan:
			d.loadConfig()
			if getEnv("SPARKPLUG_ENABLED", "true") == "true" {
				d.initSparkplugClient()
			}
			d.configMu.RLock()
			ticker.Reset(scanRateDuration(d.config.Gateway.ScanRateMs))
			d.configMu.RUnlock()
		case <-healthTicker.C:
			status := "online"
			d.pollFailMu.Lock()
			if d.pollFailures >= maxPollFailures {
				status = "error"
			}
			d.pollFailMu.Unlock()
			d.mqttClient.PublishWithQoS(fmt.Sprintf("sys/health/%d", d.gatewayID), status, 1, true)
			d.checkConnection()
		case <-ticker.C:
			d.poll()
		}
	}
}

func (d *Driver) checkConnection() {
	if d.redisClient == nil {
		d.setConnectionState(false)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := d.redisClient.Ping(ctx).Err()
	d.setConnectionState(err == nil)
}

// poll reads all tags from Redis using MGET for efficiency
func (d *Driver) poll() {
	d.configMu.RLock()
	config := d.config
	d.configMu.RUnlock()

	if config == nil || !config.Gateway.Enabled || d.redisClient == nil {
		return
	}

	if len(config.Tags) == 0 {
		return
	}

	// Collect keys
	var keys []string
	for _, tag := range config.Tags {
		keys = append(keys, tag.Code)
	}

	// MGET optimization
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	values, err := d.redisClient.MGet(ctx, keys...).Result()
	if err != nil {
		log.Printf("[DRIVER-REDIS] Error executing MGET: %v", err)
		d.setConnectionState(false)
		d.recordPollFailure(config)
		return
	}

	// Mark as connected if we got values
	d.setConnectionState(true)
	d.recordPollSuccess()

	timestamp := time.Now().UnixMilli()

	for i, val := range values {
		tag := config.Tags[i]

		// Parse value
		parsedVal := val

		if val != nil {
			strVal := fmt.Sprintf("%v", val)
			switch tag.DataType {
			case "INT", "DINT", "UINT":
				if v, err := strconv.Atoi(strVal); err == nil {
					parsedVal = v
				} else if v, err := strconv.ParseFloat(strVal, 64); err == nil {
					parsedVal = int(v)
				}
			case "REAL", "FLOAT":
				if v, err := strconv.ParseFloat(strVal, 64); err == nil {
					parsedVal = v
				}
			case "BOOL":
				if v, err := strconv.ParseBool(strVal); err == nil {
					parsedVal = v
				} else if strVal == "1" {
					parsedVal = true
				} else if strVal == "0" {
					parsedVal = false
				}
			}
		}

		quality := 192 // GOOD (for alarm manager - OPC UA standard)
		if val == nil {
			quality = 0 // BAD
		}

		// Evaluate alarms via AlarmManager (uses OPC UA standard: 192=GOOD, 0=BAD)
		if d.alarmManager != nil {
			d.alarmManager.EvaluateTag(tag.ID, tag.Alias, parsedVal, quality)
		}

		// Convert quality to internal standard for publish (0=GOOD, >0=BAD)
		publishQuality := 0
		if val == nil {
			publishQuality = 2 // BAD (0=GOOD,1=UNCERTAIN,2=BAD), consistent with other drivers
		}

		// Check if we should publish (RBE logic)
		if d.shouldPublish(tag.ID, parsedVal, publishQuality) {
			d.publishTagValue(tag, parsedVal, timestamp, publishQuality)
			d.updatePreviousValue(tag.ID, parsedVal)
		}
	}
}

// recordPollFailure counts consecutive poll failures. Once the threshold is
// reached, BAD quality (q=2) is published once per tag on the legacy path so
// downstream consumers don't keep trusting stale GOOD values during a Redis
// outage. Health "error" is reported by the health ticker while in this state.
func (d *Driver) recordPollFailure(config *GatewayConfig) {
	d.pollFailMu.Lock()
	d.pollFailures++
	publishBad := d.pollFailures >= maxPollFailures && !d.badPublished
	if publishBad {
		d.badPublished = true
	}
	failures := d.pollFailures
	d.pollFailMu.Unlock()

	if !publishBad {
		return
	}

	log.Printf("[DRIVER-REDIS] %d consecutive poll failures — publishing BAD quality for %d tags", failures, len(config.Tags))
	timestamp := time.Now().UnixMilli()
	for _, tag := range config.Tags {
		d.prevValuesMu.Lock()
		lastValue := d.previousValues[tag.ID]
		// Clear the cached value so the first successful poll republishes
		// GOOD quality even if the value is unchanged.
		delete(d.previousValues, tag.ID)
		d.prevValuesMu.Unlock()

		// Route through publishDual so publish_mode is honored: in
		// sparkplug_only deployments the legacy path reaches nobody and
		// Sparkplug consumers would keep trusting the stale GOOD value.
		d.publishDual(tag.ID, tag.Alias, lastValue, tag.DataType, 2, timestamp)
	}
}

// recordPollSuccess resets the consecutive failure counter.
func (d *Driver) recordPollSuccess() {
	d.pollFailMu.Lock()
	d.pollFailures = 0
	d.badPublished = false
	d.pollFailMu.Unlock()
}

func (d *Driver) shouldPublish(tagID int, newValue interface{}, quality int) bool {
	if d.settingsManager == nil {
		return d.hasValueChanged(tagID, newValue)
	}

	d.prevValuesMu.RLock()
	oldValue, _ := d.previousValues[tagID]
	d.prevValuesMu.RUnlock()

	// For Redis driver, we don't track quality separately, so use 0 for old quality
	return d.settingsManager.ShouldPublish(tagID, newValue, oldValue, quality, 0)
}

func (d *Driver) handleReloadCommand(topic string, payload []byte) {
	if d.alarmManager != nil {
		d.alarmManager.LoadDefinitions()
	}
	select {
	case d.reloadChan <- struct{}{}:
	default:
	}
}

func (d *Driver) publishTagValue(tag models.Tag, value interface{}, timestamp int64, quality int) {
	d.publishDual(tag.ID, tag.Alias, value, tag.DataType, quality, timestamp)
}

func (d *Driver) publishDual(tagID int, alias string, value interface{}, dataType string, quality int, timestamp int64) {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	publishMode := models.PublishModeDual
	if d.settingsManager != nil {
		publishMode = d.settingsManager.Get().PublishMode
	}

	d.sparkplugMu.RLock()
	dualPublisher := d.dualPublisher
	sparkplugClient := d.sparkplugClient
	d.sparkplugMu.RUnlock()

	switch publishMode {
	case models.PublishModeLegacyOnly:
		d.publishLegacy(tagID, alias, value, timestamp, quality, cfg)

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
				log.Printf("[DRIVER-REDIS] Sparkplug publish error for %s: %v", alias, err)
			}
		}

	default: // PublishModeDual
		if dualPublisher != nil {
			if err := dualPublisher.Publish(tagID, alias, value, dataType, quality, timestamp, d.mqttClient); err != nil {
				log.Printf("[DRIVER-REDIS] Dual publish error for %s: %v", alias, err)
			}
		} else {
			d.publishLegacy(tagID, alias, value, timestamp, quality, cfg)
		}
	}
}

func (d *Driver) publishLegacy(tagID int, alias string, value interface{}, timestamp int64, quality int, cfg *GatewayConfig) {
	topic := fmt.Sprintf("data/%s/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
	type TagPayload struct {
		TagID     int         `json:"tag_id"`
		OrgID     int         `json:"org_id"`
		Value     interface{} `json:"v"`
		Timestamp int64       `json:"ts"`
		Quality   int         `json:"q"`
	}
	payload, _ := json.Marshal(TagPayload{tagID, cfg.OrgID, value, timestamp, quality})
	d.mqttClient.PublishWithQoS(topic, string(payload), 1, false)
}

// AlarmEventPayload is the sys/alarms message consumed by core-api
// (handleAlarmEvent) and forwarded to the cloud by engine-historian.
// EventID is the alarm_events row internal/alarms already wrote through the
// driver's own DB connection: core-api uses it to skip its INSERT/UPDATE so
// one alarm never produces two rows.
type AlarmEventPayload struct {
	EventID        int     `json:"event_id"`
	TagID          int     `json:"tag_id"`
	DefinitionID   int     `json:"definition_id"`
	Status         string  `json:"status"`
	AlarmType      string  `json:"alarm_type"`
	Severity       string  `json:"severity"`
	Message        string  `json:"message"`
	ValueAtTrigger float64 `json:"value_at_trigger"`
	Threshold      float64 `json:"threshold"`
	TagAlias       string  `json:"tag_alias"`
	Timestamp      int64   `json:"timestamp"`
}

// publishAlarmEvent publishes an alarm state change on
// sys/alarms/{org_id}/{site}/{area}/{gateway}/{tag_alias}, using the same
// slugified path components as the data/ topics (see publishLegacy).
// Always legacy JSON, never Sparkplug: core-api only subscribes to sys/alarms.
func (d *Driver) publishAlarmEvent(eventID int, tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	var threshold float64
	if def.Threshold != nil {
		threshold = *def.Threshold
	}

	topic := fmt.Sprintf("sys/alarms/%d/%s/%s/%s/%s",
		cfg.OrgID, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
	payload, err := json.Marshal(AlarmEventPayload{
		EventID:        eventID,
		TagID:          tagID,
		DefinitionID:   def.ID,
		Status:         status,
		AlarmType:      def.AlarmType,
		Severity:       def.Severity,
		Message:        def.Message,
		ValueAtTrigger: val,
		Threshold:      threshold,
		TagAlias:       alias,
		Timestamp:      time.Now().UnixMilli(),
	})
	if err != nil {
		log.Printf("[DRIVER-REDIS] Failed to encode alarm event for %s: %v", alias, err)
		return
	}
	// QoS 1: an alarm that nobody is paged for is the whole point of this
	// topic, so it must survive a broker hiccup. Not retained: it's an event.
	if err := d.mqttClient.PublishWithQoS(topic, string(payload), 1, false); err != nil {
		log.Printf("[DRIVER-REDIS] Failed to publish alarm event for %s on %s: %v", alias, topic, err)
	}
}

func (d *Driver) hasValueChanged(tagID int, newValue interface{}) bool {
	d.prevValuesMu.RLock()
	prev, exists := d.previousValues[tagID]
	d.prevValuesMu.RUnlock()

	if !exists {
		return true
	}
	return prev != newValue
}

func (d *Driver) updatePreviousValue(tagID int, value interface{}) {
	d.prevValuesMu.Lock()
	d.previousValues[tagID] = value
	d.prevValuesMu.Unlock()
}

// scanRateDuration converts scan_rate_ms to a ticker duration with a
// defensive floor: values below 100ms (including 0/negative from the DB)
// would panic time.NewTicker/Reset or hammer Redis, so fall back to 1s.
func scanRateDuration(ms int) time.Duration {
	if ms < 100 {
		log.Printf("[DRIVER-REDIS] Warning: invalid scan_rate_ms %d (< 100), using 1000ms", ms)
		return 1000 * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}
