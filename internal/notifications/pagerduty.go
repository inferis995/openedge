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
	eventsURL  string
	client     *http.Client
}

func newPagerDutyChannel(cfg map[string]string) Channel {
	return newPagerDutyChannelWithURL(cfg, pagerDutyEventsURL)
}

// newPagerDutyChannelWithURL allows overriding the endpoint URL (for tests).
func newPagerDutyChannelWithURL(cfg map[string]string, url string) Channel {
	key := strings.TrimSpace(cfg["notif_pagerduty_routing_key"])
	enabled := strings.EqualFold(cfg["notif_pagerduty_enabled"], "true") && key != ""
	return &pagerDutyChannel{
		enabled:    enabled,
		routingKey: key,
		eventsURL:  url,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *pagerDutyChannel) Name() string  { return "pagerduty" }
func (p *pagerDutyChannel) Enabled() bool { return p.enabled }

// dedupKey builds the PagerDuty incident key. It must be:
//   - STABLE across the ACTIVE → CLEARED pair of one alarm, otherwise the
//     "resolve" targets a different incident than the "trigger" and incidents
//     never auto-resolve;
//   - DISTINCT per alert source, otherwise unrelated alerts collapse into a
//     single incident.
//
// alarm_events.id can satisfy neither: it is 0 on CLEARED events published
// without a persisted row and 0 for synthetic OEE alerts, which used to make
// every alert share the key openedge-alarm-0 while their trigger used
// openedge-alarm-<row id>. The alarm definition id is the stable identity of a
// rule (same on trigger and resolve, different per rule); when it is missing
// (OEE and other synthetic alerts) we fall back to the alert source label,
// which is what distinguishes those alerts from one another.
func dedupKey(e *Event) string {
	if e.DefinitionID > 0 {
		return fmt.Sprintf("openedge-alarm-def-%d", e.DefinitionID)
	}
	src := strings.ToLower(strings.TrimSpace(e.TagAlias))
	src = strings.ReplaceAll(src, " ", "-")
	if src == "" {
		src = fmt.Sprintf("event-%d", e.AlarmID)
	}
	if e.OrgID > 0 {
		return fmt.Sprintf("openedge-alarm-org%d-%s", e.OrgID, src)
	}
	return "openedge-alarm-" + src
}

func (p *pagerDutyChannel) Send(ctx context.Context, e *Event) error {
	eventAction := "trigger"
	if e.Status == "CLEARED" {
		eventAction = "resolve"
	}

	var pdSeverity string
	switch e.Severity {
	case "critical":
		pdSeverity = "critical"
	case "high":
		pdSeverity = "error"
	case "medium", "warning": // "warning" is the product vocabulary, "medium" the legacy one
		pdSeverity = "warning"
	default:
		pdSeverity = "info"
	}

	payload := map[string]interface{}{
		"routing_key":  p.routingKey,
		"event_action": eventAction,
		"dedup_key":    dedupKey(e),
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

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pagerduty marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.eventsURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty returned %d", resp.StatusCode)
	}
	return nil
}
