package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

const (
	// Timing
	pollInterval        = 10 * time.Second
	imagePullTimeout    = 5 * time.Minute
	containerStartTimeout = 30 * time.Second
	containerStopTimeout = 10 * time.Second

	// Retry configuration
	maxPullRetries    = 3
	maxStartRetries   = 5
	retryBaseDelay    = 1 * time.Second
	retryMaxDelay     = 30 * time.Second

	// Docker
	dockerNetworkName = "industrial-network"

	// Error codes
	ErrCodeImagePull   = "ERR_DRIVER_IMAGE_PULL"
	ErrCodeContainerCreate = "ERR_DRIVER_CREATE"
	ErrCodeContainerStart  = "ERR_DRIVER_START"
	ErrCodeDBQuery       = "ERR_DB_QUERY"
	ErrCodeDBConnect     = "ERR_DB_CONNECT"
)

// Resource limits per driver type (CPU in nanoseconds, memory in bytes)
type ResourceLimits struct {
	CPU     int64
	Memory  int64
}

var driverResourceLimits = map[string]ResourceLimits{
	"S7":        {CPU: 500000000, Memory: 128 * 1024 * 1024},  // 0.5 CPU, 128MB
	"MODBUS_TCP": {CPU: 250000000, Memory: 64 * 1024 * 1024},   // 0.25 CPU, 64MB
	"MQTT":      {CPU: 250000000, Memory: 64 * 1024 * 1024},   // 0.25 CPU, 64MB
	"OPC_UA":    {CPU: 1000000000, Memory: 256 * 1024 * 1024}, // 1.0 CPU, 256MB
}

// GatewayState tracks the current state of a gateway container
type GatewayState struct {
	Gateway            models.Gateway
	ContainerID        string
	Running            bool
	ConsecutiveErrors  int
	LastError          string
	LastErrorTime      time.Time
}

// GatewayStatus represents the status published to MQTT
type GatewayStatus struct {
	GatewayID   int       `json:"gateway_id"`
	Status      string    `json:"status"` // online, offline, error
	ContainerID string    `json:"container_id,omitempty"`
	Error       string    `json:"error,omitempty"`
	Timestamp   int64     `json:"timestamp"`
	DriverType  string    `json:"driver_type"`
}

// MQTTClient interface for publishing status
type MQTTClient interface {
	Publish(topic string, payload interface{}) error
	PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error
}

// Manager manages driver container lifecycle
type Manager struct {
	database      *sql.DB
	dbCfg         db.Config
	dockerClient  *client.Client
	gatewayStates map[int]*GatewayState
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	networkID     string
	consecutiveErrors int
	mqttClient    MQTTClient
}

func main() {
	log.Println("[DRIVER-MANAGER] Starting driver-manager...")

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
		log.Fatalf("[DRIVER-MANAGER] Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("[DRIVER-MANAGER] Connected to PostgreSQL")

	// Create Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("[DRIVER-MANAGER] Failed to create Docker client: %v", err)
	}
	defer dockerClient.Close()
	log.Println("[DRIVER-MANAGER] Connected to Docker daemon")

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create MQTT client for status publishing
	mqttClient := newMQTTStatusClient(
		getEnv("MQTT_HOST", "mosquitto"),
		getEnvInt("MQTT_PORT", 1883),
	)

	// Create manager instance
	manager := &Manager{
		database:        database,
		dbCfg:           dbCfg,
		dockerClient:    dockerClient,
		gatewayStates:   make(map[int]*GatewayState),
		ctx:             ctx,
		cancel:          cancel,
		mqttClient:      mqttClient,
	}

	// Get or create Docker network
	if err := manager.ensureNetwork(); err != nil {
		log.Printf("[DRIVER-MANAGER] WARNING: Failed to setup Docker network: %v", err)
	}

	// Initial sync
	if err := manager.syncGateways(); err != nil {
		log.Printf("[DRIVER-MANAGER] Initial gateway sync failed: %v", err)
	}

	// Start polling loop
	go manager.pollLoop()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[DRIVER-MANAGER] Shutting down driver-manager...")
	cancel()

	// Stop all managed containers
	manager.stopAllContainers()
}

// mqttStatusClient publishes gateway status via MQTT bridge
type mqttStatusClient struct {
	mqttHost string
	mqttPort int
	httpClient *http.Client
}

func newMQTTStatusClient(host string, port int) *mqttStatusClient {
	return &mqttStatusClient{
		mqttHost: host,
		mqttPort: port,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (m *mqttStatusClient) Publish(topic string, payload interface{}) error {
	return m.PublishWithQoS(topic, payload, 1, false)
}

func (m *mqttStatusClient) PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error {
	// Convert payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Build URL with query parameters
	url := fmt.Sprintf("http://%s:%d/mqtt/publish", m.mqttHost, m.mqttPort)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}

	q := req.URL.Query()
	q.Set("topic", topic)
	q.Set("qos", fmt.Sprintf("%d", qos))
	q.Set("retained", fmt.Sprintf("%t", retained))
	q.Set("message", string(payloadBytes))
	req.URL.RawQuery = q.Encode()

	// Send request
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MQTT publish failed with status %d", resp.StatusCode)
	}

	log.Printf("[MQTT-STATUS] Published to topic %s: status=%s, qos=%d, retained=%t",
		topic, payload, qos, retained)
	return nil
}

// ensureNetwork gets or creates the Docker network for container communication
// Note: Docker API calls are thread-safe, no lock needed here
func (m *Manager) ensureNetwork() error {
	// Try to find existing network
	networks, err := m.dockerClient.NetworkList(m.ctx, types.NetworkListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	for _, nw := range networks {
		if nw.Name == dockerNetworkName || strings.HasSuffix(nw.Name, "_"+dockerNetworkName) {
			m.networkID = nw.ID
			log.Printf("[DRIVER-MANAGER] Found existing Docker network: %s (ID: %s)", nw.Name, nw.ID)
			return nil
		}
	}

	// Create network if not found
	log.Printf("[DRIVER-MANAGER] Creating Docker network: %s", dockerNetworkName)
	networkResp, err := m.dockerClient.NetworkCreate(m.ctx, dockerNetworkName, types.NetworkCreate{
		CheckDuplicate: true,
		Driver:         "bridge",
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{
				{Subnet: "172.20.0.0/16"},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	m.networkID = networkResp.ID
	log.Printf("[DRIVER-MANAGER] Created Docker network: %s (ID: %s)", dockerNetworkName, m.networkID)
	return nil
}

// resolveHostname resolves a hostname to IP, with fallback to original hostname
func (m *Manager) resolveHostname(hostname string) string {
	ips, err := net.LookupHost(hostname)
	if err != nil {
		log.Printf("[DRIVER-MANAGER] WARNING: Failed to resolve %s: %v, using hostname", hostname, err)
		return hostname
	}

	// Find first non-loopback IP
	for _, ip := range ips {
		if ip != "127.0.0.1" && ip != "::1" && !strings.HasPrefix(ip, "127.") && !strings.HasPrefix(ip, "::1") {
			log.Printf("[DRIVER-MANAGER] Resolved %s to IP %s", hostname, ip)
			return ip
		}
	}

	log.Printf("[DRIVER-MANAGER] WARNING: Only loopback IPs found for %s, using hostname", hostname)
	return hostname
}

// pullImageWithRetry pulls a Docker image with retry logic and exponential backoff
func (m *Manager) pullImageWithRetry(imageName string) error {
	log.Printf("[DRIVER-MANAGER] Checking image: %s", imageName)

	// Check if image exists locally
	_, _, err := m.dockerClient.ImageInspectWithRaw(m.ctx, imageName)
	if err == nil {
		log.Printf("[DRIVER-MANAGER] Image %s exists locally", imageName)
		return nil
	}

	log.Printf("[DRIVER-MANAGER] Image %s not found locally, starting pull...", imageName)

	// Retry with exponential backoff
	for attempt := 0; attempt < maxPullRetries; attempt++ {
		if attempt > 0 {
			backoff := calculateBackoff(retryBaseDelay, attempt, retryMaxDelay)
			log.Printf("[DRIVER-MANAGER] Pull attempt %d/%d failed, retrying in %v", attempt+1, maxPullRetries, backoff)
			time.Sleep(backoff)
		}

		// Create pull context with timeout
		pullCtx, pullCancel := context.WithTimeout(m.ctx, imagePullTimeout)

		reader, err := m.dockerClient.ImagePull(pullCtx, imageName, types.ImagePullOptions{})
		if err != nil {
			pullCancel()
			if attempt == maxPullRetries-1 {
				return fmt.Errorf("%s: failed to pull image after %d attempts: %w", ErrCodeImagePull, maxPullRetries, err)
			}
			continue
		}

		// Read pull output to completion and log progress
		defer reader.Close()
		defer pullCancel()

		decoder := json.NewDecoder(reader)
		pullProgress := 0
		for {
			var progress map[string]interface{}
			if err := decoder.Decode(&progress); err != nil {
				if err == io.EOF {
					break
				}
				// Log non-fatal decode errors
				continue
			}

			// Log progress if available
			if status, ok := progress["status"].(string); ok && status != "" {
				if id, ok := progress["id"].(string); ok {
					log.Printf("[DRIVER-MANAGER] Pulling %s: %s", id, status)
				}
			}

			// Extract progress percentage
			if progressDetail, ok := progress["progressDetail"].(map[string]interface{}); ok {
				if current, ok := progressDetail["current"].(float64); ok {
					if total, ok := progressDetail["total"].(float64); ok && total > 0 {
						newProgress := int((current / total) * 100)
						if newProgress > pullProgress {
							pullProgress = newProgress
							if pullProgress%20 == 0 { // Log every 20%
								log.Printf("[DRIVER-MANAGER] Pull progress: %d%%", pullProgress)
							}
						}
					}
				}
			}
		}

		log.Printf("[DRIVER-MANAGER] Successfully pulled image %s", imageName)
		return nil
	}

	return fmt.Errorf("%s: failed to pull image after %d attempts", ErrCodeImagePull, maxPullRetries)
}

// calculateBackoff calculates exponential backoff with jitter
func calculateBackoff(baseDelay time.Duration, attempt int, maxDelay time.Duration) time.Duration {
	backoff := baseDelay * time.Duration(1<<uint(attempt))
	if backoff > maxDelay {
		backoff = maxDelay
	}
	return backoff
}

// publishGatewayStatus publishes gateway status to MQTT
func (m *Manager) publishGatewayStatus(gatewayID int, status string, errorMsg string) {
	statusObj := GatewayStatus{
		GatewayID: gatewayID,
		Status:    status,
		Timestamp:  time.Now().UnixMilli(),
	}

	if errorMsg != "" {
		statusObj.Error = errorMsg
	}

	// Get driver type if available
	if state, exists := m.gatewayStates[gatewayID]; exists {
		statusObj.DriverType = state.Gateway.DriverType
		statusObj.ContainerID = state.ContainerID
	}

	topic := fmt.Sprintf("sys/health/%d", gatewayID)
	_ = m.mqttClient.PublishWithQoS(topic, statusObj, 1, true) // retained
}

// startGatewayContainer starts a driver container for a gateway
func (m *Manager) startGatewayContainer(gateway models.Gateway) error {
	log.Printf("[DRIVER-MANAGER] Starting container for gateway %d (%s, type: %s)", gateway.ID, gateway.Name, gateway.DriverType)

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
	case "OPC_UA":
		imageName = "industrial-driver-opcua:latest"
		containerNamePrefix = "driver-opcua"
	default:
		return fmt.Errorf("unsupported driver type: %s", gateway.DriverType)
	}

	containerName := fmt.Sprintf("%s-%d", containerNamePrefix, gateway.ID)

	// 1. Ensure network exists
	if err := m.ensureNetwork(); err != nil {
		return fmt.Errorf("network verification failed: %w", err)
	}

	// 2. Pull image with retry
	if err := m.pullImageWithRetry(imageName); err != nil {
		m.publishGatewayStatus(gateway.ID, "error", fmt.Sprintf("Image pull failed: %v", err))
		return fmt.Errorf("image pull failed: %w", err)
	}

	// 3. Resolve hostnames to IPs
	dbHost := m.resolveHostname(getEnv("DB_HOST", "postgres"))
	mqttHost := m.resolveHostname(getEnv("MQTT_HOST", "mosquitto"))

	// 4. Create container config with resource limits
	// TODO: Re-enable resource limits after fixing Docker SDK v25 compatibility
	// resources := driverResourceLimits[gateway.DriverType]

	// 5. Create container config
	env := []string{
		fmt.Sprintf("GATEWAY_ID=%d", gateway.ID),
		fmt.Sprintf("DB_HOST=%s", dbHost),
		fmt.Sprintf("DB_PORT=%s", getEnv("DB_PORT", "5432")),
		fmt.Sprintf("DB_USER=%s", getEnv("DB_USER", "industrial_user")),
		fmt.Sprintf("DB_PASSWORD=%s", getEnv("DB_PASSWORD", "industrial_pass")),
		fmt.Sprintf("DB_NAME=%s", getEnv("DB_NAME", "industrial_edge")),
		fmt.Sprintf("MQTT_HOST=%s", mqttHost),
		fmt.Sprintf("MQTT_PORT=%s", getEnv("MQTT_PORT", "1883")),
		"SPARKPLUG_ENABLED=true",
		"TZ=" + getEnv("TZ", "Europe/Rome"),
	}

	containerConfig := &container.Config{
		Image: imageName,
		Env:   env,
	}

	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": "10m",
				"max-file": "3",
			},
		},
		// TODO: Resources causing issue with Docker SDK v25, investigate
		// Resources: container.Resources{
		// 	NanoCPUs: resources.CPU,
		// 	Memory:   resources.Memory,
		// },
		NetworkMode: container.NetworkMode(m.networkID),
		ExtraHosts:  []string{"host.docker.internal:host-gateway"},
	}

	// 6. Remove existing container with same name if it exists
	_ = m.dockerClient.ContainerRemove(m.ctx, containerName, types.ContainerRemoveOptions{Force: true})

	// 7. Create container
	createCtx, createCancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer createCancel()

	resp, err := m.dockerClient.ContainerCreate(createCtx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		errMsg := fmt.Sprintf("%s: failed to create container: %v", ErrCodeContainerCreate, err)
		m.publishGatewayStatus(gateway.ID, "error", errMsg)
		return fmt.Errorf("failed to create container: %w", err)
	}

	// 8. Start container
	startCtx, startCancel := context.WithTimeout(m.ctx, containerStartTimeout)
	defer startCancel()

	if err := m.dockerClient.ContainerStart(startCtx, resp.ID, types.ContainerStartOptions{}); err != nil {
		// Clean up created container if start fails
		_ = m.dockerClient.ContainerRemove(m.ctx, resp.ID, types.ContainerRemoveOptions{})
		errMsg := fmt.Sprintf("%s: failed to start container: %v", ErrCodeContainerStart, err)
		m.publishGatewayStatus(gateway.ID, "error", errMsg)
		return fmt.Errorf("failed to start container: %w", err)
	}

	// 9. Update state
	m.gatewayStates[gateway.ID] = &GatewayState{
		Gateway:           gateway,
		ContainerID:       resp.ID,
		Running:           true,
		ConsecutiveErrors: 0,
		LastError:         "",
	}

	// 10. Publish online status
	m.publishGatewayStatus(gateway.ID, "online", "")

	log.Printf("[DRIVER-MANAGER] ✓ Started container for gateway %d (%s, type: %s, container: %s)",
		gateway.ID, gateway.Name, gateway.DriverType, containerName)

	return nil
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
		m.consecutiveErrors++
		return fmt.Errorf("%s: failed to query gateways: %w", ErrCodeDBQuery, err)
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
				log.Printf("[DRIVER-MANAGER] Container for gateway %d is no longer running (was %s), marking for restart",
					gateway.ID, state.ContainerID)
				state.Running = false
			}
		}

		if gateway.Enabled {
			// Gateway should be running
			if !exists || !state.Running {
				// Start container
				if err := m.startGatewayContainer(gateway); err != nil {
					log.Printf("[DRIVER-MANAGER] ERROR: Failed to start container for gateway %d (%s): %v",
						gateway.ID, gateway.Name, err)

					// Update error state
					if state != nil {
						state.ConsecutiveErrors++
						state.LastError = err.Error()
						state.LastErrorTime = time.Now()

						// Mark as permanently failed after max retries
						if state.ConsecutiveErrors >= maxStartRetries {
							log.Printf("[DRIVER-MANAGER] Gateway %d exceeded max retries (%d), marking as failed",
								gateway.ID, maxStartRetries)
							m.publishGatewayStatus(gateway.ID, "error",
								fmt.Sprintf("Exceeded max retries. Last error: %s", err.Error()))
						}
					}
					continue
				}
			}
		} else {
			// Gateway should be stopped
			if exists && state.Running {
				// Stop container
				if err := m.stopGatewayContainer(gateway.ID); err != nil {
					log.Printf("[DRIVER-MANAGER] ERROR: Failed to stop container for gateway %d (%s): %v",
						gateway.ID, gateway.Name, err)
					continue
				}
				log.Printf("[DRIVER-MANAGER] Stopped container for gateway %d (%s)", gateway.ID, gateway.Name)
			}
		}
	}

	// Handle deleted gateways (not in current query results)
	for id, state := range m.gatewayStates {
		if !processedIDs[id] && state.Running {
			log.Printf("[DRIVER-MANAGER] Gateway %d no longer exists in database, stopping container", id)
			if err := m.stopGatewayContainer(id); err != nil {
				log.Printf("[DRIVER-MANAGER] ERROR: Failed to stop container for deleted gateway %d: %v", id, err)
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
	timeout := int(containerStopTimeout)
	if err := m.dockerClient.ContainerStop(m.ctx, state.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Remove container
	if err := m.dockerClient.ContainerRemove(m.ctx, state.ContainerID, types.ContainerRemoveOptions{}); err != nil {
		log.Printf("[DRIVER-MANAGER] WARNING: Failed to remove container %s: %v", state.ContainerID, err)
	}

	// Publish offline status
	m.publishGatewayStatus(gatewayID, "offline", "")

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
			log.Printf("[DRIVER-MANAGER] Stopping container for gateway %d...", gatewayID)
			timeout := int(containerStopTimeout)
			if err := m.dockerClient.ContainerStop(m.ctx, state.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
				log.Printf("[DRIVER-MANAGER] ERROR: Failed to stop container %s: %v", state.ContainerID, err)
			}
		}
	}

	log.Println("[DRIVER-MANAGER] All driver containers stopped")
}

// pollLoop runs the periodic gateway polling
func (m *Manager) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Printf("[DRIVER-MANAGER] Starting gateway polling loop with interval: %v", pollInterval)

	for {
		select {
		case <-m.ctx.Done():
			log.Println("[DRIVER-MANAGER] Stopping gateway polling loop")
			return
		case <-ticker.C:
			if err := m.syncGateways(); err != nil {
				log.Printf("[DRIVER-MANAGER] Gateway sync failed: %v", err)
				m.consecutiveErrors++

				// If we get 3 consecutive errors, try to reconnect to DB
				if m.consecutiveErrors >= 3 {
					log.Println("[DRIVER-MANAGER] Too many consecutive errors, attempting DB reconnect...")
					if err := m.reconnectDB(); err != nil {
						log.Printf("[DRIVER-MANAGER] DB reconnect failed: %v", err)
					} else {
						log.Println("[DRIVER-MANAGER] DB reconnected successfully")
						m.consecutiveErrors = 0
					}
				}
			} else {
				m.consecutiveErrors = 0
			}
		}
	}
}

// reconnectDB closes and reopens the database connection
func (m *Manager) reconnectDB() error {
	// Close existing connection
	m.database.Close()

	// Reconnect
	database, err := db.Connect(m.dbCfg)
	if err != nil {
		return err
	}
	m.database = database
	return nil
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

// Helper function to make HTTP POST requests to MQTT bridge
func publishToMQTT(host string, port int, topic string, payload interface{}, qos byte, retained bool) error {
	url := fmt.Sprintf("http://%s:%d/mqtt/publish", host, port)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}

	q := req.URL.Query()
	q.Set("topic", topic)
	q.Set("qos", fmt.Sprintf("%d", qos))
	q.Set("retained", fmt.Sprintf("%t", retained))
	q.Set("message", string(payloadBytes))
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MQTT publish failed with status %d", resp.StatusCode)
	}

	return nil
}
