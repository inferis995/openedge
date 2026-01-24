package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/influxdata/influxdb-client-go/v2"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/handlers"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
)

func main() {
	// Load configuration from environment variables with defaults
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnvInt("DB_PORT", 5432)
	dbUser := getEnv("DB_USER", "postgres")
	dbPass := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "industrial_edge")

	cfg := db.Config{
		Host:     dbHost,
		Port:     dbPort,
		User:     dbUser,
		Password: dbPass,
		Database: dbName,
	}

	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Load Redis configuration (needed before MQTT for health tracking)
	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort := getEnvInt("REDIS_PORT", 6379)
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB := getEnvInt("REDIS_DB", 0)

	redisCfg := redis.Config{
		Host:     redisHost,
		Port:     redisPort,
		Password: redisPassword,
		DB:       redisDB,
	}

	redisClient := redis.NewClient(redisCfg)
	if err := redisClient.Connect(); err != nil {
		log.Printf("Warning: Failed to connect to Redis: %v", err)
		log.Println("Current value queries will not be available")
		redisClient = nil
	} else {
		log.Println("Redis client connected successfully")
		defer func() {
			if redisClient != nil {
				redisClient.Disconnect()
			}
		}()
	}

	// Load InfluxDB configuration
	influxURL := getEnv("INFLUX_URL", "http://localhost:8086")
	influxToken := getEnv("INFLUX_TOKEN", "")
	influxOrg := getEnv("INFLUX_ORG", "industrial")
	influxBucket := getEnv("INFLUX_BUCKET", "historian")

	var influxClient influxdb2.Client
	if influxToken == "" {
		log.Printf("Warning: INFLUX_TOKEN not set, historical data queries will not be available")
	} else {
		influxClient = influxdb2.NewClientWithOptions(influxURL, influxToken,
			influxdb2.DefaultOptions().SetBatchSize(1000).SetFlushInterval(1000))
		log.Println("InfluxDB client configured")
	}

	// Load MQTT configuration
	mqttHost := getEnv("MQTT_HOST", "localhost")
	mqttPort := getEnvInt("MQTT_PORT", 1883)
	mqttClientID := getEnv("MQTT_CLIENT_ID", "core-api")

	mqttCfg := mqtt.Config{
		Host:          mqttHost,
		Port:          mqttPort,
		ClientID:      mqttClientID,
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
	}

	mqttClient := mqtt.NewClient(mqttCfg)
	if err := mqttClient.Connect(); err != nil {
		log.Printf("Warning: Failed to connect to MQTT broker: %v", err)
		log.Println("MQTT reload commands will not be available")
	} else {
		log.Println("MQTT client connected successfully")
		defer mqttClient.Disconnect(250)

		// Subscribe to gateway health status updates
		if err := mqttClient.Subscribe("sys/health/#", func(topic string, payload []byte) {
			handleGatewayHealthUpdate(topic, payload, redisClient)
		}); err != nil {
			log.Printf("Warning: Failed to subscribe to gateway health topic: %v", err)
		} else {
			log.Println("Subscribed to gateway health status updates")
		}
	}

	log.Println("Industrial Edge Middleware - core-api starting...")

	// Create Gin router
	router := gin.Default()

	// Create handlers with MQTT client
	orgsHandler := handlers.NewOrganizationsHandler(database)
	sitesHandler := handlers.NewSitesHandler(database)
	areasHandler := handlers.NewAreasHandler(database)
	gatewaysHandler := handlers.NewGatewaysHandler(database, mqttClient)
	tagsHandler := handlers.NewTagsHandler(database, mqttClient, redisClient)
	alarmsHandler := handlers.NewAlarmsHandler(database, mqttClient)

	// Create history handler with InfluxDB client (optional)
	var historyHandler *handlers.HistoryHandler
	if influxClient != nil {
		historyHandler = handlers.NewHistoryHandler(influxClient, influxOrg, influxBucket)
	}

	// Register routes
	api := router.Group("/api")
	{
		// Organizations endpoints
		orgs := api.Group("/organizations")
		{
			orgs.POST("", orgsHandler.Create)
			orgs.GET("", orgsHandler.List)
		}

		// Sites endpoints
		sites := api.Group("/sites")
		{
			sites.POST("", sitesHandler.Create)
			sites.GET("", sitesHandler.List)
		}

		// Areas endpoints
		areas := api.Group("/areas")
		{
			areas.POST("", areasHandler.Create)
			areas.GET("", areasHandler.List)
		}

		// Gateways endpoints
		gateways := api.Group("/gateways")
		{
			gateways.POST("", gatewaysHandler.Create)
			gateways.GET("", gatewaysHandler.List)
			gateways.PUT("/:id", gatewaysHandler.Update)
		}

		// Tags endpoints
		tags := api.Group("/tags")
		{
			tags.POST("", tagsHandler.Create)
			tags.GET("", tagsHandler.List)
			tags.PUT("/:id", tagsHandler.Update)
			tags.GET("/:id/current", tagsHandler.GetCurrentValue)
		}

		// Alarms endpoints
		alarms := api.Group("/alarms")
		{
			alarms.POST("/:id/acknowledge", alarmsHandler.Acknowledge)
		}

		// History endpoint (only if InfluxDB is configured)
		if historyHandler != nil {
			api.GET("/history", historyHandler.Query)
		}
	}

	// Start server
	port := getEnv("PORT", "8080")
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
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

// GatewayHealthStatus represents the health status cached in Redis
type GatewayHealthStatus struct {
	Status    string `json:"status"`    // "online" or "offline"
	LastSeen  int64  `json:"last_seen"` // Unix timestamp in milliseconds
}

// handleGatewayHealthUpdate processes gateway health status updates from MQTT
// Topics: sys/health/{gateway_id}
// Payload: "online" or "offline"
func handleGatewayHealthUpdate(topic string, payload []byte, redisClient *redis.Client) {
	if redisClient == nil {
		return
	}

	// Parse topic: sys/health/{gateway_id}
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		log.Printf("Invalid health topic format: %s", topic)
		return
	}

	gatewayIDStr := parts[2]
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		log.Printf("Invalid gateway ID in health topic: %s", gatewayIDStr)
		return
	}

	// Parse status from payload
	status := strings.ToLower(strings.TrimSpace(string(payload)))
	if status != "online" && status != "offline" {
		log.Printf("Invalid health status payload: %s", status)
		return
	}

	// Create health status structure
	healthStatus := GatewayHealthStatus{
		Status:   status,
		LastSeen: time.Now().UnixMilli(),
	}

	// Store in Redis with key: gateway_health:{gateway_id}
	// No expiration - status persists until updated
	statusJSON, err := json.Marshal(healthStatus)
	if err != nil {
		log.Printf("Error marshaling health status for gateway %d: %v", gatewayID, err)
		return
	}

	key := fmt.Sprintf("gateway_health:%d", gatewayID)
	if err := redisClient.Set(key, string(statusJSON), 0); err != nil {
		log.Printf("Error storing health status for gateway %d: %v", gatewayID, err)
		return
	}

	log.Printf("Gateway %d health status updated: %s", gatewayID, status)
}
