package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	_ "github.com/ralph/industrial-edge-middleware/docs"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/handlers"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
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

		// Subscribe to data updates
		if err := mqttClient.Subscribe("data/#", func(topic string, payload []byte) {
			handleDataUpdate(topic, payload, redisClient)
		}); err != nil {
			log.Printf("Warning: Failed to subscribe to data topic: %v", err)
		} else {
			log.Println("Subscribed to data updates")
		}
	}

	log.Println("Industrial Edge Middleware - core-api starting...")

	// Create Gin router
	router := gin.Default()

	// CORS Configuration
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:3004", "http://127.0.0.1:3004", "http://localhost:4000", "http://127.0.0.1:4000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Organization-ID"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Create handlers with MQTT client and Redis client
	orgsHandler := handlers.NewOrganizationsHandler(database)
	sitesHandler := handlers.NewSitesHandler(database)
	areasHandler := handlers.NewAreasHandler(database)
	gatewaysHandler := handlers.NewGatewaysHandler(database, mqttClient, redisClient)
	tagsHandler := handlers.NewTagsHandler(database, mqttClient, redisClient)
	systemHandler := handlers.NewSystemHandler(database, mqttClient)
	realtimeHandler := handlers.NewRealtimeHandler(redisClient)

	// Create history handler with InfluxDB client (optional)
	var historyHandler *handlers.HistoryHandler
	if influxClient != nil {
		historyHandler = handlers.NewHistoryHandler(influxClient, influxOrg, influxBucket, database)
	}

	// Create auth service and handler
	authService := auth.NewService(database)
	authHandler := handlers.NewAuthHandler(authService)

	// Create users handler
	usersHandler := handlers.NewUsersHandler(database)

	// Register routes
	api := router.Group("/api")
	{
		// Auth endpoints (public)
		auth := api.Group("/auth")
		{
			auth.POST("/login", authHandler.Login)
		}
		// Organizations endpoints
		orgs := api.Group("/organizations")
		orgs.Use(middleware.RequireAuth)
		{
			orgs.POST("", middleware.RequireRole(models.RoleAdmin), orgsHandler.Create)
			orgs.GET("", orgsHandler.List)
			orgs.GET("/:id", orgsHandler.Get)
			orgs.PUT("/:id", middleware.RequireRole(models.RoleAdmin), orgsHandler.Update)
			orgs.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), orgsHandler.Delete)
		}

		// Multi-tenant protected endpoints - require organization context
		// Sites endpoints
		sites := api.Group("/sites")
		sites.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			sites.POST("", middleware.RequireRole(models.RoleAdmin), sitesHandler.Create)
			sites.GET("", sitesHandler.List)
			sites.GET("/:id", sitesHandler.Get)
			sites.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), sitesHandler.Delete)
			sites.PUT("/:id", middleware.RequireRole(models.RoleAdmin), sitesHandler.Update)
		}

		// Areas endpoints
		areas := api.Group("/areas")
		areas.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			areas.POST("", middleware.RequireRole(models.RoleAdmin), areasHandler.Create)
			areas.GET("", areasHandler.List)
			areas.GET("/:id", areasHandler.Get)
			areas.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), areasHandler.Delete)
			areas.PUT("/:id", middleware.RequireRole(models.RoleAdmin), areasHandler.Update)
		}

		// Gateways endpoints
		gateways := api.Group("/gateways")
		gateways.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			gateways.POST("", middleware.RequireRole(models.RoleAdmin), gatewaysHandler.Create)
			gateways.GET("", gatewaysHandler.List)
			gateways.GET("/:id", gatewaysHandler.Get)
			gateways.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), gatewaysHandler.Delete)
			gateways.PUT("/:id", middleware.RequireRole(models.RoleAdmin), gatewaysHandler.Update)
			gateways.POST("/:id/test", middleware.RequireRole(models.RoleAdmin), gatewaysHandler.TestConnection)
		}

		// Tags endpoints
		tags := api.Group("/tags")
		tags.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			log.Println("[API] Registering Tags routes including Import/Export")
			// Import/Export endpoints for bulk tag management - params must be registered before wildcards if possible (though Gin handles priority)
			tags.POST("/import", middleware.RequireRole(models.RoleAdmin), tagsHandler.ImportTags)
			tags.GET("/export", tagsHandler.ExportTags)
			tags.PUT("/reorder", middleware.RequireRole(models.RoleAdmin), tagsHandler.ReorderTags)

			tags.POST("", middleware.RequireRole(models.RoleAdmin), tagsHandler.Create)
			tags.GET("", tagsHandler.List)
			tags.GET("/:id", tagsHandler.Get)
			tags.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), tagsHandler.Delete)
			tags.PUT("/:id", middleware.RequireRole(models.RoleAdmin), tagsHandler.Update)
			tags.GET("/:id/current", tagsHandler.GetCurrentValue)
		}

		// System endpoints
		system := api.Group("/system")
		system.Use(middleware.RequireAuth)
		{
			system.POST("/reload", middleware.RequireRole(models.RoleAdmin), systemHandler.Reload)
		}

		config := api.Group("/config")
		config.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
		{
			config.GET("/export", systemHandler.ExportConfig)
			config.POST("/import", systemHandler.ImportConfig)
		}

		// Users management endpoints (admin only)
		users := api.Group("/users")
		users.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
		{
			users.GET("", usersHandler.List)
			users.POST("", usersHandler.Create)
			users.PUT("/:id", usersHandler.Update)
			users.DELETE("/:id", usersHandler.Delete)
		}

		// History endpoint (only if InfluxDB is configured)
		if historyHandler != nil {
			history := api.Group("/history")
			history.Use(middleware.RequireAuth, middleware.OrganizationContext())
			{
				history.GET("", historyHandler.Query)
				history.GET("/events", historyHandler.QueryEvents)
			}
		}

		// WebSocket endpoints
		api.GET("/ws/realtime", realtimeHandler.HandleRealtime)
	}

	// Swagger documentation endpoints
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

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
	Status   string `json:"status"`    // "online" or "offline"
	LastSeen int64  `json:"last_seen"` // Unix timestamp in milliseconds
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

// handleDataUpdate processes tag data updates from MQTT
// Payload: TagPayload JSON
func handleDataUpdate(topic string, payload []byte, redisClient *redis.Client) {
	if redisClient == nil {
		return
	}

	var update struct {
		TagID     int         `json:"tag_id"`
		OrgID     int         `json:"org_id"`
		Value     interface{} `json:"v"`
		Timestamp int64       `json:"ts"`
		Quality   int         `json:"q"`
	}

	if err := json.Unmarshal(payload, &update); err != nil {
		// Only log verbose error if needed, avoiding spam
		return
	}

	if update.TagID == 0 {
		return
	}

	// 1. Store in Redis for "Current Value" endpoints
	key := fmt.Sprintf("realtime:%d", update.TagID)
	// Cache for 24h or indefinite? 0 = no expire
	if err := redisClient.Set(key, string(payload), 0); err != nil {
		log.Printf("Error storing realtime value for tag %d: %v", update.TagID, err)
	}

	// 2. Publish to Redis Channel for WebSockets
	// Channel: realtime_updates:{org_id}
	// If OrgID is 0 (missing), we can't route efficiently.
	// But driver-modbus NOW includes OrgID.
	if update.OrgID > 0 {
		channel := fmt.Sprintf("realtime_updates:%d", update.OrgID)
		if err := redisClient.Publish(channel, string(payload)); err != nil {
			log.Printf("Error publishing realtime update for org %d: %v", update.OrgID, err)
		}
	}
}
