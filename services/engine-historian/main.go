package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
)

const (
	mqttTopicData = "data/#"
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

	log.Printf("Received data on topic %s: %s", topic, string(payload))

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

	// Parse payload - expecting JSON: {"v": value, "ts": timestamp_ms, "q": quality}
	// For now, we'll store the raw payload. Full parsing will be done in US-022.

	log.Printf("Parsed data - Org: %s, Site: %s, Area: %s, Gateway: %s, Alias: %s",
		org, site, area, gateway, alias)

	// TODO: In US-022, this will:
	// 1. Parse the JSON payload to extract v (value), ts (timestamp), q (quality)
	// 2. Apply deadband filtering using Redis cache
	// 3. Add to buffer for batch writing
	// 4. Store latest value in Redis cache
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

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
