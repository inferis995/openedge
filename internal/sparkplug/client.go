package sparkplug

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
)

// UseProtobuf controls whether to use Protobuf (true) or JSON (false) encoding
// Set to true for true Sparkplug B compliance
// NOTE: To enable true Protobuf, you need to:
// 1. Install protoc: https://grpc.io/docs/protoc-installation/
// 2. Generate Go code from sparkplug_b.proto (from Eclipse Tahu)
// 3. Replace the manual protobuf.go implementation with generated code
var UseProtobuf = false

// Client wraps the MQTT client with Sparkplug B functionality
type SparkplugClient struct {
	config      Config
	mqttClient  *mqtt.Client
	seqNum      uint64
	connected   bool
	mu          sync.RWMutex
	birthSent   bool
}

// NewClient creates a new Sparkplug B client
func NewClient(config Config, mqttClient *mqtt.Client) *SparkplugClient {
	return &SparkplugClient{
		config:     config,
		mqttClient: mqttClient,
		seqNum:     0,
		connected:  false,
		birthSent:  false,
	}
}

// Connect establishes connection and sends NBIRTH message
func (c *SparkplugClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// The MQTT client should already be connected
	// Send NBIRTH message to announce the edge node
	if err := c.sendNBIRTH(); err != nil {
		return fmt.Errorf("failed to send NBIRTH: %w", err)
	}

	c.connected = true
	c.birthSent = true
	log.Printf("[SPARKPLUG] Client connected, NBIRTH sent for group=%s, node=%s (Protobuf=%v)",
		c.config.GroupID, c.config.EdgeNodeID, UseProtobuf)

	return nil
}

// Disconnect sends NDEATH and disconnects
func (c *SparkplugClient) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return
	}

	// Send NDEATH message
	c.sendNDEATH()

	c.connected = false
	c.birthSent = false
	log.Printf("[SPARKPLUG] Client disconnected, NDEATH sent")
}

// PublishDDATA publishes device data (DDATA message)
func (c *SparkplugClient) PublishDDATA(deviceID string, tags []TagData) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return fmt.Errorf("client not connected")
	}

	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	// Build topic: spBv1.0/{group_id}/DDATA/{edge_node_id}/{device_id}
	topic := BuildDDATATopic(c.config.GroupID, c.config.EdgeNodeID, deviceID)

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Use Protobuf encoding (true Sparkplug B)
		payloadBytes, err = CreateProtoDDATAPayload(deviceID, tags)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf DDATA: %w", err)
		}
	} else {
		// Use JSON encoding (legacy compatibility)
		payload := CreateDDATAPayload(deviceID, tags)
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal JSON payload: %w", err)
		}
	}

	// Publish with QoS 0 (Sparkplug B typically uses QoS 0 for data)
	if err := c.mqttClient.PublishWithQoS(topic, string(payloadBytes), 0, false); err != nil {
		return fmt.Errorf("failed to publish DDATA: %w", err)
	}

	log.Printf("[SPARKPLUG] Published DDATA to %s with %d metrics (Protobuf=%v)", topic, len(tags), UseProtobuf)

	return nil
}

// PublishSingleTag publishes a single tag value as DDATA
func (c *SparkplugClient) PublishSingleTag(tag TagData) error {
	return c.PublishDDATA(tag.DeviceID, []TagData{tag})
}

// PublishDBIRTH sends a device birth message
func (c *SparkplugClient) PublishDBIRTH(deviceID string, tags []TagData) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return fmt.Errorf("client not connected")
	}

	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	// Build topic
	topic := BuildDBIRTHTopic(c.config.GroupID, c.config.EdgeNodeID, deviceID)

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Use Protobuf encoding (true Sparkplug B)
		payloadBytes, err = CreateProtoDBIRTHPayload(deviceID, tags)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf DBIRTH: %w", err)
		}
	} else {
		// Use JSON encoding (legacy compatibility)
		payload := CreateDBIRTHPayload(deviceID, tags)
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal DBIRTH payload: %w", err)
		}
	}

	// Publish with QoS 0, retained=true for birth messages
	if err := c.mqttClient.PublishWithQoS(topic, string(payloadBytes), 0, true); err != nil {
		return fmt.Errorf("failed to publish DBIRTH: %w", err)
	}

	log.Printf("[SPARKPLUG] Published DBIRTH for device %s with %d metrics (Protobuf=%v)", deviceID, len(tags), UseProtobuf)

	return nil
}

// PublishDDEATH sends a device death message
func (c *SparkplugClient) PublishDDEATH(deviceID string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return fmt.Errorf("client not connected")
	}

	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	// Build topic
	topic := BuildDDEATHTopic(c.config.GroupID, c.config.EdgeNodeID, deviceID)

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Use Protobuf encoding (true Sparkplug B)
		payloadBytes, err = CreateProtoDDEATHPayload(deviceID)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf DDEATH: %w", err)
		}
	} else {
		// Use JSON encoding (legacy compatibility)
		payload := &Payload{
			Timestamp: time.Now().UnixMilli(),
			Seq:       c.nextSeq(),
			Metrics:   []Metric{},
		}
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal DDEATH payload: %w", err)
		}
	}

	// Publish with QoS 0, retained=true for death messages
	if err := c.mqttClient.PublishWithQoS(topic, string(payloadBytes), 0, true); err != nil {
		return fmt.Errorf("failed to publish DDEATH: %w", err)
	}

	log.Printf("[SPARKPLUG] Published DDEATH for device %s (Protobuf=%v)", deviceID, UseProtobuf)

	return nil
}

// PublishDual publishes in both Sparkplug B and legacy formats for backward compatibility
func (c *SparkplugClient) PublishDual(tag TagData, org, site, area, gateway string) error {
	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	// 1. Always publish legacy format
	legacyTopic := BuildLegacyTopic(org, site, area, gateway, tag.DeviceID)
	legacyPayload := LegacyPayload{
		TagID:     tag.TagID,
		OrgID:     tag.OrgID,
		Value:     tag.Value,
		Timestamp: tag.Timestamp,
		Quality:   tag.Quality,
	}

	legacyBytes, err := json.Marshal(legacyPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal legacy payload: %w", err)
	}

	if err := c.mqttClient.PublishWithQoS(legacyTopic, string(legacyBytes), 1, false); err != nil {
		log.Printf("[SPARKPLUG] Warning: failed to publish legacy topic: %v", err)
	}

	// 2. Publish Sparkplug B format if connected
	if c.connected {
		if err := c.PublishSingleTag(tag); err != nil {
			log.Printf("[SPARKPLUG] Warning: failed to publish Sparkplug B: %v", err)
			// Don't return error - legacy was successful
		}
	}

	return nil
}

// sendNBIRTH sends node birth message
func (c *SparkplugClient) sendNBIRTH() error {
	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	topic := BuildNBIRTHTopic(c.config.GroupID, c.config.EdgeNodeID)

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Create minimal NBIRTH payload with Protobuf
		payload := &Payload{
			Timestamp: time.Now().UnixMilli(),
			Seq:       c.nextSeq(),
			Metrics: []Metric{
				{
					Name:      "bdSeq",
					DataType:  DataTypeUInt64,
					Timestamp: time.Now().UnixMilli(),
					Value:     0,
				},
			},
		}
		payloadBytes, err = EncodePayload(payload)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf NBIRTH: %w", err)
		}
	} else {
		// JSON encoding
		payload := &Payload{
			Timestamp: time.Now().UnixMilli(),
			Seq:       c.nextSeq(),
			Metrics: []Metric{
				{
					Name:      "bdSeq",
					DataType:  DataTypeUInt64,
					Timestamp: time.Now().UnixMilli(),
					Value:     0,
				},
			},
		}
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}

	// NBIRTH should be retained
	return c.mqttClient.PublishWithQoS(topic, string(payloadBytes), 0, true)
}

// sendNDEATH sends node death message
func (c *SparkplugClient) sendNDEATH() error {
	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	topic := BuildNDEATHTopic(c.config.GroupID, c.config.EdgeNodeID)

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		payloadBytes, err = CreateProtoNDEATHPayload()
		if err != nil {
			return fmt.Errorf("failed to encode protobuf NDEATH: %w", err)
		}
	} else {
		payload := &Payload{
			Timestamp: time.Now().UnixMilli(),
			Seq:       c.nextSeq(),
			Metrics:   []Metric{},
		}
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}

	return c.mqttClient.PublishWithQoS(topic, string(payloadBytes), 0, true)
}

// nextSeq returns the next sequence number
func (c *SparkplugClient) nextSeq() uint64 {
	c.seqNum = (c.seqNum + 1) % 256
	return c.seqNum
}

// IsConnected returns whether the client is connected
func (c *SparkplugClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// SetConnected sets the connection state
func (c *SparkplugClient) SetConnected(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = connected
}

// GetConfig returns the client configuration
func (c *SparkplugClient) GetConfig() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// DualPublisher handles dual publishing (legacy + Sparkplug B)
type DualPublisher struct {
	sparkplugClient *SparkplugClient
	enabled         bool
	orgName         string
	siteName        string
	areaName        string
	gatewayName     string
	orgID           int
}

// NewDualPublisher creates a new dual publisher
func NewDualPublisher(sparkplugClient *SparkplugClient, orgName, siteName, areaName, gatewayName string, orgID int) *DualPublisher {
	return &DualPublisher{
		sparkplugClient: sparkplugClient,
		enabled:         sparkplugClient != nil && sparkplugClient.IsConnected(),
		orgName:         orgName,
		siteName:        siteName,
		areaName:        areaName,
		gatewayName:     gatewayName,
		orgID:           orgID,
	}
}

// Publish publishes a tag value in both formats
func (dp *DualPublisher) Publish(tagID int, alias string, value interface{}, dataType string, quality int, timestamp int64, mqttClient *mqtt.Client) error {
	// 1. Always publish legacy format
	legacyTopic := BuildLegacyTopic(dp.orgName, dp.siteName, dp.areaName, dp.gatewayName, alias)
	legacyPayload := LegacyPayload{
		TagID:     tagID,
		OrgID:     dp.orgID,
		Value:     value,
		Timestamp: timestamp,
		Quality:   quality,
	}

	legacyBytes, err := json.Marshal(legacyPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal legacy payload: %w", err)
	}

	if err := mqttClient.PublishWithQoS(legacyTopic, string(legacyBytes), 1, false); err != nil {
		return fmt.Errorf("failed to publish legacy: %w", err)
	}

	// 2. Publish Sparkplug B format if available
	if dp.enabled && dp.sparkplugClient != nil {
		tagData := TagData{
			TagID:     tagID,
			DeviceID:  alias,
			Value:     value,
			DataType:  dataType,
			Timestamp: timestamp,
			Quality:   quality,
			OrgID:     dp.orgID,
		}

		if err := dp.sparkplugClient.PublishSingleTag(tagData); err != nil {
			// Log but don't fail - legacy was successful
			log.Printf("[SPARKPLUG] Warning: failed to publish Sparkplug B for %s: %v", alias, err)
		}
	}

	return nil
}

// UpdateConfig updates the publisher configuration
func (dp *DualPublisher) UpdateConfig(orgName, siteName, areaName, gatewayName string, orgID int) {
	dp.orgName = orgName
	dp.siteName = siteName
	dp.areaName = areaName
	dp.gatewayName = gatewayName
	dp.orgID = orgID
}

// SetEnabled enables or disables Sparkplug B publishing
func (dp *DualPublisher) SetEnabled(enabled bool) {
	dp.enabled = enabled && dp.sparkplugClient != nil && dp.sparkplugClient.IsConnected()
}
