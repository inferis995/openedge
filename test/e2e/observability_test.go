//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The failure this file exists to prevent.
//
// telemetry.HTTPRequestsTotal, HTTPRequestDuration, MQTTMessagesReceived and
// MQTTConnected were all declared, registered, and exported at /metrics — and
// written by nothing except their own unit tests. Those unit tests passed,
// because they set the metric themselves and then read it back. /metrics
// answered 200. Prometheus scraped it happily. Grafana drew empty panels, and
// the APIHighLatency rule could never fire, because the series it queries was
// never emitted by a real request.
//
// That is the same shape as every other defect in this suite: both halves
// present, nothing joining them, every unit test green. So these tests do the
// only thing that can catch it — drive real traffic through the assembled
// stack, then read the actual scrape output.

// scrapeMetrics fetches /metrics, honouring METRICS_TOKEN when the deployment
// sets one.
func scrapeMetrics(t *testing.T) string {
	t.Helper()
	// /metrics is core-api's own port. The cloud overlay does not publish it
	// and nginx does not proxy it, on purpose: a scrape endpoint on the public
	// domain hands every request rate, route and tag count to anyone who asks.
	// TestMetricsAreNotPublic asserts that; this asserts the metrics are real,
	// which needs the port the on-prem deployment does publish.
	requireDirectAccess(t, "core-api's /metrics port")

	req, err := http.NewRequest(http.MethodGet, apiBase()+"/metrics", nil)
	if err != nil {
		t.Fatalf("building /metrics request: %v", err)
	}
	if tok := os.Getenv("METRICS_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading /metrics: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics returned %d — body: %s", resp.StatusCode, truncate(body))
	}
	return string(body)
}

// metricValue returns the value of the first sample whose line starts with the
// given prefix. Prometheus text format: "name{labels} value [timestamp]".
func metricValue(t *testing.T, scrape, prefix string) (float64, bool) {
	t.Helper()
	for _, line := range strings.Split(scrape, "\n") {
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
}

// sumMetric adds up every sample matching the prefix, across label sets.
func sumMetric(scrape, prefix string) float64 {
	var total float64
	for _, line := range strings.Split(scrape, "\n") {
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if v, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
			total += v
		}
	}
	return total
}

// A served request must show up in the HTTP counter.
//
// Before the Metrics middleware existed, this counter was absent from the
// scrape entirely no matter how much traffic the API served.
func TestHTTPRequestsAreCounted(t *testing.T) {
	admin, _ := adminSession(t)

	before := sumMetric(scrapeMetrics(t), "openedge_http_requests_total")

	// Any authenticated route will do; /api/organizations is cheap.
	for i := 0; i < 3; i++ {
		admin.mustDo(http.MethodGet, "/api/organizations", nil, http.StatusOK)
	}

	after := sumMetric(scrapeMetrics(t), "openedge_http_requests_total")
	if after < before+3 {
		t.Fatalf("served 3 requests but the counter moved from %v to %v — "+
			"the metrics middleware is not recording traffic", before, after)
	}
}

// The latency histogram must exist, because an alert rule reads it.
//
// APIHighLatency queries http_request_duration_seconds_bucket. If that series
// is never emitted, the rule is silently dead and the absence looks identical
// to good health.
func TestLatencyHistogramIsExported(t *testing.T) {
	admin, _ := adminSession(t)
	admin.mustDo(http.MethodGet, "/api/organizations", nil, http.StatusOK)

	scrape := scrapeMetrics(t)
	if !strings.Contains(scrape, "openedge_http_request_duration_seconds_bucket") {
		t.Fatal("openedge_http_request_duration_seconds_bucket is absent from /metrics — " +
			"the APIHighLatency alert rule can never fire")
	}
}

// Route labels must be the TEMPLATE, not the concrete path.
//
// Labelling by request path mints one Prometheus series per tag id, which is
// the standard way to bring down a monitoring stack with its own
// instrumentation. This asserts the id does not leak into the label.
func TestMetricsLabelsUseRouteTemplates(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	orgScoped.mustDo(http.MethodGet, fmt.Sprintf("/api/tags/%d", fx.tagID), nil, http.StatusOK)

	scrape := scrapeMetrics(t)
	concrete := fmt.Sprintf("/api/tags/%d", fx.tagID)
	for _, line := range strings.Split(scrape, "\n") {
		if strings.HasPrefix(line, "openedge_http_request") && strings.Contains(line, `"`+concrete+`"`) {
			t.Fatalf("the concrete path %q appears as a metric label — cardinality grows "+
				"with the number of tags:\n  %s", concrete, line)
		}
	}
}

// A value published by a driver must move the MQTT counter.
//
// This is the metric the NoFieldDataReceived alert is built on: if it stays at
// zero while data flows, the alert fires on a healthy plant; if it never
// exists, the alert never fires at all. Both failures are silent.
func TestPublishedValueIsCounted(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)

	before := sumMetric(scrapeMetrics(t), `openedge_mqtt_messages_received_total{prefix="data"`)

	pub := mqttConnect(t, "e2e-metrics-publisher-"+uniqueSuffix())
	publish(t, pub, fx.dataTopic, map[string]interface{}{
		"value":     42.5,
		"quality":   192,
		"timestamp": time.Now().UnixMilli(),
	})

	// The counter is incremented on receipt, so it moves as soon as the broker
	// has delivered — poll briefly rather than sleeping a fixed amount.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if sumMetric(scrapeMetrics(t), `openedge_mqtt_messages_received_total{prefix="data"`) > before {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("published a value on the data topic but openedge_mqtt_messages_received_total " +
		"did not move — the NoFieldDataReceived alert would fire on a healthy plant")
}

// The broker-connection gauge must report the truth.
//
// core-api is connected to the broker throughout this suite — every other test
// depends on it — so the gauge must read 1. A gauge stuck at 0 would page
// somebody every night; one stuck at 1 would stay silent through an outage.
func TestBrokerConnectionGaugeIsTruthful(t *testing.T) {
	scrape := scrapeMetrics(t)

	v, ok := metricValue(t, scrape, "openedge_mqtt_connected")
	if !ok {
		t.Fatal("openedge_mqtt_connected is absent from /metrics — " +
			"the CoreAPIDisconnectedFromBroker alert has nothing to read")
	}
	if v != 1 {
		t.Fatalf("openedge_mqtt_connected = %v, but core-api is demonstrably connected: "+
			"the rest of this suite publishes through the same broker", v)
	}
}

// The tag gauge gates the NoFieldDataReceived alert.
//
// That rule is written as "no data AND tags configured", so a fresh install
// does not page anybody. If openedge_tags_total is missing, the `and on()`
// clause matches nothing and the alert can never fire — the gate silently
// disables the rule it was meant to qualify.
func TestTagsGaugeIsExported(t *testing.T) {
	admin, _ := adminSession(t)
	newFixture(t, admin) // guarantees at least one tag exists

	// CollectDBMetrics runs once a minute; give it a turn.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := metricValue(t, scrapeMetrics(t), "openedge_tags_total"); ok && v > 0 {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("openedge_tags_total never became positive despite a configured tag — " +
		"NoFieldDataReceived is gated on it and would never fire")
}
