package settings

import (
	"database/sql"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// Manager handles global settings with caching and hot-reload support
type Manager struct {
	db     *sql.DB
	mu     sync.RWMutex
	config *models.GlobalSettings

	// Last publish times per tag for heartbeat tracking
	lastPublish map[int]time.Time

	// Metrics tracking
	publishedCount int
	skippedCount   int
	metricsMu      sync.Mutex
}

// NewManager creates a new settings manager
func NewManager(db *sql.DB) *Manager {
	m := &Manager{
		db:          db,
		lastPublish: make(map[int]time.Time),
	}
	if err := m.Load(); err != nil {
		log.Printf("[SETTINGS] Failed to load initial settings: %v, using defaults", err)
		m.config = &models.GlobalSettings{
			PublishMode:         models.PublishModeDual,
			RBEHeartbeatSeconds: 60,
			RBEDeadbandPercent:  0.5,
		}
	}
	return m
}

// Load reads settings from database
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	query := `SELECT key, value FROM global_settings`
	rows, err := m.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		settings[key] = value
	}

	config := &models.GlobalSettings{
		PublishMode:         models.PublishMode(settings["publish_mode"]),
		RBEHeartbeatSeconds: parseInt(settings["rbe_heartbeat_seconds"], 60),
		RBEDeadbandPercent:  parseFloat(settings["rbe_deadband_percent"], 0.5),
	}

	// Validate publish mode
	if !config.PublishMode.IsValid() {
		config.PublishMode = models.PublishModeDual
	}

	m.config = config
	log.Printf("[SETTINGS] Loaded: publish_mode=%s, heartbeat=%ds, deadband=%.1f%%",
		config.PublishMode, config.RBEHeartbeatSeconds, config.RBEDeadbandPercent)
	return nil
}

// Get returns current settings
func (m *Manager) Get() *models.GlobalSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config == nil {
		return &models.GlobalSettings{
			PublishMode:         models.PublishModeDual,
			RBEHeartbeatSeconds: 60,
			RBEDeadbandPercent:  0.5,
		}
	}
	return m.config
}

// ShouldPublish determines if a value should be published based on mode and RBE logic
// Returns true if should publish, false if should skip
func (m *Manager) ShouldPublish(tagID int, newValue, oldValue interface{}, newQuality, oldQuality int) bool {
	m.mu.RLock()
	config := m.config
	m.mu.RUnlock()

	if config == nil {
		return true
	}

	// Universal RBE Logic (Applies to Legacy, Sparkplug, and Dual)
	// 1. Publish if quality changed
	if newQuality != oldQuality {
		log.Printf("[RBE-DEBUG] Publishing Tag %d due to QUALITY CHANGE (%d -> %d)", tagID, oldQuality, newQuality)
		m.recordPublish()
		m.updateLastPublish(tagID)
		return true
	}

	// 2. Publish if value changed (considering deadband)
	if !valuesEqual(newValue, oldValue, config.RBEDeadbandPercent) {
		log.Printf("[RBE-DEBUG] Publishing Tag %d due to VALUE CHANGE. Old: %v (%T), New: %v (%T)", tagID, oldValue, oldValue, newValue, newValue)
		m.recordPublish()
		m.updateLastPublish(tagID)
		return true
	}

	// 3. Heartbeat logic:
	// If heartbeat == 0, it means Real-Time (publish every single poll without waiting)
	// If heartbeat < 0 (e.g. -1), it means "Solo cambio" (only publish on value/quality changes, which were already handled above)
	if config.RBEHeartbeatSeconds == 0 {
		m.recordPublish()
		m.updateLastPublish(tagID)
		return true
	} else if config.RBEHeartbeatSeconds > 0 {
		m.mu.RLock()
		lastPub, exists := m.lastPublish[tagID]
		m.mu.RUnlock()

		if !exists || time.Since(lastPub) > time.Duration(config.RBEHeartbeatSeconds)*time.Second {
			log.Printf("[RBE-DEBUG] Publishing Tag %d due to Heartbeat elapsed", tagID)
			m.recordPublish()
			m.updateLastPublish(tagID)
			return true
		}
	}

	// Skip publishing - value unchanged and heartbeat not elapsed (or disabled)
	m.recordSkip()
	return false
}

// updateLastPublish updates the last publish time for a tag
func (m *Manager) updateLastPublish(tagID int) {
	m.mu.Lock()
	m.lastPublish[tagID] = time.Now()
	m.mu.Unlock()
}

// recordPublish increments published counter
func (m *Manager) recordPublish() {
	m.metricsMu.Lock()
	m.publishedCount++
	m.metricsMu.Unlock()
}

// recordSkip increments skipped counter
func (m *Manager) recordSkip() {
	m.metricsMu.Lock()
	m.skippedCount++
	m.metricsMu.Unlock()
}

// GetMetrics returns current publish metrics and resets counters
func (m *Manager) GetMetrics() models.PublishMetrics {
	m.metricsMu.Lock()
	defer m.metricsMu.Unlock()

	published := m.publishedCount
	skipped := m.skippedCount
	total := published + skipped

	var savedPercent float64
	if total > 0 {
		savedPercent = float64(skipped) / float64(total) * 100
	}

	// Reset counters
	m.publishedCount = 0
	m.skippedCount = 0

	return models.PublishMetrics{
		Published:    published,
		Skipped:      skipped,
		SavedPercent: savedPercent,
	}
}

// valuesEqual compares two values with optional deadband for numeric types
func valuesEqual(a, b interface{}, deadbandPercent float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Handle numeric types with deadband
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return false
		}
		if deadbandPercent > 0 && av != 0 {
			tolerance := av * deadbandPercent / 100
			diff := av - bv
			if diff < 0 {
				diff = -diff
			}
			return diff <= tolerance
		}
		return av == bv
	case float32:
		bv, ok := b.(float32)
		if !ok {
			return false
		}
		if deadbandPercent > 0 && av != 0 {
			tolerance := float64(av) * deadbandPercent / 100
			diff := float64(av - bv)
			if diff < 0 {
				diff = -diff
			}
			return diff <= tolerance
		}
		return av == bv
	case int:
		bv, ok := b.(int)
		return ok && av == bv
	case int16:
		bv, ok := b.(int16)
		return ok && av == bv
	case int32:
		bv, ok := b.(int32)
		return ok && av == bv
	case uint16:
		bv, ok := b.(uint16)
		return ok && av == bv
	case uint32:
		bv, ok := b.(uint32)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return a == b
	}
}

func parseInt(s string, def int) int {
	val, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return val
}

func parseFloat(s string, def float64) float64 {
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return val
}
