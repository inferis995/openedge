package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// telegramChannel sends alarm events to a Telegram chat via the Bot API.
// Setup is short: the operator creates a bot with @BotFather, gets the
// token, then either DMs the bot once OR adds it to a group; in both
// cases the chat_id is fetched from
// https://api.telegram.org/bot<TOKEN>/getUpdates.
type telegramChannel struct {
	enabled bool
	token   string
	chatID  string
	http    *http.Client
}

func newTelegramChannel(cfg map[string]string) Channel {
	c := &telegramChannel{
		enabled: strings.EqualFold(cfg["notif_telegram_enabled"], "true"),
		token:   strings.TrimSpace(cfg["notif_telegram_bot_token"]),
		chatID:  strings.TrimSpace(cfg["notif_telegram_chat_id"]),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
	if c.token == "" || c.chatID == "" {
		c.enabled = false
	}
	return c
}

func (c *telegramChannel) Name() string  { return "telegram" }
func (c *telegramChannel) Enabled() bool { return c.enabled }

func (c *telegramChannel) Send(ctx context.Context, e *Event) error {
	body := renderMarkdown(*e)
	// Telegram limits messages to 4096 chars; truncate defensively even
	// though we generate ~200 — defends against a future huge description.
	if len(body) > 4000 {
		body = body[:3997] + "..."
	}

	form := url.Values{}
	form.Set("chat_id", c.chatID)
	form.Set("text", body)
	form.Set("parse_mode", "Markdown")
	form.Set("disable_web_page_preview", "true")

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", url.PathEscape(c.token))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		// Decode Telegram's structured error so the operator UI shows
		// "chat not found" rather than just "400 Bad Request".
		var tgErr struct {
			Description string `json:"description"`
			ErrorCode   int    `json:"error_code"`
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &tgErr)
		if tgErr.Description != "" {
			return fmt.Errorf("telegram %d: %s", tgErr.ErrorCode, tgErr.Description)
		}
		return fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// renderMarkdown produces a compact Telegram message. The leading icon
// is a quick visual severity cue for an operator scrolling chat history.
func renderMarkdown(e Event) string {
	icon := "🟢"
	switch strings.ToLower(e.Severity) {
	case "medium":
		icon = "🟡"
	case "high":
		icon = "🟠"
	case "critical":
		icon = "🔴"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s *%s* — %s\n", icon, strings.ToUpper(e.Status), tgEscape(e.TagAlias))
	fmt.Fprintf(&b, "Severity: `%s`\n", strings.ToUpper(e.Severity))
	fmt.Fprintf(&b, "Value: `%g`  Threshold: `%g`\n", e.Value, e.Threshold)
	fmt.Fprintf(&b, "When: `%s`\n", e.OccurredAt.Format(time.RFC3339))
	if e.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", tgEscape(e.Description))
	}
	return b.String()
}

// tgEscape neutralises the handful of Markdown characters Telegram
// treats as formatting. A tag alias like "Line_1/temp[2]" would
// otherwise render with subscripts/links.
var tgMarkdownReplacer = strings.NewReplacer(
	"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "`", "\\`",
)

func tgEscape(s string) string { return tgMarkdownReplacer.Replace(s) }
