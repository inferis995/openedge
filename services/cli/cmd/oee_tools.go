package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The OEE read-only MCP tools. Every OEE history endpoint answers 400 without
// both from and to, so the period is always sent: the last 24 hours unless
// the caller gives one.

const (
	oeeFromHelp = "Start, RFC3339 or YYYY-MM-DD (optional: 24 h before the end)"
	oeeToHelp   = "End, RFC3339 or YYYY-MM-DD (optional: now). A bare date is that day's midnight, so the day itself is left out: to include 10 November give 2026-11-11"
)

// oeePeriod is the period to send: what the caller gave, with the missing
// end filled in. A bare date is midnight UTC, as the API reads it.
func oeePeriod(args map[string]interface{}, now time.Time) (from, to time.Time, err error) {
	parse := func(key string) (time.Time, bool, error) {
		v := strings.TrimSpace(strArg(args, key))
		if v == "" {
			return time.Time{}, false, nil
		}
		if t, perr := time.Parse(time.RFC3339, v); perr == nil {
			return t, true, nil
		}
		if t, perr := time.Parse("2006-01-02", v); perr == nil {
			return t, true, nil
		}
		return time.Time{}, false, fmt.Errorf("%s: %q is not RFC3339 or YYYY-MM-DD", key, v)
	}
	from, hasFrom, err := parse("from")
	if err != nil {
		return from, to, err
	}
	to, hasTo, err := parse("to")
	if err != nil {
		return from, to, err
	}
	if !hasTo {
		to = now
	}
	if !hasFrom {
		from = to.Add(-24 * time.Hour)
	}
	if !from.Before(to) {
		return from, to, fmt.Errorf("from must be before to")
	}
	return from, to, nil
}

// oeeToolRequest builds the API path and query for an OEE tool. history and
// by-shift take an optional profile (none: the whole plant); loss tree and
// reliability need one.
func oeeToolRequest(name string, args map[string]interface{}, now time.Time) (string, url.Values, error) {
	profileID := intArg(args, "profile_id")
	from, to, err := oeePeriod(args, now)
	if err != nil {
		return "", nil, err
	}
	q := url.Values{}
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	switch name {
	case "get_oee_history", "get_oee_by_shift":
		if profileID > 0 {
			q.Set("profile_id", strconv.Itoa(profileID))
		}
		if name == "get_oee_by_shift" {
			return "/api/oee/by-shift", q, nil
		}
		if b := strArg(args, "bucket"); b != "" {
			q.Set("bucket", b)
		}
		return "/api/oee/history-v2", q, nil
	case "get_loss_tree":
		if profileID <= 0 {
			return "", nil, fmt.Errorf("profile_id is required")
		}
		q.Set("profile_id", strconv.Itoa(profileID))
		return "/api/oee/loss-tree", q, nil
	case "get_oee_reliability":
		if profileID <= 0 {
			return "", nil, fmt.Errorf("profile_id is required")
		}
		return fmt.Sprintf("/api/oee/profiles/%d/reliability", profileID), q, nil
	}
	return "", nil, fmt.Errorf("unknown OEE tool %s", name)
}
