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

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"

	_ "github.com/lib/pq"
)

const (
	mqttTopicData              = "data/#"
	mqttTopicAlarmEvents       = "events/alarms"
	mqttTopicAlarmAcknowledge  = "sys/command/acknowledge"
	redisKeyAlarmState         = "alarm:%d"
)

// AlarmState represents the current state of an alarm
type AlarmState string

const (
	AlarmStateActive       AlarmState = "ACTIVE"
	AlarmStateRTN          AlarmState = "RTN"
	AlarmStateAcknowledged AlarmState = "ACKNOWLEDGED"
	AlarmStateClear        AlarmState = "CLEAR"
)

// AlarmEvent represents an alarm event published to MQTT
type AlarmEvent struct {
	TagID     int        `json:"tag"`
	State     AlarmState `json:"state"`
	Message   string     `json:"msg"`
	Timestamp int64      `json:"ts"`
}

// MQTTPayload represents the data payload from MQTT
type MQTTPayload struct {
	V  interface{} `json:"v"`
	Ts int64       `json:"ts"`
	Q  int         `json:"q"`
}

// TagAlarmConfig holds alarm configuration from database
type TagAlarmConfig struct {
	TagID          int
	AlarmEnabled   bool
	AlarmThreshold float64
	AlarmOperator  string
	AlarmPriority  int
}

// AlarmService manages the alarm detection and state tracking
type AlarmService struct {
	mqttClient    *mqtt.Client
	redisClient   *redis.Client
	db            *sql.DB
	alarmStates   map[int]AlarmState
	alarmMutex    sync.RWMutex
	shutdown      chan struct{}
	wg            sync.WaitGroup
}

func main() {
	log.Println("Starting engine-alarm service...")

	// Get configuration from environment variables
	mqttHost := getEnv("MQTT_HOST", "localhost")
	mqttPort, _ := strconv.Atoi(getEnv("MQTT_PORT", "1883"))
	mqttClientID := getEnv("MQTT_CLIENT_ID", "engine-alarm")

	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort, _ := strconv.Atoi(getEnv("REDIS_PORT", "6379"))
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))

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
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer database.Close()
	log.Println("Connected to PostgreSQL")

	// Create MQTT client
	mqttClient := mqtt.NewClient(mqtt.Config{
		Host:     mqttHost,
		Port:     mqttPort,
		ClientID: mqttClientID,
	})

	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", err)
	}
	log.Println("Connected to MQTT broker")
	defer mqttClient.Disconnect(1000)

	// Create Redis client
	redisClient := redis.NewClient(redis.Config{
		Host:     redisHost,
		Port:     redisPort,
		Password: redisPassword,
		DB:       redisDB,
	})

	if err := redisClient.Connect(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")
	defer redisClient.Disconnect()

	// Create alarm service
	service := &AlarmService{
		mqttClient:  mqttClient,
		redisClient: redisClient,
		db:          database,
		alarmStates: make(map[int]AlarmState),
		shutdown:    make(chan struct{}),
	}

	// Load existing alarm states from Redis on startup
	if err := service.loadAlarmStates(); err != nil {
		log.Printf("Warning: Failed to load alarm states from Redis: %v", err)
	}

	// Subscribe to data topics for alarm detection
	log.Printf("Subscribing to MQTT topic: %s", mqttTopicData)
	if err := mqttClient.Subscribe(mqttTopicData, service.handleDataMessage); err != nil {
		log.Fatalf("Failed to subscribe to %s: %v", mqttTopicData, err)
	}
	log.Println("Successfully subscribed to data topics")

	// Subscribe to acknowledge command topics
	log.Printf("Subscribing to MQTT topic: %s", mqttTopicAlarmAcknowledge)
	if err := mqttClient.Subscribe(mqttTopicAlarmAcknowledge, service.handleAcknowledgeCommand); err != nil {
		log.Fatalf("Failed to subscribe to %s: %v", mqttTopicAlarmAcknowledge, err)
	}
	log.Println("Successfully subscribed to acknowledge command topic")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	log.Println("engine-alarm service is running...")

	<-sigChan
	log.Println("Shutdown signal received, stopping engine-alarm service...")
	close(service.shutdown)
	service.wg.Wait()
	log.Println("engine-alarm service stopped")
}

// handleDataMessage processes incoming data messages and checks for alarm conditions
func (s *AlarmService) handleDataMessage(topic string, payload []byte) {
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

	// Parse MQTT payload
	var mqttPayload MQTTPayload
	if err := json.Unmarshal(payload, &mqttPayload); err != nil {
		log.Printf("Failed to parse MQTT payload from topic %s: %v", topic, err)
		return
	}

	// Skip bad quality values
	if mqttPayload.Q != 0 {
		return
	}

	// Get tag alarm configuration from database
	tagConfig, err := s.getTagAlarmConfig(org, site, area, gateway, alias)
	if err != nil {
		log.Printf("Failed to get tag alarm config for topic %s: %v", topic, err)
		return
	}

	// Skip if alarm is not enabled for this tag
	if !tagConfig.AlarmEnabled {
		return
	}

	// Check alarm condition
	s.checkAlarmCondition(tagConfig, mqttPayload.V, mqttPayload.Ts)
}

// getTagAlarmConfig retrieves alarm configuration for a tag based on its topic
func (s *AlarmService) getTagAlarmConfig(org, site, area, gateway, alias string) (*TagAlarmConfig, error) {
	query := `
		SELECT t.id, t.alarm_enabled, t.alarm_threshold, t.alarm_operator, t.alarm_priority
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites si ON a.site_id = si.id
		JOIN organizations o ON si.org_id = o.id
		WHERE LOWER(o.name) = LOWER($1)
		AND LOWER(si.name) = LOWER($2)
		AND LOWER(a.name) = LOWER($3)
		AND LOWER(g.name) = LOWER($4)
		AND LOWER(t.alias) = LOWER($5)
	`

	var config TagAlarmConfig
	err := s.db.QueryRow(query, org, site, area, gateway, alias).Scan(
		&config.TagID,
		&config.AlarmEnabled,
		&config.AlarmThreshold,
		&config.AlarmOperator,
		&config.AlarmPriority,
	)

	if err != nil {
		return nil, err
	}

	return &config, nil
}

// checkAlarmCondition evaluates if the value triggers an alarm condition
func (s *AlarmService) checkAlarmCondition(config *TagAlarmConfig, value interface{}, timestamp int64) {
	// Convert value to float64 for comparison
	var floatValue float64
	switch v := value.(type) {
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
	case bool:
		// Boolean values don't use threshold comparison
		// They trigger on true, clear on false
		if v {
			s.transitionToAlarm(config.TagID, timestamp, "Value is true")
		} else {
			s.transitionToRTN(config.TagID, timestamp)
		}
		return
	default:
		log.Printf("Unsupported value type for alarm check: %T", value)
		return
	}

	// Get current alarm state
	currentState := s.getAlarmState(config.TagID)

	// Evaluate alarm condition based on operator
	var alarmTriggered bool
	var message string

	switch config.AlarmOperator {
	case ">":
		alarmTriggered = floatValue > config.AlarmThreshold
		message = fmt.Sprintf("Value %.2f exceeds threshold %.2f", floatValue, config.AlarmThreshold)
	case "<":
		alarmTriggered = floatValue < config.AlarmThreshold
		message = fmt.Sprintf("Value %.2f below threshold %.2f", floatValue, config.AlarmThreshold)
	case "=":
		alarmTriggered = floatValue == config.AlarmThreshold
		message = fmt.Sprintf("Value equals threshold %.2f", floatValue)
	default:
		log.Printf("Unknown alarm operator: %s", config.AlarmOperator)
		return
	}

	// Handle state transitions
	if alarmTriggered {
		if currentState != AlarmStateActive && currentState != AlarmStateAcknowledged {
			s.transitionToAlarm(config.TagID, timestamp, message)
		}
	} else {
		if currentState == AlarmStateActive || currentState == AlarmStateAcknowledged {
			s.transitionToRTN(config.TagID, timestamp)
		}
	}
}

// transitionToAlarm transitions alarm state to ACTIVE
func (s *AlarmService) transitionToAlarm(tagID int, timestamp int64, message string) {
	s.setAlarmState(tagID, AlarmStateActive)

	// Persist to Redis
	key := fmt.Sprintf(redisKeyAlarmState, tagID)
	stateData := map[string]interface{}{
		"state":     AlarmStateActive,
		"message":   message,
		"timestamp": timestamp,
	}
	stateJSON, _ := json.Marshal(stateData)
	if err := s.redisClient.Set(key, string(stateJSON), 0); err != nil {
		log.Printf("Failed to store alarm state in Redis: %v", err)
	}

	// Persist to PostgreSQL alarms table
	triggeredAt := time.Unix(timestamp/1000, 0)
	if err := s.insertAlarmRecord(tagID, AlarmStateActive, message, triggeredAt); err != nil {
		log.Printf("Failed to insert alarm record for tag %d: %v", tagID, err)
	}

	// Publish alarm event to MQTT
	event := AlarmEvent{
		TagID:     tagID,
		State:     AlarmStateActive,
		Message:   message,
		Timestamp: timestamp,
	}
	eventJSON, _ := json.Marshal(event)
	topic := fmt.Sprintf("%s/%d", mqttTopicAlarmEvents, tagID)
	if err := s.mqttClient.Publish(topic, string(eventJSON)); err != nil {
		log.Printf("Failed to publish alarm event: %v", err)
	}

	log.Printf("Alarm ACTIVE for tag %d: %s", tagID, message)
}

// transitionToRTN transitions alarm state to RTN (Return to Normal)
func (s *AlarmService) transitionToRTN(tagID int, timestamp int64) {
	s.setAlarmState(tagID, AlarmStateRTN)

	// Persist to Redis
	key := fmt.Sprintf(redisKeyAlarmState, tagID)
	stateData := map[string]interface{}{
		"state":     AlarmStateRTN,
		"message":   "Return to normal",
		"timestamp": timestamp,
	}
	stateJSON, _ := json.Marshal(stateData)
	if err := s.redisClient.Set(key, string(stateJSON), 0); err != nil {
		log.Printf("Failed to store alarm state in Redis: %v", err)
	}

	// Update existing ACTIVE alarm to CLEAR in PostgreSQL
	clearedAt := time.Unix(timestamp/1000, 0)
	if err := s.updateAlarmToClear(tagID, clearedAt); err != nil {
		log.Printf("Failed to update alarm to CLEAR for tag %d: %v", tagID, err)
	}

	// Insert new RTN alarm record
	if err := s.insertAlarmRecord(tagID, AlarmStateRTN, "Return to normal", clearedAt); err != nil {
		log.Printf("Failed to insert RTN alarm record for tag %d: %v", tagID, err)
	}

	// Publish alarm event to MQTT
	event := AlarmEvent{
		TagID:     tagID,
		State:     AlarmStateRTN,
		Message:   "Return to normal",
		Timestamp: timestamp,
	}
	eventJSON, _ := json.Marshal(event)
	topic := fmt.Sprintf("%s/%d", mqttTopicAlarmEvents, tagID)
	if err := s.mqttClient.Publish(topic, string(eventJSON)); err != nil {
		log.Printf("Failed to publish alarm event: %v", err)
	}

	log.Printf("Alarm RTN for tag %d", tagID)
}

// getAlarmState retrieves the current alarm state for a tag
func (s *AlarmService) getAlarmState(tagID int) AlarmState {
	s.alarmMutex.RLock()
	defer s.alarmMutex.RUnlock()

	if state, exists := s.alarmStates[tagID]; exists {
		return state
	}
	return ""
}

// setAlarmState sets the alarm state for a tag
func (s *AlarmService) setAlarmState(tagID int, state AlarmState) {
	s.alarmMutex.Lock()
	defer s.alarmMutex.Unlock()

	s.alarmStates[tagID] = state
}

// loadAlarmStates loads existing alarm states from Redis on startup
func (s *AlarmService) loadAlarmStates() error {
	keys, err := s.redisClient.Keys("alarm:*")
	if err != nil {
		return err
	}

	for _, key := range keys {
		var tagID int
		if _, err := fmt.Sscanf(key, "alarm:%d", &tagID); err == nil {
			stateJSON, err := s.redisClient.Get(key)
			if err == nil && stateJSON != "" {
				var stateData map[string]interface{}
				if err := json.Unmarshal([]byte(stateJSON), &stateData); err == nil {
					if stateStr, ok := stateData["state"].(string); ok {
						s.setAlarmState(tagID, AlarmState(stateStr))
						log.Printf("Loaded alarm state for tag %d: %s", tagID, stateStr)
					}
				}
			}
		}
	}

	return nil
}

// insertAlarmRecord inserts a new alarm record into the database
func (s *AlarmService) insertAlarmRecord(tagID int, state AlarmState, message string, triggeredAt time.Time) error {
	query := `
		INSERT INTO alarms (tag_id, state, message, triggered_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := s.db.Exec(query, tagID, state, message, triggeredAt)
	return err
}

// updateAlarmToClear updates the most recent ACTIVE alarm for a tag to CLEAR state
func (s *AlarmService) updateAlarmToClear(tagID int, clearedAt time.Time) error {
	query := `
		UPDATE alarms
		SET state = $1, cleared_at = $2
		WHERE tag_id = $3 AND state = $4
		ORDER BY triggered_at DESC
		LIMIT 1
	`
	result, err := s.db.Exec(query, AlarmStateClear, clearedAt, tagID, AlarmStateActive)
	if err != nil {
		return err
	}
	// Check if any row was actually updated
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		log.Printf("No ACTIVE alarm found to update to CLEAR for tag %d", tagID)
	}
	return nil
}

// handleAcknowledgeCommand processes acknowledge commands from the API
func (s *AlarmService) handleAcknowledgeCommand(topic string, payload []byte) {
	select {
	case <-s.shutdown:
		return
	default:
	}

	// Parse payload: expected format {"alarm_id": <id>}
	var ackCmd struct {
		AlarmID int `json:"alarm_id"`
	}
	if err := json.Unmarshal(payload, &ackCmd); err != nil {
		log.Printf("Failed to parse acknowledge command payload: %v", err)
		return
	}

	// Get the alarm record from database to verify it's ACTIVE
	var tagID int
	var currentState string
	err := s.db.QueryRow("SELECT tag_id, state FROM alarms WHERE id = $1", ackCmd.AlarmID).Scan(&tagID, &currentState)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Alarm %d not found", ackCmd.AlarmID)
		} else {
			log.Printf("Failed to query alarm %d: %v", ackCmd.AlarmID, err)
		}
		return
	}

	// Only transition to ACKNOWLEDGED if currently ACTIVE
	if currentState != string(AlarmStateActive) {
		log.Printf("Alarm %d is not in ACTIVE state (current: %s), skipping acknowledge", ackCmd.AlarmID, currentState)
		return
	}

	// Update state to ACKNOWLEDGED in memory and Redis
	s.setAlarmState(tagID, AlarmStateAcknowledged)

	key := fmt.Sprintf(redisKeyAlarmState, tagID)
	stateData := map[string]interface{}{
		"state":     AlarmStateAcknowledged,
		"message":   "Acknowledged",
		"timestamp": time.Now().UnixMilli(),
	}
	stateJSON, _ := json.Marshal(stateData)
	if err := s.redisClient.Set(key, string(stateJSON), 0); err != nil {
		log.Printf("Failed to update alarm state in Redis: %v", err)
	}

	// Publish alarm event to MQTT
	event := AlarmEvent{
		TagID:     tagID,
		State:     AlarmStateAcknowledged,
		Message:   "Acknowledged",
		Timestamp: time.Now().UnixMilli(),
	}
	eventJSON, _ := json.Marshal(event)
	eventTopic := fmt.Sprintf("%s/%d", mqttTopicAlarmEvents, tagID)
	if err := s.mqttClient.Publish(eventTopic, string(eventJSON)); err != nil {
		log.Printf("Failed to publish alarm event: %v", err)
	}

	log.Printf("Alarm %d for tag %d acknowledged via API command", ackCmd.AlarmID, tagID)
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
