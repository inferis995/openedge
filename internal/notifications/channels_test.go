package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testEvent is a ready-made event for use in all channel tests.
var testEvent = &Event{
	AlarmID:     42,
	TagAlias:    "reactor_temp",
	Severity:    "critical",
	Status:      "ACTIVE",
	Threshold:   95.0,
	Value:       102.3,
	Description: "Temperature exceeded limit",
	OccurredAt:  time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	OrgID:       1,
}

// ── Slack ─────────────────────────────────────────────────────────────────────

func TestSlack_DisabledWhenNoURL(t *testing.T) {
	ch := newSlackChannel(map[string]string{
		"notif_slack_enabled":     "true",
		"notif_slack_webhook_url": "",
	})
	if ch.Enabled() {
		t.Error("slack should be disabled when webhook URL is empty")
	}
}

func TestSlack_DisabledWhenFlagFalse(t *testing.T) {
	ch := newSlackChannel(map[string]string{
		"notif_slack_enabled":     "false",
		"notif_slack_webhook_url": "https://hooks.slack.com/test",
	})
	if ch.Enabled() {
		t.Error("slack should be disabled when notif_slack_enabled=false")
	}
}

func TestSlack_SendsCorrectPayload(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newSlackChannel(map[string]string{
		"notif_slack_enabled":     "true",
		"notif_slack_webhook_url": srv.URL,
	})

	if err := ch.Send(context.Background(), testEvent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v — body was: %s", err, gotBody)
	}
	if payload["attachments"] == nil {
		t.Error("expected 'attachments' key in payload")
	}
}

func TestSlack_ReturnsErrorOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := newSlackChannel(map[string]string{
		"notif_slack_enabled":     "true",
		"notif_slack_webhook_url": srv.URL,
	})
	if err := ch.Send(context.Background(), testEvent); err == nil {
		t.Error("expected error on 5xx response")
	}
}

func TestSeverityEmoji(t *testing.T) {
	tests := []struct{ sev, want string }{
		{"critical", "🚨"},
		{"high", "⚠️"},
		{"medium", "🔶"},
		{"info", "ℹ️"},
		{"unknown", "ℹ️"},
	}
	for _, tt := range tests {
		got := severityEmoji(tt.sev)
		if got != tt.want {
			t.Errorf("severityEmoji(%q) = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

// ── Teams ─────────────────────────────────────────────────────────────────────

func TestTeams_DisabledWhenNoURL(t *testing.T) {
	ch := newTeamsChannel(map[string]string{
		"notif_teams_enabled":     "true",
		"notif_teams_webhook_url": "",
	})
	if ch.Enabled() {
		t.Error("teams should be disabled when webhook URL is empty")
	}
}

func TestTeams_SendsMessageCard(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTeamsChannel(map[string]string{
		"notif_teams_enabled":     "true",
		"notif_teams_webhook_url": srv.URL,
	})

	if err := ch.Send(context.Background(), testEvent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if payload["@type"] != "MessageCard" {
		t.Errorf("@type = %v, want MessageCard", payload["@type"])
	}
}

func TestTeams_ClearedUsesGreenColor(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTeamsChannel(map[string]string{
		"notif_teams_enabled":     "true",
		"notif_teams_webhook_url": srv.URL,
	})

	cleared := *testEvent
	cleared.Status = "CLEARED"
	if err := ch.Send(context.Background(), &cleared); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.Contains(string(gotBody), "36a64f") {
		t.Error("expected green themeColor (36a64f) for CLEARED event")
	}
}

// ── PagerDuty ─────────────────────────────────────────────────────────────────

func TestPagerDuty_DisabledWhenNoKey(t *testing.T) {
	ch := newPagerDutyChannel(map[string]string{
		"notif_pagerduty_enabled":     "true",
		"notif_pagerduty_routing_key": "",
	})
	if ch.Enabled() {
		t.Error("pagerduty should be disabled when routing key is empty")
	}
}

func TestPagerDuty_TriggerOnActive(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted) // PD returns 202
	}))
	defer srv.Close()

	ch := newPagerDutyChannelWithURL(map[string]string{
		"notif_pagerduty_enabled":     "true",
		"notif_pagerduty_routing_key": "test-routing-key",
	}, srv.URL)

	if err := ch.Send(context.Background(), testEvent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["event_action"] != "trigger" {
		t.Errorf("event_action = %v, want trigger", payload["event_action"])
	}
	// Verify dedup key format
	dedupKey, _ := payload["dedup_key"].(string)
	if !strings.HasPrefix(dedupKey, "openedge-alarm-") {
		t.Errorf("dedup_key = %q, want prefix 'openedge-alarm-'", dedupKey)
	}
}

func TestPagerDuty_ResolveOnCleared(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ch := newPagerDutyChannelWithURL(map[string]string{
		"notif_pagerduty_enabled":     "true",
		"notif_pagerduty_routing_key": "test-key",
	}, srv.URL)

	cleared := *testEvent
	cleared.Status = "CLEARED"
	if err := ch.Send(context.Background(), &cleared); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var payload map[string]interface{}
	json.Unmarshal(gotBody, &payload)
	if payload["event_action"] != "resolve" {
		t.Errorf("event_action = %v, want resolve", payload["event_action"])
	}
}

func TestPagerDuty_SeverityMapping(t *testing.T) {
	tests := []struct{ input, want string }{
		{"critical", "critical"},
		{"high", "error"},
		{"medium", "warning"},
		{"info", "info"},
		{"low", "info"},
	}

	for _, tt := range tests {
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusAccepted)
		}))

		ch := newPagerDutyChannelWithURL(map[string]string{
			"notif_pagerduty_enabled":     "true",
			"notif_pagerduty_routing_key": "key",
		}, srv.URL)

		e := *testEvent
		e.Severity = tt.input
		ch.Send(context.Background(), &e)

		var payload map[string]interface{}
		json.Unmarshal(gotBody, &payload)
		inner, _ := payload["payload"].(map[string]interface{})
		got, _ := inner["severity"].(string)
		if got != tt.want {
			t.Errorf("severity(%q) mapped to %q, want %q", tt.input, got, tt.want)
		}
		srv.Close()
	}
}
