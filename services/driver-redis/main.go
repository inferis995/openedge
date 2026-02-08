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

	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/redis/go-redis/v9"
)

// TagPayload represents the MQTT message payload for tag values
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
}

func main() {
	log.Println("Starting driver-redis...")

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
	log.Println("Connected to PostgreSQL")

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
	log.Println("Connected to MQTT broker")

	// Publish online status
	if err := mqttClient.PublishWithQoS(fmt.Sprintf("sys/health/%d", gatewayID), "online", 1, true); err != nil {
		log.Printf("Failed to publish online status: %v", err)
	}

	driver := &Driver{
		gatewayID:      gatewayID,
		database:       database,
		mqttClient:     mqttClient,
		stopChan:       make(chan struct{}),
		reloadChan:     make(chan struct{}, 1),
		previousValues: make(map[int]interface{}),
	}

	if err := driver.loadConfig(); err != nil {
		log.Fatalf("Failed to load gateway configuration: %v", err)
	}

	// Subscribe to reload command
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	if err := mqttClient.Subscribe(reloadTopic, driver.handleReloadCommand); err != nil {
		log.Fatalf("Failed to subscribe to reload topic: %v", err)
	}

	go driver.run()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down driver-redis...")
	close(driver.stopChan)

	if driver.redisClient != nil {
		if err := driver.redisClient.Close(); err != nil {
			log.Printf("Error closing Redis client: %v", err)
		}
	}
}

func (d *Driver) loadConfig() error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	// Load gateway
	query := `
		SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled,
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
		&gateway.ID,
		&gateway.AreaID,
		&gateway.Name,
		&gateway.DriverType,
		&connConfigBytes,
		&gateway.ScanRateMs,
		&gateway.Enabled,
		&orgName,
		&siteName,
		&areaName,
	)
	if err != nil {
		return fmt.Errorf("failed to load gateway: %w", err)
	}

	// Parse connection config (expecting host, port, etc.)
	// We'll use a generic map for flexibility
	if err := json.Unmarshal(connConfigBytes, &gateway.ConnectionConfig); err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}

	if gateway.DriverType != "REDIS" && gateway.DriverType != "GENERIC" {
		log.Printf("Warning: Driver type is %s, expected REDIS", gateway.DriverType)
	}

	// Load tags
	tagsQuery := `
		SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband,
		       alarm_enabled, alarm_threshold, alarm_operator, alarm_priority
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
			&tag.Code, // This will be the Redis Key
			&tag.Alias,
			&tag.DataType,
			&tag.Historize,
			&tag.HistorizeDeadband,
			&tag.AlarmEnabled,
			&tag.AlarmThreshold,
			&tag.AlarmOperator,
			&tag.AlarmPriority,
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
	}

	// Reconnect/Update Redis connection if needed
	if d.redisClient != nil {
		_ = d.redisClient.Close()
	}
	if err := d.connectRedis(gateway.ConnectionConfig); err != nil {
		log.Printf("Error connecting to Redis: %v", err)
	}

	log.Printf("Loaded generic/redis config: %d tags", len(tags))
	return nil
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

func (d *Driver) run() {
	d.configMu.RLock()
	scanRate := time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond
	d.configMu.RUnlock()

	ticker := time.NewTicker(scanRate)
	defer ticker.Stop()

	healthTicker := time.NewTicker(30 * time.Second)
	defer healthTicker.Stop()

	log.Printf("Starting polling loop with rate: %v", scanRate)

	for {
		select {
		case <-d.stopChan:
			return
		case <-d.reloadChan:
			d.loadConfig()
			d.configMu.RLock()
			ticker.Reset(time.Duration(d.config.Gateway.ScanRateMs) * time.Millisecond)
			d.configMu.RUnlock()
		case <-healthTicker.C:
			// Heartbeat
			d.mqttClient.PublishWithQoS(fmt.Sprintf("sys/health/%d", d.gatewayID), "online", 1, true)
		case <-ticker.C:
			d.poll()
		}
	}
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
		log.Printf("Error executing MGET: %v", err)
		return
	}

	timestamp := time.Now().UnixMilli()
	topicPrefix := fmt.Sprintf("data/%s/%s/%s/%s", config.OrgName, config.Site, config.Area, slugify(config.Gateway.Name))

	for i, val := range values {
		tag := config.Tags[i]

		// Parse value
		parsedVal := val // Default to raw interface{} (string or nil)

		// Convert if necessary based on DataType, though Redis returns strings/nil
		if val != nil {
			strVal := fmt.Sprintf("%v", val)
			switch tag.DataType {
			case "INT", "DINT", "UINT":
				if v, err := strconv.Atoi(strVal); err == nil {
					parsedVal = v
				} else if v, err := strconv.ParseFloat(strVal, 64); err == nil {
					parsedVal = int(v) // Handle "123.0"
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

		quality := 0
		if val == nil {
			quality = 1 // Bad quality if key missing
		}

		if d.hasValueChanged(tag.ID, parsedVal) {
			d.publishTagValue(topicPrefix, tag, parsedVal, timestamp, quality)
			d.updatePreviousValue(tag.ID, parsedVal)
		}
	}
}

func (d *Driver) handleReloadCommand(topic string, payload []byte) {
	// ... (Same reuse as Modbus mostly)
	select {
	case d.reloadChan <- struct{}{}:
	default:
	}
}

func (d *Driver) publishTagValue(topicPrefix string, tag models.Tag, value interface{}, timestamp int64, quality int) {
	topic := fmt.Sprintf("%s/%s", topicPrefix, slugify(tag.Alias))
	payload := TagPayload{Value: value, Timestamp: timestamp, Quality: quality}

	bytes, _ := json.Marshal(payload)
	d.mqttClient.PublishWithQoS(topic, string(bytes), 1, false)
}

func (d *Driver) hasValueChanged(tagID int, newValue interface{}) bool {
	d.prevValuesMu.RLock()
	prev, exists := d.previousValues[tagID]
	d.prevValuesMu.RUnlock()

	if !exists {
		return true
	}
	return prev != newValue // Simple comparison for now
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
