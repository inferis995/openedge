package telemetry

import (
	"database/sql"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Health of the plant and of the backups, for alerts that fit how a site
// actually reports. "No message for 20 minutes" is an outage on a PLC polled
// every second and an ordinary afternoon for a data logger that sends a frame
// every eight hours; each gateway's own timeout (stale_after_minutes, applied
// by the health check) is the one that knows the difference.

var (
	GatewaysEnabled = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_gateways_enabled",
		Help: "Gateways enabled.",
	})
	GatewaysOffline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_gateways_offline",
		Help: "Enabled gateways offline: no data within their own timeout, or no heartbeat from their driver for 5 minutes.",
	})
	BackupLastTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_backup_last_timestamp_seconds",
		Help: "Time of the newest backup file (unix seconds); 0 when there is none.",
	})
)

// gatewaysOfflineQuery counts what the dashboard shows as offline, and also
// the gateways whose driver went quiet: a dead driver sends no heartbeat, and
// its last reported status would otherwise stay 'online' for ever.
const gatewaysOfflineQuery = `
	SELECT COUNT(*) FROM gateways
	WHERE enabled
	  AND (health_status IS DISTINCT FROM 'online'
	       OR health_reported_at IS NULL
	       OR health_reported_at < NOW() - INTERVAL '5 minutes')`

func collectPlantMetrics(db *sql.DB) {
	var n float64
	if err := db.QueryRow(`SELECT COUNT(*) FROM gateways WHERE enabled`).Scan(&n); err == nil {
		GatewaysEnabled.Set(n)
	}
	if err := db.QueryRow(gatewaysOfflineQuery).Scan(&n); err == nil {
		GatewaysOffline.Set(n)
	}
	BackupLastTimestamp.Set(float64(newestBackup(backupDir()).Unix()))
}

func backupDir() string {
	if p := os.Getenv("BACKUP_PATH"); p != "" {
		return p
	}
	return "/backups"
}

// newestBackup is the modification time of the newest backup file in dir —
// those scripts/backup.sh writes (openedge_*.sql.gz) and the backup service
// and the UI write (.dump, .dump.age, .zip) — or the zero time. The safety
// copies taken before a restore (pre-restore_*) are not backups.
func newestBackup(dir string) time.Time {
	var newest time.Time
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Unix(0, 0)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, "pre-restore_") || strings.HasPrefix(name, ".") ||
			!(strings.HasSuffix(name, ".sql.gz") || strings.HasSuffix(name, ".dump") ||
				strings.HasSuffix(name, ".dump.age") || strings.HasSuffix(name, ".zip")) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		return time.Unix(0, 0)
	}
	return newest
}
