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
	scanRate := time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond
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
			ticker.Reset(time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond)
			d.configMu.RUnlock()
		case <-healthTicker.C:
			d.mqttClient.PublishWithQoS(fmt.Sprintf("sys/health/%d", d.gatewayID), "online", 1, true)
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
		return
	}

	// Mark as connected if we got values
	d.setConnectionState(true)

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

		quality := 192 // GOOD
		if val == nil {
			quality = 0 // BAD
		}

		// Evaluate alarms via AlarmManager
		if d.alarmManager != nil {
			d.alarmManager.EvaluateTag(tag.ID, tag.Alias, parsedVal, quality)
		}

		// Check if we should publish (RBE logic)
		if d.shouldPublish(tag.ID, parsedVal, quality) {
			d.publishTagValue(tag, parsedVal, timestamp, quality)
			d.updatePreviousValue(tag.ID, parsedVal)
		}
	}
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
