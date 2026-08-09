package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/alarms"
	"github.com/ralph/industrial-edge-middleware/internal/db"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/sparkplug"
)

// TagMapping maps a source MQTT topic to a system publish topic and tag metadata
type TagMapping struct {
	Tag          models.Tag
	SourceTopic  string // The PLC's MQTT topic (from tag.Code)
	PublishTopic string // The system topic: data/{org}/{site}/{area}/{gw}/{alias}
	OrgID        int
}

// GatewayConfig holds the loaded gateway configuration
type GatewayConfig struct {
	Gateway     models.Gateway
	Tags        []models.Tag
	TagMappings []TagMapping
	OrgName     string
	OrgID       int
	Site        string
	Area        string
}

// sourceMessage is an inbound PLC message queued for asynchronous processing.
// subTopic is the subscription key used to resolve the CURRENT TagMapping at
// processing time (so reloads take effect for in-flight subscriptions).
type sourceMessage struct {
	subTopic string
	topic    string
	payload  []byte
}

// TagPayload is the standard system message format
type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// Driver is the main MQTT-to-MQTT bridge driver
type Driver struct {
	gatewayID    int
	database     *sql.DB
	mqttClient   *mqtt.Client // Internal broker (system) — used to publish bridged data
	sourceClient *mqtt.Client // Optional EXTERNAL broker — used to subscribe to PLC topics.
	                          // When nil, the driver subscribes on the internal broker
	                          // (the PLCs publish straight to OpenEdge).
	// sourceWanted is true when connection_config asks for an external broker.
	// Used to suppress wrong subscriptions on the internal broker while the
	// external one is still (re)connecting.
	sourceWanted   bool
	sourceRetrying bool   // a retry goroutine is already running
	sourceParams   string // connection params the current source client was built with
	sourceMu       sync.Mutex
	config         *GatewayConfig
	configMu       sync.RWMutex
	stopChan       chan struct{}
	reloadChan     chan struct{}
	isConnected    atomic.Bool
	connStateMu    sync.Mutex // serializes isConnected swap + health publish
	subscribedTags map[string]bool // Track subscribed source topics
	subMu          sync.Mutex

	// Decoupled inbound processing: subscription callbacks only enqueue into
	// msgChan (never block paho dispatch); a worker goroutine consumes it and
	// resolves the CURRENT mapping from topicMappings at message time.
	msgChan       chan sourceMessage
	topicMappings map[string]TagMapping // source topic → current mapping
	mappingMu     sync.RWMutex

	// Source connectivity tracking
	lastMessageTime map[int]time.Time
	connectionLost  map[int]bool
	msgTimeMu       sync.Mutex

	// Sparkplug B support
	sparkplugClient *sparkplug.SparkplugClient
	dualPublisher   *sparkplug.DualPublisher
	sparkplugMu     sync.RWMutex

	// Alarm Manager
	alarmManager *alarms.Manager
}

// WriteCommand represents a write command received via MQTT
type WriteCommand struct {
	TagID    int         `json:"tag_id"`
	Code     string      `json:"code"`
	Value    interface{} `json:"value"`
	DataType string      `json:"data_type"`
}

// WriteResult is published to MQTT to confirm write success/failure
type WriteResult struct {
	TagID     int         `json:"tag_id"`
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	ReadBack  interface{} `json:"read_back,omitempty"`
	Timestamp int64       `json:"ts"`
}

func main() {
	log.Println("[DRIVER-MQTT] Starting driver-mqtt...")

	gatewayIDStr := os.Getenv("GATEWAY_ID")
	if gatewayIDStr == "" {
		log.Fatal("[DRIVER-MQTT] GATEWAY_ID environment variable is required")
	}
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		log.Fatalf("[DRIVER-MQTT] Invalid GATEWAY_ID: %v", err)
	}

	// Connect to database
	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "industrial_user"),
		Password: getEnv("DB_PASSWORD", "industrial_pass"),
		Database: getEnv("DB_NAME", "industrial_edge"),
	}

	database, err := db.Connect(dbCfg)
	if err != nil {
		log.Fatalf("[DRIVER-MQTT] Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("[DRIVER-MQTT] Connected to database")

	// Connect to internal MQTT broker (system broker)
	mqttCfg := mqtt.Config{
		Host:          getEnv("MQTT_HOST", "localhost"),
		Port:          getEnvInt("MQTT_PORT", 1883),
		ClientID:      fmt.Sprintf("driver-mqtt-%d", gatewayID),
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
		LWTTopic:      fmt.Sprintf("sys/health/%d", gatewayID),
		LWTPayload:    "offline",
		LWTRetained:   true,
	}

	mqttClient := mqtt.NewClient(mqttCfg)
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("[DRIVER-MQTT] Failed to connect to MQTT broker: %v", err)
	}
	defer mqttClient.Disconnect(1000)
	log.Println("[DRIVER-MQTT] Connected to internal MQTT broker")

	// NOTE: Health status will be published by setConnectionState() when subscriptions are active
	// This ensures the status reflects actual driver readiness, not just startup

	driver := &Driver{
		gatewayID:       gatewayID,
		database:        database,
		mqttClient:      mqttClient,
		stopChan:        make(chan struct{}),
		reloadChan:      make(chan struct{}, 1),
		subscribedTags:  make(map[string]bool),
		msgChan:         make(chan sourceMessage, 1024),
		topicMappings:   make(map[string]TagMapping),
		lastMessageTime: make(map[int]time.Time),
		connectionLost:  make(map[int]bool),
	}

	// Initialize Sparkplug B client (optional - for dual publishing)
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		log.Println("[DRIVER-MQTT] Sparkplug B dual publishing enabled")
	}

	// Load initial configuration
	if err := driver.loadConfig(); err != nil {
		log.Fatalf("[DRIVER-MQTT] Failed to load config: %v", err)
	}

	// Subscribe to reload commands
	reloadTopic := fmt.Sprintf("sys/command/reload/%d", gatewayID)
	mqttClient.Subscribe(reloadTopic, driver.handleReloadCommand)
	log.Printf("[DRIVER-MQTT] Subscribed to reload topic: %s", reloadTopic)

	// Subscribe to write commands
	writeTopic := fmt.Sprintf("cmd/write/%d", gatewayID)
	mqttClient.Subscribe(writeTopic, driver.handleWriteCommand)
	log.Printf("[DRIVER-MQTT] Subscribed to write topic: %s", writeTopic)

	// Subscribe to all source PLC topics
	driver.subscribeToSourceTopics()

	// Initialize Alarm Manager
	driver.alarmManager = alarms.NewManager(database, mqttClient, gatewayID)

	driver.alarmManager.OnAlarmEvent = func(eventID int, tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
		// Publish on sys/alarms: this is the ONLY route to core-api's
		// handleAlarmEvent and therefore to the notification dispatcher.
		//
		// The previous publishDual(tagID, alias+"_Alarm", status == "ACTIVE", ...)
		// was dropped on purpose: publishDual emits a data/ payload keyed by
		// tag_id, so core-api's handleDataUpdate overwrote realtime:{tagID} and
		// tag_shadow:{tagID} with true/false — the measured value was replaced
		// by the alarm boolean on the HMI until the next poll. The alarm state
		// now travels on its own topic, where it cannot be mistaken for it.
		driver.publishAlarmEvent(eventID, tagID, alias, def, val, status)
	}

	go driver.alarmManager.StartTicker(context.Background())

	// Start the main loop
	go driver.run()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[DRIVER-MQTT] Shutting down...")
	// Announce the edge node's death to Sparkplug hosts before we go.
	driver.publishSparkplugNodeDeath()
	close(driver.stopChan)

	// Publish offline status on shutdown
	healthTopic := fmt.Sprintf("sys/health/%d", gatewayID)
	mqttClient.PublishWithQoS(healthTopic, "offline", 1, true)
	log.Println("[DRIVER-MQTT] Shutdown complete")
}

// publishSparkplugNodeDeath publishes NDEATH on a graceful shutdown.
//
// It cannot ride on the MQTT Last Will: MQTT allows exactly one Will per
// connection and this driver's single mqtt.Client already wills
// sys/health/{gateway_id}="offline", which core-api and the OpenEdge UI depend
// on for gateway status (published again just below on a clean stop). So the
// death is announced explicitly here; without it a host keeps this node's
// metrics marked live forever after a clean stop.
func (d *Driver) publishSparkplugNodeDeath() {
	d.sparkplugMu.RLock()
	spClient := d.sparkplugClient
	d.sparkplugMu.RUnlock()

	if spClient == nil {
		return
	}
	spClient.Disconnect() // sends NDEATH
}

// loadConfig reads the gateway and tag configuration from the database
func (d *Driver) loadConfig() error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	query := `
		SELECT g.id, g.area_id, g.name, g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled, g.zero_based,
		       o.name as org_name, o.id as org_id, s.name as site_name, a.name as area_name
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`

	var gateway models.Gateway
	var connConfigBytes []byte
	var orgName, siteName, areaName string
	var orgID int

	err := d.database.QueryRow(query, d.gatewayID).Scan(
		&gateway.ID, &gateway.AreaID, &gateway.Name, &gateway.DriverType, &connConfigBytes,
		&gateway.ScanRateMs, &gateway.Enabled, &gateway.ZeroBased, &orgName, &orgID, &siteName, &areaName,
	)
	if err != nil {
		return fmt.Errorf("failed to load gateway: %w", err)
	}

	if err := json.Unmarshal(connConfigBytes, &gateway.ConnectionConfig); err != nil {
		log.Printf("[DRIVER-MQTT] ERROR: failed to parse connection_config: %v — external broker settings unavailable, using internal broker", err)
	}

	// If the gateway's connection_config points to an EXTERNAL MQTT broker
	// (e.g. the customer's factory broker where PLCs publish), open a second
	// MQTT client dedicated to source subscriptions. When the field is empty
	// the driver falls back to subscribing on the internal broker (the PLCs
	// publish straight to OpenEdge — the legacy behaviour).
	d.ensureSourceClient(gateway.ConnectionConfig)

	// Load tags (includes optional json_path for JSON payload extraction)
	tagsQuery := `SELECT id, gateway_id, code, alias, data_type, historize, historize_deadband, json_path FROM tags WHERE gateway_id = $1`
	rows, err := d.database.Query(tagsQuery, d.gatewayID)
	if err != nil {
		return fmt.Errorf("failed to load tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.GatewayID, &t.Code, &t.Alias, &t.DataType, &t.Historize, &t.HistorizeDeadband, &t.JsonPath); err != nil {
			log.Printf("[DRIVER-MQTT] ERROR: failed to scan tag row: %v — skipping row", err)
			continue
		}
		tags = append(tags, t)
	}
	// Abort the config swap on iteration error — keep the previous config
	// rather than silently applying a truncated tag list.
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate tags: %w", err)
	}

	// Build topic prefix: data/{org}/{site}/{area}/{gateway}
	prefix := fmt.Sprintf("data/%s/%s/%s/%s",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name))

	// Create tag mappings: source topic → system publish topic
	var mappings []TagMapping
	for _, tag := range tags {
		sourceTopic := strings.TrimSpace(tag.Code) // tag.Code = PLC's MQTT topic
		if sourceTopic == "" {
			log.Printf("[DRIVER-MQTT] WARNING: Tag %d (%s) has empty code, skipping", tag.ID, tag.Alias)
			continue
		}

		publishTopic := fmt.Sprintf("%s/%s", prefix, slugify(tag.Alias))

		mappings = append(mappings, TagMapping{
			Tag:          tag,
			SourceTopic:  sourceTopic,
			PublishTopic: publishTopic,
			OrgID:        orgID,
		})
	}

	// Atomically replace the per-topic mapping table so the message worker
	// resolves the CURRENT mapping (json_path, alias, publish topic) even for
	// subscriptions created before this reload.
	newTopicMappings := make(map[string]TagMapping, len(mappings))
	for _, m := range mappings {
		if _, exists := newTopicMappings[m.SourceTopic]; !exists {
			newTopicMappings[m.SourceTopic] = m
		}
	}
	d.mappingMu.Lock()
	d.topicMappings = newTopicMappings
	d.mappingMu.Unlock()

	d.config = &GatewayConfig{
		Gateway:     gateway,
		Tags:        tags,
		TagMappings: mappings,
		OrgName:     slugify(orgName),
		OrgID:       orgID,
		Site:        slugify(siteName),
		Area:        slugify(areaName),
	}

	// Initialize or update Sparkplug B client
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		d.initSparkplugClientLocked(slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name), orgID)
	}

	// Subscribe to legacy format write commands: cmd/{org}/{site}/{area}/{gateway}/+
	legacyWriteTopic := fmt.Sprintf("cmd/%s/%s/%s/%s/+",
		slugify(orgName), slugify(siteName), slugify(areaName), slugify(gateway.Name))
	d.mqttClient.Subscribe(legacyWriteTopic, d.handleLegacyWriteCommand)
	log.Printf("[DRIVER-MQTT] Subscribed to legacy write topic: %s", legacyWriteTopic)

	// Subscribe to Sparkplug B DCMD (Device Command) if enabled
	if getEnv("SPARKPLUG_ENABLED", "false") == "true" {
		groupID := sparkplug.BuildGroupID(slugify(orgName), slugify(siteName))
		edgeNodeID := sparkplug.BuildEdgeNodeID(slugify(areaName), slugify(gateway.Name))
		dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", groupID, edgeNodeID)
		d.mqttClient.Subscribe(dcmdTopic, d.handleSparkplugDCMD)
		log.Printf("[DRIVER-MQTT] Subscribed to Sparkplug B DCMD topic: %s", dcmdTopic)
	}

	log.Printf("[DRIVER-MQTT] Config loaded: %d tags, %d mappings", len(tags), len(mappings))
	return nil
}

// initSparkplugClientLocked initializes Sparkplug client while holding configMu lock
func (d *Driver) initSparkplugClientLocked(orgName, siteName, areaName, gatewayName string, orgID int) {
	d.sparkplugMu.Lock()
	defer d.sparkplugMu.Unlock()

	// Build Sparkplug B identifiers
	groupID := sparkplug.BuildGroupID(orgName, siteName)
	edgeNodeID := sparkplug.BuildEdgeNodeID(areaName, gatewayName)

	config := sparkplug.Config{
		MQTTHost:     getEnv("MQTT_HOST", "localhost"),
		MQTTPort:     getEnvInt("MQTT_PORT", 1883),
		MQTTClientID: fmt.Sprintf("sparkplug-mqtt-%d", d.gatewayID),
		GroupID:      groupID,
		EdgeNodeID:   edgeNodeID,
		EnableLegacy: true,
	}

	if d.sparkplugClient == nil {
		// SetConnected(true) publishes the NBIRTH announcing this edge node.
		// It must happen before any DBIRTH/DDATA, or a Sparkplug host discards
		// everything this gateway sends and loops asking for a rebirth.
		d.sparkplugClient = sparkplug.NewClient(config, d.mqttClient)
		d.sparkplugClient.SetConnected(true)
	}

	// Create or update dual publisher
	d.dualPublisher = sparkplug.NewDualPublisher(
		d.sparkplugClient,
		orgName,
		siteName,
		areaName,
		gatewayName,
		orgID,
	)

	log.Printf("[DRIVER-MQTT] Sparkplug B client initialized: group=%s, node=%s", groupID, edgeNodeID)
}

// publishDual publishes a tag value in both legacy and Sparkplug B formats
func (d *Driver) publishDual(tagID int, alias string, value interface{}, dataType string, quality int, timestamp int64) {
	d.sparkplugMu.RLock()
	dualPublisher := d.dualPublisher
	d.sparkplugMu.RUnlock()

	if dualPublisher != nil {
		// Use dual publisher
		if err := dualPublisher.Publish(tagID, alias, value, dataType, quality, timestamp, d.mqttClient); err != nil {
			log.Printf("[DRIVER-MQTT] Dual publish error for %s: %v", alias, err)
		}
	} else {
		// Legacy-only publish
		d.configMu.RLock()
		cfg := d.config
		d.configMu.RUnlock()

		if cfg != nil {
			topic := fmt.Sprintf("data/%s/%s/%s/%s/%s", cfg.OrgName, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
			payload, _ := json.Marshal(TagPayload{tagID, cfg.OrgID, value, timestamp, quality})
			if err := d.mqttClient.PublishWithQoS(topic, string(payload), 1, false); err != nil {
				log.Printf("[DRIVER-MQTT] Publish error for %s on %s: %v", alias, topic, err)
			}
		}
	}
}

// AlarmEventPayload is the sys/alarms message consumed by core-api
// (handleAlarmEvent) and forwarded to the cloud by engine-historian.
// EventID is the alarm_events row internal/alarms already wrote through the
// driver's own DB connection: core-api uses it to skip its INSERT/UPDATE so
// one alarm never produces two rows.
type AlarmEventPayload struct {
	EventID        int     `json:"event_id"`
	TagID          int     `json:"tag_id"`
	DefinitionID   int     `json:"definition_id"`
	Status         string  `json:"status"`
	AlarmType      string  `json:"alarm_type"`
	Severity       string  `json:"severity"`
	Message        string  `json:"message"`
	ValueAtTrigger float64 `json:"value_at_trigger"`
	Threshold      float64 `json:"threshold"`
	TagAlias       string  `json:"tag_alias"`
	Timestamp      int64   `json:"timestamp"`
}

// publishAlarmEvent publishes an alarm state change on
// sys/alarms/{org_id}/{site}/{area}/{gateway}/{tag_alias}, using the same
// slugified path components as the data/ topics (see publishDual).
// Always legacy JSON, never Sparkplug: core-api only subscribes to sys/alarms.
func (d *Driver) publishAlarmEvent(eventID int, tagID int, alias string, def models.AlarmDefinition, val float64, status string) {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	var threshold float64
	if def.Threshold != nil {
		threshold = *def.Threshold
	}

	topic := fmt.Sprintf("sys/alarms/%d/%s/%s/%s/%s",
		cfg.OrgID, cfg.Site, cfg.Area, slugify(cfg.Gateway.Name), slugify(alias))
	payload, err := json.Marshal(AlarmEventPayload{
		EventID:        eventID,
		TagID:          tagID,
		DefinitionID:   def.ID,
		Status:         status,
		AlarmType:      def.AlarmType,
		Severity:       def.Severity,
		Message:        def.Message,
		ValueAtTrigger: val,
		Threshold:      threshold,
		TagAlias:       alias,
		Timestamp:      time.Now().UnixMilli(),
	})
	if err != nil {
		log.Printf("[DRIVER-MQTT] Failed to encode alarm event for %s: %v", alias, err)
		return
	}
	// QoS 1: an alarm that nobody is paged for is the whole point of this
	// topic, so it must survive a broker hiccup. Not retained: it's an event.
	if err := d.mqttClient.PublishWithQoS(topic, string(payload), 1, false); err != nil {
		log.Printf("[DRIVER-MQTT] Failed to publish alarm event for %s on %s: %v", alias, topic, err)
	}
}

// clearSubscribedTags forgets all tracked source-topic subscriptions,
// optionally unsubscribing them from the given client first (pass nil when
// the old client is already disconnected — its subscriptions died with it).
// Callers must not hold subMu. Lock order: sourceMu (optional) → subMu.
func (d *Driver) clearSubscribedTags(unsubClient *mqtt.Client) {
	d.subMu.Lock()
	defer d.subMu.Unlock()
	for topic := range d.subscribedTags {
		if unsubClient != nil {
			unsubClient.Unsubscribe(topic)
		}
		delete(d.subscribedTags, topic)
	}
}

// ensureSourceClient opens or refreshes the dedicated MQTT client to the
// EXTERNAL broker described by the gateway connection_config. Called from
// every loadConfig: creates the client when broker_host appears, reconnects
// when the broker parameters change and tears the client down when
// broker_host is removed. Recognised keys (all optional, only broker_host is
// required to enable the feature):
//   broker_host, broker_port (default 1883), broker_tls (bool, default false),
//   broker_username, broker_password, broker_client_id (default auto).
// When broker_host is empty the driver keeps subscribing on the internal
// broker, matching the legacy behaviour.
func (d *Driver) ensureSourceClient(cc map[string]interface{}) {
	d.sourceMu.Lock()
	defer d.sourceMu.Unlock()

	host, _ := cc["broker_host"].(string)
	host = strings.TrimSpace(host)
	if host == "" {
		// No external broker (anymore). Tear down a previous source client and
		// forget its subscriptions so topics get re-subscribed internally.
		if d.sourceWanted {
			if d.sourceClient != nil {
				log.Printf("[DRIVER-MQTT] broker_host removed — disconnecting external source broker, falling back to internal broker")
				d.sourceClient.Disconnect(1000)
				d.sourceClient = nil
			}
			d.sourceWanted = false
			d.sourceParams = ""
			d.clearSubscribedTags(nil)
		}
		return
	}

	port := 1883
	if v, ok := cc["broker_port"]; ok {
		switch n := v.(type) {
		case float64:
			port = int(n)
		case int:
			port = n
		}
	}
	scheme := "tcp"
	if v, _ := cc["broker_tls"].(bool); v {
		scheme = "ssl"
	}
	username, _ := cc["broker_username"].(string)
	password, _ := cc["broker_password"].(string)
	clientID, _ := cc["broker_client_id"].(string)
	if clientID == "" {
		clientID = fmt.Sprintf("openedge-mqtt-src-%d", d.gatewayID)
	}

	cfg := mqtt.Config{
		Host:          host,
		Port:          port,
		Scheme:        scheme,
		ClientID:      clientID,
		Username:      username,
		Password:      password,
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
	}
	params := fmt.Sprintf("%s://%s:%d|%s|%s|%s", scheme, host, port, username, password, clientID)

	if d.sourceWanted && d.sourceParams == params {
		// Same broker as last time — nothing to do if the client exists or a
		// retry goroutine is already working on it.
		if d.sourceClient != nil || d.sourceRetrying {
			return
		}
	} else if d.sourceClient != nil {
		// Broker parameters changed at runtime: drop the old client and its
		// (now dead) subscriptions, then connect with the new parameters.
		log.Printf("[DRIVER-MQTT] source broker parameters changed — reconnecting to %s://%s:%d", scheme, host, port)
		d.sourceClient.Disconnect(1000)
		d.sourceClient = nil
		d.clearSubscribedTags(nil)
	} else if !d.sourceWanted {
		// broker_host was ADDED: existing subscriptions live on the INTERNAL
		// broker. Remove them there so the topics get subscribed on the new
		// external client instead.
		d.clearSubscribedTags(d.mqttClient)
	}
	d.sourceWanted = true
	d.sourceParams = params

	log.Printf("[DRIVER-MQTT] Opening EXTERNAL source broker connection: %s://%s:%d (user=%q)", scheme, host, port, username)
	src := mqtt.NewClient(cfg)
	if err := src.Connect(); err == nil {
		d.sourceClient = src
		return
	} else {
		log.Printf("[DRIVER-MQTT] external source broker connect failed (%v) — retrying in background; NOT falling back to internal broker (would subscribe to the wrong place)", err)
	}

	// Connect failed. Spawn a background goroutine that keeps trying with
	// capped backoff. Once it succeeds, set sourceClient and re-run
	// subscribeToSourceTopics so the bridged data starts flowing. A goroutine
	// left over from OLD parameters exits on its staleness check.
	d.sourceRetrying = true
	go d.retrySourceConnect(cfg, params)
}

// retrySourceConnect keeps trying to reach the external broker. Stops when the
// driver is stopping or when the broker parameters it was started for are no
// longer current. Backoff: 5s, then doubles up to 60s.
func (d *Driver) retrySourceConnect(cfg mqtt.Config, params string) {
	backoff := 5 * time.Second
	for {
		select {
		case <-d.stopChan:
			return
		case <-time.After(backoff):
		}
		d.sourceMu.Lock()
		stale := !d.sourceWanted || d.sourceParams != params || d.sourceClient != nil
		d.sourceMu.Unlock()
		if stale {
			return
		}
		src := mqtt.NewClient(cfg)
		if err := src.Connect(); err != nil {
			log.Printf("[DRIVER-MQTT] external source broker still unreachable (%v) — next try in %s", err, backoff)
			if backoff < 60*time.Second {
				backoff *= 2
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
			}
			continue
		}
		d.sourceMu.Lock()
		if !d.sourceWanted || d.sourceParams != params || d.sourceClient != nil {
			// Config changed while we were connecting — discard this client.
			d.sourceMu.Unlock()
			src.Disconnect(1000)
			return
		}
		d.sourceClient = src
		d.sourceRetrying = false
		d.sourceMu.Unlock()
		log.Printf("[DRIVER-MQTT] external source broker recovered — subscribing")
		d.subscribeToSourceTopics()
		return
	}
}

// subscribeClient returns the MQTT client that should be used for PLC source
// topic subscriptions. When an external broker is configured but not (yet)
// connected, returns nil — subscribeToSourceTopics will skip rather than
// subscribe on the wrong (internal) broker.
func (d *Driver) subscribeClient() *mqtt.Client {
	d.sourceMu.Lock()
	wanted := d.sourceWanted
	src := d.sourceClient
	d.sourceMu.Unlock()
	if wanted {
		return src // may be nil while the retry goroutine is connecting
	}
	return d.mqttClient
}

// subscribeToSourceTopics subscribes to all PLC source topics
func (d *Driver) subscribeToSourceTopics() {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		return
	}

	// Resolve the client before taking subMu (lock order: sourceMu → subMu,
	// never subMu → sourceMu — clearSubscribedTags is called under sourceMu).
	subClient := d.subscribeClient()
	if subClient == nil {
		log.Printf("[DRIVER-MQTT] external source broker not yet connected — skipping subscribe (will retry automatically)")
		return
	}

	d.subMu.Lock()
	defer d.subMu.Unlock()

	// Unsubscribe from topics no longer in config
	newTopics := make(map[string]bool)
	for _, m := range cfg.TagMappings {
		newTopics[m.SourceTopic] = true
	}

	for topic := range d.subscribedTags {
		if !newTopics[topic] {
			subClient.Unsubscribe(topic)
			delete(d.subscribedTags, topic)
			log.Printf("[DRIVER-MQTT] Unsubscribed from removed topic: %s", topic)
		}
	}

	// Subscribe to new topics. The closure captures ONLY the topic string —
	// the worker resolves the current TagMapping at message time, so reloaded
	// mappings (json_path, alias, publish topic) apply to old subscriptions.
	// The handler must never block: it enqueues and returns immediately so
	// paho dispatch (and config reload) can't stall behind processing.
	for _, mapping := range cfg.TagMappings {
		if d.subscribedTags[mapping.SourceTopic] {
			continue // Already subscribed
		}

		subTopic := mapping.SourceTopic
		err := subClient.Subscribe(subTopic, func(topic string, payload []byte) {
			select {
			case d.msgChan <- sourceMessage{subTopic: subTopic, topic: topic, payload: payload}:
			default:
				log.Printf("[DRIVER-MQTT] WARNING: message queue full — dropping message on %s", topic)
			}
		})
		if err != nil {
			log.Printf("[DRIVER-MQTT] ERROR: Failed to subscribe to %s: %v", subTopic, err)
			continue
		}

		d.subscribedTags[subTopic] = true
		log.Printf("[DRIVER-MQTT] Subscribed to PLC topic: %s → %s", subTopic, mapping.PublishTopic)
	}

	// Mark connection as online since we're listening
	if len(d.subscribedTags) > 0 && subClient.IsConnected() {
		d.setConnectionState(true)
	}
}

// processSourceMessages is the worker that consumes queued PLC messages and
// does the (potentially slow) alarm evaluation and QoS-1 publishing, keeping
// the MQTT subscription callbacks non-blocking.
func (d *Driver) processSourceMessages() {
	for {
		select {
		case <-d.stopChan:
			return
		case msg := <-d.msgChan:
			d.mappingMu.RLock()
			mapping, ok := d.topicMappings[msg.subTopic]
			d.mappingMu.RUnlock()
			if !ok {
				// Topic was removed from config after the message was queued
				continue
			}
			d.handleSourceMessage(mapping, msg.topic, msg.payload)
		}
	}
}

// handleSourceMessage processes a message received from a PLC source topic
func (d *Driver) handleSourceMessage(mapping TagMapping, topic string, payload []byte) {
	// Parse the incoming value based on data type. When the tag has a json_path
	// configured we first extract that field from the JSON payload (e.g.
	// {"temp":22.5,"hum":55} + json_path="temp" -> 22.5).
	jsonPath := ""
	if mapping.Tag.JsonPath != nil {
		jsonPath = strings.TrimSpace(*mapping.Tag.JsonPath)
	}
	value, err := parseIncomingValue(payload, mapping.Tag.DataType, jsonPath)
	if err != nil {
		log.Printf("[DRIVER-MQTT] ERROR parsing value from %s: %v (raw: %s)", topic, err, string(payload))
		return
	}

	d.msgTimeMu.Lock()
	d.lastMessageTime[mapping.Tag.ID] = time.Now()
	if d.connectionLost[mapping.Tag.ID] {
		log.Printf("[DRIVER-MQTT] Source connection restored for tag %s", mapping.Tag.Alias)
		d.connectionLost[mapping.Tag.ID] = false
	}
	d.msgTimeMu.Unlock()

	timestamp := time.Now().UnixMilli()

	// Evaluate alarms via AlarmManager (192 = GOOD quality)
	if d.alarmManager != nil {
		d.alarmManager.EvaluateTag(mapping.Tag.ID, mapping.Tag.Alias, value, 192)
	}

	// Use dual publishing (legacy + Sparkplug B)
	d.publishDual(mapping.Tag.ID, mapping.Tag.Alias, value, mapping.Tag.DataType, 0, timestamp)

	log.Printf("[DRIVER-MQTT] BRIDGED: %s → %s = %v", topic, mapping.Tag.Alias, value)
}

// extractJSONPath traverses a parsed JSON value with a dotted path. Supports
// nested keys ("a.b.c") and numeric array indices ("data.0.temp"). Returns
// (nil, false) when the path can't be resolved against the payload shape.
func extractJSONPath(root interface{}, path string) (interface{}, bool) {
	cur := root
	for _, segment := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if segment == "" || segment == "$" {
			continue
		}
		switch node := cur.(type) {
		case map[string]interface{}:
			next, ok := node[segment]
			if !ok {
				return nil, false
			}
			cur = next
		case []interface{}:
			idx, err := strconv.Atoi(segment)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// parseIncomingValue parses the raw MQTT payload into a typed value. When
// jsonPath is set, the payload is treated as JSON and only the value at that
// path is converted (e.g. {"temp":22} + "temp" -> 22). Both dotted ("a.b") and
// JSONPath-lite ("$.a.b") notations are accepted.
func parseIncomingValue(payload []byte, dataType, jsonPath string) (interface{}, error) {
	raw := strings.TrimSpace(string(payload))
	if raw == "" {
		return nil, fmt.Errorf("empty payload")
	}

	// Explicit json_path: parse, traverse, convert. Hard-fail if the path
	// doesn't resolve so the operator sees the typo in logs.
	if jsonPath != "" {
		var root interface{}
		if err := json.Unmarshal(payload, &root); err != nil {
			return nil, fmt.Errorf("payload is not JSON (json_path=%q): %w", jsonPath, err)
		}
		v, ok := extractJSONPath(root, jsonPath)
		if !ok {
			return nil, fmt.Errorf("json_path %q not found in payload", jsonPath)
		}
		return convertValue(v, dataType)
	}

	// Try to parse as JSON first (e.g. {"value": 23.5} or just 23.5)
	var jsonVal interface{}
	if err := json.Unmarshal(payload, &jsonVal); err == nil {
		// If it's a map, try to extract "value" or "v" field
		if m, ok := jsonVal.(map[string]interface{}); ok {
			if v, exists := m["value"]; exists {
				return convertValue(v, dataType)
			}
			if v, exists := m["v"]; exists {
				return convertValue(v, dataType)
			}
			if v, exists := m["val"]; exists {
				return convertValue(v, dataType)
			}
			return nil, fmt.Errorf("JSON object has no 'value', 'v', or 'val' field")
		}
		// It's a raw JSON value (number, string, bool)
		return convertValue(jsonVal, dataType)
	}

	// Not JSON, treat as raw string value
	return convertStringValue(raw, dataType)
}

// convertValue converts a parsed JSON value to the expected data type
func convertValue(v interface{}, dataType string) (interface{}, error) {
	switch strings.ToUpper(dataType) {
	case "BOOL":
		switch val := v.(type) {
		case bool:
			return val, nil
		case float64:
			return val != 0, nil
		case string:
			return strings.ToLower(val) == "true" || val == "1", nil
		}
		return false, nil

	case "INT", "UINT", "DINT":
		switch val := v.(type) {
		case float64:
			return val, nil
		case string:
			f, err := strconv.ParseFloat(val, 64)
			return f, err
		}
		return nil, fmt.Errorf("cannot convert %T to %s", v, dataType)

	case "REAL":
		switch val := v.(type) {
		case float64:
			return val, nil
		case string:
			f, err := strconv.ParseFloat(val, 64)
			return f, err
		}
		return nil, fmt.Errorf("cannot convert %T to %s", v, dataType)

	case "STRING":
		return fmt.Sprintf("%v", v), nil

	default:
		// Default: try to return as-is
		return v, nil
	}
}

// convertStringValue converts a raw string value to the expected data type
func convertStringValue(raw string, dataType string) (interface{}, error) {
	switch strings.ToUpper(dataType) {
	case "BOOL":
		lower := strings.ToLower(raw)
		return lower == "true" || lower == "1" || lower == "on" || lower == "yes", nil

	case "INT", "UINT", "DINT":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse '%s' as number: %w", raw, err)
		}
		return f, nil

	case "REAL":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse '%s' as float: %w", raw, err)
		}
		return f, nil

	case "STRING":
		return raw, nil

	default:
		// Try as number, fallback to string
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f, nil
		}
		return raw, nil
	}
}

// handleReloadCommand handles reload commands from the system
func (d *Driver) handleReloadCommand(topic string, payload []byte) {
	log.Printf("[DRIVER-MQTT] Reload command received: %s", string(payload))
	if d.alarmManager != nil {
		d.alarmManager.LoadDefinitions()
	}
	select {
	case d.reloadChan <- struct{}{}:
	default:
	}
}

// handleWriteCommand handles write commands for MQTT-based PLCs
// For MQTT PLCs, "writing" means publishing a value to the PLC's command topic
func (d *Driver) handleWriteCommand(topic string, payload []byte) {
	log.Printf("[DRIVER-MQTT] Received write command: %s", string(payload))

	var cmd WriteCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("[DRIVER-MQTT] Failed to unmarshal write command: %v", err)
		d.publishWriteResult(0, false, fmt.Sprintf("Invalid command: %v", err), nil)
		return
	}

	go d.executeWrite(cmd)
}

func (d *Driver) executeWrite(cmd WriteCommand) {
	// For writing, we publish to a write topic (convention: {source_topic}/set or cmd/{source_topic})
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		d.publishWriteResult(cmd.TagID, false, "Driver not configured", nil)
		return
	}

	// Find the tag mapping
	var targetMapping *TagMapping
	for i, m := range cfg.TagMappings {
		if m.Tag.ID == cmd.TagID {
			targetMapping = &cfg.TagMappings[i]
			break
		}
	}

	if targetMapping == nil {
		d.publishWriteResult(cmd.TagID, false, fmt.Sprintf("Tag %d not found in config", cmd.TagID), nil)
		return
	}

	// Build the write topic: convention is {source_topic}/set
	// Many MQTT-enabled PLCs use this convention (e.g. Wago, Tasmota)
	writeTopic := targetMapping.SourceTopic + "/set"

	// If connection_config has a write_topic_suffix override, use that
	if cfg.Gateway.ConnectionConfig != nil {
		if suffix, ok := cfg.Gateway.ConnectionConfig["write_topic_suffix"].(string); ok && suffix != "" {
			writeTopic = targetMapping.SourceTopic + suffix
		}
	}

	// Format the write value
	writePayload := formatWriteValue(cmd.Value, cmd.DataType)

	// Publish the write command on the broker the PLC actually listens to:
	// the external source broker when one is configured, otherwise the
	// internal broker (legacy setup where PLCs talk to OpenEdge directly).
	d.sourceMu.Lock()
	writeClient := d.sourceClient
	sourceWanted := d.sourceWanted
	d.sourceMu.Unlock()
	if writeClient == nil {
		if sourceWanted {
			d.publishWriteResult(cmd.TagID, false, "External source broker not connected", nil)
			return
		}
		writeClient = d.mqttClient
	}

	if err := writeClient.PublishWithQoS(writeTopic, writePayload, 1, false); err != nil {
		log.Printf("[DRIVER-MQTT] Write failed to %s: %v", writeTopic, err)
		d.publishWriteResult(cmd.TagID, false, fmt.Sprintf("Publish failed: %v", err), nil)
		return
	}

	log.Printf("[DRIVER-MQTT] Write sent to %s: %s", writeTopic, writePayload)
	d.publishWriteResult(cmd.TagID, true, "Write command sent to PLC topic", cmd.Value)
}

// formatWriteValue converts the value to a string suitable for MQTT publishing
func formatWriteValue(value interface{}, dataType string) string {
	switch strings.ToUpper(dataType) {
	case "BOOL":
		switch v := value.(type) {
		case bool:
			if v {
				return "true"
			}
			return "false"
		case float64:
			if v != 0 {
				return "true"
			}
			return "false"
		default:
			return fmt.Sprintf("%v", value)
		}
	default:
		return fmt.Sprintf("%v", value)
	}
}

// publishWriteResult publishes the write result to MQTT for feedback
func (d *Driver) publishWriteResult(tagID int, success bool, message string, readBack interface{}) {
	result := WriteResult{
		TagID:     tagID,
		Success:   success,
		Message:   message,
		ReadBack:  readBack,
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(result)
	if err != nil {
		log.Printf("[DRIVER-MQTT] Failed to marshal write result: %v", err)
		return
	}

	resultTopic := fmt.Sprintf("cmd/write/result/%d", d.gatewayID)
	d.mqttClient.PublishWithQoS(resultTopic, string(payload), 1, false)

	status := "✓"
	if !success {
		status = "✗"
	}
	log.Printf("[DRIVER-MQTT] Write result: tag=%d %s %s", tagID, status, message)
}

// handleLegacyWriteCommand handles write commands in legacy MQTT format
// Topic format: cmd/{org}/{site}/{area}/{gateway}/{alias}
// Payload: {"value": <value>} or just <value>
func (d *Driver) handleLegacyWriteCommand(topic string, payload []byte) {
	log.Printf("[DRIVER-MQTT] Received legacy write command: %s", topic)

	// Extract alias from topic
	parts := strings.Split(topic, "/")
	if len(parts) < 6 {
		log.Printf("[DRIVER-MQTT] Invalid legacy write topic format: %s", topic)
		return
	}
	alias := parts[5]

	// Find tag by alias
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER-MQTT] No config loaded, cannot process write")
		return
	}

	var targetTag *models.Tag
	for i := range cfg.Tags {
		if strings.EqualFold(cfg.Tags[i].Alias, alias) || strings.EqualFold(slugify(cfg.Tags[i].Alias), alias) {
			targetTag = &cfg.Tags[i]
			break
		}
	}

	if targetTag == nil {
		log.Printf("[DRIVER-MQTT] Tag with alias '%s' not found", alias)
		return
	}

	// Parse value from payload
	var value interface{}
	var cmd WriteCommand

	// Try to parse as full command first
	if err := json.Unmarshal(payload, &cmd); err == nil && cmd.Value != nil {
		value = cmd.Value
	} else {
		// Try to parse as direct value
		if err := json.Unmarshal(payload, &value); err != nil {
			log.Printf("[DRIVER-MQTT] Failed to parse value from payload: %v", err)
			return
		}
	}

	// Create WriteCommand and process it
	writeCmd := WriteCommand{
		TagID:    targetTag.ID,
		Code:     targetTag.Code,
		Value:    value,
		DataType: targetTag.DataType,
	}

	// Marshal and process through existing handler
	cmdBytes, _ := json.Marshal(writeCmd)
	d.handleWriteCommand(fmt.Sprintf("cmd/write/%d", d.gatewayID), cmdBytes)
}

// handleSparkplugDCMD handles Sparkplug B Device Commands
// Topic format: spBv1.0/{group}/DCMD/{node}/{device}
// Payload: Sparkplug B payload with metrics
func (d *Driver) handleSparkplugDCMD(topic string, payload []byte) {
	log.Printf("[DRIVER-MQTT] Received Sparkplug B DCMD: %s", topic)

	// Parse Sparkplug B topic
	_, err := sparkplug.ParseTopic(topic)
	if err != nil {
		log.Printf("[DRIVER-MQTT] Invalid Sparkplug DCMD topic: %v", err)
		return
	}

	// Parse Sparkplug B payload (simplified JSON format)
	var spPayload struct {
		Timestamp int64 `json:"timestamp"`
		Seq       int   `json:"seq"`
		Metrics   []struct {
			Name      string      `json:"name"`
			DataType  int         `json:"dataType"`
			Value     interface{} `json:"value"`
			Timestamp int64       `json:"timestamp"`
		} `json:"metrics"`
	}

	if err := json.Unmarshal(payload, &spPayload); err != nil {
		log.Printf("[DRIVER-MQTT] Failed to parse Sparkplug DCMD payload: %v", err)
		return
	}

	// Find config
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil {
		log.Printf("[DRIVER-MQTT] No config loaded, cannot process DCMD")
		return
	}

	// Process each metric
	for _, metric := range spPayload.Metrics {
		// Find tag by alias (metric name)
		var targetTag *models.Tag
		for i := range cfg.Tags {
			alias := cfg.Tags[i].Alias
			// Match with underscores (Sparkplug format) or original alias
			if strings.EqualFold(alias, metric.Name) ||
				strings.EqualFold(strings.ReplaceAll(alias, "-", "_"), metric.Name) {
				targetTag = &cfg.Tags[i]
				break
			}
		}

		if targetTag == nil {
			log.Printf("[DRIVER-MQTT] DCMD: Tag '%s' not found", metric.Name)
			continue
		}

		// Create WriteCommand and process
		writeCmd := WriteCommand{
			TagID:    targetTag.ID,
			Code:     targetTag.Code,
			Value:    metric.Value,
			DataType: targetTag.DataType,
		}

		log.Printf("[DRIVER-MQTT] DCMD: Writing to tag %d (%s) = %v", targetTag.ID, targetTag.Alias, metric.Value)

		cmdBytes, _ := json.Marshal(writeCmd)
		d.handleWriteCommand(fmt.Sprintf("cmd/write/%d", d.gatewayID), cmdBytes)
	}
}

// run is the main loop that handles config reloads and periodic health checks
func (d *Driver) run() {
	// Worker that processes queued PLC messages (keeps paho dispatch free)
	go d.processSourceMessages()

	healthTicker := time.NewTicker(30 * time.Second)
	refreshTicker := time.NewTicker(5 * time.Minute)

	log.Println("[DRIVER-MQTT] Main loop started (event-driven, no polling)")

	for {
		select {
		case <-d.stopChan:
			healthTicker.Stop()
			refreshTicker.Stop()
			return

		case <-d.reloadChan:
			log.Println("[DRIVER-MQTT] Reloading config...")
			if err := d.loadConfig(); err != nil {
				log.Printf("[DRIVER-MQTT] Config reload failed: %v", err)
			} else {
				// Re-subscribe to source topics (handles additions/removals)
				d.subscribeToSourceTopics()
				log.Println("[DRIVER-MQTT] Config reloaded and subscriptions updated")
			}

		case <-refreshTicker.C:
			log.Println("[DRIVER-MQTT] Periodic config refresh...")
			if err := d.loadConfig(); err == nil {
				d.subscribeToSourceTopics()
			}

		case <-healthTicker.C:
			// Periodic health ping. Derive health from the ACTUAL client the
			// driver ingests from (source client when an external broker is
			// configured, internal otherwise) so a dropped external broker
			// flips the gateway offline instead of staying latched online.
			ingestClient := d.subscribeClient()
			connected := ingestClient != nil && ingestClient.IsConnected()
			d.setConnectionState(connected)

			if connected {
				healthTopic := fmt.Sprintf("sys/health/%d", d.gatewayID)
				d.mqttClient.PublishWithQoS(healthTopic, "online", 1, true)
			}

			// Check for source topic timeouts
			d.checkSourceTimeouts()
		}
	}
}

// setConnectionState updates the connection state and publishes health status
// when it changes. connStateMu serializes the swap WITH its retained publish:
// without it, two concurrent callers with opposite values (health ticker vs
// source-reconnect goroutine) can publish out of order, leaving a stale
// retained "offline" on a healthy gateway.
func (d *Driver) setConnectionState(connected bool) {
	d.connStateMu.Lock()
	defer d.connStateMu.Unlock()

	if d.isConnected.Swap(connected) == connected {
		return
	}

	status := "offline"
	if connected {
		status = "online"
	}

	topic := fmt.Sprintf("sys/health/%d", d.gatewayID)
	d.mqttClient.PublishWithQoS(topic, status, 1, true)
	log.Printf("[DRIVER-MQTT] Health status changed to: %s", status)
}

// checkSourceTimeouts verifies if any tags have stopped receiving data
func (d *Driver) checkSourceTimeouts() {
	d.configMu.RLock()
	cfg := d.config
	d.configMu.RUnlock()

	if cfg == nil || !cfg.Gateway.Enabled {
		return
	}

	// Use 3x scan rate or 60s minimum as timeout
	timeoutMs := cfg.Gateway.ScanRateMs * 3
	if timeoutMs < 60000 {
		timeoutMs = 60000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	now := time.Now()
	timestamp := now.UnixMilli()

	d.msgTimeMu.Lock()
	defer d.msgTimeMu.Unlock()

	for _, tag := range cfg.Tags {
		lastTime, exists := d.lastMessageTime[tag.ID]
		// If we haven't heard from the tag for longer than the timeout, mark as connection lost
		if exists && now.Sub(lastTime) > timeout {
			if !d.connectionLost[tag.ID] {
				log.Printf("[DRIVER-MQTT] Source timeout for tag %s (ID:%d). No data for %v", tag.Alias, tag.ID, timeout)
				d.connectionLost[tag.ID] = true
				// Publish with BAD quality
				d.publishDual(tag.ID, tag.Alias, 0, tag.DataType, 2, timestamp)
			}
		}
	}
}

// Utility functions

func slugify(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "-")
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getEnvInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		i, _ := strconv.Atoi(v)
		return i
	}
	return d
}
