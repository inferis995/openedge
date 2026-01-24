package mqtt

import (
	"fmt"
	"log"
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
	config      Config
	client      mqtt.Client
	handlers    map[string]mqtt.MessageHandler
	handlersMu  sync.RWMutex
	connectOnce sync.Once
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
		config:   config,
		handlers: make(map[string]mqtt.MessageHandler),
	}

	opts.OnConnect = c.onConnect
	opts.SetConnectionLostHandler(c.onConnectionLost)

	client := mqtt.NewClient(opts)
	c.client = client

	return c
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

// Subscribe subscribes to a topic with a message handler
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	c.handlersMu.Lock()
	c.handlers[topic] = func(client mqtt.Client, msg mqtt.Message) {
		handler(msg.Topic(), msg.Payload())
	}
	c.handlersMu.Unlock()

	token := c.client.Subscribe(topic, 0, c.handlers[topic])
	if token.Wait() && token.Error() != nil {
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
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	log.Printf("[MQTT] Unsubscribed from topic: %s", topic)
	return nil
}

// Publish publishes a message to a topic
func (c *Client) Publish(topic string, payload string) error {
	token := c.client.Publish(topic, 0, false, payload)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	log.Printf("[MQTT] Published to topic %s: %s", topic, payload)
	return nil
}

// PublishWithQoS publishes a message to a topic with specified QoS and retain flag
func (c *Client) PublishWithQoS(topic string, payload string, qos byte, retained bool) error {
	token := c.client.Publish(topic, qos, retained, payload)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	log.Printf("[MQTT] Published to topic %s (QoS=%d, retained=%t): %s", topic, qos, retained, payload)
	return nil
}

// onConnect is called when the client connects to the broker
func (c *Client) onConnect(client mqtt.Client) {
	log.Println("[MQTT] Connection established")
	// Re-subscribe to all topics on reconnect
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()

	for topic := range c.handlers {
		client.Subscribe(topic, 0, c.handlers[topic])
		log.Printf("[MQTT] Re-subscribed to topic: %s", topic)
	}
}

// onConnectionLost is called when the connection to the broker is lost
func (c *Client) onConnectionLost(client mqtt.Client, err error) {
	log.Printf("[MQTT] Connection lost: %v", err)
}
