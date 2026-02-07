package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	_ "github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
)

const (
	mqttTopicData       = "data/#"
	mqttTopicHealth     = "sys/health/+"
	bufferFlushSize     = 1000
	bufferFlushInterval = 1 * time.Second
)

type HistorianService struct {
	mqttClient     *mqtt.Client
	redisClient    *redis.Client
	influxClient   influxdb2.Client
	influxWriteAPI api.WriteAPI
	influxOrg      string
	influxBucket   string
	db             *sql.DB
	wg             sync.WaitGroup
	shutdown       chan struct{}
	buffer         []*DataPoint
	bufferMutex    sync.Mutex
	flushTicker    *time.Ticker
}

// DataPoint represents a single data point received from MQTT
type DataPoint struct {
	Measurement string                 `influx:"_measurement"` // Will be set to "tag_data"
	Tags        map[string]string      `influx:"_,tag"`
	Fields      map[string]interface{} `influx:"_,field"`
	Timestamp   int64                  `influx:"_time"`
	Org         string
	Site        string
	Area        string
	Gateway     string
	Alias       string
}

// MQTTPayload represents the JSON payload from MQTT
type MQTTPayload struct {
	V  interface{} `json:"v"`  // Value (bool, float64, int)
	Ts int64       `json:"ts"` // Timestamp in milliseconds
	Q  int         `json:"q"`  // Quality (0 = good, 1 = bad)
}

// TagInfo holds tag configuration loaded from PostgreSQL
type TagInfo struct {
	ID                int
	OrganizationID    int
	Historize         bool
	HistorizeDeadband float64
}

// GatewayInfo holds gateway hierarchy info
type GatewayInfo struct {
	ID       int
	Name     string
	AreaID   int
	AreaName string
	SiteID   int
	SiteName string
	OrgID    int
	OrgName  string
}

// PreviousValue represents the previous value stored in Redis for deadband comparison
type PreviousValue struct {
	Value   interface{} `json:"v"`
	Quality int         `json:"q"`
}

// RealtimeValue represents the current value stored in Redis for real-time queries
type RealtimeValue struct {
	V  interface{} `json:"v"`  // Value
	Ts int64       `json:"ts"` // Timestamp in milliseconds
	Q  int         `json:"q"`  // Quality (0 = good, 1 = bad)
}

const (
	realtimeCacheTTL = 5184000 // 60 days in seconds
)

func main() {
	// Load configuration from environment variables
	mqttHost := getEnv("MQTT_HOST", "localhost")
	mqttPort, _ := strconv.Atoi(getEnv("MQTT_PORT", "1883"))
	mqttClientID := getEnv("MQTT_CLIENT_ID", "engine-historian")

	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort, _ := strconv.Atoi(getEnv("REDIS_PORT", "6379"))
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))

	// PostgreSQL configuration
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "industrial_edge")

	influxURL := getEnv("INFLUX_URL", "http://localhost:8086")
	influxToken := getEnv("INFLUX_TOKEN", "")
	influxOrg := getEnv("INFLUX_ORG", "industrial")
	influxBucket := getEnv("INFLUX_BUCKET", "historian")

	// Validate required configuration
	if influxToken == "" {
		log.Fatal("INFLUX_TOKEN environment variable is required")
	}

	// Connect to PostgreSQL
	dbConnStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)
	database, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	if err := database.Ping(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("Connected to PostgreSQL")

	// Create MQTT client
	mqttClient := mqtt.NewClient(mqtt.Config{
		Host:     mqttHost,
		Port:     mqttPort,
		ClientID: mqttClientID,
	})

	// Connect to MQTT broker
	log.Printf("Connecting to MQTT broker at %s:%d...", mqttHost, mqttPort)
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", err)
	}
	log.Println("Connected to MQTT broker")

	// Create Redis client
	redisClient := redis.NewClient(redis.Config{
		Host:     redisHost,
		Port:     redisPort,
		Password: redisPassword,
		DB:       redisDB,
	})

	// Connect to Redis
	log.Printf("Connecting to Redis at %s:%d...", redisHost, redisPort)
	if err := redisClient.Connect(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")

	// Create InfluxDB client
	log.Printf("Connecting to InfluxDB at %s...", influxURL)
	influxClient := influxdb2.NewClientWithOptions(influxURL, influxToken,
		influxdb2.DefaultOptions().SetBatchSize(1000).SetFlushInterval(1000))
	influxWriteAPI := influxClient.WriteAPI(influxOrg, influxBucket)

	// Verify InfluxDB connection using proper context
	ctx := context.Background()
	_, err = influxClient.Health(ctx)
	if err != nil {
		log.Fatalf("Failed to connect to InfluxDB: %v", err)
	}
	log.Println("Connected to InfluxDB")

	// Create historian service
	service := &HistorianService{
		mqttClient:     mqttClient,
		redisClient:    redisClient,
		influxClient:   influxClient,
		influxWriteAPI: influxWriteAPI,
		influxOrg:      influxOrg,
		influxBucket:   influxBucket,
		db:             database,
		shutdown:       make(chan struct{}),
		buffer:         make([]*DataPoint, 0, bufferFlushSize),
		flushTicker:    time.NewTicker(bufferFlushInterval),
	}

	// Subscribe to data topics
	log.Printf("Subscribing to MQTT topic: %s", mqttTopicData)
	if err := mqttClient.Subscribe(mqttTopicData, service.handleDataMessage); err != nil {
		log.Fatalf("Failed to subscribe to MQTT topic: %v", err)
	}
	log.Println("Successfully subscribed to data topics")

	// Subscribe to health topics (gateway connection events)
	log.Printf("Subscribing to MQTT topic: %s", mqttTopicHealth)
	if err := mqttClient.Subscribe(mqttTopicHealth, service.handleHealthMessage); err != nil {
		log.Fatalf("Failed to subscribe to health topic: %v", err)
	}
	log.Println("Successfully subscribed to health topics")

	// Start InfluxDB write API error handling
	service.wg.Add(1)
	go service.handleInfluxErrors()

	// Start periodic buffer flush
	service.wg.Add(1)
	go service.periodicFlush()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	log.Println("Historian service running. Press Ctrl+C to shutdown...")

	<-sigChan
	log.Println("Shutdown signal received, stopping historian service...")

	// Graceful shutdown
	close(service.shutdown)
	service.flushTicker.Stop()
	mqttClient.Disconnect(1000)
	redisClient.Disconnect()
	influxClient.Close()
	service.wg.Wait()

	log.Println("Historian service stopped")
}

func (s *HistorianService) handleHealthMessage(topic string, payload []byte) {
	select {
	case <-s.shutdown:
		return
	default:
	}

	// Parse topic: sys/health/{gateway_id}
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		log.Printf("Invalid health topic format: %s", topic)
		return
	}

	gatewayIDStr := parts[2]
	status := string(payload) // "online" or "offline"

	log.Printf("[HISTORIAN] Received health event for Gateway %s: %s", gatewayIDStr, status)

	// Get gateway info to populate tags (Org, Site, etc.) for filtering
	gatewayID, _ := strconv.Atoi(gatewayIDStr)
	gwInfo, err := s.getGatewayInfo(gatewayID)

	orgName := "unknown"
	siteName := "unknown"
	areaName := "unknown"
	gwName := gatewayIDStr

	if err == nil && gwInfo != nil {
		orgName = gwInfo.OrgName
		siteName = gwInfo.SiteName
		areaName = gwInfo.AreaName
		gwName = gwInfo.Name
	} else {
		log.Printf("[HISTORIAN] Warning: Could not resolve info for Gateway ID %d: %v", gatewayID, err)
	}

	// Determine message based on status
	var message string
	if status == "online" {
		message = "Gateway connected"
	} else {
		message = "Gateway disconnected"
	}

	// Create event data point
	dp := &DataPoint{
		Measurement: "system_events",
		Tags: map[string]string{
			"type":         "connection",
			"gateway_id":   gatewayIDStr,
			"organization": orgName,
			"site":         siteName,
			"area":         areaName,
			"gateway":      gwName,
		},
		Fields: map[string]interface{}{
			"status":  status,
			"message": message,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	// Add to buffer (reusing the same buffer logic for efficiency)
	s.addToBuffer(dp)
}

func (s *HistorianService) handleDataMessage(topic string, payload []byte) {
	select {
	case <-s.shutdown:
		return
	default:
	}

	// Parse topic structure: data/{org}/{site}/{area}/{gateway}/{alias}
	parts := strings.Split(topic, "/")
	if len(parts) < 6 || parts[0] != "data" {
		log.Printf("Invalid topic format: %s", topic)
		return
	}

	org := parts[1]
	site := parts[2]
	area := parts[3]
	gateway := parts[4]
	alias := parts[5]

	// Unslugify: convert hyphens back to spaces for DB lookup (except alias, which can contain hyphens)
	org = strings.ReplaceAll(org, "-", " ")
	site = strings.ReplaceAll(site, "-", " ")
	area = strings.ReplaceAll(area, "-", " ")
	gateway = strings.ReplaceAll(gateway, "-", " ")
	// Note: alias is NOT unslugified because tag aliases can legitimately contain hyphens (e.g., "test-1")

	// Parse JSON payload: {"v": value, "ts": timestamp_ms, "q": quality}
	var mqttPayload MQTTPayload
	if err := json.Unmarshal(payload, &mqttPayload); err != nil {
		log.Printf("[HISTORIAN] Failed to parse MQTT payload from %s: %v", topic, err)
		return
	}

	log.Printf("[HISTORIAN] Received data for topic: %s, value: %v", topic, mqttPayload.V)

	// Get tag info (includes deadband configuration)
	tagInfo, err := s.getTagInfo(org, site, area, gateway, alias)
	if err != nil {
		log.Printf("[HISTORIAN] Tag lookup failed for alias '%s' in %s/%s/%s/%s: %v", alias, org, site, area, gateway, err)
		return
	}

	// Store current value in Redis for real-time queries (always store, even if not historized)
	s.storeRealtimeValue(tagInfo.ID, mqttPayload)

	// Broadast real-time update via Redis Pub/Sub
	s.broadcastRealtimeUpdate(tagInfo.OrganizationID, tagInfo.ID, mqttPayload)

	// Skip if historize is disabled
	if !tagInfo.Historize {
		log.Printf("[HISTORIAN] Tag %d has historize=false, skipping InfluxDB storage", tagInfo.ID)
		return
	}

	// Check deadband filter
	if !s.shouldStoreValue(tagInfo, mqttPayload.V, mqttPayload.Q) {
		// Value within deadband, skip storing
		log.Printf("[HISTORIAN] Tag %d value within deadband (%.2f), skipping storage",
			tagInfo.ID, tagInfo.HistorizeDeadband)
		return
	}

	// Store previous value in Redis for next deadband comparison
	s.storePreviousValue(tagInfo.ID, mqttPayload.V, mqttPayload.Q)

	log.Printf("[HISTORIAN] Storing data point for tag %d: value=%v, ts=%d",
		tagInfo.ID, mqttPayload.V, mqttPayload.Ts)

	// Create data point
	dp := &DataPoint{
		Measurement: "tag_data",
		Tags: map[string]string{
			"tag_id":       strconv.Itoa(tagInfo.ID),
			"organization": org,
			"site":         site,
			"area":         area,
			"gateway":      gateway,
			"alias":        alias,
		},
		Fields: map[string]interface{}{
			"value":   mqttPayload.V,
			"quality": mqttPayload.Q,
		},
		Timestamp: mqttPayload.Ts,
		Org:       org,
		Site:      site,
		Area:      area,
		Gateway:   gateway,
		Alias:     alias,
	}

	// Add to buffer
	s.addToBuffer(dp)
}

func (s *HistorianService) handleInfluxErrors() {
	defer s.wg.Done()

	errCh := s.influxWriteAPI.Errors()
	for {
		select {
		case <-s.shutdown:
			return
		case err, ok := <-errCh:
			if !ok {
				return
			}
			log.Printf("InfluxDB write error: %v", err)
		}
	}
}

// addToBuffer adds a data point to the buffer and triggers flush if needed
func (s *HistorianService) addToBuffer(dp *DataPoint) {
	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	s.buffer = append(s.buffer, dp)

	// Flush buffer if it has reached the configured size
	if len(s.buffer) >= bufferFlushSize {
		s.flushBuffer()
	}
}

// periodicFlush flushes the buffer at regular intervals
func (s *HistorianService) periodicFlush() {
	defer s.wg.Done()

	for {
		select {
		case <-s.shutdown:
			// Flush remaining buffer before shutdown
			s.bufferMutex.Lock()
			if len(s.buffer) > 0 {
				s.flushBufferUnsafe()
			}
			s.bufferMutex.Unlock()
			return
		case <-s.flushTicker.C:
			s.bufferMutex.Lock()
			if len(s.buffer) > 0 {
				s.flushBufferUnsafe()
			}
			s.bufferMutex.Unlock()
		}
	}
}

// flushBuffer flushes the buffer (assumes mutex is held)
func (s *HistorianService) flushBuffer() {
	if len(s.buffer) == 0 {
		return
	}

	log.Printf("Flushing %d data points to InfluxDB", len(s.buffer))
	s.flushBufferUnsafe()
}

// flushBufferUnsafe performs the actual flush without locking
func (s *HistorianService) flushBufferUnsafe() {
	for _, dp := range s.buffer {
		// Convert timestamp from milliseconds to nanoseconds for InfluxDB
		timestamp := time.Unix(0, dp.Timestamp*1000000)

		// Create InfluxDB point
		point := influxdb2.NewPoint(dp.Measurement, dp.Tags, dp.Fields, timestamp)
		s.influxWriteAPI.WritePoint(point)
	}

	// Force flush to InfluxDB
	s.influxWriteAPI.Flush()

	// Clear buffer
	s.buffer = s.buffer[:0]
}

// getTagInfo retrieves tag information from PostgreSQL, with Redis caching
func (s *HistorianService) getTagInfo(org, site, area, gateway, alias string) (*TagInfo, error) {
	// Trim whitespace from all parameters to handle cases with extra spaces
	org = strings.TrimSpace(org)
	site = strings.TrimSpace(site)
	area = strings.TrimSpace(area)
	gateway = strings.TrimSpace(gateway)
	alias = strings.TrimSpace(alias)

	// First try to get from Redis cache
	cacheKey := fmt.Sprintf("tag_info:%s:%s:%s:%s:%s", org, site, area, gateway, alias)
	cached, err := s.redisClient.Get(cacheKey)
	if err == nil && cached != "" {
		// Parse cached tag info
		var tagInfo TagInfo
		if err := json.Unmarshal([]byte(cached), &tagInfo); err == nil {
			return &tagInfo, nil
		}
	}

	// Not in cache, query from PostgreSQL
	query := `
		SELECT t.id, s.org_id, t.historize, t.historize_deadband
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE TRIM(LOWER(o.name)) = TRIM(LOWER($1))
		  AND TRIM(LOWER(s.name)) = TRIM(LOWER($2))
		  AND TRIM(LOWER(a.name)) = TRIM(LOWER($3))
		  AND TRIM(LOWER(g.name)) = TRIM(LOWER($4))
		  AND TRIM(LOWER(t.alias)) = TRIM(LOWER($5))
	`

	var tagInfo TagInfo
	err = s.db.QueryRow(query, org, site, area, gateway, alias).Scan(
		&tagInfo.ID,
		&tagInfo.OrganizationID,
		&tagInfo.Historize,
		&tagInfo.HistorizeDeadband,
	)
	if err != nil {
		log.Printf("[HISTORIAN] Tag lookup failed: org=%q site=%q area=%q gateway=%q alias=%q: %v",
			org, site, area, gateway, alias, err)
		return nil, fmt.Errorf("failed to query tag info: %w", err)
	}

	log.Printf("[HISTORIAN] Found tag: ID=%d, historize=%v, deadband=%.2f",
		tagInfo.ID, tagInfo.Historize, tagInfo.HistorizeDeadband)

	// Cache in Redis for 1 hour (3600 seconds)
	tagInfoJSON, _ := json.Marshal(tagInfo)
	s.redisClient.Set(cacheKey, string(tagInfoJSON), 3600)

	return &tagInfo, nil
}

// getGatewayInfo retrieves gateway hierarchy info from PostgreSQL, with Redis caching
func (s *HistorianService) getGatewayInfo(gatewayID int) (*GatewayInfo, error) {
	// Cache key
	cacheKey := fmt.Sprintf("gateway_info:%d", gatewayID)
	cached, err := s.redisClient.Get(cacheKey)
	if err == nil && cached != "" {
		var gwInfo GatewayInfo
		if err := json.Unmarshal([]byte(cached), &gwInfo); err == nil {
			return &gwInfo, nil
		}
	}

	// Query DB
	query := `
		SELECT g.id, g.name, a.id, a.name, s.id, s.name, o.id, o.name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`

	var gwInfo GatewayInfo
	err = s.db.QueryRow(query, gatewayID).Scan(
		&gwInfo.ID, &gwInfo.Name,
		&gwInfo.AreaID, &gwInfo.AreaName,
		&gwInfo.SiteID, &gwInfo.SiteName,
		&gwInfo.OrgID, &gwInfo.OrgName,
	)
	if err != nil {
		return nil, err
	}

	// Cache
	gwInfoJSON, _ := json.Marshal(gwInfo)
	s.redisClient.Set(cacheKey, string(gwInfoJSON), 3600)

	return &gwInfo, nil
}

// shouldStoreValue checks if the value should be stored based on deadband filtering
func (s *HistorianService) shouldStoreValue(tagInfo *TagInfo, newValue interface{}, newQuality int) bool {
	// If deadband is 0 or negative, always store (no filtering)
	if tagInfo.HistorizeDeadband <= 0 {
		return true
	}

	// Get previous value from Redis
	prevValueKey := fmt.Sprintf("prev_value:%d", tagInfo.ID)
	cached, err := s.redisClient.Get(prevValueKey)
	if err != nil || cached == "" {
		// No previous value, always store first value
		return true
	}

	// Parse previous value
	var prevValue PreviousValue
	if err := json.Unmarshal([]byte(cached), &prevValue); err != nil {
		// Failed to parse, store the value
		return true
	}

	// CHECK QUALITY CHANGE: If quality changed, ALWAYS store, regardless of value deadband
	if prevValue.Quality != newQuality {
		return true
	}

	// Compare values based on type
	return s.exceedsDeadband(prevValue.Value, newValue, tagInfo.HistorizeDeadband)
}

// exceedsDeadband checks if the new value exceeds the deadband from the previous value
func (s *HistorianService) exceedsDeadband(prev, new interface{}, deadband float64) bool {
	// Handle different value types
	switch prevVal := prev.(type) {
	case float64:
		newVal, ok := new.(float64)
		if !ok {
			return true // Type mismatch, store the value
		}
		return math.Abs(newVal-prevVal) >= deadband
	case int:
		// JSON numbers are unmarshaled as float64 by default
		newVal, ok := new.(float64)
		if !ok {
			return true
		}
		return math.Abs(newVal-float64(prevVal)) >= deadband
	case bool:
		// For boolean values, any change should be stored
		newVal, ok := new.(bool)
		if !ok {
			return true
		}
		return newVal != prevVal
	default:
		// Unknown type, store the value
		return true
	}
}

// storePreviousValue stores the previous value in Redis for deadband comparison
func (s *HistorianService) storePreviousValue(tagID int, value interface{}, quality int) {
	prevValueKey := fmt.Sprintf("prev_value:%d", tagID)
	prevValue := PreviousValue{Value: value, Quality: quality}
	prevValueJSON, _ := json.Marshal(prevValue)
	// Store with no expiration (will be overwritten on next value)
	s.redisClient.Set(prevValueKey, string(prevValueJSON), 0)
}

// storeRealtimeValue stores the current value in Redis for real-time queries
func (s *HistorianService) storeRealtimeValue(tagID int, payload MQTTPayload) {
	realtimeKey := fmt.Sprintf("realtime:%d", tagID)
	realtimeValue := RealtimeValue{
		V:  payload.V,
		Ts: payload.Ts,
		Q:  payload.Q,
	}
	realtimeJSON, _ := json.Marshal(realtimeValue)
	// Store with 60 day TTL
	s.redisClient.Set(realtimeKey, string(realtimeJSON), time.Duration(realtimeCacheTTL)*time.Second)
}

// broadcastRealtimeUpdate publishes the update to Redis Pub/Sub
func (s *HistorianService) broadcastRealtimeUpdate(orgID int, tagID int, payload MQTTPayload) {
	channel := fmt.Sprintf("realtime_updates:%d", orgID)
	message := map[string]interface{}{
		"tag_id": tagID,
		"v":      payload.V,
		"ts":     payload.Ts,
		"q":      payload.Q,
	}
	messageJSON, _ := json.Marshal(message)
	if err := s.redisClient.Publish(channel, string(messageJSON)); err != nil {
		log.Printf("Failed to publish real-time update to Redis: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
