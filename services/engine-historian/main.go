package main

import (
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

	"database/sql"

	_ "github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

const (
	mqttTopicData      = "data/#"
	mqttTopicSparkplug = "spBv1.0/#" // Sparkplug B topic
	mqttTopicHealth    = "sys/health/+"
)

type HistorianService struct {
	mqttClient  *mqtt.Client
	redisClient *redis.Client
	db          *sql.DB
	wg          sync.WaitGroup
	shutdown    chan struct{}

	// deviceTagMap tracks which tag IDs have been seen from each Sparkplug
	// device.  Key = "groupID/edgeNodeID/deviceID", Value = set of tag IDs.
	// Used to mark tags offline on DDEATH/NDEATH without a DB query.
	deviceTagMap   map[string]map[int]bool
	deviceTagMapMu sync.RWMutex
}

// DataPoint represents a single data point received from MQTT
type DataPoint struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]interface{}
	Timestamp   int64
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

	// Create historian service
	service := &HistorianService{
		mqttClient:   mqttClient,
		redisClient:  redisClient,
		db:           database,
		shutdown:     make(chan struct{}),
		deviceTagMap: make(map[string]map[int]bool),
	}

	// Subscribe to data topics
	log.Printf("Subscribing to MQTT topic: %s", mqttTopicData)
	if err := mqttClient.Subscribe(mqttTopicData, service.handleDataMessage); err != nil {
		log.Fatalf("Failed to subscribe to MQTT topic: %v", err)
	}
	log.Println("Successfully subscribed to data topics")

	// Subscribe to Sparkplug B topics (dual format support)
	log.Printf("Subscribing to MQTT topic: %s", mqttTopicSparkplug)
	if err := mqttClient.Subscribe(mqttTopicSparkplug, service.handleSparkplugMessage); err != nil {
		log.Printf("Warning: Failed to subscribe to Sparkplug topic: %v", err)
	} else {
		log.Println("Successfully subscribed to Sparkplug B topics")
	}

	// Subscribe to health topics (gateway connection events)
	log.Printf("Subscribing to MQTT topic: %s", mqttTopicHealth)
	if err := mqttClient.Subscribe(mqttTopicHealth, service.handleHealthMessage); err != nil {
		log.Fatalf("Failed to subscribe to health topic: %v", err)
	}
	log.Println("Successfully subscribed to health topics")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	log.Println("Historian service running. Press Ctrl+C to shutdown...")

	<-sigChan
	log.Println("Shutdown signal received, stopping historian service...")

	// Graceful shutdown
	close(service.shutdown)
	mqttClient.Disconnect(1000)
	redisClient.Disconnect()
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

	// Get gateway name for historical record (survives gateway deletion)
	var gatewayName string
	err := s.db.QueryRow(`SELECT name FROM gateways WHERE id = $1`, gatewayID).Scan(&gatewayName)
	if err != nil {
		gatewayName = "[deleted gateway]"
	}

	// Determine message based on status
	var message string
	if status == "online" {
		message = "Gateway connected"
	} else {
		message = "Gateway disconnected"
	}

	// Store system event in PostgreSQL
	// Table schema: (id, time, gateway_id, status, message) — no gateway_name column
	fullMessage := fmt.Sprintf("%s (%s)", message, gatewayName)
	_, err = s.db.Exec(`
		INSERT INTO system_events (gateway_id, status, message)
		VALUES ($1, $2, $3)
	`, gatewayID, status, fullMessage)

	if err != nil {
		log.Printf("[HISTORIAN] ERROR inserting system event to PostgreSQL: %v", err)
	} else {
		log.Printf("[HISTORIAN] Saved system event: Gateway %d (%s) %s", gatewayID, gatewayName, status)
	}

	// ── OFFLINE handling ──────────────────────────────────────────────────
	// When a gateway goes offline, mark ALL its tags as BAD quality in Redis.
	// This works for all driver types: Modbus, OPC-UA, S7, Redis, MQTT.
	// When the gateway comes back online, normal data messages will
	// restore GOOD quality automatically.
	if status == "offline" {
		tagIDs, tagErr := s.getGatewayTagIDs(gatewayID)
		if tagErr != nil {
			log.Printf("[HISTORIAN] Failed to get tags for offline gateway %d: %v", gatewayID, tagErr)
		} else {
			log.Printf("[HISTORIAN] Marking %d tags offline for gateway %d", len(tagIDs), gatewayID)
			for _, tagID := range tagIDs {
				s.markTagOffline(tagID)
			}
		}
	}
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

	// Note: We DON'T unslugify org/site/area/gateway because:
	// 1. The SQL query uses LOWER() for case-insensitive matching
	// 2. Names like "opc-ua1" should keep their hyphens (not become "opc ua1")
	// 3. The slugify function only replaces spaces with hyphens, so names without spaces stay the same

	// For alias: convert hyphens to underscores to match DB naming convention
	// MQTT topic uses "tag-name" but DB stores "Tag_Name"
	alias = strings.ReplaceAll(alias, "-", "_")

	// Parse JSON payload: {"v": value, "ts": timestamp_ms, "q": quality}
	var mqttPayload MQTTPayload
	if err := json.Unmarshal(payload, &mqttPayload); err != nil {
		log.Printf("[HISTORIAN] Failed to parse MQTT payload from %s: %v", topic, err)
		return
	}

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
		return
	}

	// Check deadband filter
	if !s.shouldStoreValue(tagInfo, mqttPayload.V, mqttPayload.Q) {
		// Value within deadband, skip storing
		return
	}

	// Store previous value in Redis for next deadband comparison
	s.storePreviousValue(tagInfo.ID, mqttPayload.V, mqttPayload.Q)

	// Convert value to float64
	var floatValue float64
	switch v := mqttPayload.V.(type) {
	case bool:
		if v {
			floatValue = 1.0
		} else {
			floatValue = 0.0
		}
	case float64:
		floatValue = v
	case int:
		floatValue = float64(v)
	default:
		// Attempt numeric conversion for other types
		if val, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
			floatValue = val
		}
	}

	// Save directly to PostgreSQL
	s.saveToPostgreSQL(tagInfo.ID, floatValue, mqttPayload.Ts, mqttPayload.Q, "mqtt")
}

// handleSparkplugMessage handles incoming Sparkplug B messages
// Topic format: spBv1.0/{group_id}/{message_type}/{edge_node_id}/{device_id}
func (s *HistorianService) handleSparkplugMessage(topic string, payload []byte) {
	select {
	case <-s.shutdown:
		return
	default:
	}

	// Parse Sparkplug B topic
	topicInfo, err := sparkplug.ParseTopic(topic)
	if err != nil {
		// Not a valid Sparkplug topic, ignore
		return
	}

	// ── Handle DEATH messages ─────────────────────────────────────────────
	// When a device/node goes offline, mark all its known tags as BAD quality
	// in Redis so the trend shows N/A instead of stale data.
	if topicInfo.MessageType == sparkplug.MessageTypeDDEATH || topicInfo.MessageType == sparkplug.MessageTypeNDEATH {
		deviceKey := fmt.Sprintf("%s/%s/%s", topicInfo.GroupID, topicInfo.EdgeNodeID, topicInfo.DeviceID)
		log.Printf("[HISTORIAN] Sparkplug %s received for %s — marking tags offline", topicInfo.MessageType, deviceKey)
		s.markDeviceTagsOffline(deviceKey)
		return
	}

	// Only process DDATA (data) messages from here on
	if topicInfo.MessageType != sparkplug.MessageTypeDDATA {
		return
	}

	// Decode Protobuf payload (Sparkplug B official format)
	sparkplugPayload, err := sparkplug.DecodePayloadProtobuf(payload)
	if err != nil {
		log.Printf("[HISTORIAN] Failed to decode Sparkplug Protobuf payload: %v", err)
		return
	}

	// Device key for tracking which tags belong to this device
	deviceKey := fmt.Sprintf("%s/%s/%s", topicInfo.GroupID, topicInfo.EdgeNodeID, topicInfo.DeviceID)

	// Process each metric - look up tags by alias directly
	for _, metric := range sparkplugPayload.Metrics {
		if metric.Name == "" {
			continue
		}

		// Convert quality from Sparkplug to legacy format
		legacyQuality := sparkplug.ConvertSparkplugToLegacyQuality(metric.Quality)

		// Look up tag by alias directly (same approach as core-api)
		tagInfo, err := s.getTagInfoByAlias(metric.Name)
		if err != nil {
			// Tag not found - skip silently (reduces log noise)
			continue
		}

		// Track this tag under its device for DEATH handling
		s.trackDeviceTag(deviceKey, tagInfo.ID)

		// Use metric timestamp or payload timestamp
		timestamp := metric.Timestamp
		if timestamp == 0 {
			timestamp = sparkplugPayload.Timestamp
		}
		if timestamp == 0 {
			timestamp = time.Now().UnixMilli()
		}

		// Store in Redis for real-time queries
		mqttPayload := MQTTPayload{
			V:  metric.Value,
			Ts: timestamp,
			Q:  legacyQuality,
		}
		s.storeRealtimeValue(tagInfo.ID, mqttPayload)
		s.broadcastRealtimeUpdate(tagInfo.OrganizationID, tagInfo.ID, mqttPayload)

		// Skip if historize is disabled
		if !tagInfo.Historize {
			continue
		}

		// Check deadband filter (same logic as legacy handler)
		if !s.shouldStoreValue(tagInfo, metric.Value, legacyQuality) {
			continue
		}

		// Store previous value in Redis for next deadband comparison
		s.storePreviousValue(tagInfo.ID, metric.Value, legacyQuality)

		// Convert value to float64
		var floatValue float64
		switch v := metric.Value.(type) {
		case bool:
			if v {
				floatValue = 1.0
			} else {
				floatValue = 0.0
			}
		case float64:
			floatValue = v
		case float32:
			floatValue = float64(v)
		case int:
			floatValue = float64(v)
		case int32:
			floatValue = float64(v)
		case int64:
			floatValue = float64(v)
		default:
			if val, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
				floatValue = val
			}
		}

		// Save directly to PostgreSQL
		s.saveToPostgreSQL(tagInfo.ID, floatValue, timestamp, legacyQuality, "sparkplug_b")
	}
}

// saveToPostgreSQL saves a data point directly to PostgreSQL tag_history table
// If quality > 0 (BAD), we insert a NULL value to guarantee a chart gap.
func (s *HistorianService) saveToPostgreSQL(tagID int, value float64, timestampMs int64, quality int, source string) {
	ts := time.UnixMilli(timestampMs)

	// If quality is BAD (>0), we store an explicit NULL to create a gap in the chart
	if quality > 0 {
		_, err := s.db.Exec(`
			INSERT INTO tag_history (time, tag_id, value, source)
			VALUES ($1, $2, NULL, 'offline')
		`, ts, tagID)

		if err != nil {
			log.Printf("[HISTORIAN] ERROR inserting gap marker to PostgreSQL: %v", err)
		}
		return
	}

	// Insert standard good value into PostgreSQL
	_, err := s.db.Exec(`
		INSERT INTO tag_history (time, tag_id, value, source)
		VALUES ($1, $2, $3, $4)
	`, ts, tagID, value, source)

	if err != nil {
		log.Printf("[HISTORIAN] ERROR inserting to PostgreSQL: %v", err)
	}
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
	// The query checks both the exact name and the slugified version (hyphens vs spaces)
	// This handles both "cella 1" (DB) vs "cella-1" (topic) AND "opc-ua1" (DB) vs "opc-ua1" (topic)
	query := `
		SELECT t.id, s.org_id, t.historize, t.historize_deadband
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE TRIM(LOWER(o.name)) = TRIM(LOWER($1))
		  AND (TRIM(LOWER(s.name)) = TRIM(LOWER($2)) OR REPLACE(TRIM(LOWER(s.name)), ' ', '-') = TRIM(LOWER($2)))
		  AND (TRIM(LOWER(a.name)) = TRIM(LOWER($3)) OR REPLACE(TRIM(LOWER(a.name)), ' ', '-') = TRIM(LOWER($3)))
		  AND (TRIM(LOWER(g.name)) = TRIM(LOWER($4)) OR REPLACE(TRIM(LOWER(g.name)), ' ', '-') = TRIM(LOWER($4)))
		  AND (TRIM(LOWER(t.alias)) = TRIM(LOWER($5)) OR REPLACE(TRIM(LOWER(t.alias)), ' ', '-') = TRIM(LOWER($5)) OR REPLACE(TRIM(LOWER(t.alias)), '_', '-') = TRIM(LOWER($5)))
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

	// Cache in Redis for 1 minute (60 seconds)
	tagInfoJSON, _ := json.Marshal(tagInfo)
	s.redisClient.Set(cacheKey, string(tagInfoJSON), 60*time.Second)

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
	s.redisClient.Set(cacheKey, string(gwInfoJSON), 60*time.Second)

	return &gwInfo, nil
}

// getTagInfoByAlias retrieves tag info by alias directly (for Sparkplug B messages)
func (s *HistorianService) getTagInfoByAlias(alias string) (*TagInfo, error) {
	alias = strings.TrimSpace(alias)

	// Try cache first
	cacheKey := fmt.Sprintf("tag_by_alias:%s", alias)
	cached, err := s.redisClient.Get(cacheKey)
	if err == nil && cached != "" {
		var tagInfo TagInfo
		if err := json.Unmarshal([]byte(cached), &tagInfo); err == nil {
			return &tagInfo, nil
		}
	}

	// Query database by alias
	query := `
		SELECT t.id, s.org_id, t.historize, t.historize_deadband
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE LOWER(t.alias) = LOWER($1)
	`

	var tagInfo TagInfo
	err = s.db.QueryRow(query, alias).Scan(
		&tagInfo.ID,
		&tagInfo.OrganizationID,
		&tagInfo.Historize,
		&tagInfo.HistorizeDeadband,
	)
	if err != nil {
		return nil, fmt.Errorf("tag not found: %s", alias)
	}

	// Cache for 1 minute
	tagInfoJSON, _ := json.Marshal(tagInfo)
	s.redisClient.Set(cacheKey, string(tagInfoJSON), 60*time.Second)

	return &tagInfo, nil
}

// shouldStoreValue checks if the value should be stored based on quality and deadband filtering.
// Quality codes: 0 = GOOD, 1 = UNCERTAIN, 2 = BAD
// Logic: always store on BAD quality, quality change, or value change exceeding deadband.
func (s *HistorianService) shouldStoreValue(tagInfo *TagInfo, newValue interface{}, newQuality int) bool {
	// Always store BAD/UNCERTAIN quality to create visible gaps in charts
	if newQuality > 0 {
		return true
	}

	// Retrieve previous value from Redis for comparison
	prevValueKey := fmt.Sprintf("prev_value:%d", tagInfo.ID)
	cached, err := s.redisClient.Get(prevValueKey)
	if err != nil || cached == "" {
		// No previous value recorded yet — always store the first occurrence
		return true
	}

	var prevValue PreviousValue
	if err := json.Unmarshal([]byte(cached), &prevValue); err != nil {
		return true
	}

	// Always store when quality transitions (e.g., BAD → GOOD recovery)
	if prevValue.Quality != newQuality {
		return true
	}

	// If deadband is not configured (≤ 0), store only on actual value change (on-change storage)
	if tagInfo.HistorizeDeadband <= 0 {
		changed := s.hasValueChanged(prevValue.Value, newValue)
		return changed
	}

	// Apply configured numeric deadband
	exceeded := s.exceedsDeadband(prevValue.Value, newValue, tagInfo.HistorizeDeadband)
	return exceeded
}

// hasValueChanged returns true if the new value differs from the previous value.
func (s *HistorianService) hasValueChanged(prev, new interface{}) bool {
	switch prevVal := prev.(type) {
	case float64:
		switch nv := new.(type) {
		case float64:
			return nv != prevVal
		case bool:
			boolAsFloat := 0.0
			if nv {
				boolAsFloat = 1.0
			}
			return boolAsFloat != prevVal
		default:
			return true
		}
	case bool:
		switch nv := new.(type) {
		case bool:
			return nv != prevVal
		case float64:
			return (nv >= 0.5) != prevVal
		default:
			return true
		}
	default:
		return fmt.Sprintf("%v", prev) != fmt.Sprintf("%v", new)
	}
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

// ── DEATH / OFFLINE helpers ──────────────────────────────────────────────

// trackDeviceTag records that a tag ID was seen from a Sparkplug device.
// Called during DDATA processing to build the device→tags mapping.
func (s *HistorianService) trackDeviceTag(deviceKey string, tagID int) {
	s.deviceTagMapMu.Lock()
	defer s.deviceTagMapMu.Unlock()
	if s.deviceTagMap[deviceKey] == nil {
		s.deviceTagMap[deviceKey] = make(map[int]bool)
	}
	s.deviceTagMap[deviceKey][tagID] = true
}

// markDeviceTagsOffline marks all known tags of a Sparkplug device as BAD
// quality in Redis.  Called when DDEATH/NDEATH is received.
func (s *HistorianService) markDeviceTagsOffline(deviceKey string) {
	s.deviceTagMapMu.RLock()
	tagIDs := s.deviceTagMap[deviceKey]
	s.deviceTagMapMu.RUnlock()

	if len(tagIDs) == 0 {
		log.Printf("[HISTORIAN] No tracked tags for device %s — trying DB lookup", deviceKey)
		return
	}

	for tagID := range tagIDs {
		s.markTagOffline(tagID)
	}
	log.Printf("[HISTORIAN] Marked %d tags offline for Sparkplug device %s", len(tagIDs), deviceKey)
}

// markTagOffline does two things when a tag goes offline:
// 1. Updates Redis realtime value with BAD quality (frontend shows N/A)
// 2. Stores a NULL-value "offline marker" in tag_history (trend shows gap)
//
// Duplicate protection: if the tag is already marked offline in Redis
// (quality > 0), skip the DB marker to avoid flooding tag_history.
func (s *HistorianService) markTagOffline(tagID int) {
	realtimeKey := fmt.Sprintf("realtime:%d", tagID)
	cached, err := s.redisClient.Get(realtimeKey)

	alreadyOffline := false
	if err == nil && cached != "" {
		var rv RealtimeValue
		if err := json.Unmarshal([]byte(cached), &rv); err == nil {
			alreadyOffline = rv.Q > 0

			// ── 1. Update Redis realtime quality ────────────────────────
			rv.Q = 1 // BAD quality
			rv.Ts = time.Now().UnixMilli()
			rvJSON, _ := json.Marshal(rv)
			s.redisClient.Set(realtimeKey, string(rvJSON), time.Duration(realtimeCacheTTL)*time.Second)
		}
	}

	// ── 2. Store ONE offline marker in PostgreSQL (skip if already offline)
	if !alreadyOffline {
		_, dbErr := s.db.Exec(`
			INSERT INTO tag_history (time, tag_id, value, source)
			VALUES (NOW(), $1, NULL, 'offline')
		`, tagID)
		if dbErr != nil {
			log.Printf("[HISTORIAN] Failed to save offline marker for tag %d: %v", tagID, dbErr)
		}
		log.Printf("[HISTORIAN] Tag %d marked offline: Redis quality=BAD + DB gap marker", tagID)
	}
}

// getGatewayTagIDs returns all tag IDs belonging to a gateway.
// Used by the health handler to mark all tags offline when a gateway disconnects.
func (s *HistorianService) getGatewayTagIDs(gatewayID int) ([]int, error) {
	query := `SELECT id FROM tags WHERE gateway_id = $1`
	rows, err := s.db.Query(query, gatewayID)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
