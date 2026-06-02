// Package notifications dispatches alarm events to the operator's
// out-of-band channels (email, Telegram, …) so a critical condition is
// seen even when nobody is looking at the UI.
//
// Wiring: core-api's handleAlarmEvent calls Dispatch() right after the
// DB write. Settings are loaded from global_settings on first use and
// re-loaded once a minute, so changes in the admin UI take effect
// without restarting the service.
package notifications

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is the minimum payload the notifier needs to render a message.
// Kept lean on purpose so the package doesn't pull in the full
// models.AlarmEvent (and its DB-dependent tags) — easier to unit-test.
type Event struct {
	AlarmID     int       // alarm_events.id
	TagAlias    string    // human-friendly tag name
	Severity    string    // low | medium | high | critical
	Status      string    // ACTIVE | CLEARED
	Threshold   float64   // configured limit
	Value       float64   // measured value that fired the alarm
	Description string    // free-text from the alarm definition
	OccurredAt  time.Time // when the event happened (UTC)
	OrgID       int       // for multi-tenant scope filtering (0 = on-prem)
}

// Channel is anything that can deliver a notification. Implementations
// (email, telegram, …) live in their own files so each one is
// independent and skippable when its config is incomplete.
type Channel interface {
	Name() string                       // for logs ("email", "telegram", …)
	Enabled() bool                      // operator switch
	Send(ctx context.Context, e Event) error
}

// Dispatcher owns the notifier lifecycle: it reads the operator
// configuration from global_settings, wires up the enabled channels,
// applies rate-limiting + severity filtering, and runs the actual sends
// in a background goroutine so the caller's hot path is never blocked.
type Dispatcher struct {
	db *sql.DB

	mu              sync.RWMutex
	channels        []Channel
	minSeverity     int // 0=low … 3=critical
	notifyOnCleared bool
	rateLimit       *rateLimiter

	// MaintenanceCheck è opzionale: quando settata e ritorna true il
	// Dispatch silenzia le notifiche (gli allarmi restano in DB però).
	// Usata per integrare maintenance_windows senza creare un import
	// cycle handlers ↔ notifications.
	MaintenanceCheck func() bool

	// Settings are re-fetched on this cadence so the admin UI doesn't
	// require a restart to apply changes.
	refreshInterval time.Duration
}

// NewDispatcher builds a Dispatcher and starts the background refresh
// loop. Safe to call once at startup; Dispatch() can fire from any
// goroutine afterwards.
func NewDispatcher(db *sql.DB) *Dispatcher {
	d := &Dispatcher{
		db:              db,
		refreshInterval: 60 * time.Second,
		rateLimit:       newRateLimiter(60, time.Minute), // default 60/min global cap
	}
	d.reload()
	go d.refreshLoop()
	return d
}

// Dispatch hands an event to every enabled channel that should receive
// it according to the operator's filtering rules. Non-blocking — the
// actual send happens in a goroutine; the caller returns immediately.
func (d *Dispatcher) Dispatch(e Event) {
	d.mu.RLock()
	channels := append([]Channel(nil), d.channels...)
	minSev := d.minSeverity
	notifyCleared := d.notifyOnCleared
	d.mu.RUnlock()

	if len(channels) == 0 {
		return
	}
	if severityRank(e.Severity) < minSev {
		return
	}
	if e.Status == "CLEARED" && !notifyCleared {
		return
	}
	if !d.rateLimit.allow() {
		log.Printf("[NOTIF] rate-limited, dropping alarm %d (%s)", e.AlarmID, e.Severity)
		return
	}
	// Maintenance window check — silenziamo le notifiche durante le
	// finestre di manutenzione programmata. Il check è fail-open: se la
	// callback non è settata o crasha, l'evento esce comunque.
	if d.MaintenanceCheck != nil && d.MaintenanceCheck() {
		log.Printf("[NOTIF] maintenance window active, skipping alarm %d (%s)", e.AlarmID, e.Severity)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, ch := range channels {
			if !ch.Enabled() {
				continue
			}
			if err := ch.Send(ctx, e); err != nil {
				log.Printf("[NOTIF/%s] failed for alarm %d: %v", ch.Name(), e.AlarmID, err)
			}
		}
	}()
}

// TestSend bypasses the rate limiter and severity filter and pushes a
// synthetic event to every enabled channel. Used by the admin UI
// "Send test" button.
func (d *Dispatcher) TestSend(ctx context.Context) []error {
	e := Event{
		AlarmID:     0,
		TagAlias:    "test.alarm",
		Severity:    "high",
		Status:      "ACTIVE",
		Threshold:   100,
		Value:       150,
		Description: "Test notification from OpenEdge",
		OccurredAt:  time.Now().UTC(),
	}
	d.mu.RLock()
	channels := append([]Channel(nil), d.channels...)
	d.mu.RUnlock()

	var errs []error
	for _, ch := range channels {
		if !ch.Enabled() {
			errs = append(errs, fmt.Errorf("%s: not enabled", ch.Name()))
			continue
		}
		if err := ch.Send(ctx, e); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch.Name(), err))
		}
	}
	return errs
}

func (d *Dispatcher) refreshLoop() {
	t := time.NewTicker(d.refreshInterval)
	defer t.Stop()
	for range t.C {
		d.reload()
	}
}

// reload rebuilds the channel list + filters from global_settings.
// Conservative: if a setting fails to parse, we keep the previous value
// rather than silently disabling notifications. The admin sees the
// stale value in the UI but doesn't lose alerts.
func (d *Dispatcher) reload() {
	cfg := loadSettings(d.db)

	channels := []Channel{
		newEmailChannel(cfg),
		newTelegramChannel(cfg),
	}

	rateMax := 60
	if cfg["notif_rate_limit_per_min"] != "" {
		if n, err := strconv.Atoi(cfg["notif_rate_limit_per_min"]); err == nil && n > 0 {
			rateMax = n
		}
	}

	d.mu.Lock()
	d.channels = channels
	d.minSeverity = severityRank(cfg["notif_min_severity"])
	d.notifyOnCleared = strings.EqualFold(cfg["notif_on_cleared"], "true")
	d.rateLimit = newRateLimiter(rateMax, time.Minute)
	d.mu.Unlock()
}

// severityRank turns the severity string into a comparable int so the
// minimum-severity filter is a simple ≥ check. Unknown values map to 0
// (i.e. always pass) to err on the side of NOT silencing alerts.
func severityRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return 0
	case "medium":
		return 1
	case "high":
		return 2
	case "critical":
		return 3
	}
	return 0
}

// loadSettings fetches every key under the notif_* prefix in one query.
// Returns a map (possibly empty) — missing keys translate to empty
// strings, which the channel constructors interpret as "disabled".
func loadSettings(db *sql.DB) map[string]string {
	out := map[string]string{}
	rows, err := db.Query(`SELECT key, value FROM global_settings WHERE key LIKE 'notif\_%' ESCAPE '\'`)
	if err != nil {
		log.Printf("[NOTIF] failed to load settings: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			out[k] = v
		}
	}
	return out
}
