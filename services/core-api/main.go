package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
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
	"github.com/ralph/industrial-edge-middleware/internal/notifications"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// notifDispatcher fans alarm events out to email/Telegram/etc. when
// the operator has configured at least one channel. Package-level so
// handleAlarmEvent (which is a free function called from a goroutine)
// can reach it without threading it through every signature.
var notifDispatcher *notifications.Dispatcher

func main() {
	// Structured logging — LOG_FORMAT=json for production (machine-parseable),
	// default is text (human-readable for local dev).
	if os.Getenv("LOG_FORMAT") == "json" {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))
		log.SetFlags(0) // disable timestamp prefix — slog adds it
		log.SetOutput(os.Stdout)
	}

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
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
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
		slog.Warn("failed to connect to Redis — realtime queries unavailable", "error", err)
		redisClient = nil
	} else {
		slog.Info("Redis connected")
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
		slog.Warn("failed to connect to MQTT broker — reload commands unavailable", "error", err)
	} else {
		slog.Info("MQTT connected")
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

		// Subscribe to Alarm updates
		if err := mqttClient.Subscribe("sys/alarms/#", func(topic string, payload []byte) {
			handleAlarmEvent(topic, payload, database)
		}); err != nil {
			log.Printf("Warning: Failed to subscribe to alarms topic: %v", err)
		} else {
			log.Println("Subscribed to alarm events")
		}

		// Subscribe to Sparkplug B topics (dual format support)
		if err := mqttClient.Subscribe("spBv1.0/#", func(topic string, payload []byte) {
			handleSparkplugUpdate(topic, payload, redisClient, database)
		}); err != nil {
			log.Printf("Warning: Failed to subscribe to Sparkplug topic: %v", err)
		} else {
			log.Println("Subscribed to Sparkplug B updates")
		}

		// Subscribe to write commands from external sources (cloud, mobile apps, etc.)
		if err := mqttClient.Subscribe("sys/write/#", func(topic string, payload []byte) {
			handleWriteCommand(topic, payload, database, mqttClient)
		}); err != nil {
			log.Printf("Warning: Failed to subscribe to write command topic: %v", err)
		} else {
			log.Println("Subscribed to write commands from external sources")
		}
	}

	// Connect to cloud MQTT broker for receiving write commands from cloud
	// This is a SECOND connection specifically for cloud broker
	cloudConfig := getCloudMQTTConfig(database)
	var cloudMqttClient *mqtt.Client
	if cloudConfig != nil {
		log.Printf("[CLOUD MQTT] Connecting to cloud broker: %s:%d", cloudConfig.Host, cloudConfig.Port)

		cloudMqttCfg := mqtt.Config{
			Host:          cloudConfig.Host,
			Port:          cloudConfig.Port,
			ClientID:      fmt.Sprintf("core-api-cloud-%d", time.Now().Unix()),
			CleanSession:  true,
			AutoReconnect: true,
			KeepAlive:     30 * time.Second,
			Username:      cloudConfig.Username,
			Password:      cloudConfig.Password,
		}

		cloudMqttClient = mqtt.NewClient(cloudMqttCfg)
		if err := cloudMqttClient.Connect(); err != nil {
			log.Printf("[CLOUD MQTT] Failed to connect to cloud broker: %v", err)
			log.Println("[CLOUD MQTT] Cloud write commands will not be available")
			cloudMqttClient = nil
		} else {
			log.Println("[CLOUD MQTT] Connected to cloud broker successfully")
			defer cloudMqttClient.Disconnect(250)

			// Subscribe to write commands from cloud broker
			cloudWriteTopic := fmt.Sprintf("%s/sys/write/#", cloudConfig.Prefix)
			if err := cloudMqttClient.Subscribe(cloudWriteTopic, func(topic string, payload []byte) {
				// Remove MQTT prefix and handle normally
				// {prefix}/sys/write/do_valvola_1 -> sys/write/do_valvola_1
				cleanTopic := strings.TrimPrefix(topic, cloudConfig.Prefix+"/")
				log.Printf("[CLOUD MQTT] Received write command from cloud: %s -> %s", topic, cleanTopic)
				handleWriteCommand(cleanTopic, payload, database, mqttClient)
			}); err != nil {
				log.Printf("[CLOUD MQTT] Failed to subscribe to cloud write topic: %v", err)
			} else {
				log.Printf("[CLOUD MQTT] Subscribed to cloud write commands: %s", cloudWriteTopic)
			}
		}
	} else {
		log.Println("[CLOUD MQTT] Cloud sync disabled, cloud broker connection skipped")
	}

	log.Println("Industrial Edge Middleware - core-api starting...")

	// Create Gin router
	router := gin.Default()

	// Security headers — applied to every response.
	// HSTS is intentionally omitted until TLS is configured.
	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	})

	// CORS — origins loaded from ALLOWED_ORIGINS env var (comma-separated).
	// Default covers localhost dev ports only; set explicitly for production.
	allowedOrigins := []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		allowedOrigins = strings.Split(raw, ",")
		for i, o := range allowedOrigins {
			allowedOrigins[i] = strings.TrimSpace(o)
		}
	}
	router.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
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
	alarmsHandler := handlers.NewAlarmHandler(database, mqttClient)
	realtimeHandler := handlers.NewRealtimeHandler(redisClient, allowedOrigins)

	// Create settings manager for publish mode configuration
	settingsMgr := settings.NewManager(database)

	// Create history handler with PostgreSQL (no InfluxDB)
	historyHandler := handlers.NewHistoryHandler(database)

	// Initialize TimescaleDB retention policies based on global settings
	historyHandler.InitializeRetentionPolicy()

	systemHandler := handlers.NewSystemHandler(database, mqttClient, settingsMgr, historyHandler)
	notificationsHandler := handlers.NewNotificationsHandler(notifDispatcher)
	diagnosticsHandler := handlers.NewDiagnosticsHandler(database, redisClient)

	// Create auth service and handler
	authService := auth.NewService(database)
	authHandler := handlers.NewAuthHandler(authService)

	// Alarm notification fan-out (email, Telegram, ...). Loads its own
	// settings from global_settings and re-reads them once a minute, so
	// the admin UI's edits take effect without a restart.
	notifDispatcher = notifications.NewDispatcher(database)

	// Create users handler
	usersHandler := handlers.NewUsersHandler(database)

	// Create audit handler
	auditHandler := handlers.NewAuditHandler(database)

	// Register routes
	api := router.Group("/api")
	api.Use(middleware.GlobalRateLimit())
	{
		// Auth endpoints (public)
		auth := api.Group("/auth")
		{
			auth.POST("/login", middleware.LoginRateLimit(), authHandler.Login)
		}
		// Organizations endpoints
		orgs := api.Group("/organizations")
		orgs.Use(middleware.RequireAuth, middleware.OrganizationContext())
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
			gateways.GET("/:id/alarms/count", alarmsHandler.GetGatewayAlarmCounts)
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

			// Alarm configuration per tag
			tags.GET("/:id/alarms", alarmsHandler.GetTagAlarmConfig)
			tags.PUT("/:id/alarms", middleware.RequireRole(models.RoleAdmin), alarmsHandler.SaveTagAlarmConfig)

			tags.POST("", middleware.RequireRole(models.RoleAdmin), tagsHandler.Create)
			tags.GET("", tagsHandler.List)
			tags.GET("/:id", tagsHandler.Get)
			tags.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), tagsHandler.Delete)
			tags.PUT("/:id", middleware.RequireRole(models.RoleAdmin), tagsHandler.Update)
			tags.GET("/:id/current", tagsHandler.GetCurrentValue)
			tags.POST("/:id/write", tagsHandler.Write)
		}

		// Alarms endpoints (Global logic for viewing and acknowledging)
		alarms := api.Group("/alarms")
		alarms.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			alarms.GET("/active", alarmsHandler.GetActiveAlarms)
			alarms.GET("/history", alarmsHandler.GetAlarmHistory)
			alarms.GET("/count/all", alarmsHandler.GetAllAlarmCounts)
			alarms.POST("/:id/ack", middleware.RequireRole(models.RoleAdmin), alarmsHandler.AcknowledgeAlarm)
			alarms.DELETE("/history/all", middleware.RequireRole(models.RoleAdmin), alarmsHandler.DeleteAllAlarmHistory)
			alarms.DELETE("/history/:id", middleware.RequireRole(models.RoleAdmin), alarmsHandler.DeleteAlarmHistory)
		}

		// System endpoints
		system := api.Group("/system")
		system.Use(middleware.RequireAuth)
		{
			system.POST("/reload", middleware.RequireRole(models.RoleAdmin), systemHandler.Reload)
			// GET /system/settings used to be open to any authenticated user AND
			// returned decrypted broker/cloud passwords — a credential leak. Gate
			// to admin only; the handler also masks passwords (returns empty).
			system.GET("/settings", middleware.RequireRole(models.RoleAdmin), systemHandler.GetSettings)
			system.PUT("/settings", middleware.RequireRole(models.RoleAdmin), systemHandler.UpdateSettings)
			system.GET("/metrics", systemHandler.GetMetrics)
			// Fire a synthetic alarm to every configured notification channel.
			// Operator-facing "is my SMTP / Telegram setup actually working?"
			// button — returns per-channel success/error.
			system.POST("/notifications/test", middleware.RequireRole(models.RoleAdmin), notificationsHandler.SendTest)
			// Hardware + service health snapshot. Used by the System /
			// Diagnostics page so an operator can see disk fill, CPU load,
			// link errors and the postgres/redis ping in one place.
			system.GET("/diagnostics", diagnosticsHandler.Get)

			// Backup & Restore
			backupHandler := handlers.NewBackupHandler(database, mqttClient)
			system.GET("/backup", middleware.RequireRole(models.RoleAdmin), backupHandler.ExportBackup)
			system.POST("/restore", middleware.RequireRole(models.RoleAdmin), backupHandler.ImportRestore)
			system.POST("/restore/restart", middleware.RequireRole(models.RoleAdmin), backupHandler.PostRestore)

			// Automatic backup settings
			backupHandler.EnsureTimescaleDBStructures()
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

		// CSV reports — operator-facing exports for history, alarms and the
		// org-wide audit log. Standard ranges + optional tag filter.
		reportsHandler := handlers.NewReportsHandler(database)
		reports := api.Group("/reports")
		reports.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			reports.GET("/history.csv", reportsHandler.HistoryCSV)
			reports.GET("/alarms.csv", reportsHandler.AlarmsCSV)
			// Audit log spans every user across the platform, so it is
			// gated behind global admin only — org admins read their own
			// scoped events through other UI surfaces.
			reports.GET("/audit.csv", middleware.RequireRole(models.RoleAdmin), reportsHandler.AuditCSV)
		}

		// Recipe management — operator loads a named set of (tag, value) pairs
		// in one shot to the PLCs. Scoped to the caller's organization.
		recipesHandler := handlers.NewRecipesHandler(database, mqttClient)
		recipes := api.Group("/recipes")
		recipes.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			recipes.GET("", recipesHandler.List)
			recipes.POST("", middleware.RequireRole(models.RoleAdmin), recipesHandler.Create)
			recipes.GET("/:id", recipesHandler.Get)
			recipes.PUT("/:id", middleware.RequireRole(models.RoleAdmin), recipesHandler.Update)
			recipes.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), recipesHandler.Delete)
			// Load: any authenticated org member can trigger; the run is
			// audited with the actor's user_id + username for accountability.
			recipes.POST("/:id/load", recipesHandler.Load)
			recipes.GET("/:id/runs", recipesHandler.Runs)
		}

		// Dashboard overview — un singolo endpoint che aggrega tutto
		// quello che la pagina dashboard mostra (system / alarms / gateways
		// / operations / activity timeline / KPI). Refresh ogni 30s lato UI.
		dashboardHandler := handlers.NewDashboardHandler(database)
		dashboard := api.Group("/dashboard")
		dashboard.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			dashboard.GET("/overview", dashboardHandler.Overview)
		}

		// History endpoints (PostgreSQL-based)
		history := api.Group("/history")
		history.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			history.GET("", historyHandler.Query)
			history.GET("/stats", historyHandler.GetTagStats)
			history.GET("/events", historyHandler.QueryEvents)
			history.GET("/data-range", historyHandler.GetDataRange)
		}

		// AI-Ops endpoints (read-only, consumed by Paperclip agents)
		aiopsHandler := handlers.NewAIopsHandler(database)
		aiops := api.Group("/aiops")
		aiops.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			aiops.GET("/summary",       aiopsHandler.GetOrgSummary)
			aiops.GET("/anomalies",     aiopsHandler.GetTagAnomalies)
			aiops.GET("/alarms/digest", aiopsHandler.GetAlarmDigest)
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

		// i3X Access API (CESMII standard) – read/write industrial data via
		// a vendor-neutral REST interface compatible with CESMII i3X v1 spec.
		i3xHandler := handlers.NewI3XHandler(database, mqttClient, redisClient)
		i3x := api.Group("/i3x/v1")
		i3x.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			i3x.GET("/equipment", i3xHandler.ListEquipment)
			i3x.GET("/equipment/:id", i3xHandler.GetEquipment)
			i3x.GET("/equipment/:id/properties", i3xHandler.ListEquipmentProperties)
			i3x.GET("/equipment/:id/properties/:propId", i3xHandler.GetEquipmentProperty)

			i3x.GET("/properties", i3xHandler.ListProperties)
			i3x.GET("/properties/:id", i3xHandler.GetProperty)
			i3x.PUT("/properties/:id/value", i3xHandler.WritePropertyValue)

			i3x.GET("/alarms", i3xHandler.ListAlarms)
			i3x.GET("/alarms/history", i3xHandler.ListAlarmHistory)
		}
	}

	// Swagger — enabled only when SWAGGER_ENABLED=true (never in production)
	if os.Getenv("SWAGGER_ENABLED") == "true" {
		router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		log.Println("[WARNING] Swagger UI is enabled — disable in production (SWAGGER_ENABLED=true)")
	}

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

// handleAlarmEvent processes alarm state changes from MQTT drivers.
// Beyond persisting the event, it fans the same event out to the
// notification dispatcher so configured channels (email, Telegram, ...)
// can reach the operator out-of-band.
func handleAlarmEvent(topic string, payload []byte, db *sql.DB) {
	log.Printf("[ALARM] Received event - topic: %s", topic)

	if db == nil {
		log.Printf("[ALARM] Database is nil, cannot store event")
		return
	}

	var event struct {
		TagID          int     `json:"tag_id"`
		DefinitionID   int     `json:"definition_id"`
		Status         string  `json:"status"` // "ACTIVE", "CLEARED"
		AlarmType      string  `json:"alarm_type"`
		Severity       string  `json:"severity"`
		Message        string  `json:"message"`
		ValueAtTrigger float64 `json:"value_at_trigger"`
		Threshold      float64 `json:"threshold"`
		TagAlias       string  `json:"tag_alias"`
		Timestamp      int64   `json:"timestamp"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		log.Printf("[ALARM] Failed to parse payload: %v", err)
		return
	}

	eventTime := time.UnixMilli(event.Timestamp)
	var insertedID int

	if event.Status == "ACTIVE" {
		err := db.QueryRow(`
			INSERT INTO alarm_events
				(tag_id, definition_id, status, alarm_type, severity, message, value_at_trigger, trigger_time)
			VALUES
				($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id
		`, event.TagID, event.DefinitionID, "ACTIVE", event.AlarmType, event.Severity, event.Message, event.ValueAtTrigger, eventTime).
			Scan(&insertedID)
		if err != nil {
			log.Printf("[ALARM] Failed to insert trigger event: %v", err)
		}
	} else if event.Status == "CLEARED" {
		_, err := db.Exec(`
			UPDATE alarm_events
			SET status = 'CLEARED', clear_time = $1
			WHERE definition_id = $2
			  AND tag_id = $3
			  AND clear_time IS NULL
		`, eventTime, event.DefinitionID, event.TagID)
		if err != nil {
			log.Printf("[ALARM] Failed to update clear event: %v", err)
		}
	}

	// Fan-out to email / Telegram / ... — non-blocking; the dispatcher
	// runs the actual send in a goroutine and applies its own rate-
	// limiting + severity filter. Tag alias falls back to a synthetic
	// "tag #<id>" so the notification is still readable when the driver
	// didn't include the alias.
	if notifDispatcher != nil {
		alias := event.TagAlias
		if alias == "" {
			alias = fmt.Sprintf("tag #%d", event.TagID)
		}
		notifDispatcher.Dispatch(notifications.Event{
			AlarmID:     insertedID,
			TagAlias:    alias,
			Severity:    event.Severity,
			Status:      event.Status,
			Threshold:   event.Threshold,
			Value:       event.ValueAtTrigger,
			Description: event.Message,
			OccurredAt:  eventTime.UTC(),
		})
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

// handleWriteCommand handles write commands from external sources (cloud, mobile apps, etc.)
func handleWriteCommand(topic string, payload []byte, db *sql.DB, mqttClient *mqtt.Client) {
	log.Printf("[WRITE CMD] Received write command on topic: %s", topic)
	log.Printf("[WRITE CMD] Payload: %s", string(payload))

	// Parse the topic: sys/write/{orgID}/{site}/{area}/{gateway}/{tagAlias}
	// OR: {prefix}/.../{tagAlias} (legacy format)
	parts := strings.Split(topic, "/")
	if len(parts) < 2 {
		log.Printf("[WRITE CMD] Invalid topic format, ignoring")
		return
	}

	// Extract tag alias (last part)
	tagAlias := parts[len(parts)-1]
	log.Printf("[WRITE CMD] Tag alias: %s", tagAlias)

	// Parse the payload - support multiple formats
	// Format 1: {"value": x, "timestamp": y}
	// Format 2 (legacy): {"v": x, "ts": y}
	// Format 3: {"value": x} (timestamp optional)
	var writeRequest struct {
		Value     interface{} `json:"value,omitempty"`
		V         interface{} `json:"v,omitempty"`     // Legacy format
		Timestamp int64       `json:"timestamp,omitempty"`
		TS        int64       `json:"ts,omitempty"`        // Legacy format
	}

	if err := json.Unmarshal(payload, &writeRequest); err != nil {
		log.Printf("[WRITE CMD] Failed to parse payload: %v", err)
		return
	}

	// Extract value (try both "value" and "v" for legacy support)
	value := writeRequest.Value
	if value == nil {
		value = writeRequest.V
	}

	if value == nil {
		log.Printf("[WRITE CMD] No value found in payload")
		return
	}

	log.Printf("[WRITE CMD] Write value: %v (type: %T)", value, value)

	// Query database to find tag details
	var tag struct {
		ID        int
		GatewayID int
		Code      string
		DataType  string
	}

	// First try to find by exact alias match (case-insensitive)
	query := `
		SELECT t.id, t.gateway_id, t.code, t.data_type
		FROM tags t
		WHERE LOWER(t.alias) = LOWER($1)
	`

	err := db.QueryRow(query, tagAlias).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.DataType)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("[WRITE CMD] Tag not found with alias: %s", tagAlias)

			// Try with unslugified alias (replace - with space and vice versa)
			unslugified := strings.ReplaceAll(tagAlias, "-", " ")
			slugified := strings.ReplaceAll(strings.ToLower(tagAlias), " ", "-")

			// Try slugified version
			err = db.QueryRow(query, slugified).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.DataType)
			if err != nil {
				// Try unslugified version
				err = db.QueryRow(query, unslugified).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.DataType)
				if err != nil {
					log.Printf("[WRITE CMD] Tag not found with slugified/unslugified alias either")
					return
				}
			}
			log.Printf("[WRITE CMD] Found tag with slugified alias: %s", slugified)
		} else {
			log.Printf("[WRITE CMD] Database error: %v", err)
			return
		}
	}

	log.Printf("[WRITE CMD] Found tag - ID: %d, Gateway: %d, Code: %s, Type: %s",
		tag.ID, tag.GatewayID, tag.Code, tag.DataType)

	// Build the command payload for the driver
	cmd := struct {
		TagID    int         `json:"tag_id"`
		Code     string      `json:"code"`
		Value    interface{} `json:"value"`
		DataType string      `json:"data_type"`
	}{
		TagID:    tag.ID,
		Code:     tag.Code,
		Value:    value,
		DataType: tag.DataType,
	}

	cmdPayload, err := json.Marshal(cmd)
	if err != nil {
		log.Printf("[WRITE CMD] Failed to marshal command: %v", err)
		return
	}

	// Publish to the driver on sys/command/write/{gatewayID}
	driverTopic := fmt.Sprintf("sys/command/write/%d", tag.GatewayID)
	if err := mqttClient.Publish(driverTopic, string(cmdPayload)); err != nil {
		log.Printf("[WRITE CMD] Failed to publish to driver: %v", err)
		return
	}

	log.Printf("[WRITE CMD] Successfully sent write command to driver on topic: %s, payload: %s", driverTopic, string(cmdPayload))
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

// getCloudMQTTPrefix retrieves the cloud MQTT prefix from system settings
// Uses the same prefix configured for Cloud Sync (cloud_mqtt_topic)
// Extracts the prefix from the topic (e.g. "sorical/data/" -> "sorical")
// Returns empty string if cloud sync is disabled or no topic is configured
func getCloudMQTTPrefix(db *sql.DB) string {
	// Check if cloud sync is enabled
	var cloudSyncEnabled string
	err := db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_sync_enabled'").Scan(&cloudSyncEnabled)
	if err != nil || cloudSyncEnabled != "true" {
		log.Printf("[CLOUD MQTT] Cloud sync disabled, cloud write subscription skipped")
		return ""
	}

	// Get the cloud MQTT topic (e.g. "sorical/data/" or "sorical/spBv1.0/")
	var cloudTopic string
	err = db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_mqtt_topic'").Scan(&cloudTopic)
	if err != nil {
		log.Printf("[CLOUD MQTT] No cloud topic configured, cloud write subscription skipped: %v", err)
		return ""
	}

	if cloudTopic == "" {
		log.Printf("[CLOUD MQTT] Cloud topic is empty, cloud write subscription skipped")
		return ""
	}

	// Extract prefix from topic (e.g. "sorical/data/" -> "sorical")
	// Split by "/" and take the first part
	parts := strings.Split(strings.Trim(cloudTopic, "/"), "/")
	if len(parts) == 0 {
		log.Printf("[CLOUD MQTT] Invalid cloud topic format, cloud write subscription skipped")
		return ""
	}

	prefix := parts[0]
	log.Printf("[CLOUD MQTT] Using prefix from cloud topic: '%s' (from topic: '%s')", prefix, cloudTopic)
	return prefix
}

// CloudMQTTConfig holds the cloud broker connection settings
type CloudMQTTConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Prefix   string
}

// getCloudMQTTConfig retrieves all cloud MQTT settings from system settings
// Returns the configuration if cloud sync is enabled, otherwise returns nil
func getCloudMQTTConfig(db *sql.DB) *CloudMQTTConfig {
	// Check if cloud sync is enabled
	var cloudSyncEnabled string
	err := db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_sync_enabled'").Scan(&cloudSyncEnabled)
	if err != nil || cloudSyncEnabled != "true" {
		return nil
	}

	// Get all cloud MQTT settings
	var host, username, password, topic string
	var port int

	err = db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_mqtt_host'").Scan(&host)
	if err != nil {
		return nil
	}

	err = db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_mqtt_port'").Scan(&port)
	if err != nil {
		port = 1883 // Default port
	}

	err = db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_mqtt_username'").Scan(&username)
	if err != nil {
		username = ""
	}

	err = db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_mqtt_password'").Scan(&password)
	if err != nil {
		password = ""
	}

	err = db.QueryRow("SELECT value FROM global_settings WHERE key = 'cloud_mqtt_topic'").Scan(&topic)
	if err != nil || topic == "" {
		return nil
	}

	// Extract prefix from topic
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	if len(parts) == 0 {
		return nil
	}

	return &CloudMQTTConfig{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		Prefix:   parts[0],
	}
}
