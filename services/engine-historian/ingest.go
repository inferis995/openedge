package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// This file holds the parts of the broker-to-database path that are pure
// decisions rather than I/O, so they can be exercised without a broker, a
// Redis and a Postgres. They were previously inline in handleDataMessage and
// handleSparkplugMessage — in two copies that had already diverged.

// dataTopicLevels is the exact shape every driver publishes on:
// data/{org}/{site}/{area}/{gateway}/{alias}. Each level is a naming.Slug, so
// none of them can contain a '/' and the count is fixed.
const dataTopicLevels = 6

type dataTopic struct {
	org, site, area, gateway, alias string
}

// parseDataTopic splits a legacy data topic into its five names.
//
// It rejects a topic with more levels than the contract instead of reading the
// first five and discarding the rest. That truncation is how a '/' inside a
// name used to disappear: the extra level shifted everything down, the lookup
// searched for a hierarchy that does not exist, and the tag was never
// historised — with the topic looking perfectly well-formed in a broker trace.
func parseDataTopic(topic string) (dataTopic, error) {
	parts := strings.Split(topic, "/")
	if len(parts) == 0 || parts[0] != "data" {
		return dataTopic{}, fmt.Errorf("not a data topic: %q", topic)
	}
	if len(parts) != dataTopicLevels {
		return dataTopic{}, fmt.Errorf("data topic has %d levels, want %d: %q — a "+
			"name containing '/' would produce this", len(parts), dataTopicLevels, topic)
	}
	for i, p := range parts {
		if p == "" {
			return dataTopic{}, fmt.Errorf("data topic level %d is empty: %q", i, topic)
		}
	}
	return dataTopic{org: parts[1], site: parts[2], area: parts[3], gateway: parts[4], alias: parts[5]}, nil
}

// historianFloat converts a value of any shape a driver may publish into the
// float64 column tag_history stores.
//
// The second return is what matters. Falling through with 0 — as both handlers
// once did — writes a GOOD-quality 0.0 that an operator cannot tell apart from
// a real zero reading: 0 bar, 0 °C, valve shut. A value that is not a number
// is not history; it is dropped and logged instead.
//
// NaN and the infinities are refused for a different reason. tag_history.value
// is FLOAT8, which accepts them happily, so there is no error to notice; but
// Postgres sorts NaN above every other value, so a single NaN makes AVG, MIN
// and MAX over its whole window return NaN. One corrupt register from a PLC
// would poison every aggregate of the period, not one point of it.
func historianFloat(v interface{}) (float64, bool) {
	f, ok := historianNumber(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func historianNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case nil:
		return 0, false
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		f, err := strconv.ParseFloat(fmt.Sprintf("%v", n), 64)
		return f, err == nil
	}
}

// sampleTimestamp picks the first usable timestamp from the candidates a
// payload offers, in order of preference, and falls back to now.
//
// "Usable" means strictly positive. Zero and negative both land the row before
// 1970, where it is invisible on every trend and is swept by the retention
// worker on its next pass — the sample is accepted, written, and gone. The
// Sparkplug path only rejected zero, so a negative timestamp went through.
func sampleTimestamp(now func() time.Time, candidates ...int64) int64 {
	for _, ts := range candidates {
		if ts > 0 {
			return ts
		}
	}
	return now().UnixMilli()
}
