package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config holds the Redis client configuration
type Config struct {
	Host     string
	Port     int
	Password string
	DB       int
}

// Client wraps the Redis client with application-specific methods
type Client struct {
	client *redis.Client
	ctx    context.Context
}

// NewClient creates a new Redis client with the given configuration
func NewClient(cfg Config) *Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
		// Auto-reconnect settings
		MaxRetries:      3,
		MaxRetryBackoff: 2 * time.Second,
		MinRetryBackoff: 100 * time.Millisecond,
		// Connection pool settings
		PoolSize:     10,
		MinIdleConns: 5,
		// Keep-alive settings
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	})

	return &Client{
		client: rdb,
		ctx:    context.Background(),
	}
}

// Connect establishes connection to Redis and verifies connectivity
func (c *Client) Connect() error {
	_, err := c.client.Ping(c.ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	return nil
}

// Disconnect closes the Redis connection
func (c *Client) Disconnect() error {
	return c.client.Close()
}

// Set stores a key-value pair with the specified TTL
func (c *Client) Set(key string, value interface{}, ttl time.Duration) error {
	err := c.client.Set(c.ctx, key, value, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}
	return nil
}

// Get retrieves a value by key
// Returns the value as a string, or an error if the key doesn't exist
func (c *Client) Get(key string) (string, error) {
	val, err := c.client.Get(c.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", fmt.Errorf("key %s does not exist", key)
		}
		return "", fmt.Errorf("failed to get key %s: %w", key, err)
	}
	return val, nil
}

// GetInt retrieves an integer value by key
func (c *Client) GetInt(key string) (int, error) {
	val, err := c.Get(key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(val)
}

// GetFloat retrieves a float value by key
func (c *Client) GetFloat(key string) (float64, error) {
	val, err := c.Get(key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(val, 64)
}

// Delete removes a key from Redis
func (c *Client) Delete(key string) error {
	err := c.client.Del(c.ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}
	return nil
}

// Exists checks if a key exists in Redis
func (c *Client) Exists(key string) (bool, error) {
	count, err := c.client.Exists(c.ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check existence of key %s: %w", key, err)
	}
	return count > 0, nil
}

// SetJSON stores a JSON string value with TTL (helper for storing structured data)
func (c *Client) SetJSON(key string, jsonStr string, ttl time.Duration) error {
	return c.Set(key, jsonStr, ttl)
}

// GetJSON retrieves a JSON string value (helper for structured data)
func (c *Client) GetJSON(key string) (string, error) {
	return c.Get(key)
}

// Keys returns all keys matching a pattern
func (c *Client) Keys(pattern string) ([]string, error) {
	keys, err := c.client.Keys(c.ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys with pattern %s: %w", pattern, err)
	}
	return keys, nil
}

// FlushDB clears all keys in the current database (use with caution)
func (c *Client) FlushDB() error {
	return c.client.FlushDB(c.ctx).Err()
}

// Ping checks the connection to Redis
func (c *Client) Ping() error {
	return c.client.Ping(c.ctx).Err()
}

// Subscribe returns a Redis PubSub channel
func (c *Client) Subscribe(channel string) *redis.PubSub {
	return c.client.Subscribe(c.ctx, channel)
}

// Publish publishes a message to a channel
func (c *Client) Publish(channel string, message interface{}) error {
	return c.client.Publish(c.ctx, channel, message).Err()
}

// IsConnected returns true if the client is connected to Redis
func (c *Client) IsConnected() bool {
	return c.client.Ping(c.ctx).Err() == nil
}

// ScanKeys iterates over all keys matching a pattern using SCAN (production-safe)
// This is safer than Keys() for large datasets as it doesn't block
func (c *Client) ScanKeys(pattern string) ([]string, error) {
	var keys []string
	iter := c.client.Scan(c.ctx, 0, pattern, 0).Iterator()
	for iter.Next(c.ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan keys with pattern %s: %w", pattern, err)
	}
	return keys, nil
}

// SetMany sets multiple key-value pairs with the same TTL (batch operation)
func (c *Client) SetMany(keyValues map[string]string, ttl time.Duration) error {
	pipe := c.client.Pipeline()
	for key, value := range keyValues {
		pipe.Set(c.ctx, key, value, ttl)
	}
	_, err := pipe.Exec(c.ctx)
	if err != nil {
		return fmt.Errorf("failed to set multiple keys: %w", err)
	}
	return nil
}
