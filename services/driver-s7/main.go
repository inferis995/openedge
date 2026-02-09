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

	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/s7"
)

// GatewayConfig holds the loaded gateway configuration
type GatewayConfig struct {
	Gateway models.Gateway
	Tags    []models.Tag
	OrgName string
	Site    string
	Area    string
}

// TagPayload represents the MQTT message payload for tag values
type TagPayload struct {
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
	// Report by Exception: store previous values for change detection
	previousValues map[int]interface{}
	prevValuesMu   sync.RWMutex
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

	// Publish online status
	if err := mqttClient.PublishWithQoS(fmt.Sprintf("sys/health/%d", gatewayID), "online", 1, true); err != nil {
		log.Printf("Failed to publish online status: %v", err)
	}

	// Create driver instance
	driver := &Driver{
		gatewayID:      gatewayID,
		database:       database,
		mqttClient:     mqttClient,
		stopChan:       make(chan struct{}),
		reloadChan:     make(chan struct{}, 1),
		previousValues: make(map[int]interface{}),
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
		OrgName: slugify(orgName),
		Site:    slugify(siteName),
		Area:    slugify(areaName),
	}

	log.Printf("Loaded gateway config: %s (ID: %d) with %d tags", gateway.Name, gateway.ID, len(tags))
	log.Printf("Topic prefix: data/%s/%s/%s/%s", d.config.OrgName, d.config.Site, d.config.Area, slugify(gateway.Name))

	return nil
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

	// Ensure S7 connection is established
	if d.s7Client == nil || !d.s7Client.IsConnected() {
		log.Printf("S7 not connected, attempting reconnection...")
		if err := d.connectS7(); err != nil {
			log.Printf("S7 reconnection failed: %v", err)
			return
		}
	}

	// Build topic prefix: data/{org}/{site}/{area}/{gateway}
	topicPrefix := fmt.Sprintf("data/%s/%s/%s/%s",
		config.OrgName,
		config.Site,
		config.Area,
		slugify(config.Gateway.Name),
	)

	timestamp := time.Now().UnixMilli()

	// Read and publish each tag
	for _, tag := range config.Tags {
		// Read value from PLC
		dataType := s7.DataType(tag.DataType)
		result := d.s7Client.ReadTag(tag.Code, dataType)

		// Check for read error
		if result.Error != nil {
			log.Printf("Error reading tag %s (ID:%d): %v", tag.Alias, tag.ID, result.Error)
			// Publish with bad quality
			d.publishTagValue(topicPrefix, tag, nil, timestamp, 1)
			continue
		}

		// Report by Exception: check if value has changed
		if !d.hasValueChanged(tag.ID, result.Value) {
			continue // Skip publish - value hasn't changed
		}

		// Update previous value
		d.updatePreviousValue(tag.ID, result.Value)

		// Publish to MQTT
		d.publishTagValue(topicPrefix, tag, result.Value, timestamp, result.Quality)
	}
}

// publishTagValue publishes a tag value to MQTT
func (d *Driver) publishTagValue(topicPrefix string, tag models.Tag, value interface{}, timestamp int64, quality int) {
	// Build topic: data/{org}/{site}/{area}/{gateway}/{alias}
	topic := fmt.Sprintf("%s/%s", topicPrefix, slugify(tag.Alias))

	// Build payload
	payload := TagPayload{
		Value:     value,
		Timestamp: timestamp,
		Quality:   quality,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling payload for tag %s: %v", tag.Alias, err)
		return
	}

	// Publish to MQTT (QoS 1 for reliability)
	if err := d.mqttClient.PublishWithQoS(topic, string(payloadBytes), 1, false); err != nil {
		log.Printf("Error publishing tag %s to %s: %v", tag.Alias, topic, err)
	}
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

// updatePreviousValue updates the stored previous value for a tag
func (d *Driver) updatePreviousValue(tagID int, value interface{}) {
	d.prevValuesMu.Lock()
	d.previousValues[tagID] = value
	d.prevValuesMu.Unlock()
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
