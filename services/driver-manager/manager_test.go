package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"
)

// ─── imageNameForDriver ────────────────────────────────────────────────────────

func TestImageNameForDriver(t *testing.T) {
	m := &Manager{}

	tests := []struct {
		driverType string
		want       string
	}{
		{"S7", "industrial-driver-s7:latest"},
		{"MODBUS_TCP", "industrial-driver-modbus:latest"},
		{"MQTT", "industrial-driver-mqtt:latest"},
		{"OPC_UA", "industrial-driver-opcua:latest"},
		{"LORAWAN", "industrial-driver-lorawan:latest"},
		{"REDIS", "industrial-driver-redis:latest"},
		{"UNKNOWN", ""},
	}

	for _, tt := range tests {
		t.Run(tt.driverType, func(t *testing.T) {
			got := m.imageNameForDriver(tt.driverType)
			if got != tt.want {
				t.Errorf("imageNameForDriver(%q) = %q, want %q", tt.driverType, got, tt.want)
			}
		})
	}
}

// ─── calculateBackoff ─────────────────────────────────────────────────────────

func TestCalculateBackoff(t *testing.T) {
	base := 1 * time.Second
	max := 30 * time.Second

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},  // base * 2^0 = 1s
		{1, 2 * time.Second},  // base * 2^1 = 2s
		{2, 4 * time.Second},  // base * 2^2 = 4s
		{10, 30 * time.Second}, // base * 2^10 = 1024s → capped at maxDelay
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			got := calculateBackoff(base, tt.attempt, max)
			if got != tt.want {
				t.Errorf("calculateBackoff(base=%v, attempt=%d, max=%v) = %v, want %v",
					base, tt.attempt, max, got, tt.want)
			}
		})
	}
}

// ─── verifySHA256 ─────────────────────────────────────────────────────────────

func TestVerifySHA256(t *testing.T) {
	content := []byte("hello openedge")

	// Write content to a temp file.
	dir := t.TempDir()
	path := dir + "/artifact.bin"
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Compute the correct SHA256.
	sum := sha256.Sum256(content)
	correctHex := fmt.Sprintf("%x", sum)

	t.Run("correct_hash", func(t *testing.T) {
		if err := verifySHA256(path, correctHex); err != nil {
			t.Errorf("verifySHA256 with correct hash returned error: %v", err)
		}
	})

	t.Run("wrong_hash", func(t *testing.T) {
		wrongHex := "0000000000000000000000000000000000000000000000000000000000000000"
		if err := verifySHA256(path, wrongHex); err == nil {
			t.Error("verifySHA256 with wrong hash should return an error, got nil")
		}
	})
}
