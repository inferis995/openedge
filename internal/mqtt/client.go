package mqtt

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/ralph/industrial-edge-middleware/internal/logging"
	"github.com/ralph/industrial-edge-middleware/internal/telemetry"
)

// topicPrefix returns the first segment of a topic, for use as a metric label.
// Anything unrecognized collapses to "other" so a malformed or hostile topic
// cannot mint unbounded series in Prometheus.
func topicPrefix(topic string) string {
	seg := topic
	if i := strings.IndexByte(topic, '/'); i >= 0 {
		seg = topic[:i]
	}
	switch seg {
	case "data", "sys", "spBv1.0":
		return seg
	default:
		return "other"
	}
}

// opTimeout bounds every broker round-trip (connect, subscribe, publish).
// Without it a stalled broker (TCP alive but no ACKs) blocks the caller's
// poll loop forever; drivers must degrade to an error instead of hanging.
const opTimeout = 10 * time.Second

// Config holds the MQTT client configuration
type Config struct {
	Host          string
	Port          int
	Scheme        string // "tcp" (default) or "ssl" for TLS
	ClientID      string
	Username      string
	Password      string
	CleanSession  bool
	AutoReconnect bool
	KeepAlive     time.Duration
	LWTTopic      string
	LWTPayload    string
	LWTRetained   bool

	// SpoolPath turns on store-and-forward: when set, messages that fail to
	// go out are written there and resent on reconnect. Empty keeps the old
	// behavior — publish or lose — which is the right one for a
	// subscribe-only client, where an outbound queue nobody drains is just a
	// disk filling up.
	SpoolPath     string
	SpoolMaxBytes int64
}

// MessageHandler is a function that handles incoming MQTT messages
type MessageHandler func(topic string, payload []byte)

// Client represents an MQTT client
type Client struct {
	config           Config
	client           mqtt.Client
	handlers         map[string]MessageHandler // map of topic to handler
	handlersMu       sync.RWMutex
	subscribedTopics map[string]bool
	subscribeMu      sync.Mutex
	spool            *Spool
	drainMu          sync.Mutex
}

// NewClient creates a new MQTT client with the given configuration
func NewClient(config Config) *Client {
	opts := mqtt.NewClientOptions()
	scheme := config.Scheme
	if scheme == "" {
		scheme = "tcp"
	}
	opts.AddBroker(fmt.Sprintf("%s://%s:%d", scheme, config.Host, config.Port))
	if scheme == "ssl" {
		// Use the system CA bundle by default; callers can extend later if
		// they need a custom cafile or InsecureSkipVerify.
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	opts.SetClientID(config.ClientID)

	// Set AutoReconnect - default to true if not specified
	autoReconnect := config.AutoReconnect
	if !autoReconnect {
		autoReconnect = true
	}
	opts.SetAutoReconnect(autoReconnect)
	opts.SetMaxReconnectInterval(10 * time.Second)
	// AutoReconnect only re-establishes a PREVIOUSLY successful connection.
	// ConnectRetry covers the cold-boot race (broker not up yet): the client
	// keeps dialing in the background even when the first Connect() fails.
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)

	// Set KeepAlive if specified
	if config.KeepAlive > 0 {
		opts.SetKeepAlive(config.KeepAlive)
	}

	// Always set CleanSession explicitly. Previously it was only set when true,
	// so a caller asking for a PERSISTENT session (false) silently got paho's
	// default of true — the broker then kept no subscription state and queued
	// nothing across a reconnect. That is invisible for stateless publishers but
	// loses data for a QoS-1 subscriber such as the historian.
	opts.SetCleanSession(config.CleanSession)

	// Set authentication if provided
	if config.Username != "" {
		opts.SetUsername(config.Username)
	}
	if config.Password != "" {
		opts.SetPassword(config.Password)
	}

	// Set Last Will and Testament if provided
	if config.LWTTopic != "" {
		lwtPayload := config.LWTPayload
		if lwtPayload == "" {
			lwtPayload = "offline"
		}
		opts.SetWill(config.LWTTopic, lwtPayload, 0, config.LWTRetained)
	}

	c := &Client{
		config:           config,
		handlers:         make(map[string]MessageHandler),
		subscribedTopics: make(map[string]bool),
	}

	// A spool that will not open must not stop the driver from starting: with
	// no buffer we are back to the old behavior, which is worse but works.
	// Halting acquisition because a file could not be created would treat a
	// partial loss of history as a plant failure.
	if config.SpoolPath != "" {
		sp, err := NewSpool(config.SpoolPath, config.SpoolMaxBytes)
		if err != nil {
			log.Printf("[MQTT] store-and-forward disabled, cannot open %s: %v",
				config.SpoolPath, err)
		} else {
			c.spool = sp
			if n, _, _ := sp.Stats(); n > 0 {
				log.Printf("[MQTT] store-and-forward: %d bytes waiting from a previous session", n)
			}
		}
	}

	// Set a single default message handler that dispatches to registered handlers
	opts.SetDefaultPublishHandler(c.handleIncomingMessage)

	opts.OnConnect = c.onConnect
	opts.SetConnectionLostHandler(c.onConnectionLost)

	client := mqtt.NewClient(opts)
	c.client = client

	return c
}

// handleIncomingMessage is the single default handler that dispatches to registered handlers
func (c *Client) handleIncomingMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	// Counted here rather than in each subscriber, so it cannot drift as
	// subscriptions are added. The label is the first topic segment — data,
	// sys, spBv1.0 — which keeps cardinality at a handful of series instead of
	// one per tag.
	telemetry.MQTTMessagesReceived.WithLabelValues(topicPrefix(topic)).Inc()

	// One line per inbound message, payload included. Useful when tracing a
	// device; ruinous at plant rates, where it both costs CPU and evicts the
	// startup errors from the container's rotated logs. See internal/logging.
	logging.Debugf("[MQTT-INcoming] Received message on topic: %s, payload: %s", topic, string(payload))

	// Find matching handler
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()

	// First, try exact match
	if handler, ok := c.handlers[topic]; ok {
		handler(topic, payload)
		return
	}

	// Then, try wildcard match
	for registeredTopic, handler := range c.handlers {
		if c.topicMatch(registeredTopic, topic) {
			handler(topic, payload)
			return
		}
	}

	log.Printf("[MQTT] No handler found for topic: %s", topic)
}

// topicMatch checks if a subscribed topic matches a received topic
// Supports simple wildcards (+ for single level, # for multi-level)
func (c *Client) topicMatch(subscribedTopic, receivedTopic string) bool {
	if subscribedTopic == receivedTopic {
		return true
	}

	subParts := strings.Split(subscribedTopic, "/")
	recvParts := strings.Split(receivedTopic, "/")

	// Multi-level wildcard (#)
	if subParts[len(subParts)-1] == "#" {
		if len(subParts) > len(recvParts) {
			return false
		}
		// Check if all parts before # match
		for i := 0; i < len(subParts)-1; i++ {
			if subParts[i] != "+" && subParts[i] != recvParts[i] {
				return false
			}
		}
		return true
	}

	// Same length required for single-level wildcards (+)
	if len(subParts) != len(recvParts) {
		return false
	}

	for i := range subParts {
		if subParts[i] != "+" && subParts[i] != recvParts[i] {
			return false
		}
	}

	return true
}

// Connect establishes a connection to the MQTT broker
func (c *Client) Connect() error {
	token := c.client.Connect()
	if !token.WaitTimeout(opTimeout) {
		return fmt.Errorf("connect timeout after %s", opTimeout)
	}
	if token.Error() != nil {
		return token.Error()
	}
	log.Printf("[MQTT] Connected to broker at %s:%d as %s", c.config.Host, c.config.Port, c.config.ClientID)
	return nil
}

// Disconnect closes the connection to the MQTT broker
func (c *Client) Disconnect(timeout int) {
	c.client.Disconnect(uint(timeout))
	// Also cleared here: a deliberate shutdown never fires onConnectionLost, so
	// without this the gauge would stay at 1 in the final scrape and the last
	// thing Prometheus recorded about a stopped service would be "connected".
	telemetry.MQTTConnected.Set(0)
	log.Printf("[MQTT] Disconnected from broker")
}

// IsConnected returns whether the client is currently connected to the broker
func (c *Client) IsConnected() bool {
	if c.client == nil {
		return false
	}
	return c.client.IsConnected()
}

// Subscribe subscribes to a topic with a message handler
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	c.handlersMu.Lock()
	c.handlers[topic] = handler
	c.handlersMu.Unlock()

	// Create a wrapper that calls our default handler
	// This ensures the default handler is properly registered with paho
	// Record the topic BEFORE waiting for the SUBACK: if the ack arrives
	// after the timeout the subscription is live on the broker, and it must
	// be in subscribedTopics so onConnect re-subscribes it after reconnects.
	c.subscribeMu.Lock()
	c.subscribedTopics[topic] = true
	c.subscribeMu.Unlock()

	token := c.client.Subscribe(topic, 1, func(client mqtt.Client, msg mqtt.Message) {
		c.handleIncomingMessage(client, msg)
	})
	if !token.WaitTimeout(opTimeout) {
		// Keep the registration: a late SUBACK means the subscription exists;
		// re-subscribing on the next reconnect is idempotent either way.
		return fmt.Errorf("subscribe timeout on topic %s", topic)
	}
	if token.Error() != nil {
		// Definitive rejection: undo the registration.
		c.subscribeMu.Lock()
		delete(c.subscribedTopics, topic)
		c.subscribeMu.Unlock()
		return token.Error()
	}

	log.Printf("[MQTT] Subscribed to topic: %s", topic)
	return nil
}

// Unsubscribe unsubscribes from a topic
func (c *Client) Unsubscribe(topic string) error {
	c.handlersMu.Lock()
	delete(c.handlers, topic)
	c.handlersMu.Unlock()

	token := c.client.Unsubscribe(topic)
	if !token.WaitTimeout(opTimeout) {
		return fmt.Errorf("unsubscribe timeout on topic %s", topic)
	}
	if token.Error() != nil {
		return token.Error()
	}

	c.subscribeMu.Lock()
	delete(c.subscribedTopics, topic)
	c.subscribeMu.Unlock()

	log.Printf("[MQTT] Unsubscribed from topic: %s", topic)
	return nil
}

// Publish publishes a message to a topic
func (c *Client) Publish(topic string, payload interface{}) error {
	return c.PublishWithQoS(topic, payload, 0, false)
}

// PublishWithQoS publishes a message to a topic with specified QoS and retain flag.
//
// With store-and-forward on, a publish that fails — broker down, timeout,
// reconnect in progress — puts the message on disk instead of nowhere, and it
// goes out on reconnect. The function then returns nil: the message has been
// accepted for delivery, which is the truth, and the caller has nothing to do
// about it. Returning an error for something already handled would push every
// driver to write handling that duplicates this.
//
// With no spool the behavior is the old one, error included.
func (c *Client) PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error {
	err := c.publishNow(topic, payload, qos, retained)
	if err == nil {
		log.Printf("[MQTT] Published to topic %s (QoS=%d, retained=%t): %v", topic, qos, retained, payload)
		return nil
	}
	if c.spool == nil {
		return err
	}

	buf, convErr := payloadBytes(payload)
	if convErr != nil {
		// A payload we cannot serialize is one we cannot queue either: return
		// the original publish error, which is the useful one.
		return err
	}
	if spErr := c.spool.Add(SpooledMessage{Topic: topic, QoS: qos, Retained: retained, Payload: string(buf)}); spErr != nil {
		log.Printf("[MQTT] store-and-forward: cannot queue %s: %v (original error: %v)", topic, spErr, err)
		return err
	}
	return nil
}

// publishNow is the real publish, with no safety net.
func (c *Client) publishNow(topic string, payload interface{}, qos byte, retained bool) error {
	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("not connected to the broker")
	}
	token := c.client.Publish(topic, qos, retained, payload)
	if !token.WaitTimeout(opTimeout) {
		return fmt.Errorf("publish timeout on topic %s", topic)
	}
	return token.Error()
}

// payloadBytes reduces to bytes the types the drivers actually pass.
func payloadBytes(payload interface{}) ([]byte, error) {
	switch v := payload.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return json.Marshal(v)
	}
}

// drainSpool resends whatever was left on disk.
//
// It runs in a goroutine because it is called from onConnect, which is a paho
// callback: blocking there stops the re-subscriptions and, with a day's worth
// of spool, would hold the client still for minutes at the exact moment it has
// just come back.
func (c *Client) drainSpool() {
	if c.spool == nil {
		return
	}
	if !c.drainMu.TryLock() {
		return // a replay is already running
	}
	defer c.drainMu.Unlock()

	pending, _, _ := c.spool.Stats()
	if pending == 0 {
		return
	}
	log.Printf("[MQTT] store-and-forward: resending %d buffered bytes", pending)

	var sent int
	err := c.spool.Drain(func(m SpooledMessage) error {
		if e := c.publishNow(m.Topic, []byte(m.Payload), m.QoS, m.Retained); e != nil {
			return e
		}
		sent++
		return nil
	})

	remaining, dropped, corrupt := c.spool.Stats()
	if err != nil {
		log.Printf("[MQTT] store-and-forward: resent %d messages, then stopped (%v); %d bytes still waiting",
			sent, err, remaining)
		return
	}
	log.Printf("[MQTT] store-and-forward: resent %d messages, queue empty (dropped for a full queue: %d, unreadable lines: %d)",
		sent, dropped, corrupt)
}

// onConnect is called when the client connects to the broker
func (c *Client) onConnect(client mqtt.Client) {
	log.Println("[MQTT] Connection established")
	telemetry.MQTTConnected.Set(1)

	go c.drainSpool()

	// Re-subscribe to all topics on reconnect
	c.subscribeMu.Lock()
	defer c.subscribeMu.Unlock()

	for topic := range c.subscribedTopics {
		// Use explicit handler instead of nil
		token := client.Subscribe(topic, 1, func(cl mqtt.Client, msg mqtt.Message) {
			c.handleIncomingMessage(cl, msg)
		})
		if !token.WaitTimeout(opTimeout) {
			log.Printf("[MQTT] Re-subscribe timeout on %s", topic)
		} else if token.Error() != nil {
			log.Printf("[MQTT] Failed to re-subscribe to %s: %v", topic, token.Error())
		} else {
			log.Printf("[MQTT] Re-subscribed to topic: %s", topic)
		}
	}
}

// onConnectionLost is called when the connection to the broker is lost
func (c *Client) onConnectionLost(client mqtt.Client, err error) {
	log.Printf("[MQTT] Connection lost: %v", err)
	telemetry.MQTTConnected.Set(0)
}
