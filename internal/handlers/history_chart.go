package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// A chart asks for about as many values as it has pixels. The fixed levels of
// seriesForTag ignored that: a 24-hour window came back as 24 hourly averages,
// thirty days as thirty daily ones, and a spike or a dip inside a bucket was
// averaged away. chartSeries sizes the bucket to the request and returns the
// minimum and maximum of every bucket with its value, so the chart can draw
// the envelope — and when the window holds few samples (a data logger
// reporting every few hours), it returns the samples themselves.

const (
	minChartPoints = 50
	maxChartPoints = 5000
)

// niceBuckets are the bucket sizes a chart may get: round numbers, so bucket
// boundaries fall on readable instants.
var niceBuckets = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	24 * time.Hour, 2 * 24 * time.Hour, 7 * 24 * time.Hour,
}

// chartBucket is the smallest nice bucket that keeps [start, end] within
// `points` buckets.
func chartBucket(start, end time.Time, points int) time.Duration {
	want := end.Sub(start) / time.Duration(points)
	for _, b := range niceBuckets {
		if b >= want {
			return b
		}
	}
	return niceBuckets[len(niceBuckets)-1]
}

// aggregateSource picks the narrowest table that can answer a bucket: a
// continuous aggregate whose own buckets are no wider than the request's, or
// the raw table. Reading a month of 1-second data from tag_history to draw
// 1000 points is 2.6 million rows; from tag_history_1h it is 720.
func aggregateSource(bucket time.Duration) (table string, level string) {
	switch {
	case bucket >= 24*time.Hour:
		return "tag_history_1d", "1d"
	case bucket >= time.Hour:
		return "tag_history_1h", "1h"
	case bucket >= time.Minute:
		return "tag_history_1m", "1m"
	}
	return "tag_history", "raw"
}

// chartSeries answers a chart that asked for about `points` values of a tag
// across [start, end]. agg is the SQL-safe name chosen by the caller's
// whitelist ("avg", "min", "max", "last").
func (h *HistoryHandler) chartSeries(ctx context.Context, tagID int, start, end time.Time, agg string, points int) ([]HistoryDataPoint, error) {
	if points < minChartPoints {
		points = minChartPoints
	}
	if points > maxChartPoints {
		points = maxChartPoints
	}

	// Few enough samples to draw them all: the real readings, at their real
	// instants, not averages of them. The LIMIT keeps the count cheap on a
	// window holding millions of rows.
	var n int
	if err := h.db.QueryRowContext(ctx, `
		SELECT count(*) FROM (
			SELECT 1 FROM tag_history WHERE tag_id = $1 AND time >= $2 AND time <= $3 LIMIT $4
		) s`, tagID, start, end, points*2+1).Scan(&n); err != nil {
		return nil, fmt.Errorf("count samples: %w", err)
	}

	var out []HistoryDataPoint
	var err error
	if n <= points*2 {
		out, err = h.queryRaw(tagID, start, end)
	} else {
		bucket := chartBucket(start, end, points)
		table, level := aggregateSource(bucket)
		if table != "tag_history" {
			out, err = h.bucketsFromAggregate(ctx, tagID, start, end, bucket, agg, table, level)
			if err != nil {
				// A deployment without TimescaleDB has no continuous aggregates.
				log.Printf("[HISTORY] %s not available (%v), bucketing raw data", table, err)
			}
		}
		if table == "tag_history" || err != nil {
			out, err = h.bucketsFromRaw(ctx, tagID, start, end, bucket, agg)
		}
	}
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []HistoryDataPoint{}
	}

	// The last good value before the window, as seriesForTag does: a tag that
	// did not change during the window still has a value to draw.
	startMs := start.UnixMilli()
	if len(out) == 0 || out[0].Timestamp > startMs+500 {
		if seed, seedErr := h.getSeedValue(tagID, start); seedErr == nil && seed != nil {
			out = append([]HistoryDataPoint{*seed}, out...)
		}
	}
	return out, nil
}

func bucketValueColumn(agg string, avg, min, max, last string) string {
	switch agg {
	case "min":
		return min
	case "max":
		return max
	case "last":
		return last
	}
	return avg
}

// bucketsFromRaw groups tag_history into buckets. A bucket whose rows are all
// offline markers (NULL value) comes back as a gap; an empty bucket is absent.
func (h *HistoryHandler) bucketsFromRaw(ctx context.Context, tagID int, start, end time.Time, bucket time.Duration, agg string) ([]HistoryDataPoint, error) {
	valueExpr := bucketValueColumn(agg, "AVG(value)", "MIN(value)", "MAX(value)",
		"(array_agg(value ORDER BY time DESC) FILTER (WHERE value IS NOT NULL))[1]")
	// #nosec G201 -- valueExpr is one of four fixed expressions above.
	query := fmt.Sprintf(`
		SELECT EXTRACT(EPOCH FROM date_bin($4::interval, time, $2::timestamptz))::BIGINT * 1000 AS ts,
		       %s AS val, MIN(value), MAX(value), COUNT(value)
		FROM tag_history
		WHERE tag_id = $1 AND time >= $2 AND time <= $3
		GROUP BY 1 ORDER BY 1`, valueExpr)
	return h.scanBuckets(ctx, query, "raw", tagID, start, end, bucket)
}

// bucketsFromAggregate re-buckets a continuous aggregate. The average is
// weighted by each source bucket's sample count; min, max and last are exact.
func (h *HistoryHandler) bucketsFromAggregate(ctx context.Context, tagID int, start, end time.Time, bucket time.Duration, agg, table, level string) ([]HistoryDataPoint, error) {
	valueExpr := bucketValueColumn(agg,
		"SUM(avg_value * sample_count) / NULLIF(SUM(sample_count), 0)",
		"MIN(min_value)", "MAX(max_value)",
		"(array_agg(last_value ORDER BY bucket DESC) FILTER (WHERE last_value IS NOT NULL))[1]")
	// #nosec G201 -- table is one of three fixed names (aggregateSource) and
	// valueExpr one of four fixed expressions above.
	query := fmt.Sprintf(`
		SELECT EXTRACT(EPOCH FROM date_bin($4::interval, bucket, $2::timestamptz))::BIGINT * 1000 AS ts,
		       %s AS val, MIN(min_value), MAX(max_value), SUM(sample_count)
		FROM %s
		WHERE tag_id = $1 AND bucket >= $2 AND bucket <= $3
		GROUP BY 1 ORDER BY 1`, valueExpr, table)
	return h.scanBuckets(ctx, query, level, tagID, start, end, bucket)
}

func (h *HistoryHandler) scanBuckets(ctx context.Context, query, source string, tagID int, start, end time.Time, bucket time.Duration) ([]HistoryDataPoint, error) {
	rows, err := h.db.QueryContext(ctx, query, tagID, start, end, fmt.Sprintf("%d seconds", int64(bucket.Seconds())))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []HistoryDataPoint
	for rows.Next() {
		var ts int64
		var val, lo, hi sql.NullFloat64
		var count sql.NullInt64
		if err := rows.Scan(&ts, &val, &lo, &hi, &count); err != nil {
			return nil, fmt.Errorf("scan bucket: %w", err)
		}
		p := HistoryDataPoint{Timestamp: ts, Source: source, SampleCount: count.Int64}
		if val.Valid {
			v, mn, mx := val.Float64, lo.Float64, hi.Float64
			p.Value, p.Min, p.Max = &v, &mn, &mx
		} else {
			p.Quality = 1 // only offline markers in this bucket: a gap
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
