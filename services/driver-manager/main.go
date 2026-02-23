package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

const (
	pollInterval      = 10 * time.Second
	dockerNetworkName = "industrial-network"
)

// GatewayState tracks the current state of a gateway container
type GatewayState struct {
	Gateway     models.Gateway
	ContainerID string
	Running     bool
}

// Manager manages driver container lifecycle
type Manager struct {
	database      *sql.DB
	dockerClient  *client.Client
	gatewayStates map[int]*GatewayState
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	networkID     string
}

func main() {
	log.Println("Starting driver-manager...")

	// Connect to PostgreSQL
	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "industrial_user"),
		Password: getEnv("DB_PASSWORD", "industrial_pass"),
		Database: getEnv("DB_NAME", "industrial_edge"),
	}

	database, err := db.Connect(dbCfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("Connected to PostgreSQL")

	// Create Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.44"))
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerClient.Close()
	log.Println("Connected to Docker daemon")

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create manager instance
	manager := &Manager{
		database:      database,
		dockerClient:  dockerClient,
		gatewayStates: make(map[int]*GatewayState),
		ctx:           ctx,
		cancel:        cancel,
	}

	// Get or create Docker network
	if err := manager.setupNetwork(); err != nil {
		log.Printf("Warning: Failed to setup Docker network: %v (container networking may not work)", err)
	}

	// Initial sync
	if err := manager.syncGateways(); err != nil {
		log.Printf("Initial gateway sync failed: %v", err)
	}

	// Start polling loop
	go manager.pollLoop()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down driver-manager...")
	cancel()

	// Stop all managed containers
	manager.stopAllContainers()
}

// setupNetwork gets or creates the Docker network for container communication
func (m *Manager) setupNetwork() error {
	// Try to find existing network
	networks, err := m.dockerClient.NetworkList(m.ctx, types.NetworkListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	for _, nw := range networks {
		if nw.Name == dockerNetworkName {
			m.networkID = nw.ID
			log.Printf("Found existing Docker network: %s (ID: %s)", dockerNetworkName, nw.ID)
			return nil
		}
	}

	// Create network if not found
	log.Printf("Creating Docker network: %s", dockerNetworkName)
	networkResp, err := m.dockerClient.NetworkCreate(m.ctx, dockerNetworkName, types.NetworkCreate{
		CheckDuplicate: true,
		Driver:         "bridge",
	})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	m.networkID = networkResp.ID
	log.Printf("Created Docker network: %s (ID: %s)", dockerNetworkName, m.networkID)
	return nil
}

// pollLoop runs the periodic gateway polling
func (m *Manager) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Printf("Starting gateway polling loop with interval: %v", pollInterval)

	for {
		select {
		case <-m.ctx.Done():
			log.Println("Stopping gateway polling loop")
			return
		case <-ticker.C:
			if err := m.syncGateways(); err != nil {
				log.Printf("Gateway sync failed: %v", err)
			}
		}
	}
}

// syncGateways synchronizes container states with database gateway states
func (m *Manager) syncGateways() error {
	// Load all gateways from database
	query := `
		SELECT id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled
		FROM gateways
	`

	rows, err := m.database.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query gateways: %w", err)
	}
	defer rows.Close()

	var gateways []models.Gateway
	for rows.Next() {
		var g models.Gateway
		var connConfigBytes []byte

		err := rows.Scan(
			&g.ID,
			&g.AreaID,
			&g.Name,
			&g.DriverType,
			&connConfigBytes,
			&g.ScanRateMs,
			&g.Enabled,
		)
		if err != nil {
			return fmt.Errorf("failed to scan gateway: %w", err)
		}

		gateways = append(gateways, g)
	}

	// Sync each gateway
	m.mu.Lock()
	defer m.mu.Unlock()

	// Track which gateway IDs we've processed
	processedIDs := make(map[int]bool)

	for _, gateway := range gateways {
		processedIDs[gateway.ID] = true

		state, exists := m.gatewayStates[gateway.ID]

		// If we think the container is running, verify it's actually running
		if exists && state.Running {
			running, err := m.isContainerRunning(state.ContainerID)
			if err != nil || !running {
				log.Printf("Container for gateway %d is no longer running (was %s), marking for restart", gateway.ID, state.ContainerID)
				state.Running = false
			}
		}

		if gateway.Enabled {
			// Gateway should be running
			if !exists || !state.Running {
				// Start container
				if err := m.startGatewayContainer(gateway); err != nil {
					log.Printf("Failed to start container for gateway %d (%s): %v", gateway.ID, gateway.Name, err)
					continue
				}
				log.Printf("Started container for gateway %d (%s, type: %s)", gateway.ID, gateway.Name, gateway.DriverType)
			}
		} else {
			// Gateway should be stopped
			if exists && state.Running {
				// Stop container
				if err := m.stopGatewayContainer(gateway.ID); err != nil {
					log.Printf("Failed to stop container for gateway %d (%s): %v", gateway.ID, gateway.Name, err)
					continue
				}
				log.Printf("Stopped container for gateway %d (%s)", gateway.ID, gateway.Name)
			}
		}
	}

	// Handle deleted gateways (not in current query results)
	for id, state := range m.gatewayStates {
		if !processedIDs[id] && state.Running {
			log.Printf("Gateway %d no longer exists in database, stopping container", id)
			if err := m.stopGatewayContainer(id); err != nil {
				log.Printf("Failed to stop container for deleted gateway %d: %v", id, err)
			}
		}
	}

	return nil
}

// isContainerRunning checks if a container is actually running
func (m *Manager) isContainerRunning(containerID string) (bool, error) {
	inspect, err := m.dockerClient.ContainerInspect(m.ctx, containerID)
	if err != nil {
		return false, err
	}
	return inspect.State.Running, nil
}

// startGatewayContainer starts a driver container for a gateway
func (m *Manager) startGatewayContainer(gateway models.Gateway) error {
	// Determine driver image and service name based on driver type
	var imageName, containerNamePrefix string
	switch gateway.DriverType {
	case "S7":
		imageName = "industrial-driver-s7:latest"
		containerNamePrefix = "driver-s7"
	case "MODBUS_TCP":
		imageName = "industrial-driver-modbus:latest"
		containerNamePrefix = "driver-modbus"
	case "REDIS":
		imageName = "industrial-driver-redis:latest"
		containerNamePrefix = "driver-redis"
	case "MQTT":
		imageName = "industrial-driver-mqtt:latest"
		containerNamePrefix = "driver-mqtt"
	default:
		return fmt.Errorf("unsupported driver type: %s", gateway.DriverType)
	}

	containerName := fmt.Sprintf("%s-%d", containerNamePrefix, gateway.ID)

	// Resolve DB_HOST to IP to avoid DNS issues in child containers
	// (hostname resolution inside child containers seems flaky, resolving to 127.0.0.1 in some cases)
	dbHost := getEnv("DB_HOST", "postgres")
	if ips, err := net.LookupHost(dbHost); err == nil {
		for _, ip := range ips {
			if ip != "127.0.0.1" && ip != "::1" {
				log.Printf("Resolved DB_HOST %s to IP %s", dbHost, ip)
				dbHost = ip
				break
			}
		}
	} else {
		log.Printf("Warning: Failed to resolve DB_HOST %s: %v", dbHost, err)
	}

	// Resolve MQTT_HOST to IP
	mqttHost := getEnv("MQTT_HOST", "mosquitto")
	if ips, err := net.LookupHost(mqttHost); err == nil {
		for _, ip := range ips {
			if ip != "127.0.0.1" && ip != "::1" {
				log.Printf("Resolved MQTT_HOST %s to IP %s", mqttHost, ip)
				mqttHost = ip
				break
			}
		}
	} else {
		log.Printf("Warning: Failed to resolve MQTT_HOST %s: %v", mqttHost, err)
	}

	// Create container config
	env := []string{
		fmt.Sprintf("GATEWAY_ID=%d", gateway.ID),
		fmt.Sprintf("DB_HOST=%s", dbHost),
		fmt.Sprintf("DB_PORT=%s", getEnv("DB_PORT", "5432")),
		fmt.Sprintf("DB_USER=%s", getEnv("DB_USER", "industrial_user")),
		fmt.Sprintf("DB_PASSWORD=%s", getEnv("DB_PASSWORD", "industrial_pass")),
		fmt.Sprintf("DB_NAME=%s", getEnv("DB_NAME", "industrial_edge")),
		fmt.Sprintf("MQTT_HOST=%s", mqttHost),
		fmt.Sprintf("MQTT_PORT=%s", getEnv("MQTT_PORT", "1883")),
	}

	containerConfig := &container.Config{
		Image: imageName,
		Env:   env,
	}

	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
		NetworkMode: container.NetworkMode(dockerNetworkName),
		ExtraHosts:  []string{"host.docker.internal:host-gateway"},
	}

	// Pull image if needed (skip if image exists locally)
	_, _, err := m.dockerClient.ImageInspectWithRaw(m.ctx, imageName)
	if err != nil {
		// Image doesn't exist, pull it
		log.Printf("Pulling image %s for gateway %d...", imageName, gateway.ID)
		// Note: In production, you'd use ImagePull here
		// For now, assume images are built locally
	}

	// Remove existing container with same name if it exists to avoid conflicts
	// This handles cases where the manager was restarted but containers were left running
	_ = m.dockerClient.ContainerRemove(m.ctx, containerName, types.ContainerRemoveOptions{Force: true})

	// Create container
	resp, err := m.dockerClient.ContainerCreate(m.ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	if err := m.dockerClient.ContainerStart(m.ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		// Clean up created container if start fails
		_ = m.dockerClient.ContainerRemove(m.ctx, resp.ID, types.ContainerRemoveOptions{})
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Update state
	m.gatewayStates[gateway.ID] = &GatewayState{
		Gateway:     gateway,
		ContainerID: resp.ID,
		Running:     true,
	}

	return nil
}

// stopGatewayContainer stops a driver container for a gateway
func (m *Manager) stopGatewayContainer(gatewayID int) error {
	state, exists := m.gatewayStates[gatewayID]
	if !exists {
		return fmt.Errorf("no state found for gateway %d", gatewayID)
	}

	if !state.Running {
		return nil // Already stopped
	}

	// Stop container (with timeout)
	timeout := int(10 * time.Second)
	if err := m.dockerClient.ContainerStop(m.ctx, state.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Remove container
	if err := m.dockerClient.ContainerRemove(m.ctx, state.ContainerID, types.ContainerRemoveOptions{}); err != nil {
		log.Printf("Warning: Failed to remove container %s: %v", state.ContainerID, err)
	}

	// Update state
	state.Running = false

	return nil
}

// stopAllContainers stops all running containers managed by this manager
func (m *Manager) stopAllContainers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for gatewayID, state := range m.gatewayStates {
		if state.Running {
			log.Printf("Stopping container for gateway %d...", gatewayID)
			timeout := int(5 * time.Second)
			if err := m.dockerClient.ContainerStop(m.ctx, state.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
				log.Printf("Failed to stop container %s: %v", state.ContainerID, err)
			}
		}
	}

	log.Println("All driver containers stopped")
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
