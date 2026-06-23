package telemetry_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/ralph/industrial-edge-middleware/internal/telemetry"
)

// ---------------------------------------------------------------------------
// Metric registration — all exported vars must be non-nil
// ---------------------------------------------------------------------------

func TestMQTTConnected_SetAndGet(t *testing.T) {
	telemetry.MQTTConnected.Set(1)
	if got := testutil.ToFloat64(telemetry.MQTTConnected); got != 1 {
		t.Errorf("MQTTConnected = %v, want 1", got)
	}
	telemetry.MQTTConnected.Set(0)
	if got := testutil.ToFloat64(telemetry.MQTTConnected); got != 0 {
		t.Errorf("MQTTConnected = %v, want 0", got)
	}
}

func TestMQTTMessagesReceived_Increment(t *testing.T) {
	telemetry.MQTTMessagesReceived.WithLabelValues("data").Add(1)
	if got := testutil.ToFloat64(telemetry.MQTTMessagesReceived.WithLabelValues("data")); got < 1 {
		t.Errorf("MQTTMessagesReceived counter = %v, want >= 1", got)
	}
}

func TestResourceGauges_SetAndGet(t *testing.T) {
	cases := []struct {
		name  string
		gauge prometheus.Gauge
		val   float64
	}{
		{"OrgsTotal", telemetry.OrgsTotal, 5},
		{"GatewaysTotal", telemetry.GatewaysTotal, 12},
		{"TagsTotal", telemetry.TagsTotal, 300},
		{"ActiveAlarmsTotal", telemetry.ActiveAlarmsTotal, 3},
		{"UsersTotal", telemetry.UsersTotal, 42},
	}
	for _, c := range cases {
		c.gauge.Set(c.val)
		if got := testutil.ToFloat64(c.gauge); got != c.val {
			t.Errorf("%s.Set(%v) → ToFloat64 = %v", c.name, c.val, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Metric names — all openedge_ metrics must have a Help string
// ---------------------------------------------------------------------------

func TestMetricNames_HaveHelpStrings(t *testing.T) {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		name := mf.GetName()
		if strings.HasPrefix(name, "openedge_") && mf.GetHelp() == "" {
			t.Errorf("metric %q has no Help string", name)
		}
	}
}

// ---------------------------------------------------------------------------
// HTTPRequestsTotal / HTTPRequestDuration lint
// ---------------------------------------------------------------------------

func TestHTTPRequestsTotal_Lint(t *testing.T) {
	telemetry.HTTPRequestsTotal.WithLabelValues("GET", "/api/tags", "200").Add(0)
	problems, err := testutil.CollectAndLint(telemetry.HTTPRequestsTotal)
	if err != nil {
		t.Fatalf("CollectAndLint error: %v", err)
	}
	for _, p := range problems {
		t.Errorf("HTTPRequestsTotal lint: %s", p.Text)
	}
}

func TestHTTPRequestDuration_Lint(t *testing.T) {
	telemetry.HTTPRequestDuration.WithLabelValues("GET", "/api/tags").Observe(0.001)
	problems, err := testutil.CollectAndLint(telemetry.HTTPRequestDuration)
	if err != nil {
		t.Fatalf("CollectAndLint error: %v", err)
	}
	for _, p := range problems {
		t.Errorf("HTTPRequestDuration lint: %s", p.Text)
	}
}
