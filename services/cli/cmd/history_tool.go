package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// get_tag_history reads GET /api/history, which needs tag_id, start and end
// (RFC3339). The period defaults to the last 24 hours; interval and agg are
// passed through as the API names them.

var (
	historyIntervals = map[string]bool{"1m": true, "5m": true, "1h": true, "1d": true}
	historyAggs      = map[string]bool{"mean": true, "min": true, "max": true, "last": true}
)

func historyToolRequest(args map[string]interface{}, now time.Time) (string, url.Values, error) {
	tagID := intArg(args, "tag_id")
	if tagID <= 0 {
		return "", nil, fmt.Errorf("tag_id is required")
	}
	parse := func(key string) (time.Time, bool, error) {
		v := strings.TrimSpace(strArg(args, key))
		if v == "" {
			return time.Time{}, false, nil
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("%s: %q is not RFC3339 (e.g. 2026-11-13T08:00:00Z)", key, v)
		}
		return t, true, nil
	}
	from, hasFrom, err := parse("from")
	if err != nil {
		return "", nil, err
	}
	to, hasTo, err := parse("to")
	if err != nil {
		return "", nil, err
	}
	if !hasTo {
		to = now
	}
	if !hasFrom {
		from = to.Add(-24 * time.Hour)
	}
	if !from.Before(to) {
		return "", nil, fmt.Errorf("from must be before to")
	}

	q := url.Values{}
	q.Set("tag_id", strconv.Itoa(tagID))
	q.Set("start", from.UTC().Format(time.RFC3339))
	q.Set("end", to.UTC().Format(time.RFC3339))
	if v := strArg(args, "interval"); v != "" {
		if !historyIntervals[v] {
			return "", nil, fmt.Errorf("interval must be one of 1m, 5m, 1h, 1d")
		}
		q.Set("interval", v)
	}
	if v := strArg(args, "agg"); v != "" {
		if !historyAggs[v] {
			return "", nil, fmt.Errorf("agg must be one of mean, min, max, last")
		}
		q.Set("agg", v)
	}
	return "/api/history", q, nil
}
