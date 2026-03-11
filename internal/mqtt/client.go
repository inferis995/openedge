package mqtt

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Config holds the MQTT client configuration
type Config struct {
	Host          string
	Port          int
	ClientID      string
	Username      string
	Password      string
	CleanSession  bool
	AutoReconnect bool
	KeepAlive     time.Duration
	LWTTopic      string
	LWTPayload    string
	LWTRetained   bool
}

// MessageHandler is a function that handles incoming MQTT messages
type MessageHandler func(topic string, payload []byte)

// Client represents an MQTT client
type Client struct {
	config         Config
	client         mqtt.Client
	handlers       map[string]MessageHandler // map of topic to handler
	handlersMu     sync.RWMutex
	connectOnce    sync.Once
	subscribedTopics map[string]bool
	subscribeMu    sync.Mutex
}

// NewClient creates a new MQTT client with the given configuration
func NewClient(config Config) *Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", config.Host, config.Port))
	opts.SetClientID(config.ClientID)

	// Set AutoReconnect - default to true if not specified
	autoReconnect := config.AutoReconnect
	if !autoReconnect {
		autoReconnect = true
	}
	opts.SetAutoReconnect(autoReconnect)
	opts.SetMaxReconnectInterval(10 * time.Second)

	// Set KeepAlive if specified
	if config.KeepAlive > 0 {
		opts.SetKeepAlive(config.KeepAlive)
	}

	// Set CleanSession if specified
	if config.CleanSession {
		opts.SetCleanSession(true)
	}

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

	log.Printf("[MQTT-INcoming] Received message on topic: %s, payload: %s", topic, string(payload))

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
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	log.Printf("[MQTT] Connected to broker at %s:%d as %s", c.config.Host, c.config.Port, c.config.ClientID)
	return nil
}

// Disconnect closes the connection to the MQTT broker
func (c *Client) Disconnect(timeout int) {
	c.client.Disconnect(uint(timeout))
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
	token := c.client.Subscribe(topic, 0, func(client mqtt.Client, msg mqtt.Message) {
		c.handleIncomingMessage(client, msg)
	})
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	c.subscribeMu.Lock()
	c.subscribedTopics[topic] = true
	c.subscribeMu.Unlock()

	log.Printf("[MQTT] Subscribed to topic: %s", topic)
	return nil
}

// Unsubscribe unsubscribes from a topic
func (c *Client) Unsubscribe(topic string) error {
	c.handlersMu.Lock()
	delete(c.handlers, topic)
	c.handlersMu.Unlock()

	token := c.client.Unsubscribe(topic)
	if token.Wait() && token.Error() != nil {
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
	token := c.client.Publish(topic, 0, false, payload)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	log.Printf("[MQTT] Published to topic %s: %v", topic, payload)
	return nil
}

// PublishWithQoS publishes a message to a topic with specified QoS and retain flag
func (c *Client) PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error {
	token := c.client.Publish(topic, qos, retained, payload)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	log.Printf("[MQTT] Published to topic %s (QoS=%d, retained=%t): %v", topic, qos, retained, payload)
	return nil
}

// onConnect is called when the client connects to the broker
func (c *Client) onConnect(client mqtt.Client) {
	log.Println("[MQTT] Connection established")

	// Re-subscribe to all topics on reconnect
	c.subscribeMu.Lock()
	defer c.subscribeMu.Unlock()

	for topic := range c.subscribedTopics {
		// Use explicit handler instead of nil
		token := client.Subscribe(topic, 0, func(cl mqtt.Client, msg mqtt.Message) {
			c.handleIncomingMessage(cl, msg)
		})
		if token.Wait() && token.Error() != nil {
			log.Printf("[MQTT] Failed to re-subscribe to %s: %v", topic, token.Error())
		} else {
			log.Printf("[MQTT] Re-subscribed to topic: %s", topic)
		}
	}
}

// onConnectionLost is called when the connection to the broker is lost
func (c *Client) onConnectionLost(client mqtt.Client, err error) {
	log.Printf("[MQTT] Connection lost: %v", err)
}
