package handlers

import (
	"testing"
	"time"
)

func TestTheBucketFollowsTheChartWidth(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		span   time.Duration
		points int
		want   time.Duration
		table  string
	}{
		{time.Hour, 1000, 5 * time.Second, "tag_history"},         // 3.6 s → 5 s, raw
		{24 * time.Hour, 1000, 2 * time.Minute, "tag_history_1m"}, // 86.4 s → 2 min (was: 24 hourly averages)
		{30 * 24 * time.Hour, 1000, time.Hour, "tag_history_1h"},  // 43 min → 1 h (was: 30 daily averages)
		{365 * 24 * time.Hour, 300, 2 * 24 * time.Hour, "tag_history_1d"},
	}
	for _, c := range cases {
		b := chartBucket(t0, t0.Add(c.span), c.points)
		if b != c.want {
			t.Errorf("%v over %d points: bucket %v, want %v", c.span, c.points, b, c.want)
		}
		if table, _ := aggregateSource(b); table != c.table {
			t.Errorf("%v buckets read %s, want %s", b, table, c.table)
		}
	}
}
