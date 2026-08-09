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
var UseProtobuf = true

// seqMax is the Sparkplug B sequence roll-over point: seq is a single byte and
// wraps 0..255. It used to live in payload.go next to a PACKAGE-LEVEL counter
// shared by every edge node built in one process — see the seqNum field below.
const seqMax uint64 = 256

// Publisher is the subset of *mqtt.Client this package needs. Depending on the
// behaviour rather than the concrete type lets the birth/death/sequence rules
// be unit-tested without a live broker; production still passes *mqtt.Client,
// which satisfies it as-is.
type Publisher interface {
	PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error
}

// Client wraps the MQTT client with Sparkplug B functionality
type SparkplugClient struct {
	config     Config
	mqttClient Publisher
	connected  bool
	mu         sync.RWMutex
	birthSent  bool

	// seqMu guards seqNum and bdSeq ONLY, never the fields above.
	//
	// Two bugs converged here. (1) There were two counters: this one, which
	// pre-incremented and therefore made NBIRTH seq=1 where the spec demands 0,
	// and a package-level one in payload.go that started at 0 and was shared by
	// EVERY edge node in the process. A host saw birth=1 followed by data
	// 0,1,2..., declared a sequence gap and looped issuing rebirth requests.
	// (2) nextSeq() mutated seqNum while the caller held only mu.RLock (the
	// PublishDDEATH JSON path) — a genuine data race. A dedicated mutex makes
	// the counter safe no matter which mode of mu the caller happens to hold.
	seqMu  sync.Mutex
	seqNum uint64 // next seq to emit
	bdSeq  uint64 // birth/death session counter, paired NBIRTH <-> NDEATH
}

// NewClient creates a new Sparkplug B client
func NewClient(config Config, mqttClient *mqtt.Client) *SparkplugClient {
	c := &SparkplugClient{
		config:    config,
		connected: false,
		birthSent: false,
		seqNum:    0,
	}
	// Assign only when non-nil: storing a typed nil *mqtt.Client in the
	// interface field would make the `c.mqttClient == nil` guards below false
	// and turn a missing client into a panic instead of an error.
	if mqttClient != nil {
		c.mqttClient = mqttClient
	}
	return c
}

// newClientWithPublisher is the test seam for NewClient: same client, arbitrary
// publisher. Unexported so drivers keep using NewClient with *mqtt.Client.
func newClientWithPublisher(config Config, p Publisher) *SparkplugClient {
	return &SparkplugClient{config: config, mqttClient: p}
}

// Connect establishes connection and sends NBIRTH message
func (c *SparkplugClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected && c.birthSent {
		return nil
	}

	c.connected = true

	// The MQTT client should already be connected
	// Send NBIRTH message to announce the edge node
	if err := c.ensureNodeBirthLocked(); err != nil {
		return fmt.Errorf("failed to send NBIRTH: %w", err)
	}

	log.Printf("[SPARKPLUG] Client connected, NBIRTH sent for group=%s, node=%s (Protobuf=%v)",
		c.config.GroupID, c.config.EdgeNodeID, UseProtobuf)

	return nil
}

// ensureNodeBirthLocked publishes NBIRTH once per session.
// The caller MUST hold c.mu (write lock).
//
// Per the Sparkplug B spec an edge node MUST publish NBIRTH before any
// DBIRTH/DDATA. A compliant host DISCARDS every message coming from a node it
// never saw born and answers with an endless stream of rebirth requests, which
// is exactly what OpenEdge produced: NBIRTH was only ever emitted by Connect(),
// and no driver called it — they all called SetConnected(true).
//
// The flag is only set on success so a broker hiccup at startup is retried by
// the next publish instead of muting the node for the rest of its lifetime.
func (c *SparkplugClient) ensureNodeBirthLocked() error {
	if c.birthSent {
		return nil
	}
	if err := c.sendNBIRTH(); err != nil {
		return err
	}
	c.birthSent = true
	return nil
}

// Disconnect sends NDEATH and disconnects.
//
// Why NDEATH is published here and not left to the MQTT Will:
// MQTT allows exactly ONE Will message per connection, and the drivers share a
// single mqtt.Client whose Will is already sys/health/{gateway_id} = "offline"
// (retained). That Will is what core-api and engine-historian subscribe to on
// sys/health/+ to drive the gateway online/offline state in OpenEdge's own UI
// and to mark tags stale, so it cannot be given up. internal/mqtt exposes a
// single LWTTopic/LWTPayload pair anyway. Consequence, stated plainly: a
// graceful shutdown announces NDEATH from here, while an ungraceful kill is
// only reported to OpenEdge (sys/health) and not to the Sparkplug host.
func (c *SparkplugClient) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return
	}

	// Send NDEATH message
	if err := c.sendNDEATH(); err != nil {
		log.Printf("[SPARKPLUG] Warning: failed to publish NDEATH: %v", err)
	}

	c.connected = false
	c.birthSent = false
	log.Printf("[SPARKPLUG] Client disconnected, NDEATH sent")
}

// PublishDDATA publishes device data (DDATA message).
//
// deviceID must be the device announced by DBIRTH (the gateway), NOT the tag:
// tags are metrics OF a device, and the metric name already carries the tag
// alias. Addressing DDATA to the tag alias — as PublishSingleTag used to —
// named a device the host had never seen born, so every value was dropped.
func (c *SparkplugClient) PublishDDATA(deviceID string, tags []TagData) error {
	// Write lock, not RLock: the birth state below is mutated when NBIRTH still
	// has to be published, and a RWMutex cannot be upgraded from read to write.
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("client not connected")
	}

	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	// NBIRTH first, always: data published before the node birth is discarded.
	if err := c.ensureNodeBirthLocked(); err != nil {
		log.Printf("[SPARKPLUG] Warning: NBIRTH still pending, publishing DDATA anyway: %v", err)
	}

	// Build topic: spBv1.0/{group_id}/DDATA/{edge_node_id}/{device_id}
	topic := BuildDDATATopic(c.config.GroupID, c.config.EdgeNodeID, deviceID)

	// One sequence source per edge node, continuing the stream opened by NBIRTH.
	seq := c.nextSeq()

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Use Protobuf encoding (true Sparkplug B)
		payloadBytes, err = CreateProtoDDATAPayload(deviceID, tags, seq)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf DDATA: %w", err)
		}
	} else {
		// Use JSON encoding (legacy compatibility)
		payload := CreateDDATAPayload(deviceID, tags, seq)
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

// PublishSingleTag publishes a single tag value as DDATA for the given device.
//
// deviceID is the DBIRTH-ed device (the gateway); the tag travels as a metric
// inside the payload, named after its alias.
func (c *SparkplugClient) PublishSingleTag(deviceID string, tag TagData) error {
	return c.PublishDDATA(deviceID, []TagData{tag})
}

// PublishDBIRTH sends a device birth message
func (c *SparkplugClient) PublishDBIRTH(deviceID string, tags []TagData) error {
	// Write lock: see PublishDDATA — NBIRTH may still have to go out first.
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("client not connected")
	}

	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	// A DBIRTH before the node's own NBIRTH is discarded by the host.
	if err := c.ensureNodeBirthLocked(); err != nil {
		log.Printf("[SPARKPLUG] Warning: NBIRTH still pending, publishing DBIRTH anyway: %v", err)
	}

	// Build topic
	topic := BuildDBIRTHTopic(c.config.GroupID, c.config.EdgeNodeID, deviceID)

	// DBIRTH is part of the node's single sequence stream (NBIRTH=0, then 1,2,…)
	seq := c.nextSeq()

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Use Protobuf encoding (true Sparkplug B)
		payloadBytes, err = CreateProtoDBIRTHPayload(deviceID, tags, seq)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf DBIRTH: %w", err)
		}
	} else {
		// Use JSON encoding (legacy compatibility)
		payload := CreateDBIRTHPayload(deviceID, tags, seq)
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

	// DDEATH carries a sequence number like any other device-level message.
	// c.nextSeq() is called here while only mu.RLock is held — safe now that the
	// counter has its own mutex (it used to be the data race in this file).
	seq := c.nextSeq()

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Use Protobuf encoding (true Sparkplug B)
		payloadBytes, err = CreateProtoDDEATHPayload(deviceID, seq)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf DDEATH: %w", err)
		}
	} else {
		// Use JSON encoding (legacy compatibility)
		payload := &Payload{
			Timestamp: time.Now().UnixMilli(),
			Seq:       seq,
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

	// 2. Publish Sparkplug B format if connected.
	// IsConnected() takes the lock: reading c.connected bare from here (this
	// method holds none) raced with SetConnected/Disconnect.
	// The DDATA is addressed to the GATEWAY device, which is what DBIRTH
	// announces — not to the tag alias, which no DBIRTH ever declared.
	if c.IsConnected() {
		if err := c.PublishSingleTag(gateway, tag); err != nil {
			log.Printf("[SPARKPLUG] Warning: failed to publish Sparkplug B: %v", err)
			// Don't return error - legacy was successful
		}
	}

	return nil
}

// sendNBIRTH sends node birth message.
// The caller MUST hold c.mu (write lock) — see ensureNodeBirthLocked.
func (c *SparkplugClient) sendNBIRTH() error {
	// Check if MQTT client is available
	if c.mqttClient == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	topic := BuildNBIRTHTopic(c.config.GroupID, c.config.EdgeNodeID)

	// NBIRTH restarts the node's sequence stream at 0 (spec: the birth is
	// always seq 0 and every following message increments by one, mod 256).
	// It also opens a new birth/death session: the bdSeq published here is the
	// one the matching NDEATH must repeat.
	seq := c.restartSeq()
	bdSeq := c.newBdSeq()
	now := time.Now().UnixMilli()

	payload := &Payload{
		Timestamp: now,
		Seq:       seq,
		Metrics: []Metric{
			{
				Name:      "bdSeq",
				DataType:  DataTypeUInt64,
				Timestamp: now,
				// Quality must be stated explicitly: the encoder always emits
				// the quality property, and the zero value means BAD.
				Quality: QualityGood,
				Value:   bdSeq,
			},
		},
	}

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		// Create minimal NBIRTH payload with Protobuf
		payloadBytes, err = EncodePayload(payload)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf NBIRTH: %w", err)
		}
	} else {
		// JSON encoding
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

	// NDEATH repeats the bdSeq of the session it closes; it does not consume a
	// sequence number (the spec exempts it, since a Will can carry none).
	bdSeq := c.currentBdSeq()

	var payloadBytes []byte
	var err error

	if UseProtobuf {
		payloadBytes, err = CreateProtoNDEATHPayload(bdSeq)
		if err != nil {
			return fmt.Errorf("failed to encode protobuf NDEATH: %w", err)
		}
	} else {
		payload := &Payload{
			Timestamp: time.Now().UnixMilli(),
			Seq:       0,
			Metrics: []Metric{
				{
					Name:      "bdSeq",
					DataType:  DataTypeUInt64,
					Timestamp: time.Now().UnixMilli(),
					Quality:   QualityGood,
					Value:     bdSeq,
				},
			},
		}
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}

	return c.mqttClient.PublishWithQoS(topic, string(payloadBytes), 0, true)
}

// nextSeq returns the next sequence number of this edge node's single stream,
// rolling over at 256. Guarded by its own mutex so it is safe from callers
// holding mu in either mode (or none).
func (c *SparkplugClient) nextSeq() uint64 {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()

	seq := c.seqNum
	c.seqNum = (c.seqNum + 1) % seqMax
	return seq
}

// restartSeq restarts the stream for an NBIRTH: it returns 0 (the sequence
// number the birth itself must carry) and arms the next message at 1.
func (c *SparkplugClient) restartSeq() uint64 {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()

	c.seqNum = 1
	return 0
}

// newBdSeq opens a new birth/death session and returns its bdSeq.
func (c *SparkplugClient) newBdSeq() uint64 {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()

	bd := c.bdSeq
	c.bdSeq = (c.bdSeq + 1) % seqMax
	return bd
}

// currentBdSeq returns the bdSeq of the session opened by the last NBIRTH.
func (c *SparkplugClient) currentBdSeq() uint64 {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()

	// bdSeq already points at the NEXT session, so step back one (mod 256).
	return (c.bdSeq + seqMax - 1) % seqMax
}

// IsConnected returns whether the client is connected
func (c *SparkplugClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// SetConnected sets the connection state.
//
// Bringing the edge node up ALSO publishes NBIRTH. This is the single place the
// birth is triggered, chosen because all five drivers already announce the node
// this way (NewClient + SetConnected(true)) and none of them ever called
// Connect() — which is why NBIRTH was never published at all. Errors are logged
// rather than returned, to keep the existing void signature: the birth is
// retried by the first publish that follows (ensureNodeBirthLocked).
func (c *SparkplugClient) SetConnected(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !connected {
		c.connected = false
		c.birthSent = false
		return
	}

	if c.connected && c.birthSent {
		return
	}

	c.connected = true
	if err := c.ensureNodeBirthLocked(); err != nil {
		log.Printf("[SPARKPLUG] Warning: NBIRTH not published for group=%s node=%s: %v (will retry on next publish)",
			c.config.GroupID, c.config.EdgeNodeID, err)
		return
	}
	log.Printf("[SPARKPLUG] NBIRTH published for group=%s, node=%s (Protobuf=%v)",
		c.config.GroupID, c.config.EdgeNodeID, UseProtobuf)
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

		// Address the DDATA to the gateway device — the one the drivers birth
		// with PublishDBIRTH(slugify(gateway.Name), …). The alias travels as the
		// metric name inside the payload (CreatePayload uses TagData.DeviceID),
		// which is what engine-historian and core-api resolve tags by.
		if err := dp.sparkplugClient.PublishSingleTag(dp.gatewayName, tagData); err != nil {
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
