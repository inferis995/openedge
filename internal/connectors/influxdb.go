// Package connectors implements push connectors to external time-series systems.
// InfluxDB v2: tag history is forwarded as line protocol batches on a configurable interval.
package connectors

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxBufferedLines caps the in-memory backlog kept across failed flushes.
// ~200k lines is a few tens of MB — enough to ride out a long InfluxDB outage
// without letting a permanent failure grow the buffer until core-api is OOM-killed.
const maxBufferedLines = 200_000

// InfluxDBConnector pushes OpenEdge tag history to an InfluxDB v2 bucket.
// It runs a background goroutine that polls global_settings every minute
// and flushes buffered points every flush_interval seconds.
type InfluxDBConnector struct {
	db     *sql.DB
	mu     sync.RWMutex
	cfg    influxCfg
	buf    []string // buffered line-protocol lines
	client *http.Client
}

type influxCfg struct {
	enabled       bool
	url           string
	token         string
	org           string
	bucket        string
	batchSize     int
	flushInterval time.Duration
}

// NewInfluxDBConnector creates the connector and starts the background worker.
func NewInfluxDBConnector(db *sql.DB) *InfluxDBConnector {
	c := &InfluxDBConnector{
		db:     db,
		client: &http.Client{Timeout: 15 * time.Second},
	}
	c.reload()
	go c.run()
	return c
}

// Write buffers a single tag data point. Called from handleDataUpdate in main.go.
// Thread-safe; returns immediately.
func (c *InfluxDBConnector) Write(tagID, orgID int, tagAlias string, value interface{}, quality int, tsMs int64) {
	c.mu.RLock()
	enabled := c.cfg.enabled
	batchSize := c.cfg.batchSize
	c.mu.RUnlock()
	if !enabled {
		return
	}

	// InfluxDB line protocol:
	// measurement,tag_id=42,org_id=1,alias=Portata_Ingresso value=41.2,quality=0i <timestamp_ns>
	alias := sanitizeTag(tagAlias)
	tsNs := tsMs * 1_000_000

	var valuePart string
	switch v := value.(type) {
	case float64:
		// NaN/±Inf render as "NaN"/"+Inf" in line protocol, which InfluxDB
		// rejects with a 400 — poisoning the whole batch it travels in. A
		// broken sensor or a divide-by-zero in a scaling formula is enough to
		// produce one, so drop the point here instead.
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return
		}
		valuePart = fmt.Sprintf("value=%g,quality=%di", v, quality)
	case int64:
		valuePart = fmt.Sprintf("value=%di,quality=%di", v, quality)
	case bool:
		if v {
			valuePart = fmt.Sprintf("value=true,quality=%di", quality)
		} else {
			valuePart = fmt.Sprintf("value=false,quality=%di", quality)
		}
	default:
		// Skip non-numeric / non-bool values
		return
	}

	line := fmt.Sprintf("openedge_tag,tag_id=%d,org_id=%d,alias=%s %s %d",
		tagID, orgID, alias, valuePart, tsNs)

	c.mu.Lock()
	c.buf = append(c.buf, line)
	buf := c.buf
	c.mu.Unlock()

	if len(buf) >= batchSize {
		go c.flush()
	}
}

func (c *InfluxDBConnector) run() {
	reloadTicker := time.NewTicker(60 * time.Second)
	defer reloadTicker.Stop()

	c.mu.RLock()
	interval := c.cfg.flushInterval
	c.mu.RUnlock()
	if interval < time.Second {
		interval = 10 * time.Second
	}
	flushTicker := time.NewTicker(interval)
	defer flushTicker.Stop()

	for {
		select {
		case <-reloadTicker.C:
			c.reload()
			// Recreate flush ticker if interval changed
			c.mu.RLock()
			newInterval := c.cfg.flushInterval
			c.mu.RUnlock()
			if newInterval != interval && newInterval >= time.Second {
				flushTicker.Reset(newInterval)
				interval = newInterval
			}
		case <-flushTicker.C:
			c.flush()
		}
	}
}

func (c *InfluxDBConnector) flush() {
	c.mu.Lock()
	if len(c.buf) == 0 {
		c.mu.Unlock()
		return
	}
	lines := c.buf
	c.buf = nil
	cfg := c.cfg
	c.mu.Unlock()

	if !cfg.enabled || cfg.url == "" || cfg.token == "" {
		return
	}

	body := strings.Join(lines, "\n")
	url := fmt.Sprintf("%s/api/v2/write?org=%s&bucket=%s&precision=ns",
		strings.TrimRight(cfg.url, "/"), cfg.org, cfg.bucket)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		log.Printf("[INFLUX] request build error: %v", err)
		return
	}
	req.Header.Set("Authorization", "Token "+cfg.token)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := c.client.Do(req)
	if err != nil {
		// Transport failure: the server may simply be down, so keep the points.
		log.Printf("[INFLUX] write error: %v — re-queuing %d points", err, len(lines))
		c.requeue(lines)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode < 300:
		// success
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// Retryable: rate limited, or the server is unhealthy.
		log.Printf("[INFLUX] write returned HTTP %d — re-queuing %d points", resp.StatusCode, len(lines))
		c.requeue(lines)
	default:
		// 4xx other than 429 is permanent: a malformed line (NaN/Inf), a
		// rejected token, a missing bucket. Re-queuing it would replay the
		// same rejection on every flush forever, growing the buffer without
		// bound and blocking every later point behind it — so drop it, loudly.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("[INFLUX] DROPPING %d points — HTTP %d (permanent): %s",
			len(lines), resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// requeue puts a failed batch back at the head of the buffer, bounded so a long
// outage cannot exhaust memory. When the cap is reached the OLDEST points are
// discarded: during an outage the freshest process data is the useful one.
func (c *InfluxDBConnector) requeue(lines []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.buf = append(lines, c.buf...)
	if len(c.buf) > maxBufferedLines {
		dropped := len(c.buf) - maxBufferedLines
		c.buf = c.buf[dropped:]
		log.Printf("[INFLUX] buffer full — dropped %d oldest points (cap %d)", dropped, maxBufferedLines)
	}
}

func (c *InfluxDBConnector) reload() {
	rows, err := c.db.QueryContext(context.Background(), `
		SELECT key, value FROM global_settings
		WHERE key LIKE 'influx\_%' ESCAPE '\'
	`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	cfg := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			log.Printf("[influxdb] config scan error: %v", err)
			continue
		}
		cfg[k] = v
	}

	batchSize := 500
	if n, err := fmt.Sscanf(cfg["influx_batch_size"], "%d", &batchSize); n == 0 || err != nil {
		batchSize = 500
	}
	flushSec := 10
	if n, err := fmt.Sscanf(cfg["influx_flush_interval"], "%d", &flushSec); n == 0 || err != nil {
		flushSec = 10
	}

	c.mu.Lock()
	c.cfg = influxCfg{
		enabled:       strings.EqualFold(cfg["influx_enabled"], "true"),
		url:           cfg["influx_url"],
		token:         cfg["influx_token"],
		org:           cfg["influx_org"],
		bucket:        cfg["influx_bucket"],
		batchSize:     batchSize,
		flushInterval: time.Duration(flushSec) * time.Second,
	}
	c.mu.Unlock()
}

// sanitizeTag replaces chars not allowed in InfluxDB tag values.
func sanitizeTag(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == ',' || r == '=' {
			return '_'
		}
		return r
	}, s)
}
