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
	"sync"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/ralph/industrial-edge-middleware/docs"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
	"github.com/ralph/industrial-edge-middleware/internal/connectors"
	"github.com/ralph/industrial-edge-middleware/internal/crypto"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/handlers"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/notifications"
	"github.com/ralph/industrial-edge-middleware/internal/redis"
	"github.com/ralph/industrial-edge-middleware/internal/scaling"
	"github.com/ralph/industrial-edge-middleware/internal/settings"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
	otSync "github.com/ralph/industrial-edge-middleware/internal/sync"
	"github.com/ralph/industrial-edge-middleware/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// notifDispatcher fans alarm events out to email/Telegram/etc. when
// the operator has configured at least one channel. Package-level so
// handleAlarmEvent (which is a free function called from a goroutine)
// can reach it without threading it through every signature.
var notifDispatcher *notifications.Dispatcher
var influxWriter *connectors.InfluxDBConnector

// scalingCache maps tag_id → scaling.Config for tags that have EU scaling
// enabled. Loaded at startup and refreshed every 30 s so config changes
// from the UI take effect without a restart.
var scalingCache sync.Map // map[int]scaling.Config

func loadScalingCache(database *sql.DB) {
	rows, err := database.Query(
		`SELECT id, scaling_raw_min, scaling_raw_max, scaling_eu_min, scaling_eu_max, scaling_clamp, invert
		 FROM tags WHERE scaling_enabled = true`,
	)
	if err != nil {
		log.Printf("[scaling] cache load error: %v", err)
		return
	}
	defer rows.Close()

	// Build a temporary map, then swap into the sync.Map atomically.
	newEntries := map[int]scaling.Config{}
	for rows.Next() {
		var id int
		var cfg scaling.Config
		cfg.Enabled = true
		if err := rows.Scan(&id, &cfg.RawMin, &cfg.RawMax, &cfg.EuMin, &cfg.EuMax, &cfg.Clamp, &cfg.Invert); err != nil {
			log.Printf("[scaling] scan error: %v", err)
			continue
		}
		newEntries[id] = cfg
	}

	// Delete stale entries.
	scalingCache.Range(func(k, _ interface{}) bool {
		if _, keep := newEntries[k.(int)]; !keep {
			scalingCache.Delete(k)
		}
		return true
	})
	// Store new/updated entries.
	for id, cfg := range newEntries {
		scalingCache.Store(id, cfg)
	}
	log.Printf("[scaling] cache refreshed: %d scaled tag(s)", len(newEntries))
}

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

	// Secrets stored in global_settings (MQTT broker and cloud-connector
	// passwords) are AES-GCM encrypted only when ENCRYPTION_KEY is a valid
	// 32-byte key. Without it the settings handler falls back to storing them
	// in cleartext, so say it once, loudly, at boot — otherwise an operator
	// only finds out by reading the database.
	if err := crypto.ValidateKey(); err != nil {
		slog.Warn("ENCRYPTION_KEY is not usable — credentials in global_settings will be stored UNENCRYPTED", "error", err)
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
	database.SetMaxOpenConns(25)
	database.SetMaxIdleConns(10)
	database.SetConnMaxLifetime(5 * time.Minute)

	// Load EU scaling config once at startup, then refresh every 30 s.
	go func() {
		loadScalingCache(database)
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			loadScalingCache(database)
		}
	}()

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
	mqttUser := getEnv("MQTT_USERNAME", "")
	mqttPass := getEnv("MQTT_PASSWORD", "")

	mqttCfg := mqtt.Config{
		Host:          mqttHost,
		Port:          mqttPort,
		ClientID:      mqttClientID,
		Username:      mqttUser,
		Password:      mqttPass,
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
			slog.Error("CRITICAL: failed to subscribe to gateway health topic — edge status tracking disabled", "error", err)
		} else {
			log.Println("Subscribed to gateway health status updates")
		}

		// Subscribe to data updates
		if err := mqttClient.Subscribe("data/#", func(topic string, payload []byte) {
			handleDataUpdate(topic, payload, redisClient)
		}); err != nil {
			slog.Error("CRITICAL: failed to subscribe to data topic — all tag writes will be lost", "error", err)
		} else {
			log.Println("Subscribed to data updates")
		}

		// Subscribe to Alarm updates
		if err := mqttClient.Subscribe("sys/alarms/#", func(topic string, payload []byte) {
			handleAlarmEvent(topic, payload, database)
		}); err != nil {
			slog.Error("CRITICAL: failed to subscribe to alarms topic — alarm events will not be persisted", "error", err)
		} else {
			log.Println("Subscribed to alarm events")
		}

		// Subscribe to Sparkplug B topics (dual format support)
		if err := mqttClient.Subscribe("spBv1.0/#", func(topic string, payload []byte) {
			handleSparkplugUpdate(topic, payload, redisClient, database)
		}); err != nil {
			slog.Error("CRITICAL: failed to subscribe to Sparkplug topic — Sparkplug B data ignored", "error", err)
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

		// Subscribe to edge manager heartbeats: sys/edge/{org_id}/ping
		// Edge manager publishes every 30 s; we store the timestamp in Redis
		// with a 90 s TTL so the edge-status endpoint can detect offline edges.
		if err := mqttClient.Subscribe("sys/edge/#", func(topic string, payload []byte) {
			// topic format: sys/edge/{org_id}/ping
			parts := strings.Split(topic, "/")
			if len(parts) == 4 && parts[3] == "ping" {
				handlers.HandleEdgePing(context.Background(), parts[2], redisClient)
			}
		}); err != nil {
			log.Printf("Warning: Failed to subscribe to edge ping topic: %v", err)
		} else {
			log.Println("Subscribed to edge heartbeats")
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

	// Wire up Mosquitto Dynamic Security client (requires MQTT_USERNAME to be the admin user)
	var dynsecClient *mqtt.DynsecClient
	if mqttUser != "" {
		dynsecClient = mqtt.NewDynsecClient(mqttClient)
		slog.Info("Mosquitto Dynamic Security client ready")
	}

	// Health check handler (declared early — used both in unauthenticated /health routes and inside api group)
	healthH := handlers.NewHealthHandler(database, redisClient)

	// Create handlers with MQTT client and Redis client
	orgsHandler := handlers.NewOrganizationsHandler(database, mqttClient, dynsecClient)
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
	authHandler := handlers.NewAuthHandler(authService, database)

	// Alarm notification fan-out (email, Telegram, ...). Loads its own
	// settings from global_settings and re-reads them once a minute, so
	// the admin UI's edits take effect without a restart.
	notifDispatcher = notifications.NewDispatcher(database)
	// Wire la verifica maintenance window: il dispatcher silenzia le
	// notifiche durante una finestra attiva ma continua a registrare
	// l'allarme in DB. Closure cattura il db handle (handlers.IsInMaintenance
	// resta puro — niente package state).
	notifDispatcher.MaintenanceCheck = func() bool {
		return handlers.IsInMaintenance(database)
	}

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
		// Authenticated user self-service
		authMe := api.Group("/auth")
		authMe.Use(middleware.RequireAuth)
		{
			authMe.GET("/me", authHandler.Me)
			authMe.PUT("/me/password", authHandler.ChangePassword)
			authMe.POST("/logout", authHandler.Logout)
			authMe.GET("/mfa/status", authHandler.MFAStatus)
			authMe.POST("/mfa/setup", authHandler.MFASetup)
			authMe.POST("/mfa/enable", authHandler.MFAEnable)
			authMe.DELETE("/mfa/disable", authHandler.MFADisable)
			authMe.POST("/mfa/recovery-codes", authHandler.MFARegenerateCodes)
		}
		// Organizations endpoints
		orgs := api.Group("/organizations")
		orgs.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			orgs.POST("", middleware.RequireRole(models.RoleAdmin), orgsHandler.Create)
			orgs.GET("", orgsHandler.List)
			orgs.GET("/:id", orgsHandler.Get)
			orgs.PUT("/:id", middleware.RequireRole(models.RoleAdmin), orgsHandler.Update)
			orgs.PUT("/:id/mfa-required", middleware.RequireRole(models.RoleAdmin), orgsHandler.SetMFARequired)
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
			// LoRaWAN device management
			lorawanHandler := handlers.NewLoRaWANHandler(database, mqttClient)
			gateways.GET("/:id/lorawan/devices", lorawanHandler.ListDevices)
			gateways.POST("/:id/lorawan/devices/import", middleware.RequireRole(models.RoleAdmin), lorawanHandler.ImportTags)
			gateways.POST("/:id/lorawan/downlink", middleware.RequireRole(models.RoleAdmin), lorawanHandler.Downlink)
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
			tags.GET("/:id/shadow", tagsHandler.GetShadow)
			tags.GET("/shadows", tagsHandler.GetShadowBatch)
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
			system.GET("/backup", middleware.RequireGlobalAdmin(), backupHandler.ExportBackup)
			system.POST("/restore", middleware.RequireGlobalAdmin(), backupHandler.ImportRestore)
			system.POST("/restore/restart", middleware.RequireGlobalAdmin(), backupHandler.PostRestore)

			// Automatic backup settings
			backupHandler.EnsureTimescaleDBStructures()
			system.GET("/backup/settings", middleware.RequireGlobalAdmin(), backupHandler.GetBackupSettings)
			system.PUT("/backup/settings", middleware.RequireGlobalAdmin(), backupHandler.UpdateBackupSettings)
			system.GET("/backup/list", middleware.RequireGlobalAdmin(), backupHandler.ListBackups)
			system.GET("/backup/files/:filename", middleware.RequireGlobalAdmin(), backupHandler.DownloadBackup)
			system.DELETE("/backup/files/:filename", middleware.RequireGlobalAdmin(), backupHandler.DeleteBackup)
			system.GET("/backup/catalog", middleware.RequireGlobalAdmin(), backupHandler.GetCatalog)
			system.GET("/backup/audit", middleware.RequireGlobalAdmin(), backupHandler.GetAudit)

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
			reports.GET("/audit.csv", middleware.RequireGlobalAdmin(), reportsHandler.AuditCSV)
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

		// Synoptics — SCADA mimic pages. Reads open to any org member
		// (operators view the live plant); writes restricted to admins
		// (the designer is an engineering task).
		synopticsHandler := handlers.NewSynopticsHandler(database)
		synoptics := api.Group("/synoptics")
		synoptics.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			synoptics.GET("", synopticsHandler.List)
			synoptics.GET("/:id", synopticsHandler.Get)
			synoptics.POST("", middleware.RequireRole(models.RoleAdmin), synopticsHandler.Create)
			synoptics.PUT("/:id", middleware.RequireRole(models.RoleAdmin), synopticsHandler.Update)
			synoptics.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), synopticsHandler.Delete)
		}

		// Shifts (turni): definizione + assegnazione operatori + detection
		// turno corrente. Le rotte di lettura sono accessibili a chiunque
		// sia autenticato (utili sia per admin che per operatore), le
		// scritture sono ristrette a admin.
		shiftsHandler := handlers.NewShiftsHandler(database)
		shifts := api.Group("/shifts")
		shifts.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			shifts.GET("", shiftsHandler.List)
			shifts.GET("/current", shiftsHandler.Current)
			shifts.POST("", middleware.RequireRole(models.RoleAdmin), shiftsHandler.Create)
			shifts.PUT("/:id", middleware.RequireRole(models.RoleAdmin), shiftsHandler.Update)
			shifts.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), shiftsHandler.Delete)
			shifts.GET("/:id/assignments", shiftsHandler.ListAssignments)
			shifts.POST("/:id/assignments", middleware.RequireRole(models.RoleAdmin), shiftsHandler.CreateAssignment)
			shifts.DELETE("/assignments/:aid", middleware.RequireRole(models.RoleAdmin), shiftsHandler.DeleteAssignment)
		}

		// Custom KPIs (metriche di produzione definite dall'operatore).
		// Lettura per tutti gli autenticati (i KPI custom appaiono nella
		// dashboard di chiunque), scritture admin-only.
		customKPIsHandler := handlers.NewCustomKPIsHandler(database)
		customKPIs := api.Group("/custom-kpis")
		customKPIs.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			customKPIs.GET("", customKPIsHandler.List)
			customKPIs.POST("", middleware.RequireRole(models.RoleAdmin), customKPIsHandler.Create)
			customKPIs.PUT("/:id", middleware.RequireRole(models.RoleAdmin), customKPIsHandler.Update)
			customKPIs.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), customKPIsHandler.Delete)
		}

		// Maintenance windows (finestre di manutenzione programmata).
		// Lettura per chiunque sia autenticato (la dashboard mostra il
		// badge "manutenzione in corso"), scritture admin-only. Quando
		// una finestra è attiva il notifier silenzia email/Telegram.
		maintenanceHandler := handlers.NewMaintenanceHandler(database)
		maintenance := api.Group("/maintenance")
		maintenance.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			maintenance.GET("", maintenanceHandler.List)
			maintenance.POST("", middleware.RequireRole(models.RoleAdmin), maintenanceHandler.Create)
			maintenance.PUT("/:id", middleware.RequireRole(models.RoleAdmin), maintenanceHandler.Update)
			maintenance.DELETE("/:id", middleware.RequireRole(models.RoleAdmin), maintenanceHandler.Delete)
		}

		// OEE (Overall Equipment Effectiveness) — la metrica regina della
		// produzione. Lampante in dashboard come card grande; calcolato
		// con fallback euristici così funziona out-of-the-box, e diventa
		// "vero" quando admin configura i tag di produzione via System.
		oeeHandler := handlers.NewOEEHandler(database)
		oeeGrp := api.Group("/oee")
		oeeGrp.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			oeeGrp.GET("", oeeHandler.Snapshot)
			oeeGrp.GET("/history", oeeHandler.History)
			// Test pre-config: verifica che un tag sia adatto al ruolo
			// (running BOOL o counter monotono) prima che l'admin salvi.
			oeeGrp.GET("/test-tag/:id", oeeHandler.TestTag)
			// Profiles CRUD — admin-only sulle scritture. La lettura è
			// per tutti gli autenticati (la dashboard renderizza la lista).
			oeeGrp.GET("/profiles", oeeHandler.ListProfiles)
			oeeGrp.GET("/profiles/:id/history", oeeHandler.ProfileHistory)
			oeeGrp.POST("/profiles", middleware.RequireRole(models.RoleAdmin), oeeHandler.CreateProfile)
			oeeGrp.PUT("/profiles/:id", middleware.RequireRole(models.RoleAdmin), oeeHandler.UpdateProfile)
			oeeGrp.DELETE("/profiles/:id", middleware.RequireRole(models.RoleAdmin), oeeHandler.DeleteProfile)

			// Storico persistente — query del rollup orario/giornaliero
			// salvato dal cron worker. Range time-bounded; default ultime
			// 24h se non specificato.
			oeeHistoryHandler := handlers.NewOEEHistoryHandler(database, oeeHandler)
			oeeGrp.GET("/history-v2", oeeHistoryHandler.History)
			// Matrice "OEE per turno × giorno" — aggregata da oee_history
			// con join sui turni. Funziona dopo che il cron ha popolato
			// le ore con shift_id stampato.
			oeeGrp.GET("/by-shift", oeeHistoryHandler.ByShift)

			// Loss tree + Pareto cause + MTBF/MTTR. Popolato in automatico
			// dal cron worker (PopulateLossEvents).
			lossesHandler := handlers.NewLossesHandler(database)
			oeeGrp.GET("/loss-tree", lossesHandler.LossTree)
			oeeGrp.GET("/loss-categories", lossesHandler.ListCategories)
			oeeGrp.GET("/profiles/:id/reliability", lossesHandler.MTBFMTTR)

			// Alert rules — admin definisce "OEE Linea A < 70% per 60min →
			// email warning". Valutate dal cron worker ogni 5 minuti.
			alertsHandler := handlers.NewAlertsHandler(database, notifDispatcher)
			oeeGrp.GET("/alert-rules", alertsHandler.List)
			oeeGrp.POST("/alert-rules", middleware.RequireRole(models.RoleAdmin), alertsHandler.Create)
			oeeGrp.PUT("/alert-rules/:id", middleware.RequireRole(models.RoleAdmin), alertsHandler.Update)
			oeeGrp.DELETE("/alert-rules/:id", middleware.RequireRole(models.RoleAdmin), alertsHandler.Delete)

			// Gerarchia Site → Area → Profile con rollup multi-livello.
			oeeGrp.GET("/hierarchy", oeeHandler.Hierarchy)

			// Export CSV — report professionali scaricabili.
			exportHandler := handlers.NewOEEExportHandler(database, oeeHandler, oeeHistoryHandler, lossesHandler)
			oeeGrp.GET("/export/history.csv", exportHandler.ExportHistoryCSV)
			oeeGrp.GET("/export/by-shift.csv", exportHandler.ExportByShiftCSV)
			oeeGrp.GET("/export/losses.csv", exportHandler.ExportLossTreeCSV)
			oeeGrp.GET("/export/profiles.csv", exportHandler.ExportProfilesCSV)
		}

		// Cron worker OEE: ogni ora salva snapshot per profilo; a
		// mezzanotte UTC aggrega in snapshot giornaliero. Ogni 5 minuti
		// valuta le alert rules. Goroutine dedicata, niente cron lib.
		go runOEECronWorker(database, oeeHandler, notifDispatcher)

		// Dashboard overview — un singolo endpoint che aggrega tutto
		// quello che la pagina dashboard mostra (system / alarms / gateways
		// / operations / activity timeline / KPI / OEE). Refresh ogni 30s.
		dashboardHandler := handlers.NewDashboardHandler(database)
		dashboardHandler.OEE = oeeHandler
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

		// API keys for edge-to-cloud authentication (org admin only)
		apiKeysHandler := handlers.NewAPIKeysHandler(database)
		orgAPIKeys := api.Group("/organizations/:id/api-keys")
		orgAPIKeys.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin), middleware.RequireOrgParam("id"))
		{
			orgAPIKeys.POST("", apiKeysHandler.Create)
			orgAPIKeys.GET("", apiKeysHandler.List)
			orgAPIKeys.DELETE("/:key_id", apiKeysHandler.Revoke)
		}

		// Edge installer — generates a ready-to-run ZIP for edge deployment (org admin only)
		installerHandler := handlers.NewEdgeInstallerHandler(database)
		orgEdge := api.Group("/organizations/:id")
		orgEdge.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin), middleware.RequireOrgParam("id"))
		{
			orgEdge.GET("/edge-installer", installerHandler.Download)
		}

		// User invites — org admins create one-time invite links for new members.
		invitesHandler := handlers.NewInvitesHandler(database, redisClient)
		orgInvites := api.Group("/organizations/:id/invites")
		orgInvites.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin), middleware.RequireOrgParam("id"))
		{
			orgInvites.POST("", invitesHandler.Create)
		}
		// Public auth endpoints — no JWT required.
		publicAuth := api.Group("/auth")
		publicAuth.POST("/accept-invite", middleware.LoginRateLimit(), invitesHandler.AcceptInvite)
		publicAuth.POST("/forgot-password", middleware.LoginRateLimit(), authHandler.ForgotPassword)
		publicAuth.POST("/reset-password", middleware.LoginRateLimit(), authHandler.ResetPassword)
		publicAuth.POST("/mfa/verify", middleware.LoginRateLimit(), authHandler.MFAVerify)
		// SSO/OIDC — OAuth2 redirect and callback, no auth required.
		publicAuth.GET("/sso/:provider/login", authHandler.SSOLogin)
		publicAuth.GET("/sso/:provider/callback", authHandler.SSOCallback)

		// Fleet, SSO, and RBAC — shared handler instance.
		fleetHandler := handlers.NewFleetHandler(database, mqttClient, redisClient)

		// Edge heartbeat status + webhook management — org admin or global admin.
		webhooksHandler := handlers.NewWebhooksHandler(database)
		orgMgmt := api.Group("/organizations/:id")
		orgMgmt.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin), middleware.RequireOrgParam("id"))
		{
			orgMgmt.GET("/edge-status", invitesHandler.EdgeStatus)
			orgMgmt.GET("/webhooks", webhooksHandler.List)
			orgMgmt.POST("/webhooks", webhooksHandler.Create)
			orgMgmt.DELETE("/webhooks/:webhook_id", webhooksHandler.Delete)

			// OTA edge updates and restart (publishes MQTT sys/update|restart/{org_id}).
			orgMgmt.POST("/edge-restart", fleetHandler.RestartEdge)
			orgMgmt.POST("/edge-update", fleetHandler.UpdateEdge)

			// SSO provider configuration per-org (admin only).
			orgMgmt.GET("/sso-providers", fleetHandler.GetSSOProviders)
			orgMgmt.POST("/sso-providers", fleetHandler.UpsertSSOProvider)
			orgMgmt.DELETE("/sso-providers/:provider", fleetHandler.DeleteSSOProvider)
		}

		// Fleet status (global admin only — spans all orgs).
		fleetGlobal := api.Group("/fleet")
		fleetGlobal.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
		{
			fleetGlobal.GET("/status", fleetHandler.GetFleetStatus)
		}

		// User permissions RBAC (admin only).
		users.GET("/:id/permissions", fleetHandler.GetUserPermissions)
		users.PUT("/:id/permissions", fleetHandler.UpdateUserPermissions)

		// Edge config pull endpoint — authenticated via API key, not JWT.
		// Edge managers call this on startup to get their gateway config + MQTT creds.
		edgeConfigHandler := handlers.NewEdgeConfigHandler(database)
		edge := api.Group("/edge")
		edge.Use(middleware.RequireAPIKey(database))
		{
			edge.GET("/config", edgeConfigHandler.Get)
		}

		// WebSocket endpoints
		api.GET("/ws/realtime", middleware.WebSocketAuth, middleware.RequireAuth, middleware.OrganizationContext(), realtimeHandler.HandleRealtime)

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

			i3x.GET("/equipment/:id/children", i3xHandler.GetEquipmentChildren)
			i3x.GET("/properties/:id/history", i3xHandler.GetPropertyHistory)
			i3x.POST("/properties/values", i3xHandler.BatchReadProperties)
			i3x.POST("/alarms/:id/acknowledge", i3xHandler.AcknowledgeAlarm)
		}

		// Security Center endpoints
		securityHandler := handlers.NewSecurityHandler(database)
		secGrp := api.Group("/security")
		secGrp.Use(middleware.RequireAuth)
		{
			secGrp.GET("/overview", securityHandler.Overview)
			secGrp.GET("/events", securityHandler.Events)
			secGrp.GET("/compliance", securityHandler.Compliance)
		}

		// Infrastructure overview (admin only)
		infraHandler := handlers.NewInfrastructureHandler(database)
		infraGrp := api.Group("/infrastructure")
		infraGrp.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
		{
			infraGrp.GET("", infraHandler.List)
		}

		// OTA release management
		updatesH := handlers.NewUpdatesHandler(database)
		api.GET("/releases", middleware.RequireAuth, updatesH.ListReleases)
		api.POST("/releases", middleware.RequireAuth, updatesH.CreateRelease)
		api.GET("/releases/fleet", middleware.RequireAuth, updatesH.GetFleetUpdateStatus)
		// Org admin update endpoints
		orgUpdates := api.Group("/organizations/:id")
		orgUpdates.Use(middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin))
		{
			orgUpdates.GET("/update", updatesH.GetPendingUpdate)
			orgUpdates.POST("/approve-update", updatesH.ApproveUpdate)
		}
		// Edge agent endpoints (RequireAuth only, no admin requirement)
		edgeUpdate := api.Group("/edge")
		edgeUpdate.Use(middleware.RequireAuth)
		{
			edgeUpdate.GET("/update-check", updatesH.EdgeUpdateCheck)
			edgeUpdate.POST("/update-status", updatesH.EdgeUpdateStatus)
			edgeUpdate.POST("/heartbeat", healthH.EdgeHeartbeat)
		}

		// Detailed health diagnostics (authenticated)
		api.GET("/health/detailed", middleware.RequireAuth, healthH.Detailed)
		api.GET("/db/stats", middleware.RequireAuth, middleware.RequireRole(models.RoleAdmin), healthH.DBStats)

		// OT Compliance — Steps 1 & 2 (Asset Discovery + Risk Posture)
		//               + Step 3 (Threat Monitor) + Step 4 (Compliance Reports)
		complianceHandler := handlers.NewComplianceHandler(database)
		threatHandler := handlers.NewThreatMonitorHandler(database)
		reportHandler := handlers.NewComplianceReportHandler(database)
		compliance := api.Group("/compliance")
		compliance.Use(middleware.RequireAuth, middleware.OrganizationContext())
		{
			// Step 1: OT asset inventory
			compliance.GET("/assets", complianceHandler.ListAssets)
			compliance.POST("/assets", middleware.RequireRole(models.RoleAdmin), complianceHandler.CreateAsset)
			compliance.PUT("/assets/:id", middleware.RequireRole(models.RoleAdmin), complianceHandler.UpdateAsset)
			compliance.DELETE("/assets/:id", middleware.RequireRole(models.RoleAdmin), complianceHandler.DeleteAsset)
			compliance.POST("/assets/:id/cves", middleware.RequireRole(models.RoleAdmin), complianceHandler.AddCVE)
			compliance.DELETE("/assets/:id/cves/:cve", middleware.RequireRole(models.RoleAdmin), complianceHandler.RemoveCVE)

			// Step 2: Risk posture + compliance framework assessments
			compliance.GET("/frameworks", complianceHandler.ListFrameworks)
			compliance.GET("/frameworks/:code/assessment", complianceHandler.GetAssessment)
			compliance.PUT("/frameworks/:code/assessment/:req_id", middleware.RequireRole(models.RoleAdmin), complianceHandler.UpdateAssessment)
			compliance.GET("/risk-posture", complianceHandler.RiskPosture)
			compliance.GET("/score", complianceHandler.ComplianceScore)
			compliance.POST("/scan", middleware.RequireRole(models.RoleAdmin), complianceHandler.TriggerScan)
			compliance.GET("/scan/:id", complianceHandler.GetScanStatus)

			// Step 3: Threat Monitor
			compliance.GET("/threats", threatHandler.ListThreats)
			compliance.POST("/threats", middleware.RequireRole(models.RoleAdmin), threatHandler.CreateThreat)
			compliance.PUT("/threats/:id/resolve", middleware.RequireRole(models.RoleAdmin), threatHandler.ResolveThreat)
			compliance.GET("/threats/summary", threatHandler.ThreatSummary)

			// Step 4: Compliance Reports
			compliance.POST("/reports", middleware.RequireRole(models.RoleAdmin), reportHandler.GenerateReport)
			compliance.GET("/reports", reportHandler.ListReports)
			compliance.GET("/reports/:id", reportHandler.GetReport)
			compliance.DELETE("/reports/:id", middleware.RequireRole(models.RoleAdmin), reportHandler.DeleteReport)

			// Gateway asset sync + auto-assess (NIS2 automation)
			compliance.POST("/sync-assets", middleware.RequireRole(models.RoleAdmin), complianceHandler.SyncAssets)
			compliance.GET("/auto-assess", complianceHandler.AutoAssess)

			// CSIRT — Art.23 NIS2 incident reporting with legal deadlines
			csirtHandler := handlers.NewCSIRTHandler(database)
			compliance.GET("/csirt", csirtHandler.ListIncidents)
			compliance.GET("/csirt/summary", csirtHandler.Summary)
			compliance.POST("/csirt", middleware.RequireRole(models.RoleAdmin), csirtHandler.CreateIncident)
			compliance.GET("/csirt/:id", csirtHandler.GetIncident)
			compliance.PUT("/csirt/:id", middleware.RequireRole(models.RoleAdmin), csirtHandler.UpdateIncident)
			compliance.PUT("/csirt/:id/early-warning", middleware.RequireRole(models.RoleAdmin), csirtHandler.MarkEarlyWarning)
			compliance.PUT("/csirt/:id/notify", middleware.RequireRole(models.RoleAdmin), csirtHandler.MarkNotification)
			compliance.PUT("/csirt/:id/close", middleware.RequireRole(models.RoleAdmin), csirtHandler.CloseIncident)

			// Vendor Risk — Art.18 NIS2 supply chain vendor risk management
			vendorHandler := handlers.NewVendorRiskHandler(database)
			compliance.GET("/vendors", vendorHandler.ListVendors)
			compliance.GET("/vendors/summary", vendorHandler.Summary)
			compliance.POST("/vendors", middleware.RequireRole(models.RoleAdmin), vendorHandler.CreateVendor)
			compliance.PUT("/vendors/:id", middleware.RequireRole(models.RoleAdmin), vendorHandler.UpdateVendor)
			compliance.DELETE("/vendors/:id", middleware.RequireRole(models.RoleAdmin), vendorHandler.DeleteVendor)
			compliance.POST("/vendors/:id/score", middleware.RequireRole(models.RoleAdmin), vendorHandler.RecalculateScore)
			compliance.POST("/vendors/sync", middleware.RequireRole(models.RoleAdmin), vendorHandler.SyncFromGateways)
		}
	}

	// Swagger — enabled only when SWAGGER_ENABLED=true (never in production)
	if os.Getenv("SWAGGER_ENABLED") == "true" {
		router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		log.Println("[WARNING] Swagger UI is enabled — disable in production (SWAGGER_ENABLED=true)")
	}

	// Prometheus metrics endpoint — scraped by Prometheus every 15s.
	//
	// The container port is published on the host, so "protected by network
	// policy" was not true in the default compose: anyone reachable on :8081
	// could read runtime internals and per-route traffic volumes. When
	// METRICS_TOKEN is set the endpoint requires it as a bearer token; it is
	// left open otherwise so existing Prometheus setups keep scraping.
	router.GET("/metrics", middleware.MetricsAuth(), gin.WrapH(promhttp.Handler()))

	// Start background DB metrics collector (updates gauges every minute).
	telemetry.StartDBMetricsCollector(database)

	// Start historian retention worker
	retentionDays := 365
	var historianRetentionVal string
	if err := database.QueryRowContext(context.Background(),
		`SELECT value FROM global_settings WHERE key = 'historian_retention_days'`).Scan(&historianRetentionVal); err == nil {
		if n, err := strconv.Atoi(historianRetentionVal); err == nil {
			retentionDays = n
		}
	}
	db.StartHistorianRetentionWorker(database, retentionDays)

	// Start OT asset sync worker (syncs gateways → ot_assets every hour for NIS2 compliance).
	otSync.StartAssetSyncWorker(database)

	// Start InfluxDB v2 push connector (no-op if influx_enabled=false in settings).
	influxWriter = connectors.NewInfluxDBConnector(database)

	// Health check endpoints (unauthenticated — probed by Docker / load balancers)
	router.GET("/health", healthH.Live)
	router.GET("/ready", healthH.Ready)

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

	// Apply EU scaling if configured for this tag.
	// The converted value replaces the raw value for all downstream consumers
	// (Redis cache, WebSocket broadcast, historian, InfluxDB, shadow).
	if cfg, ok := scalingCache.Load(update.TagID); ok {
		update.Value = scaling.Apply(update.Value, cfg.(scaling.Config))
		// Re-encode the payload with the scaled value so Redis/WS get EU data.
		if scaled, err := json.Marshal(map[string]interface{}{
			"tag_id": update.TagID,
			"org_id": update.OrgID,
			"v":      update.Value,
			"ts":     update.Timestamp,
			"q":      update.Quality,
		}); err == nil {
			payload = scaled
		}
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

	// 3. InfluxDB v2 push (if enabled in global_settings).
	if influxWriter != nil {
		influxWriter.Write(update.TagID, update.OrgID, "", update.Value, update.Quality, update.Timestamp)
	}

	// 4. Digital Twin shadow — persistent last-known-value with no TTL.
	// Written on every live MQTT update; survives edge going offline.
	// GET /api/tags/:id/shadow reads this key, falling back to tag_history.
	shadowKey := fmt.Sprintf("tag_shadow:%d", update.TagID)
	shadowPayload, _ := json.Marshal(map[string]interface{}{
		"tag_id":  update.TagID,
		"value":   update.Value,
		"quality": update.Quality,
		"ts":      update.Timestamp,
		"source":  "live",
	})
	if err := redisClient.Set(shadowKey, string(shadowPayload), 0); err != nil {
		log.Printf("Error storing tag shadow for tag %d: %v", update.TagID, err)
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

	// Deliver webhook events for alarm.active / alarm.cleared to all
	// subscribed org webhooks. Resolve org_id from tag→gateway join;
	// failures are logged but never block the alarm pipeline.
	go func() {
		var orgID int
		if err := db.QueryRow(
			`SELECT g.org_id FROM tags t JOIN gateways g ON t.gateway_id = g.id WHERE t.id = $1`,
			event.TagID,
		).Scan(&orgID); err != nil {
			log.Printf("[ALARM] webhook: could not resolve org for tag %d: %v", event.TagID, err)
			return
		}
		whEvent := "alarm.active"
		if event.Status == "CLEARED" {
			whEvent = "alarm.cleared"
		}
		handlers.DeliverWebhookEvent(db, orgID, whEvent, map[string]interface{}{
			"alarm_id":         insertedID,
			"tag_id":           event.TagID,
			"tag_alias":        event.TagAlias,
			"status":           event.Status,
			"severity":         event.Severity,
			"message":          event.Message,
			"value_at_trigger": event.ValueAtTrigger,
			"threshold":        event.Threshold,
			"occurred_at":      eventTime.UTC(),
		})
	}()
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

	// Resolve the organization from the topic: sys/write/{orgID}/...
	//
	// The org segment used to be parsed and thrown away while the tag was
	// looked up by alias across every tenant, so a publisher on one org's
	// topic could drive another org's PLC if the two happened to share a tag
	// alias. This writes to industrial equipment, so an unresolvable org
	// fails closed rather than falling back to a global lookup.
	if len(parts) < 3 || parts[0] != "sys" || parts[1] != "write" {
		log.Printf("[WRITE CMD] REJECTED: unexpected topic layout %q (want sys/write/{org_id}/...)", topic)
		return
	}
	orgID, err := strconv.Atoi(parts[2])
	if err != nil || orgID <= 0 {
		log.Printf("[WRITE CMD] REJECTED: topic %q carries no valid organization id", topic)
		return
	}

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

	// First try to find by exact alias match (case-insensitive), restricted to
	// the organization named in the topic. ORDER BY keeps the target stable
	// when an org has the same alias on more than one gateway (QueryRow would
	// otherwise pick whatever the planner returned first).
	query := `
		SELECT t.id, t.gateway_id, t.code, t.data_type
		FROM tags t
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE LOWER(t.alias) = LOWER($1) AND s.org_id = $2
		ORDER BY t.id
		LIMIT 1
	`

	err = db.QueryRow(query, tagAlias, orgID).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.DataType)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("[WRITE CMD] Tag not found with alias: %s", tagAlias)

			// Try with unslugified alias (replace - with space and vice versa)
			unslugified := strings.ReplaceAll(tagAlias, "-", " ")
			slugified := strings.ReplaceAll(strings.ToLower(tagAlias), " ", "-")

			// Try slugified version
			err = db.QueryRow(query, slugified, orgID).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.DataType)
			if err != nil {
				// Try unslugified version
				err = db.QueryRow(query, unslugified, orgID).Scan(&tag.ID, &tag.GatewayID, &tag.Code, &tag.DataType)
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

// runOEECronWorker è la goroutine che alimenta oee_history. Tick "minuto
// uno" di ogni ora: salva snapshot orario per ogni profilo abilitato +
// rollup fabbrica. A mezzanotte UTC: aggrega snapshot giornaliero.
//
// Idempotente via UNIQUE(profile_id, bucket_start, bucket_size) — se il
// servizio riparte dopo un crash, il replay non duplica righe.
//
// Niente cron expression / libreria esterna: il pattern "wake up al
// prossimo minuto:zero" è 10 righe di ticker e basta per l'uso.
func runOEECronWorker(db *sql.DB, oee *handlers.OEEHandler, dispatcher *notifications.Dispatcher) {
	log.Println("[OEE-CRON] worker started")

	// Wait until next minute:00 + 5s di margine — non sovrapporci al
	// boot di altri servizi e ci diamo tempo che il DB sia stabile.
	now := time.Now().UTC()
	next := now.Truncate(time.Minute).Add(time.Minute + 5*time.Second)
	time.Sleep(time.Until(next))

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// Recovery: al boot facciamo un primo snapshot per l'ora corrente
	// così non si aspetta fino a HH:00 prossimo.
	if err := handlers.RecordHourlySnapshot(db, oee, time.Now().UTC()); err != nil {
		log.Printf("[OEE-CRON] initial snapshot failed: %v", err)
	}

	for t := range ticker.C {
		u := t.UTC()
		// All'ora esatta (minuto 0): snapshot orario.
		if u.Minute() == 0 {
			if err := handlers.RecordHourlySnapshot(db, oee, u); err != nil {
				log.Printf("[OEE-CRON] hourly snapshot failed: %v", err)
			} else {
				log.Printf("[OEE-CRON] hourly snapshot ok @ %s", u.Format("2006-01-02 15:00"))
			}
			// Loss events: ogni ora cattura allarmi critical chiusi nelle
			// ultime 2h + maintenance setup. Idempotente.
			if err := handlers.PopulateLossEvents(db, u); err != nil {
				log.Printf("[OEE-CRON] populate loss events failed: %v", err)
			}
			// Mezzanotte UTC: aggregazione giornaliera del giorno precedente.
			if u.Hour() == 0 {
				if err := handlers.RecordDailySnapshot(db, oee, u); err != nil {
					log.Printf("[OEE-CRON] daily snapshot failed: %v", err)
				} else {
					log.Println("[OEE-CRON] daily snapshot ok")
				}
			}
		}
		// Ogni 5 minuti: valuta alert rules.
		if u.Minute()%5 == 0 {
			if err := handlers.EvaluateAlertRules(db, dispatcher, u); err != nil {
				log.Printf("[OEE-CRON] evaluate alert rules failed: %v", err)
			}
		}
	}
}
