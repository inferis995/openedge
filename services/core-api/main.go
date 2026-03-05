package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/ralph/industrial-edge-middleware/docs"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/handlers"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
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

	// Historian uses PostgreSQL (tag_history table) - no InfluxDB needed
	log.Println("Using PostgreSQL for historian storage")

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

		// Subscribe to Sparkplug B topics (dual format support)
		if err := mqttClient.Subscribe("spBv1.0/#", func(topic string, payload []byte) {
			handleSparkplugUpdate(topic, payload, redisClient, database)
		}); err != nil {
			log.Printf("Warning: Failed to subscribe to Sparkplug topic: %v", err)
		} else {
			log.Println("Subscribed to Sparkplug B updates")
		}
	}

	log.Println("Industrial Edge Middleware - core-api starting...")

	// Create Gin router
	router := gin.Default()

	// CORS Configuration
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:3004", "http://127.0.0.1:3004", "http://localhost:4000", "http://127.0.0.1:4000", "http://localhost:9090", "http://127.0.0.1:9090", "http://100.97.150.10:9090", "http://100.97.150.10:8081"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Organization-ID"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Create handlers with MQTT client and Redis client
	orgsHandler := handlers.NewOrganizationsHandler(database, mqttClient)
	sitesHandler := handlers.NewSitesHandler(database, mqttClient)
	areasHandler := handlers.NewAreasHandler(database, mqttClient)
	gatewaysHandler := handlers.NewGatewaysHandler(database, mqttClient, redisClient)
	tagsHandler := handlers.NewTagsHandler(database, mqttClient, redisClient)
	realtimeHandler := handlers.NewRealtimeHandler(redisClient)

	// Create settings manager for publish mode configuration
	settingsMgr := settings.NewManager(database)
	systemHandler := handlers.NewSystemHandler(database, mqttClient, settingsMgr)

	// Create history handler with PostgreSQL (no InfluxDB)
	historyHandler := handlers.NewHistoryHandler(database)

	// Create auth service and handler
	authService := auth.NewService(database)
	authHandler := handlers.NewAuthHandler(authService)

	// Create users handler
	usersHandler := handlers.NewUsersHandler(database)

	// Create audit handler
	auditHandler := handlers.NewAuditHandler(database)

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
			gateways.POST("/:id/browse", middleware.RequireRole(models.RoleAdmin), gatewaysHandler.BrowseNodes)
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

			// Tag hierarchy endpoints for trend page
			tags.GET("/hierarchy", tagsHandler.GetHierarchy)
			tags.GET("/with-hierarchy", tagsHandler.ListWithHierarchy)

			tags.POST("", middleware.RequireRole(models.RoleAdmin), tagsHandler.Create)
			tags.GET("", tagsHandler.List)
			tags.GET("/:id", tagsHandler.Get)
			tags.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), tagsHandler.Delete)
			tags.PUT("/:id", middleware.RequireRole(models.RoleAdmin), tagsHandler.Update)
			tags.GET("/:id/current", tagsHandler.GetCurrentValue)
			tags.POST("/:id/write", tagsHandler.Write)
		}

		// System endpoints
		system := api.Group("/system")
		system.Use(middleware.RequireAuth)
		{
			system.POST("/reload", middleware.RequireRole(models.RoleAdmin), systemHandler.Reload)
			system.GET("/settings", systemHandler.GetSettings)
			system.PUT("/settings", middleware.RequireRole(models.RoleAdmin), systemHandler.UpdateSettings)
			system.GET("/metrics", systemHandler.GetMetrics)

			// Backup & Restore
			backupHandler := handlers.NewBackupHandler(database, mqttClient)
			system.GET("/backup", middleware.RequireRole(models.RoleAdmin), backupHandler.ExportBackup)
			system.POST("/restore", middleware.RequireRole(models.RoleAdmin), backupHandler.ImportRestore)

			// Automatic backup settings
			system.GET("/backup/settings", backupHandler.GetBackupSettings)
			system.PUT("/backup/settings", middleware.RequireRole(models.RoleAdmin), backupHandler.UpdateBackupSettings)
			system.GET("/backup/list", backupHandler.ListBackups)
			system.GET("/backup/files/:filename", middleware.RequireRole(models.RoleAdmin), backupHandler.DownloadBackup)
			system.DELETE("/backup/files/:filename", middleware.RequireRole(models.RoleAdmin), backupHandler.DeleteBackup)

			// Start backup scheduler
			go startBackupScheduler(backupHandler)
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

		// History endpoints (PostgreSQL-based)
		history := api.Group("/history")
		history.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			history.GET("", historyHandler.Query)
			history.GET("/stats", historyHandler.GetTagStats)
			history.GET("/events", historyHandler.QueryEvents)
		}

		// Audit endpoints
		audit := api.Group("/audit")
		audit.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			audit.GET("/logs", auditHandler.GetAuditLogs)
			audit.GET("/actions", auditHandler.GetAuditActions)
		}

		// WebSocket endpoints
		api.GET("/ws/realtime", realtimeHandler.HandleRealtime)
	}

	// Swagger documentation endpoints
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Health check endpoints (unauthenticated, for Docker/Kubernetes probes)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
	router.GET("/ready", func(c *gin.Context) {
		// Check database connection
		if err := database.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "not ready",
				"error":  "database unavailable",
			})
			return
		}
		// Check Redis connection (optional - service can run without Redis)
		redisStatus := "available"
		if redisClient == nil {
			redisStatus = "unavailable"
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ready",
			"checks": gin.H{
				"database": "available",
				"redis":    redisStatus,
			},
		})
	})

	// Start server with graceful shutdown
	port := getEnv("PORT", "8080")
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on port %s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Give outstanding requests 30 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
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
	log.Printf("[HEALTH] Received health update - topic: %s, payload: %s", topic, string(payload))

	if redisClient == nil {
		log.Printf("[HEALTH] Redis client is nil, cannot store health status")
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

// handleSparkplugUpdate processes Sparkplug B updates from MQTT
// Topic format: spBv1.0/{group_id}/{message_type}/{edge_node_id}/{device_id}
func handleSparkplugUpdate(topic string, payload []byte, redisClient *redis.Client, db *sql.DB) {
	log.Printf("[SPARKPLUG] Received topic: %s", topic)
	if redisClient == nil {
		log.Printf("[SPARKPLUG] Redis client is nil, returning")
		return
	}

	// Check if this is a Sparkplug B topic
	if !sparkplug.IsSparkplugTopic(topic) {
		log.Printf("[SPARKPLUG] Not a Sparkplug topic: %s", topic)
		return
	}

	// Parse Sparkplug B topic
	topicInfo, err := sparkplug.ParseTopic(topic)
	if err != nil {
		log.Printf("[SPARKPLUG] Failed to parse topic: %v", err)
		return
	}

	log.Printf("[SPARKPLUG] Parsed topic - Group: %s, Type: %s, Node: %s, Device: %s",
		topicInfo.GroupID, topicInfo.MessageType, topicInfo.EdgeNodeID, topicInfo.DeviceID)

	// Only process DDATA (data) messages
	if topicInfo.MessageType != sparkplug.MessageTypeDDATA {
		log.Printf("[SPARKPLUG] Not a DDATA message (type=%s), skipping", topicInfo.MessageType)
		return
	}

	// Try to parse as JSON payload (simplified Sparkplug B format)
	var sparkplugPayload struct {
		Timestamp int64 `json:"Timestamp"`
		Seq       int   `json:"Seq"`
		Metrics   []struct {
			Name      string      `json:"Name"`
			DataType  int         `json:"DataType"`
			Value     interface{} `json:"Value"`
			Timestamp int64       `json:"Timestamp"`
			Quality   int         `json:"Quality"`
		} `json:"Metrics"`
	}

	if err := json.Unmarshal(payload, &sparkplugPayload); err != nil {
		log.Printf("[SPARKPLUG] Failed to parse JSON payload: %v", err)
		return
	}

	log.Printf("[SPARKPLUG] Parsed payload with %d metrics", len(sparkplugPayload.Metrics))

	// Process each metric
	for _, metric := range sparkplugPayload.Metrics {
		// Convert quality from Sparkplug to legacy format
		legacyQuality := sparkplug.ConvertSparkplugToLegacyQuality(int32(metric.Quality))

		// Use metric timestamp or payload timestamp
		timestamp := metric.Timestamp
		if timestamp == 0 {
			timestamp = sparkplugPayload.Timestamp
		}
		if timestamp == 0 {
			timestamp = time.Now().UnixMilli()
		}

		log.Printf("[SPARKPLUG] Processing metric: %s (value: %v, quality: %d)",
			metric.Name, metric.Value, metric.Quality)

		// Try to find tag by alias
		tagID, orgID := findTagBySparkplugPath(topicInfo, metric.Name, redisClient, db)
		if tagID == 0 {
			log.Printf("[SPARKPLUG] Tag not found for alias: %s", metric.Name)
			continue
		}

		log.Printf("[SPARKPLUG] Found tag ID: %d, org ID: %d", tagID, orgID)

		// Build the legacy payload format
		legacyPayload := map[string]interface{}{
			"tag_id": tagID,
			"org_id": orgID,
			"v":      metric.Value,
			"ts":     timestamp,
			"q":      legacyQuality,
		}
		payloadBytes, _ := json.Marshal(legacyPayload)

		// Store current value in Redis for real-time queries
		realtimeKey := fmt.Sprintf("realtime:%d", tagID)
		redisClient.Set(realtimeKey, string(payloadBytes), 0)

		// Broadcast real-time update via Redis Pub/Sub
		channel := fmt.Sprintf("realtime_updates:%d", orgID)
		redisClient.Publish(channel, string(payloadBytes))
		log.Printf("[SPARKPLUG] Published realtime update for tag %d to channel %s", tagID, channel)
	}
}

// findTagBySparkplugPath attempts to find a tag ID from Sparkplug path
// Returns (tagID, orgID) or (0, 0) if not found
func findTagBySparkplugPath(topicInfo *sparkplug.TopicInfo, metricName string, redisClient *redis.Client, db *sql.DB) (int, int) {
	// Use metric name as tag alias (it's the unslugified version)
	alias := metricName
	if alias == "" {
		// Fallback to device_id, unslugify it
		alias = strings.ReplaceAll(topicInfo.DeviceID, "-", " ")
	}

	// Try to get from cache first using just the alias
	cacheKey := fmt.Sprintf("tag_by_alias:%s", alias)
	cached, err := redisClient.Get(cacheKey)
	if err == nil && cached != "" {
		var tagInfo struct {
			ID             int `json:"ID"`
			OrganizationID int `json:"OrganizationID"`
		}
		if err := json.Unmarshal([]byte(cached), &tagInfo); err == nil {
			return tagInfo.ID, tagInfo.OrganizationID
		}
	}

	// Also try the slugified version as cache key
	slugAlias := strings.ReplaceAll(strings.ToLower(alias), " ", "-")
	cacheKeySlug := fmt.Sprintf("tag_by_alias:%s", slugAlias)
	cached, err = redisClient.Get(cacheKeySlug)
	if err == nil && cached != "" {
		var tagInfo struct {
			ID             int `json:"ID"`
			OrganizationID int `json:"OrganizationID"`
		}
		if err := json.Unmarshal([]byte(cached), &tagInfo); err == nil {
			return tagInfo.ID, tagInfo.OrganizationID
		}
	}

	// Not found in cache - query database
	if db != nil {
		var tagID, orgID int
		query := `
			SELECT t.id, o.id
			FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			JOIN organizations o ON s.org_id = o.id
			WHERE LOWER(t.alias) = LOWER($1)
		`
		err := db.QueryRow(query, alias).Scan(&tagID, &orgID)
		if err == nil {
			// Cache the result for future lookups
			tagInfo := struct {
				ID             int `json:"ID"`
				OrganizationID int `json:"OrganizationID"`
			}{
				ID:             tagID,
				OrganizationID: orgID,
			}
			if cachedBytes, err := json.Marshal(tagInfo); err == nil {
				redisClient.Set(cacheKey, string(cachedBytes), 5*time.Minute)
			}
			return tagID, orgID
		}
		// Also try with slugified alias
		err = db.QueryRow(query, slugAlias).Scan(&tagID, &orgID)
		if err == nil {
			return tagID, orgID
		}
	}

	return 0, 0
}

// startBackupScheduler runs a background goroutine that checks for scheduled backups
func startBackupScheduler(backupHandler *handlers.BackupHandler) {
	ticker := time.NewTicker(1 * time.Hour) // Check every hour
	defer ticker.Stop()

	log.Println("[BACKUP-SCHEDULER] Started - checking every hour")

	for range ticker.C {
		// Check if we need to run a backup
		backupHandler.RunScheduledBackup()
	}
}
