package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	iemqtt "github.com/ralph/industrial-edge-middleware/internal/mqtt"
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
	"MODBUS_TCP": {CPU: 250000000, Memory: 64 * 1024 * 1024},  // 0.25 CPU, 64MB
	"MQTT":      {CPU: 250000000, Memory: 64 * 1024 * 1024},   // 0.25 CPU, 64MB
	"OPC_UA":    {CPU: 1000000000, Memory: 256 * 1024 * 1024}, // 1.0 CPU, 256MB
	"LORAWAN":   {CPU: 250000000, Memory: 64 * 1024 * 1024},   // 0.25 CPU, 64MB — event-driven, low overhead
}

// GatewayState tracks the current state of a gateway container
type GatewayState struct {
	Gateway            models.Gateway
	ContainerID        string
	Running            bool
	ConsecutiveErrors  int
	LastError          string
	LastErrorTime      time.Time
	CooldownUntil      time.Time     // skip start attempts until this instant
	CooldownBackoff    time.Duration // current cooldown length (doubles up to failureCooldownMax)
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

	// Per-image pull cooldown: a registry that keeps failing must not cost
	// every sync cycle 15 minutes of serial retries, starving the other
	// gateways. Guarded by pullMu (pre-pull runs outside m.mu).
	pullMu       sync.Mutex
	pullFailures map[string]pullFailState
}

type pullFailState struct {
	failures int
	nextTry  time.Time
}

// failureCooldown returns an exponential cooldown: 5 min doubling up to 1 h.
func failureCooldown(failures int) time.Duration {
	d := 5 * time.Minute
	for i := 1; i < failures && d < time.Hour; i++ {
		d *= 2
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
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

	// Create MQTT client for status publishing.
	// Mosquitto has allow_anonymous false — credentials are required.
	mqttClient := newMQTTStatusClient(
		getEnv("MQTT_HOST", "mosquitto"),
		getEnvInt("MQTT_PORT", 1883),
		getEnv("MQTT_USERNAME", ""),
		getEnv("MQTT_PASSWORD", ""),
	)

	// Create manager instance
	manager := &Manager{
		database:        database,
		dbCfg:           dbCfg,
		dockerClient:    dockerClient,
		gatewayStates:   make(map[int]*GatewayState),
		pullFailures:    make(map[string]pullFailState),
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

	// Subscribe to OTA and restart commands from core-api.
	go manager.startOTASubscriber(
		getEnv("MQTT_HOST", "mosquitto"),
		getEnvInt("MQTT_PORT", 1883),
	)

	// OTA update poller: checks /api/edge/update-check every 5 minutes.
	// Reads CORE_API_URL and CORE_API_TOKEN from env. If those are not set,
	// the poller logs a warning and exits early — it never blocks startup.
	go runUpdatePoller(
		ctx,
		getEnv("ORG_ID", "0"),
		getEnv("AGENT_VERSION", "unknown"),
		getEnv("CORE_API_URL", ""),
		getEnv("CORE_API_TOKEN", ""),
	)

	// Start polling loop
	go manager.pollLoop()

	// Local health HTTP server for Docker health checks
	go startHealthServer(getEnv("HEALTH_PORT", "8081"))

	// Heartbeat to core API (sends org status every 30s)
	apiURL := getEnv("CORE_API_URL", "")
	apiToken := getEnv("CORE_API_TOKEN", "")
	agentVersion := getEnv("AGENT_VERSION", "unknown")
	orgIDStr := getEnv("ORG_ID", "0")
	orgIDInt := 0
	if v, err := strconv.Atoi(orgIDStr); err == nil {
		orgIDInt = v
	}
	if apiURL != "" && apiToken != "" && orgIDInt > 0 {
		go runHeartbeat(orgIDInt, apiURL, apiToken, agentVersion)
	}

	// MQTT watchdog — reconnects if broker drops
	brokerURL := fmt.Sprintf("tcp://%s:%d", getEnv("MQTT_HOST", "mosquitto"), getEnvInt("MQTT_PORT", 1883))
	watchdogOpts := mqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID("driver-manager-watchdog").
		SetUsername(getEnv("MQTT_USERNAME", "")).
		SetPassword(getEnv("MQTT_PASSWORD", "")).
		SetAutoReconnect(true)
	watchdogClient := mqtt.NewClient(watchdogOpts)
	if tok := watchdogClient.Connect(); tok.Wait() && tok.Error() == nil {
		go runMQTTWatchdog(watchdogClient, brokerURL, "driver-manager-watchdog")
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[DRIVER-MANAGER] Shutting down driver-manager...")
	cancel()

	// Stop all managed containers
	manager.stopAllContainers()
}

// mqttStatusClient publishes gateway status over a real MQTT connection.
// The previous implementation POSTed HTTP to the broker's raw MQTT port,
// which no broker understands — health updates were silently lost.
type mqttStatusClient struct {
	client *iemqtt.Client
}

func newMQTTStatusClient(host string, port int, username, password string) *mqttStatusClient {
	c := iemqtt.NewClient(iemqtt.Config{
		Host:          host,
		Port:          port,
		ClientID:      fmt.Sprintf("driver-manager-status-%d", time.Now().Unix()),
		Username:      username,
		Password:      password,
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
	})
	if err := c.Connect(); err != nil {
		// AutoReconnect keeps retrying in the background; publishes fail
		// with an error (logged by callers) until the connection is up.
		log.Printf("[MQTT-STATUS] Initial connect failed (will retry in background): %v", err)
	}
	return &mqttStatusClient{client: c}
}

func (m *mqttStatusClient) Publish(topic string, payload interface{}) error {
	return m.PublishWithQoS(topic, payload, 1, false)
}

func (m *mqttStatusClient) PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return m.client.PublishWithQoS(topic, payloadBytes, qos, retained)
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
		var streamErr error
		for {
			var progress map[string]interface{}
			if err := decoder.Decode(&progress); err != nil {
				if err != io.EOF {
					// A persistent decode error (dropped registry connection,
					// expired pullCtx) repeats forever — retrying the Decode
					// here busy-loops at 100% CPU and wedges syncGateways.
					streamErr = err
				}
				break
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

		if streamErr != nil {
			log.Printf("[DRIVER-MANAGER] Pull stream error for %s (attempt %d/%d): %v", imageName, attempt+1, maxPullRetries, streamErr)
			continue
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

// imageNameForDriver returns the Docker image name for a driver type, or "" if unknown.
func (m *Manager) imageNameForDriver(driverType string) string {
	switch driverType {
	case "S7":
		return "industrial-driver-s7:latest"
	case "MODBUS_TCP":
		return "industrial-driver-modbus:latest"
	case "REDIS":
		return "industrial-driver-redis:latest"
	case "MQTT":
		return "industrial-driver-mqtt:latest"
	case "OPC_UA":
		return "industrial-driver-opcua:latest"
	case "LORAWAN":
		return "industrial-driver-lorawan:latest"
	}
	return ""
}

// startGatewayContainer starts a driver container for a gateway.
// The caller must hold m.mu. Image must already be cached (call pullImageWithRetry first).
func (m *Manager) startGatewayContainer(gateway models.Gateway) error {
	log.Printf("[DRIVER-MANAGER] Starting container for gateway %d (%s, type: %s)", gateway.ID, gateway.Name, gateway.DriverType)

	// Determine driver image and service name based on driver type
	imageName := m.imageNameForDriver(gateway.DriverType)
	var containerNamePrefix string
	switch gateway.DriverType {
	case "S7":
		containerNamePrefix = "driver-s7"
	case "MODBUS_TCP":
		containerNamePrefix = "driver-modbus"
	case "REDIS":
		containerNamePrefix = "driver-redis"
	case "MQTT":
		containerNamePrefix = "driver-mqtt"
	case "OPC_UA":
		containerNamePrefix = "driver-opcua"
	case "LORAWAN":
		containerNamePrefix = "driver-lorawan"
	default:
		return fmt.Errorf("unsupported driver type: %s", gateway.DriverType)
	}

	containerName := fmt.Sprintf("%s-%d", containerNamePrefix, gateway.ID)

	// 1. Ensure network exists
	if err := m.ensureNetwork(); err != nil {
		return fmt.Errorf("network verification failed: %w", err)
	}

	// 2. Resolve hostnames to IPs (image already cached by syncGateways pre-pull)
	dbHost := m.resolveHostname(getEnv("DB_HOST", "postgres"))
	mqttHost := m.resolveHostname(getEnv("MQTT_HOST", "mosquitto"))

	// 4. Resolve resource limits for this driver type
	resources := driverResourceLimits[gateway.DriverType]

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
		Resources: container.Resources{
			NanoCPUs: resources.CPU,
			Memory:   resources.Memory,
		},
		NetworkMode: container.NetworkMode(m.networkID),
		ExtraHosts:  []string{"host.docker.internal:host-gateway"},
	}

	// 6. Adopt an already-running container instead of force-recreating it.
	// After a driver-manager restart the state map is empty; without this
	// check every healthy driver gets killed and recreated, causing a
	// fleet-wide data gap on every manager restart.
	// Context-bounded like every other docker call made under m.mu — a hung
	// dockerd must not wedge the manager holding the global lock.
	adoptCtx, adoptCancel := context.WithTimeout(m.ctx, 15*time.Second)
	defer adoptCancel()
	if inspect, err := m.dockerClient.ContainerInspect(adoptCtx, containerName); err == nil {
		if inspect.State != nil && inspect.State.Running && inspect.Config != nil && inspect.Config.Image == imageName {
			m.gatewayStates[gateway.ID] = &GatewayState{
				Gateway:     gateway,
				ContainerID: inspect.ID,
				Running:     true,
			}
			m.publishGatewayStatus(gateway.ID, "online", "")
			log.Printf("[DRIVER-MANAGER] ✓ Adopted running container for gateway %d (%s)", gateway.ID, containerName)
			return nil
		}
		// Exists but stopped/wrong image: remove and recreate below.
		_ = m.dockerClient.ContainerRemove(adoptCtx, containerName, types.ContainerRemoveOptions{Force: true})
	}

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

	// Pre-pull images for enabled gateways BEFORE acquiring the lock.
	// Docker image pulls can take minutes; holding the mutex during that time
	// would block OTA commands and other concurrent operations.
	// docker pull is idempotent — returns instantly when image is already cached.
	for _, gw := range gateways {
		if gw.Enabled {
			if img := m.imageNameForDriver(gw.DriverType); img != "" {
				m.pullMu.Lock()
				fail, cooling := m.pullFailures[img]
				m.pullMu.Unlock()
				if cooling && time.Now().Before(fail.nextTry) {
					continue // image in cooldown after repeated pull failures
				}
				if err := m.pullImageWithRetry(img); err != nil {
					log.Printf("[DRIVER-MANAGER] Pre-pull failed for gateway %d (%s): %v", gw.ID, gw.DriverType, err)
					m.pullMu.Lock()
					fail.failures++
					fail.nextTry = time.Now().Add(failureCooldown(fail.failures))
					m.pullFailures[img] = fail
					m.pullMu.Unlock()
				} else {
					m.pullMu.Lock()
					delete(m.pullFailures, img)
					m.pullMu.Unlock()
				}
			}
		}
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
				// After maxStartRetries consecutive failures, back off with an
				// exponential cooldown instead of retrying every poll cycle.
				if state != nil && state.ConsecutiveErrors >= maxStartRetries &&
					time.Since(state.LastErrorTime) < failureCooldown(state.ConsecutiveErrors-maxStartRetries+1) {
					continue
				}

				// Start container (image already cached by pre-pull above)
				if err := m.startGatewayContainer(gateway); err != nil {
					log.Printf("[DRIVER-MANAGER] ERROR: Failed to start container for gateway %d (%s): %v",
						gateway.ID, gateway.Name, err)

					// Track error state (creating the entry if this gateway
					// never started, so the cooldown above can apply)
					if state == nil {
						state = &GatewayState{Gateway: gateway}
						m.gatewayStates[gateway.ID] = state
					}
					state.ConsecutiveErrors++
					state.LastError = err.Error()
					state.LastErrorTime = time.Now()

					if state.ConsecutiveErrors >= maxStartRetries {
						log.Printf("[DRIVER-MANAGER] Gateway %d exceeded max retries (%d), entering cooldown",
							gateway.ID, maxStartRetries)
						m.publishGatewayStatus(gateway.ID, "error",
							fmt.Sprintf("Exceeded max retries. Last error: %s", err.Error()))
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

	// Handle deleted gateways (not in current query results).
	// The state entry must be removed once the container is stopped —
	// stale entries get resurrected by the OTA restart/update handlers,
	// which iterate gatewayStates and restart whatever they find.
	for id, state := range m.gatewayStates {
		if processedIDs[id] {
			continue
		}
		if state.Running {
			log.Printf("[DRIVER-MANAGER] Gateway %d no longer exists in database, stopping container", id)
			if err := m.stopGatewayContainer(id); err != nil {
				log.Printf("[DRIVER-MANAGER] ERROR: Failed to stop container for deleted gateway %d: %v", id, err)
				continue // keep the entry so the stop is retried next cycle
			}
		}
		delete(m.gatewayStates, id)
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

	// Stop container. StopOptions.Timeout is in SECONDS (int(duration) would
	// be a nanosecond count ≈ 317 years of SIGKILL grace); the context bound
	// keeps a hung dockerd from blocking the manager under m.mu forever.
	timeout := int(containerStopTimeout.Seconds())
	stopCtx, stopCancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer stopCancel()
	if err := m.dockerClient.ContainerStop(stopCtx, state.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
		if client.IsErrNotFound(err) {
			// Container already gone (removed out-of-band): treat as stopped
			// so the state machine doesn't retry forever.
			log.Printf("[DRIVER-MANAGER] Container for gateway %d already gone, marking stopped", gatewayID)
			state.Running = false
			return nil
		}
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
			timeout := int(containerStopTimeout.Seconds())
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := m.dockerClient.ContainerStop(stopCtx, state.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
				log.Printf("[DRIVER-MANAGER] ERROR: Failed to stop container %s: %v", state.ContainerID, err)
			}
			stopCancel()
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

// startOTASubscriber subscribes to sys/update/# and sys/restart/# via a dedicated
// MQTT connection. When a message arrives, it pulls the new image (OTA) or
// restarts all managed containers (restart).
func (m *Manager) startOTASubscriber(mqttHost string, mqttPort int) {
	broker := fmt.Sprintf("tcp://%s:%d", mqttHost, mqttPort)
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID("driver-manager-ota").
		SetUsername(getEnv("MQTT_USERNAME", "")).
		SetPassword(getEnv("MQTT_PASSWORD", "")).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Println("[OTA] Connected to MQTT broker, subscribing to sys/update/# and sys/restart/#")
			c.Subscribe("sys/update/#", 1, m.handleOTAUpdate)
			c.Subscribe("sys/restart/#", 1, m.handleOTARestart)
		})

	client := mqtt.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		log.Printf("[OTA] Initial MQTT connect failed (will retry): %v", tok.Error())
	}

	// Block until the manager context is cancelled
	<-m.ctx.Done()
	client.Disconnect(500)
}

// handleOTAUpdate processes sys/update/{org_id}: pulls the new image and
// recreates every running driver container.
func (m *Manager) handleOTAUpdate(_ mqtt.Client, msg mqtt.Message) {
	var payload struct {
		Version string `json:"version"`
		Image   string `json:"image"`
	}
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[OTA] Bad update payload: %v", err)
		return
	}
	if payload.Image == "" || payload.Version == "" {
		log.Printf("[OTA] Update message missing image or version")
		return
	}
	log.Printf("[OTA] Received OTA update command: image=%s version=%s", payload.Image, payload.Version)

	// The MQTT OTA path has no checksum/signature (unlike the HTTP update
	// poller, which verifies SHA256 — prefer that mechanism). Any client
	// with broker credentials can publish here, so restrict pulls to the
	// deployment's own registry namespace.
	allowedPrefix := getEnv("OTA_IMAGE_PREFIX", "ghcr.io/inferis995/")
	if !strings.HasPrefix(payload.Image, allowedPrefix) {
		log.Printf("[OTA] REJECTED update: image %q outside allowed prefix %q (set OTA_IMAGE_PREFIX to change)", payload.Image, allowedPrefix)
		return
	}

	// Pull the new image
	pullCtx, cancel := context.WithTimeout(m.ctx, imagePullTimeout)
	defer cancel()

	reader, err := m.dockerClient.ImagePull(pullCtx, payload.Image, types.ImagePullOptions{})
	if err != nil {
		log.Printf("[OTA] Image pull failed: %v", err)
		return
	}
	io.Copy(io.Discard, reader)
	reader.Close()
	log.Printf("[OTA] Pulled image %s", payload.Image)

	// Restart only gateways that are actually running and still enabled —
	// stale/disabled entries must not be resurrected by an OTA command.
	m.mu.Lock()
	gateways := make([]models.Gateway, 0, len(m.gatewayStates))
	for _, state := range m.gatewayStates {
		if state.Running && state.Gateway.Enabled {
			gateways = append(gateways, state.Gateway)
		}
	}
	m.mu.Unlock()

	for _, gw := range gateways {
		log.Printf("[OTA] Restarting container for gateway %d after OTA", gw.ID)
		m.mu.Lock()
		_ = m.stopGatewayContainer(gw.ID)
		m.mu.Unlock()
		m.mu.Lock()
		if err := m.startGatewayContainer(gw); err != nil {
			log.Printf("[OTA] Failed to restart gateway %d: %v", gw.ID, err)
		}
		m.mu.Unlock()
	}
	log.Printf("[OTA] OTA update complete for %d gateways", len(gateways))
}

// handleOTARestart processes sys/restart/{org_id}: bounces all running containers.
func (m *Manager) handleOTARestart(_ mqtt.Client, msg mqtt.Message) {
	log.Printf("[OTA] Received restart command (topic: %s)", msg.Topic())

	m.mu.Lock()
	gateways := make([]models.Gateway, 0, len(m.gatewayStates))
	for _, state := range m.gatewayStates {
		if state.Running && state.Gateway.Enabled {
			gateways = append(gateways, state.Gateway)
		}
	}
	m.mu.Unlock()

	for _, gw := range gateways {
		log.Printf("[OTA] Restarting gateway %d on restart command", gw.ID)
		m.mu.Lock()
		_ = m.stopGatewayContainer(gw.ID)
		_ = m.startGatewayContainer(gw)
		m.mu.Unlock()
	}
	log.Printf("[OTA] Restart complete for %d gateways", len(gateways))
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

// ─── OTA update poller ────────────────────────────────────────────────────────

// runUpdatePoller polls /api/edge/update-check every 5 minutes. When an
// approved update is found it delegates the actual apply to checkAndApplyUpdate.
func runUpdatePoller(ctx context.Context, orgIDStr, currentVersion, apiURL, apiToken string) {
	if apiURL == "" || apiToken == "" {
		log.Println("[UPDATE-POLL] CORE_API_URL or CORE_API_TOKEN not set — update polling disabled")
		return
	}
	orgID, err := strconv.Atoi(orgIDStr)
	if err != nil || orgID <= 0 {
		log.Printf("[UPDATE-POLL] Invalid ORG_ID %q — update polling disabled", orgIDStr)
		return
	}

	log.Printf("[UPDATE-POLL] Starting — org=%d current_version=%s endpoint=%s", orgID, currentVersion, apiURL)

	// Run once immediately, then on every tick.
	checkAndApplyUpdate(orgID, currentVersion, apiURL, apiToken)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[UPDATE-POLL] Stopped")
			return
		case <-ticker.C:
			checkAndApplyUpdate(orgID, currentVersion, apiURL, apiToken)
		}
	}
}

// checkAndApplyUpdate fetches update info and, if an approved update is waiting,
// downloads it, verifies SHA256, applies it, and reports the result back.
func checkAndApplyUpdate(orgID int, currentVersion, apiURL, apiToken string) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// 1. Poll update-check endpoint.
	checkURL := fmt.Sprintf("%s/api/edge/update-check?org_id=%d&current_version=%s",
		apiURL, orgID, currentVersion)
	req, err := http.NewRequest("GET", checkURL, nil)
	if err != nil {
		log.Printf("[UPDATE-POLL] Failed to build check request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[UPDATE-POLL] Check request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var checkResp struct {
		UpdateAvailable bool   `json:"update_available"`
		ReleaseID       *int   `json:"release_id"`
		Version         string `json:"version"`
		ArtifactURL     string `json:"artifact_url"`
		SHA256          string `json:"sha256"`
		Approved        bool   `json:"approved"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&checkResp); err != nil {
		log.Printf("[UPDATE-POLL] Failed to decode check response: %v", err)
		return
	}

	if !checkResp.UpdateAvailable || !checkResp.Approved || checkResp.ReleaseID == nil {
		return // nothing to do
	}
	if checkResp.ArtifactURL == "" || checkResp.SHA256 == "" {
		log.Printf("[UPDATE-POLL] Missing artifact_url or sha256 in response")
		return
	}

	log.Printf("[UPDATE-POLL] Approved update found: version=%s release_id=%d", checkResp.Version, *checkResp.ReleaseID)

	// 2. Download artifact. Sanitize version to prevent path traversal.
	safeVersion := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return '_'
	}, checkResp.Version)
	artifactPath := fmt.Sprintf("/tmp/edge-update-%s.tar.gz", safeVersion)
	if err := downloadFile(httpClient, checkResp.ArtifactURL, artifactPath); err != nil {
		log.Printf("[UPDATE-POLL] Download failed: %v", err)
		return
	}
	defer func() { _ = os.Remove(artifactPath) }()

	// 3. Verify SHA256 BEFORE applying (security critical).
	if err := verifySHA256(artifactPath, checkResp.SHA256); err != nil {
		log.Printf("[UPDATE-POLL] SHA256 verification failed: %v — aborting update", err)
		postUpdateStatus(httpClient, apiURL, apiToken, orgID, *checkResp.ReleaseID, "failed",
			"SHA256 verification failed: "+err.Error())
		return
	}
	log.Printf("[UPDATE-POLL] SHA256 verified for %s", artifactPath)

	// 4. Report updating.
	postUpdateStatus(httpClient, apiURL, apiToken, orgID, *checkResp.ReleaseID, "updating", "")

	// 5. Apply update — execute /edge-update.sh if it exists, otherwise docker pull + restart.
	applyErr := applyUpdate(artifactPath, checkResp.Version)
	if applyErr != nil {
		// No automatic rollback exists yet: report the honest status so
		// operators know the edge is still on the previous (or a broken)
		// version, instead of a fictitious "rolled_back".
		log.Printf("[UPDATE-POLL] Apply failed: %v", applyErr)
		postUpdateStatus(httpClient, apiURL, apiToken, orgID, *checkResp.ReleaseID, "apply_failed",
			"apply failed: "+applyErr.Error())
		return
	}

	// 6. Health check.
	if err := healthCheck(apiURL); err != nil {
		log.Printf("[UPDATE-POLL] Post-update health check failed: %v", err)
		postUpdateStatus(httpClient, apiURL, apiToken, orgID, *checkResp.ReleaseID, "apply_failed",
			"health check failed after apply: "+err.Error())
		return
	}

	log.Printf("[UPDATE-POLL] Update to %s completed successfully", checkResp.Version)
	postUpdateStatus(httpClient, apiURL, apiToken, orgID, *checkResp.ReleaseID, "success", "")
}

const maxArtifactBytes = 500 * 1024 * 1024 // 500 MB hard cap — prevents disk exhaustion via OTA

// downloadFile downloads a URL to a local path.
func downloadFile(httpClient *http.Client, url, destPath string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxArtifactBytes)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// verifySHA256 checks that the file at path matches the expected hex digest.
func verifySHA256(path, expectedHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := fmt.Sprintf("%x", h.Sum(nil))
	if got != strings.ToLower(expectedHex) {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, expectedHex)
	}
	return nil
}

// applyUpdate runs /edge-update.sh if it exists (custom update script),
// otherwise executes a docker pull + restart sequence with a 10-minute timeout.
func applyUpdate(artifactPath, version string) error {
	applyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// If a custom update script exists, delegate to it.
	scriptPath := "/edge-update.sh"
	if _, err := os.Stat(scriptPath); err == nil {
		log.Printf("[UPDATE-POLL] Running custom update script %s", scriptPath)
		cmd := exec.CommandContext(applyCtx, scriptPath, artifactPath, version)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Default: load the downloaded tar.gz image archive then restart.
	log.Printf("[UPDATE-POLL] No update script found — loading image from %s", artifactPath)
	loadCmd := exec.CommandContext(applyCtx, "docker", "load", "-i", artifactPath)
	loadCmd.Stdout = os.Stdout
	loadCmd.Stderr = os.Stderr
	if err := loadCmd.Run(); err != nil {
		return fmt.Errorf("docker load failed: %w", err)
	}

	// Restart the driver-manager service container.
	restartCmd := exec.CommandContext(applyCtx, "docker", "restart", "openedge-driver-manager")
	restartCmd.Stdout = os.Stdout
	restartCmd.Stderr = os.Stderr
	return restartCmd.Run()
}

// healthCheck pings /health on the core API to confirm the system is still up.
func healthCheck(apiURL string) error {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Get(apiURL + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// runMQTTWatchdog checks MQTT connectivity every 30s and reconnects with exponential backoff.
func runMQTTWatchdog(mqttClient mqtt.Client, brokerURL, clientID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !mqttClient.IsConnected() {
			log.Println("[WATCHDOG] MQTT disconnected, reconnecting...")
			for attempt, wait := 1, time.Second; attempt <= 10; attempt++ {
				token := mqttClient.Connect()
				token.Wait()
				if mqttClient.IsConnected() {
					log.Printf("[WATCHDOG] MQTT reconnected after %d attempt(s)", attempt)
					break
				}
				log.Printf("[WATCHDOG] Reconnect attempt %d failed, waiting %s", attempt, wait)
				time.Sleep(wait)
				if wait < 60*time.Second {
					wait *= 2
				}
			}
		}
	}
}

// runHeartbeat POSTs to core API every 30s.
func runHeartbeat(orgID int, apiURL, apiToken, agentVersion string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		sendHeartbeat(orgID, apiURL, apiToken, agentVersion)
	}
}

// sendHeartbeat sends a single heartbeat POST to core API.
func sendHeartbeat(orgID int, apiURL, apiToken, agentVersion string) {
	body, _ := json.Marshal(map[string]interface{}{
		"org_id":        orgID,
		"agent_version": agentVersion,
		"ts":            time.Now().Unix(),
	})
	req, err := http.NewRequest("POST", apiURL+"/api/edge/heartbeat", strings.NewReader(string(body)))
	if err != nil {
		log.Printf("[HEARTBEAT] Failed to build request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[HEARTBEAT] Request failed: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[HEARTBEAT] org=%d status=%d", orgID, resp.StatusCode)
}

// startHealthServer starts a minimal HTTP server for Docker health checks.
func startHealthServer(port string) {
	if port == "" {
		port = "8081"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","ts":%d}`, time.Now().Unix())
	})
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("[HEALTH] Health server error: %v", err)
	}
}

// postUpdateStatus sends a status update to /api/edge/update-status.
func postUpdateStatus(httpClient *http.Client, apiURL, apiToken string, orgID, releaseID int, status, errMsg string) {
	body, _ := json.Marshal(map[string]interface{}{
		"org_id":     orgID,
		"release_id": releaseID,
		"status":     status,
		"error_msg":  errMsg,
	})
	req, err := http.NewRequest("POST", apiURL+"/api/edge/update-status", strings.NewReader(string(body)))
	if err != nil {
		log.Printf("[UPDATE-POLL] postUpdateStatus build request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[UPDATE-POLL] postUpdateStatus request: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[UPDATE-POLL] Status reported: org=%d release=%d status=%s", orgID, releaseID, status)
}

// ─── End OTA update poller ────────────────────────────────────────────────────

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
