package redis_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/redis"
)

// ---------------------------------------------------------------------------
// NewClient — construction without connecting
// ---------------------------------------------------------------------------
// NewClient builds the client struct without dialing; the actual TCP
// connection is deferred to the first command.  Tests here confirm:
// (a) the constructor doesn't panic with any valid Config,
// (b) the returned client is non-nil.

func TestNewClient_NonNil(t *testing.T) {
	c := redis.NewClient(redis.Config{Host: "127.0.0.1", Port: 6379})
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestNewClient_WithPassword(t *testing.T) {
	c := redis.NewClient(redis.Config{Host: "127.0.0.1", Port: 6379, Password: "secret"})
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestNewClient_WithDB(t *testing.T) {
	c := redis.NewClient(redis.Config{Host: "127.0.0.1", Port: 6379, DB: 1})
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestNewClient_ZeroConfig(t *testing.T) {
	// Zero-value config must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewClient(zero Config) panicked: %v", r)
		}
	}()
	c := redis.NewClient(redis.Config{})
	if c == nil {
		t.Fatal("NewClient returned nil for zero config")
	}
}

// ---------------------------------------------------------------------------
// IsConnected — returns false when no real Redis is available
// ---------------------------------------------------------------------------

func TestIsConnected_FalseWithoutServer(t *testing.T) {
	// Port 1 is unlikely to have Redis; IsConnected should return false.
	c := redis.NewClient(redis.Config{Host: "127.0.0.1", Port: 1})
	if c.IsConnected() {
		t.Error("IsConnected() = true with no real Redis server, want false")
	}
}
