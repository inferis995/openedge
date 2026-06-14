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

// slackChannel sends alarm notifications to a Slack channel via Incoming Webhook.
type slackChannel struct {
	enabled    bool
	webhookURL string
	client     *http.Client
}

func newSlackChannel(cfg map[string]string) Channel {
	url := strings.TrimSpace(cfg["notif_slack_webhook_url"])
	enabled := strings.EqualFold(cfg["notif_slack_enabled"], "true") && url != ""
	return &slackChannel{
		enabled:    enabled,
		webhookURL: url,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *slackChannel) Name() string  { return "slack" }
func (s *slackChannel) Enabled() bool { return s.enabled }

func (s *slackChannel) Send(_ context.Context, e Event) error {
	emoji := severityEmoji(e.Severity)
	color := severityColor(e.Severity)
	statusText := "🔴 ACTIVE"
	if e.Status == "CLEARED" {
		statusText = "✅ CLEARED"
		color = "#36a64f"
	}

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"blocks": []map[string]interface{}{
					{
						"type": "section",
						"text": map[string]string{
							"type": "mrkdwn",
							"text": fmt.Sprintf(
								"%s *OpenEdge Alarm — %s*\n*Tag*: %s\n*Status*: %s\n*Severity*: %s\n*Message*: %s\n*Value*: %.4g (threshold: %.4g)\n*Time*: %s",
								emoji,
								strings.ToUpper(e.Severity),
								e.TagAlias,
								statusText,
								e.Severity,
								e.Description,
								e.Value,
								e.Threshold,
								e.OccurredAt.Format(time.RFC3339),
							),
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	resp, err := s.client.Post(s.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned %d", resp.StatusCode)
	}
	return nil
}

func severityEmoji(sev string) string {
	switch sev {
	case "critical":
		return "🚨"
	case "high":
		return "⚠️"
	case "medium":
		return "🔶"
	default:
		return "ℹ️"
	}
}

func severityColor(sev string) string {
	switch sev {
	case "critical":
		return "#FF0000"
	case "high":
		return "#FF6600"
	case "medium":
		return "#FFA500"
	default:
		return "#FFFF00"
	}
}
