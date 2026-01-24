package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/handlers"
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

	log.Println("Industrial Edge Middleware - core-api starting...")

	// Create Gin router
	router := gin.Default()

	// Create handlers
	orgsHandler := handlers.NewOrganizationsHandler(database)
	sitesHandler := handlers.NewSitesHandler(database)
	areasHandler := handlers.NewAreasHandler(database)
	gatewaysHandler := handlers.NewGatewaysHandler(database)
	tagsHandler := handlers.NewTagsHandler(database)

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
