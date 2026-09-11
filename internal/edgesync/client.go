package edgesync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// fetchTimeout bounds the pull. A box on a 4G link that is technically up and
// practically useless must not hold the sync loop open for minutes.
const fetchTimeout = 30 * time.Second

// maxConfigBytes caps what is read from the wire. A plant configuration is
// kilobytes; anything past this is a wrong endpoint, a captive portal, or a
// proxy serving its own error page, and decoding megabytes of it helps nobody.
const maxConfigBytes = 8 << 20

// Fetch pulls the configuration from the central platform.
//
// Authenticated with the box's API key, which is what scopes the answer to one
// organization: the box does not say which plant it is, it proves it.
func Fetch(ctx context.Context, baseURL, apiKey string) (*Config, error) {
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("the box has no address or no key for the central platform")
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + "/api/edge/config"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigBytes))
	if err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// The body is included because the two failures that actually happen —
		// a revoked key and a wrong address — look identical without it.
		return nil, fmt.Errorf("the central platform answered %d: %s",
			resp.StatusCode, strings.TrimSpace(truncate(string(body), 200)))
	}

	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("the answer is not a configuration: %w", err)
	}
	if cfg.OrgID <= 0 {
		return nil, fmt.Errorf("the configuration names no organization")
	}
	return &cfg, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
