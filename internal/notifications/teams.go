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

// teamsChannel sends alarm notifications to Microsoft Teams via Incoming Webhook.
type teamsChannel struct {
	enabled    bool
	webhookURL string
	client     *http.Client
}

func newTeamsChannel(cfg map[string]string) Channel {
	url := strings.TrimSpace(cfg["notif_teams_webhook_url"])
	enabled := strings.EqualFold(cfg["notif_teams_enabled"], "true") && url != ""
	return &teamsChannel{
		enabled:    enabled,
		webhookURL: url,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *teamsChannel) Name() string  { return "teams" }
func (t *teamsChannel) Enabled() bool { return t.enabled }

func (t *teamsChannel) Send(ctx context.Context, e *Event) error {
	themeColor := "FF0000"
	statusText := "ACTIVE"
	if e.Status == "CLEARED" {
		themeColor = "36a64f"
		statusText = "CLEARED"
	}

	payload := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": themeColor,
		"summary":    fmt.Sprintf("OpenEdge Alarm — %s — %s", e.TagAlias, statusText),
		"sections": []map[string]interface{}{
			{
				"activityTitle":    fmt.Sprintf("OpenEdge Alarm: %s", strings.ToUpper(e.Severity)),
				"activitySubtitle": fmt.Sprintf("Tag: %s", e.TagAlias),
				"facts": []map[string]string{
					{"name": "Status", "value": statusText},
					{"name": "Severity", "value": e.Severity},
					{"name": "Message", "value": e.Description},
					{"name": "Value", "value": fmt.Sprintf("%.4g (threshold: %.4g)", e.Value, e.Threshold)},
					{"name": "Time", "value": e.OccurredAt.Format(time.RFC3339)},
				},
				"markdown": true,
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("teams request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("teams post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("teams returned %d", resp.StatusCode)
	}
	return nil
}
