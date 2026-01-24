package mqtt

import (
	"fmt"
	"log"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

// MessageHandler is a callback function for received messages
type MessageHandler func(topic string, payload []byte)

// Client wraps the Paho MQTT client with reconnection and publish/subscribe capabilities
type Client struct {
	client        pahomqtt.Client
	options       *pahomqtt.ClientOptions
	mu            sync.RWMutex
	subscriptions map[string]MessageHandler
	connected     bool
}

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

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() Config {
	return Config{
		Host:          "localhost",
		Port:          1883,
		ClientID:      fmt.Sprintf("mqtt-client-%d", time.Now().UnixNano()),
		CleanSession:  true,
		AutoReconnect: true,
		KeepAlive:     30 * time.Second,
	}
}

// NewClient creates a new MQTT client with the given configuration
func NewClient(cfg Config) *Client {
	c := &Client{
		subscriptions: make(map[string]MessageHandler),
	}

	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port))
	opts.SetClientID(cfg.ClientID)
	opts.SetCleanSession(cfg.CleanSession)
	opts.SetAutoReconnect(cfg.AutoReconnect)
	opts.SetKeepAlive(cfg.KeepAlive)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	// Set Last Will Testament if configured
	if cfg.LWTTopic != "" {
		opts.SetWill(cfg.LWTTopic, cfg.LWTPayload, 1, cfg.LWTRetained)
	}

	// Connection lost handler
	opts.SetConnectionLostHandler(func(client pahomqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v", err)
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
	})

	// On connect handler - resubscribe to all topics
	opts.SetOnConnectHandler(func(client pahomqtt.Client) {
		log.Printf("MQTT connected to %s:%d", cfg.Host, cfg.Port)
		c.mu.Lock()
		c.connected = true
		c.mu.Unlock()

		// Resubscribe to all topics on reconnect
		c.resubscribe()
	})

	// Reconnecting handler
	opts.SetReconnectingHandler(func(client pahomqtt.Client, opts *pahomqtt.ClientOptions) {
		log.Println("MQTT attempting to reconnect...")
	})

	c.options = opts
	c.client = pahomqtt.NewClient(opts)

	return c
}

// Connect establishes a connection to the MQTT broker
func (c *Client) Connect() error {
	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}
	return nil
}

// Disconnect gracefully disconnects from the MQTT broker
func (c *Client) Disconnect(quiesce uint) {
	c.client.Disconnect(quiesce)
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
}

// IsConnected returns whether the client is currently connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected && c.client.IsConnected()
}

// Publish sends a message to the specified topic
func (c *Client) Publish(topic string, payload interface{}) error {
	return c.PublishWithQoS(topic, payload, 1, false)
}

// PublishWithQoS sends a message with specified QoS and retain flag
func (c *Client) PublishWithQoS(topic string, payload interface{}, qos byte, retained bool) error {
	if !c.client.IsConnected() {
		return fmt.Errorf("MQTT client not connected")
	}

	token := c.client.Publish(topic, qos, retained, payload)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish to topic %s: %w", topic, token.Error())
	}
	return nil
}

// Subscribe registers a callback for messages on the specified topic
func (c *Client) Subscribe(topic string, callback MessageHandler) error {
	return c.SubscribeWithQoS(topic, callback, 1)
}

// SubscribeWithQoS subscribes to a topic with specified QoS level
func (c *Client) SubscribeWithQoS(topic string, callback MessageHandler, qos byte) error {
	c.mu.Lock()
	c.subscriptions[topic] = callback
	c.mu.Unlock()

	handler := func(client pahomqtt.Client, msg pahomqtt.Message) {
		callback(msg.Topic(), msg.Payload())
	}

	token := c.client.Subscribe(topic, qos, handler)
	if token.Wait() && token.Error() != nil {
		c.mu.Lock()
		delete(c.subscriptions, topic)
		c.mu.Unlock()
		return fmt.Errorf("failed to subscribe to topic %s: %w", topic, token.Error())
	}

	log.Printf("MQTT subscribed to topic: %s", topic)
	return nil
}

// Unsubscribe removes a subscription from the specified topic
func (c *Client) Unsubscribe(topic string) error {
	c.mu.Lock()
	delete(c.subscriptions, topic)
	c.mu.Unlock()

	token := c.client.Unsubscribe(topic)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to unsubscribe from topic %s: %w", topic, token.Error())
	}

	log.Printf("MQTT unsubscribed from topic: %s", topic)
	return nil
}

// resubscribe restores all subscriptions after a reconnect
func (c *Client) resubscribe() {
	c.mu.RLock()
	subs := make(map[string]MessageHandler)
	for topic, handler := range c.subscriptions {
		subs[topic] = handler
	}
	c.mu.RUnlock()

	for topic, callback := range subs {
		handler := func(client pahomqtt.Client, msg pahomqtt.Message) {
			callback(msg.Topic(), msg.Payload())
		}
		token := c.client.Subscribe(topic, 1, handler)
		if token.Wait() && token.Error() != nil {
			log.Printf("Failed to resubscribe to topic %s: %v", topic, token.Error())
		} else {
			log.Printf("MQTT resubscribed to topic: %s", topic)
		}
	}
}
