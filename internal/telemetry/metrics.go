// Package telemetry exposes Prometheus metrics for the OpenEdge platform.
// The /metrics endpoint is scraped by Prometheus and powers Grafana dashboards
// and Alertmanager alerts.
package telemetry

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP request metrics (set by gin middleware).
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "openedge_http_requests_total",
		Help: "Total HTTP requests by method, path, and status code.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "openedge_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	// MQTT metrics (incremented by the MQTT subscription handler).
	MQTTMessagesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "openedge_mqtt_messages_received_total",
		Help: "MQTT messages received, by topic prefix.",
	}, []string{"prefix"})

	MQTTConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_mqtt_connected",
		Help: "1 if the broker connection is up, 0 otherwise.",
	})

	// Resource gauges — updated every minute by CollectDBMetrics.
	OrgsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_organizations_total",
		Help: "Total number of organizations.",
	})

	GatewaysTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_gateways_total",
		Help: "Total number of gateways (enabled + disabled).",
	})

	TagsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_tags_total",
		Help: "Total number of configured tags.",
	})

	ActiveAlarmsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_active_alarms_total",
		Help: "Number of currently ACTIVE alarm events.",
	})

	UsersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "openedge_users_total",
		Help: "Total number of user accounts.",
	})
)

// CollectDBMetrics queries key counters from the DB and updates the resource
// gauges. Call once on startup and then every minute via a goroutine.
func CollectDBMetrics(db *sql.DB) {
	type gaugeQuery struct {
		g     prometheus.Gauge
		query string
	}
	queries := []gaugeQuery{
		{OrgsTotal, `SELECT COUNT(*) FROM organizations`},
		{GatewaysTotal, `SELECT COUNT(*) FROM gateways`},
		{TagsTotal, `SELECT COUNT(*) FROM tags`},
		{ActiveAlarmsTotal, `SELECT COUNT(*) FROM alarm_events WHERE status = 'ACTIVE'`},
		{UsersTotal, `SELECT COUNT(*) FROM users`},
	}
	for _, q := range queries {
		var n float64
		if err := db.QueryRow(q.query).Scan(&n); err == nil {
			q.g.Set(n)
		}
	}
}

// StartDBMetricsCollector runs CollectDBMetrics every minute in a goroutine.
func StartDBMetricsCollector(db *sql.DB) {
	CollectDBMetrics(db)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for range t.C {
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("metrics collector panic", "err", r)
					}
				}()
				CollectDBMetrics(db)
			}()
		}
	}()
}
