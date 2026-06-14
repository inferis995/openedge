package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const pagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

// pagerDutyChannel creates/resolves PagerDuty incidents via Events API v2.
type pagerDutyChannel struct {
	enabled    bool
	routingKey string
	client     *http.Client
}

func newPagerDutyChannel(cfg map[string]string) Channel {
	key := strings.TrimSpace(cfg["notif_pagerduty_routing_key"])
	enabled := strings.EqualFold(cfg["notif_pagerduty_enabled"], "true") && key != ""
	return &pagerDutyChannel{
		enabled:    enabled,
		routingKey: key,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *pagerDutyChannel) Name() string  { return "pagerduty" }
func (p *pagerDutyChannel) Enabled() bool { return p.enabled }

func (p *pagerDutyChannel) Send(_ context.Context, e Event) error {
	eventAction := "trigger"
	if e.Status == "CLEARED" {
		eventAction = "resolve"
	}

	pdSeverity := "warning"
	switch e.Severity {
	case "critical":
		pdSeverity = "critical"
	case "high":
		pdSeverity = "error"
	case "medium":
		pdSeverity = "warning"
	default:
		pdSeverity = "info"
	}

	payload := map[string]interface{}{
		"routing_key":  p.routingKey,
		"event_action": eventAction,
		"dedup_key":    fmt.Sprintf("openedge-alarm-%d", e.AlarmID),
		"payload": map[string]interface{}{
			"summary":   fmt.Sprintf("[OpenEdge] %s — %s: %s", strings.ToUpper(e.Severity), e.TagAlias, e.Description),
			"timestamp": e.OccurredAt.Format(time.RFC3339),
			"severity":  pdSeverity,
			"source":    "OpenEdge",
			"custom_details": map[string]interface{}{
				"tag_alias": e.TagAlias,
				"value":     e.Value,
				"threshold": e.Threshold,
				"status":    e.Status,
				"alarm_id":  e.AlarmID,
			},
		},
	}

	body, _ := json.Marshal(payload)
	resp, err := p.client.Post(pagerDutyEventsURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty returned %d", resp.StatusCode)
	}
	return nil
}
