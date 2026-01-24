package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
)

const (
	mqttTopicData     = "data/#"
	bufferFlushSize   = 1000
	bufferFlushInterval = 1 * time.Second
)

type HistorianService struct {
	mqttClient     *mqtt.Client
	redisClient    *redis.Client
	influxClient   influxdb2.Client
	influxWriteAPI api.WriteAPI
	influxOrg      string
	influxBucket   string
	wg             sync.WaitGroup
	shutdown       chan struct{}
	buffer         []*DataPoint
	bufferMutex    sync.Mutex
	flushTicker    *time.Ticker
}

// DataPoint represents a single data point received from MQTT
type DataPoint struct {
	Measurement string                 `influx:"_measurement"` // Will be set to "tag_data"
	Tags       map[string]string       `influx:"_,tag"`
	Fields     map[string]interface{} `influx:"_,field"`
	Timestamp  int64                  `influx:"_time"`
	Org        string
	Site       string
	Area       string
	Gateway    string
	Alias      string
}

// MQTTPayload represents the JSON payload from MQTT
type MQTTPayload struct {
	V interface{} `json:"v"` // Value (bool, float64, int)
	Ts int64      `json:"ts"` // Timestamp in milliseconds
	Q  int        `json:"q"`  // Quality (0 = good, 1 = bad)
}

func main() {
	// Load configuration from environment variables
	mqttHost := getEnv("MQTT_HOST", "localhost")
	mqttPort, _ := strconv.Atoi(getEnv("MQTT_PORT", "1883"))
	mqttClientID := getEnv("MQTT_CLIENT_ID", "engine-historian")

	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort, _ := strconv.Atoi(getEnv("REDIS_PORT", "6379"))
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))

	influxURL := getEnv("INFLUX_URL", "http://localhost:8086")
	influxToken := getEnv("INFLUX_TOKEN", "")
	influxOrg := getEnv("INFLUX_ORG", "industrial")
	influxBucket := getEnv("INFLUX_BUCKET", "historian")

	// Validate required configuration
	if influxToken == "" {
		log.Fatal("INFLUX_TOKEN environment variable is required")
	}

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

	// Verify InfluxDB connection
	_, err := influxClient.Health(nil)
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

	// Parse JSON payload: {"v": value, "ts": timestamp_ms, "q": quality}
	var mqttPayload MQTTPayload
	if err := json.Unmarshal(payload, &mqttPayload); err != nil {
		log.Printf("Failed to parse MQTT payload from %s: %v", topic, err)
		return
	}

	// Create data point
	dp := &DataPoint{
		Measurement: "tag_data",
		Tags: map[string]string{
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

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
