package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// ── Quality Codes (OPC-UA / Sparkplug B standard) ──────────────────────
// These follow the OPC-UA quality code convention used by industrial
// historians like OSIsoft PI, Ignition, Wonderware, etc.
const (
	QualityGood   = 0 // Good - data is current and reliable
	QualityBad    = 1 // Bad - communication failure, sensor error
	QualityStale  = 2 // Stale/Interpolated - last known value but timestamp is old
	QualityUncert = 3 // Uncertain - data may not be reliable
)

// HistoryHandler handles historical data query requests
type HistoryHandler struct {
	db *sql.DB
}

// NewHistoryHandler creates a new history handler
func NewHistoryHandler(db *sql.DB) *HistoryHandler {
	return &HistoryHandler{
		db: db,
	}
}

// InitializeRetentionPolicy reads the db_retention_days setting and configures TimescaleDB
func (h *HistoryHandler) InitializeRetentionPolicy() {
	var retentionDays int
	err := h.db.QueryRow(`
		SELECT COALESCE(
			(SELECT value::int FROM global_settings WHERE key = 'db_retention_days'),
			30
		)
	`).Scan(&retentionDays)

	if err != nil {
		log.Printf("[TIMESCALEDB] Warning: Could not read db_retention_days, defaulting to 30 days: %v", err)
		retentionDays = 30
	}

	if retentionDays <= 0 {
		log.Printf("[TIMESCALEDB] Retention policy disabled (db_retention_days = %d)", retentionDays)
		h.db.Exec(`SELECT remove_retention_policy('tag_history', if_exists => true)`)
		h.db.Exec(`SELECT remove_retention_policy('system_events', if_exists => true)`)
		return
	}

	// Remove existing policies first to avoid duplicate errors, then add the new ones
	h.db.Exec(`SELECT remove_retention_policy('tag_history', if_exists => true)`)
	h.db.Exec(`SELECT remove_retention_policy('system_events', if_exists => true)`)

	intervalStr := fmt.Sprintf("INTERVAL '%d days'", retentionDays)
	log.Printf("[TIMESCALEDB] Configuring data retention policy: keeping %d days of history", retentionDays)

	_, err1 := h.db.Exec(fmt.Sprintf(`SELECT add_retention_policy('tag_history', %s, if_not_exists => true)`, intervalStr))
	if err1 != nil {
		log.Printf("[TIMESCALEDB] Error setting retention policy for tag_history: %v", err1)
	}

	_, err2 := h.db.Exec(fmt.Sprintf(`SELECT add_retention_policy('system_events', %s, if_not_exists => true)`, intervalStr))
	if err2 != nil {
		log.Printf("[TIMESCALEDB] Error setting retention policy for system_events: %v", err2)
	}
}

// HistoryDataPoint represents a single historical data point
type HistoryDataPoint struct {
	Timestamp   int64    `json:"timestamp"`
	Value       *float64 `json:"value"` // nil = gap in chart
	Quality     int      `json:"quality"`
	Source      string   `json:"source,omitempty"`       // "raw", "1m", "1h", "1d"
	SampleCount int64    `json:"sample_count,omitempty"` // Points aggregated into this bucket
}

// HistoryResponse wraps the data points with metadata
type HistoryResponse struct {
	Data        []HistoryDataPoint `json:"data"`
	Source      string             `json:"source"`        // Which aggregate level was used
	TotalPoints int                `json:"total_points"`  // Number of data points returned
	AutoIntvl   bool               `json:"auto_interval"` // Whether interval was auto-selected
}

// TagStatsResponse contains statistics for a tag over a time range
type TagStatsResponse struct {
	MinValue       *float64 `json:"min_value"`
	MaxValue       *float64 `json:"max_value"`
	AvgValue       *float64 `json:"avg_value"`
	StdDev         *float64 `json:"std_dev"`
	SampleCount    int64    `json:"sample_count"`
	FirstValue     *float64 `json:"first_value"`
	LastValue      *float64 `json:"last_value"`
	FirstTimestamp *int64   `json:"first_timestamp"`
	LastTimestamp  *int64   `json:"last_timestamp"`
}

// AggregationLevel represents which aggregate to query
type AggregationLevel struct {
	Source   string        // "raw", "1m", "1h", "1d"
	Interval time.Duration // Suggested interval for further downsampling
}

// determineAggregationLevel selects the optimal aggregate based on time range
func determineAggregationLevel(start, end time.Time) AggregationLevel {
	duration := end.Sub(start)

	switch {
	case duration <= time.Hour:
		// Up to 1 hour: use raw data
		return AggregationLevel{Source: "raw", Interval: 0}

	case duration <= 6*time.Hour:
		// 1-6 hours: use 1-minute aggregate
		return AggregationLevel{Source: "1m", Interval: time.Minute}

	case duration <= 7*24*time.Hour:
		// 6 hours - 7 days: use 1-hour aggregate
		return AggregationLevel{Source: "1h", Interval: time.Hour}

	default:
		// 7+ days: use 1-day aggregate
		return AggregationLevel{Source: "1d", Interval: 24 * time.Hour}
	}
}

// Query handles GET /api/history?tag_id={id}&start={iso}&end={iso}&agg={agg}&interval={interval}
// @Summary Query historical data
// @Description Query historical data for a tag from PostgreSQL with automatic aggregate selection
// @Tags history
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param tag_id query int true "Tag ID"
// @Param start query string true "Start time (ISO 8601)"
// @Param end query string true "End time (ISO 8601)"
// @Param agg query string false "Aggregation function (mean, max, min, last)"
// @Param interval query string false "Aggregation interval (e.g., 1m, 5m, 1h, 1d)"
// @Param stats query bool false "Include statistics in response"
// @Success 200 {object} HistoryResponse
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/history [get]
func (h *HistoryHandler) Query(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	tagIDStr := c.Query("tag_id")
	if tagIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag_id query parameter is required"})
		return
	}

	tagID, err := strconv.Atoi(tagIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag_id parameter"})
		return
	}

	// Verify tag ownership and get data type
	dataType, err := h.getTagDetails(tagID, orgID)
	if err != nil {
		// Tag not found or doesn't belong to this organization
		log.Printf("[HISTORY] Tag %d not found or access denied for org %d: %v", tagID, orgID, err)
		c.JSON(http.StatusForbidden, gin.H{"error": "Tag not found or access denied", "detail": err.Error()})
		return
	}

	startStr := c.Query("start")
	if startStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start query parameter is required (ISO 8601 format)"})
		return
	}

	endStr := c.Query("end")
	if endStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end query parameter is required (ISO 8601 format)"})
		return
	}

	// Parse start and end times
	startTime, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start parameter format, use ISO 8601 format (e.g., 2024-01-01T00:00:00Z)"})
		return
	}

	endTime, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end parameter format, use ISO 8601 format (e.g., 2024-01-01T23:59:59Z)"})
		return
	}

	// Validate time range
	if !endTime.After(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end time must be after start time"})
		return
	}

	// Get optional aggregation parameters
	agg := c.Query("agg")                // e.g., "mean", "max", "min", "last"
	interval := c.Query("interval")      // e.g., "1m", "5m", "1h", "1d"
	forceRaw := c.Query("raw") == "true" // Force raw data query

	// Map aggregation function
	aggFunc := "avg"
	switch agg {
	case "max":
		aggFunc = "max"
	case "min":
		aggFunc = "min"
	case "last":
		aggFunc = "last"
	case "mean":
		aggFunc = "avg"
	}

	// Determine which aggregate level to use
	aggLevel := determineAggregationLevel(startTime, endTime)
	if forceRaw {
		aggLevel.Source = "raw"
	}

	log.Printf("[HISTORY] Querying tag_id=%d, type=%s, start=%s, end=%s, agg=%s, interval=%s, source=%s",
		tagID, dataType, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), agg, interval, aggLevel.Source)

	var dataPoints []HistoryDataPoint
	var source string
	var autoIntvl bool = (interval == "")

	// Query based on source level
	switch aggLevel.Source {
	case "1m":
		dataPoints, err = h.query1mAggregate(tagID, startTime, endTime, aggFunc)
		source = "1m"
	case "1h":
		dataPoints, err = h.query1hAggregate(tagID, startTime, endTime, aggFunc)
		source = "1h"
	case "1d":
		dataPoints, err = h.query1dAggregate(tagID, startTime, endTime, aggFunc)
		source = "1d"
	default:
		// Raw query with optional downsampling
		if interval != "" {
			dataPoints, err = h.queryRawWithInterval(tagID, startTime, endTime, aggFunc, interval)
		} else {
			dataPoints, err = h.queryRaw(tagID, startTime, endTime)
		}
		source = "raw"
	}

	if err != nil {
		log.Printf("[HISTORY] Query failed for tag %d: %v", tagID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query history: %v", err)})
		return
	}

	log.Printf("[HISTORY] Query successful: returned %d data points for tag_id=%d from source=%s", len(dataPoints), tagID, source)

	if dataPoints == nil {
		dataPoints = []HistoryDataPoint{}
	}

	// ── SEED pattern ──────────────────────────────────────────────────────
	// For on-change (RBE) drivers like Sparkplug B, Modbus, OPC-UA, etc.
	// the tag may not publish during the query range if its value hasn't
	// changed.  Prepend the last known GOOD value before range start so the
	// chart always has an initial state.
	startMs := startTime.UnixMilli()
	needSeed := len(dataPoints) == 0 || (dataPoints[0].Value != nil && dataPoints[0].Timestamp > startMs+500)
	if needSeed {
		seed, seedErr := h.getSeedValue(tagID, startTime)
		if seedErr == nil && seed != nil {
			dataPoints = append([]HistoryDataPoint{*seed}, dataPoints...)
			log.Printf("[HISTORY] SEED injected for tag_id=%d at ts=%d", tagID, seed.Timestamp)
		}
	}

	// NOTE: fill-to-end is handled by the frontend with real-time quality
	// awareness (BAD quality = don't extend, GOOD = extend to range end).

	// Check if stats are requested
	includeStats := c.Query("stats") == "true"
	var stats *TagStatsResponse
	if includeStats {
		stats, _ = h.getTagStats(tagID, startTime, endTime)
	}

	response := HistoryResponse{
		Data:        dataPoints,
		Source:      source,
		TotalPoints: len(dataPoints),
		AutoIntvl:   autoIntvl,
	}

	// Add stats to response if requested
	if includeStats && stats != nil {
		c.JSON(http.StatusOK, gin.H{
			"data":          response.Data,
			"source":        response.Source,
			"total_points":  response.TotalPoints,
			"auto_interval": response.AutoIntvl,
			"stats":         stats,
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// queryRaw returns all data points within the time range.
// NULL-value rows (offline markers with source='offline') are included
// with quality=1 so the frontend renders them as chart gaps.
func (h *HistoryHandler) queryRaw(tagID int, start, end time.Time) ([]HistoryDataPoint, error) {
	query := `
		SELECT EXTRACT(EPOCH FROM time)::BIGINT * 1000 as ts, value
		FROM tag_history
		WHERE tag_id = $1 AND time >= $2 AND time <= $3
		ORDER BY time ASC
	`

	rows, err := h.db.Query(query, tagID, start, end)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	var points []HistoryDataPoint
	for rows.Next() {
		var ts int64
		var value sql.NullFloat64
		if err := rows.Scan(&ts, &value); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		if value.Valid {
			points = append(points, HistoryDataPoint{
				Timestamp:   ts,
				Value:       &value.Float64,
				Quality:     0, // GOOD
				Source:      "raw",
				SampleCount: 1,
			})
		} else {
			// NULL value = offline marker → quality BAD → frontend shows gap
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     nil,
				Quality:   1, // BAD — creates visible gap in chart
				Source:    "raw",
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return points, nil
}

// queryRawWithInterval returns raw data points without synthetic gaps
// For production: returns ONLY actual data points, no NULL filling
func (h *HistoryHandler) queryRawWithInterval(tagID int, start, end time.Time, aggFunc, interval string) ([]HistoryDataPoint, error) {
	// For short intervals (< 5 minutes), just return raw data without bucketing
	intervalDuration, err := parseInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("invalid interval: %w", err)
	}

	// Parse interval duration to check if it's short
	var intervalMs int64
	if len(interval) >= 2 {
		val, _ := strconv.Atoi(interval[:len(interval)-1])
		unit := interval[len(interval)-1:]
		switch unit {
		case "s":
			intervalMs = int64(val * 1000)
		case "m":
			intervalMs = int64(val * 60 * 1000)
		case "h":
			intervalMs = int64(val * 60 * 60 * 1000)
		}
	}

	// For intervals <= 1 minute, return raw data without bucketing
	// This avoids creating synthetic NULL values for RBE data
	if intervalMs <= 60000 {
		return h.queryRaw(tagID, start, end)
	}

	// Map aggregation function to SQL
	sqlAgg := "AVG"
	switch aggFunc {
	case "max":
		sqlAgg = "MAX"
	case "min":
		sqlAgg = "MIN"
	case "last":
		sqlAgg = "LAST"
	}

	// For longer intervals, use bucketing.
	// HAVING COUNT(tag_id) > 0 keeps buckets that have ANY data (including
	// offline markers with NULL value) but discards truly empty buckets.
	// Offline-only buckets produce val=NULL → frontend renders as gap.
	query := fmt.Sprintf(`
		SELECT
			EXTRACT(EPOCH FROM bucket)::BIGINT * 1000 as ts,
			%s(value) as val,
			COUNT(tag_id) as cnt
		FROM generate_series($2::timestamptz, $3::timestamptz, $4::interval) as bucket
		LEFT JOIN tag_history ON
			time >= bucket AND time < bucket + $4::interval AND tag_id = $1
		GROUP BY bucket
		ORDER BY bucket ASC
	`, sqlAgg)

	rows, err := h.db.Query(query, tagID, start, end, intervalDuration)
	if err != nil {
		// Fallback to simple query
		return h.queryRaw(tagID, start, end)
	}
	defer rows.Close()

	var points []HistoryDataPoint

	for rows.Next() {
		var ts int64
		var value sql.NullFloat64
		var count int64
		if err := rows.Scan(&ts, &value, &count); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		if value.Valid {
			points = append(points, HistoryDataPoint{
				Timestamp:   ts,
				Value:       &value.Float64,
				Quality:     0,
				Source:      "raw",
				SampleCount: count,
			})
		} else {
			// NULL aggregate = offline period → gap in chart
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     nil,
				Quality:   1,
				Source:    "raw",
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return points, nil
}

// query1mAggregate queries the 1-minute continuous aggregate
func (h *HistoryHandler) query1mAggregate(tagID int, start, end time.Time, aggFunc string) ([]HistoryDataPoint, error) {
	// Map aggregation function to column
	valueCol := "avg_value"
	switch aggFunc {
	case "max":
		valueCol = "max_value"
	case "min":
		valueCol = "min_value"
	case "last":
		valueCol = "last_value"
	}

	query := fmt.Sprintf(`
		SELECT
			EXTRACT(EPOCH FROM series.bucket)::BIGINT * 1000 as ts,
			%s as val,
			COALESCE(quality, 1) as quality,
			COALESCE(sample_count, 0) as sample_count
		FROM generate_series($2::timestamptz, $3::timestamptz, '1 minute'::interval) as series(bucket)
		LEFT JOIN tag_history_1m data ON series.bucket = data.bucket AND data.tag_id = $1
		ORDER BY series.bucket ASC
	`, valueCol)

	rows, err := h.db.Query(query, tagID, start, end)
	if err != nil {
		// Fallback to raw query if aggregate doesn't exist yet
		log.Printf("[HISTORY] 1m aggregate not available, falling back to raw: %v", err)
		return h.queryRaw(tagID, start, end)
	}
	defer rows.Close()

	var points []HistoryDataPoint
	for rows.Next() {
		var ts int64
		var value sql.NullFloat64
		var quality int
		var sampleCount int64
		if err := rows.Scan(&ts, &value, &quality, &sampleCount); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		if value.Valid {
			points = append(points, HistoryDataPoint{
				Timestamp:   ts,
				Value:       &value.Float64,
				Quality:     quality,
				Source:      "1m",
				SampleCount: sampleCount,
			})
		} else {
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     nil,
				Quality:   quality,
				Source:    "1m",
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return points, nil
}

// query1hAggregate queries the 1-hour continuous aggregate
func (h *HistoryHandler) query1hAggregate(tagID int, start, end time.Time, aggFunc string) ([]HistoryDataPoint, error) {
	valueCol := "avg_value"
	switch aggFunc {
	case "max":
		valueCol = "max_value"
	case "min":
		valueCol = "min_value"
	case "last":
		valueCol = "last_value"
	}

	query := fmt.Sprintf(`
		SELECT
			EXTRACT(EPOCH FROM series.bucket)::BIGINT * 1000 as ts,
			%s as val,
			COALESCE(quality, 1) as quality,
			COALESCE(sample_count, 0) as sample_count
		FROM generate_series($2::timestamptz, $3::timestamptz, '1 hour'::interval) as series(bucket)
		LEFT JOIN tag_history_1h data ON series.bucket = data.bucket AND data.tag_id = $1
		ORDER BY series.bucket ASC
	`, valueCol)

	rows, err := h.db.Query(query, tagID, start, end)
	if err != nil {
		log.Printf("[HISTORY] 1h aggregate not available, falling back to 1m: %v", err)
		return h.query1mAggregate(tagID, start, end, aggFunc)
	}
	defer rows.Close()

	var points []HistoryDataPoint
	for rows.Next() {
		var ts int64
		var value sql.NullFloat64
		var quality int
		var sampleCount int64
		if err := rows.Scan(&ts, &value, &quality, &sampleCount); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		if value.Valid {
			points = append(points, HistoryDataPoint{
				Timestamp:   ts,
				Value:       &value.Float64,
				Quality:     quality,
				Source:      "1h",
				SampleCount: sampleCount,
			})
		} else {
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     nil,
				Quality:   quality,
				Source:    "1h",
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return points, nil
}

// query1dAggregate queries the 1-day continuous aggregate
func (h *HistoryHandler) query1dAggregate(tagID int, start, end time.Time, aggFunc string) ([]HistoryDataPoint, error) {
	valueCol := "avg_value"
	switch aggFunc {
	case "max":
		valueCol = "max_value"
	case "min":
		valueCol = "min_value"
	case "last":
		valueCol = "last_value"
	}

	query := fmt.Sprintf(`
		SELECT
			EXTRACT(EPOCH FROM series.bucket)::BIGINT * 1000 as ts,
			%s as val,
			COALESCE(quality, 1) as quality,
			COALESCE(sample_count, 0) as sample_count
		FROM generate_series($2::timestamptz, $3::timestamptz, '1 day'::interval) as series(bucket)
		LEFT JOIN tag_history_1d data ON series.bucket = data.bucket AND data.tag_id = $1
		ORDER BY series.bucket ASC
	`, valueCol)

	rows, err := h.db.Query(query, tagID, start, end)
	if err != nil {
		log.Printf("[HISTORY] 1d aggregate not available, falling back to 1h: %v", err)
		return h.query1hAggregate(tagID, start, end, aggFunc)
	}
	defer rows.Close()

	var points []HistoryDataPoint
	for rows.Next() {
		var ts int64
		var value sql.NullFloat64
		var quality int
		var sampleCount int64
		if err := rows.Scan(&ts, &value, &quality, &sampleCount); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		if value.Valid {
			points = append(points, HistoryDataPoint{
				Timestamp:   ts,
				Value:       &value.Float64,
				Quality:     quality,
				Source:      "1d",
				SampleCount: sampleCount,
			})
		} else {
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     nil,
				Quality:   quality,
				Source:    "1d",
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return points, nil
}

// getTagStats retrieves statistics for a tag over a time range
func (h *HistoryHandler) getTagStats(tagID int, start, end time.Time) (*TagStatsResponse, error) {
	// Try using the helper function first
	query := `
		SELECT
			min_value, max_value, avg_value, std_dev,
			sample_count, first_value, last_value,
			first_timestamp, last_timestamp
		FROM get_tag_stats($1, $2, $3)
	`

	var stats TagStatsResponse
	var minValue, maxValue, avgValue, stdDev, firstValue, lastValue sql.NullFloat64
	var sampleCount sql.NullInt64
	var firstTs, lastTs sql.NullInt64

	err := h.db.QueryRow(query, tagID, start, end).Scan(
		&minValue, &maxValue, &avgValue, &stdDev,
		&sampleCount, &firstValue, &lastValue,
		&firstTs, &lastTs,
	)

	if err != nil {
		// Fallback to direct query on raw data
		log.Printf("[HISTORY] get_tag_stats function not available, using direct query: %v", err)

		fallbackQuery := `
			SELECT
				MIN(value) as min_value,
				MAX(value) as max_value,
				AVG(value) as avg_value,
				STDDEV(value) as std_dev,
				COUNT(*) as sample_count,
				(SELECT value FROM tag_history WHERE tag_id = $1 AND time >= $2 AND time <= $3 ORDER BY time ASC LIMIT 1) as first_value,
				(SELECT value FROM tag_history WHERE tag_id = $1 AND time >= $2 AND time <= $3 ORDER BY time DESC LIMIT 1) as last_value,
				(SELECT EXTRACT(EPOCH FROM time)::BIGINT * 1000 FROM tag_history WHERE tag_id = $1 AND time >= $2 AND time <= $3 ORDER BY time ASC LIMIT 1) as first_timestamp,
				(SELECT EXTRACT(EPOCH FROM time)::BIGINT * 1000 FROM tag_history WHERE tag_id = $1 AND time >= $2 AND time <= $3 ORDER BY time DESC LIMIT 1) as last_timestamp
			FROM tag_history
			WHERE tag_id = $1 AND time >= $2 AND time <= $3
		`

		err = h.db.QueryRow(fallbackQuery, tagID, start, end).Scan(
			&minValue, &maxValue, &avgValue, &stdDev,
			&sampleCount, &firstValue, &lastValue,
			&firstTs, &lastTs,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				return nil, nil
			}
			return nil, fmt.Errorf("stats query error: %w", err)
		}
	}

	stats.MinValue = nullFloatToPtr(minValue)
	stats.MaxValue = nullFloatToPtr(maxValue)
	stats.AvgValue = nullFloatToPtr(avgValue)
	stats.StdDev = nullFloatToPtr(stdDev)
	if sampleCount.Valid {
		stats.SampleCount = sampleCount.Int64
	}
	stats.FirstValue = nullFloatToPtr(firstValue)
	stats.LastValue = nullFloatToPtr(lastValue)
	if firstTs.Valid {
		stats.FirstTimestamp = &firstTs.Int64
	}
	if lastTs.Valid {
		stats.LastTimestamp = &lastTs.Int64
	}

	return &stats, nil
}

// nullFloatToPtr converts sql.NullFloat64 to *float64
func nullFloatToPtr(nf sql.NullFloat64) *float64 {
	if nf.Valid {
		return &nf.Float64
	}
	return nil
}

// parseInterval converts interval string (e.g., "1m", "5m", "1h") to PostgreSQL interval
func parseInterval(interval string) (string, error) {
	if len(interval) < 2 {
		return "", fmt.Errorf("invalid interval format")
	}

	unit := interval[len(interval)-1:]
	value := interval[:len(interval)-1]

	switch unit {
	case "s":
		return value + " second", nil
	case "m":
		return value + " minute", nil
	case "h":
		return value + " hour", nil
	case "d":
		return value + " day", nil
	case "w":
		return value + " week", nil
	default:
		return "", fmt.Errorf("unknown interval unit: %s", unit)
	}
}

// getSeedValue returns the last known GOOD quality value before the given
// start time. This implements the "SEED" pattern used by SCADA historians:
// it ensures the chart always has an initial state at the beginning of the
// requested time range, even when data is published only on change (RBE).
//
// IMPORTANT: The seed value is marked with QualityStale (not QualityGood)
// because it represents a historical value that predates the requested range.
// The frontend should render stale data differently (e.g., dashed line) or
// hide it entirely when the tag is currently offline.
//
// Works for all driver types: Sparkplug B, Modbus, OPC-UA, S7, Redis, MQTT.
func (h *HistoryHandler) getSeedValue(tagID int, start time.Time) (*HistoryDataPoint, error) {
	// Only seed with the last GOOD value — skip offline markers (NULL values).
	query := `
		SELECT EXTRACT(EPOCH FROM time)::BIGINT * 1000 as ts, value
		FROM tag_history
		WHERE tag_id = $1 AND time < $2 AND value IS NOT NULL
		ORDER BY time DESC
		LIMIT 1
	`

	var ts int64
	var value float64
	err := h.db.QueryRow(query, tagID, start).Scan(&ts, &value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No seed available — tag has no history before this range
		}
		return nil, fmt.Errorf("seed query error: %w", err)
	}

	// Map the seed value to the start of the requested range so the chart
	// line begins at the left edge.
	// Quality is STALE because this is a cached value from before the range.
	startMs := start.UnixMilli()
	return &HistoryDataPoint{
		Timestamp:   startMs,
		Value:       &value,
		Quality:     QualityStale, // STALE - not current data
		Source:      "seed",
		SampleCount: 0,
	}, nil
}

// getTagDetails retrieves the data type for a tag and verifies ownership
func (h *HistoryHandler) getTagDetails(tagID, orgID int) (string, error) {
	var dataType string

	query := `
		SELECT t.data_type
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE t.id = $1 AND s.org_id = $2
	`

	err := h.db.QueryRow(query, tagID, orgID).Scan(&dataType)
	if err != nil {
		return "", fmt.Errorf("failed to verify tag ownership: %w", err)
	}

	return dataType, nil
}

// GetDataRange returns the min/max timestamps of data in tag_history
func (h *HistoryHandler) GetDataRange(c *gin.Context) {
	var oldest, newest sql.NullTime
	err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT MIN(time), MAX(time) FROM tag_history
	`).Scan(&oldest, &newest)

	if err != nil || !oldest.Valid || !newest.Valid {
		c.JSON(http.StatusOK, gin.H{
			"oldest":  nil,
			"newest":  nil,
			"hasData": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"oldest":  oldest.Time.Format(time.RFC3339),
		"newest":  newest.Time.Format(time.RFC3339),
		"hasData": true,
	})
}

// GetTagStats handles GET /api/history/stats?tag_id={id}&start={iso}&end={iso}
// @Summary Get tag statistics
// @Description Get statistical summary (min, max, avg, stddev) for a tag over a time range
// @Tags history
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param tag_id query int true "Tag ID"
// @Param start query string true "Start time (ISO 8601)"
// @Param end query string true "End time (ISO 8601)"
// @Success 200 {object} TagStatsResponse
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/history/stats [get]
func (h *HistoryHandler) GetTagStats(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	tagIDStr := c.Query("tag_id")
	if tagIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag_id query parameter is required"})
		return
	}

	tagID, err := strconv.Atoi(tagIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag_id parameter"})
		return
	}

	// Verify tag ownership
	_, err = h.getTagDetails(tagID, orgID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Tag not found or access denied"})
		return
	}

	startStr := c.Query("start")
	if startStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start query parameter is required"})
		return
	}

	endStr := c.Query("end")
	if endStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end query parameter is required"})
		return
	}

	startTime, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start parameter format"})
		return
	}

	endTime, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end parameter format"})
		return
	}

	stats, err := h.getTagStats(tagID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get stats: %v", err)})
		return
	}

	if stats == nil {
		c.JSON(http.StatusOK, TagStatsResponse{})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// HistoryEvent represents a system event
type HistoryEvent struct {
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`   // connection, alert
	Source    string `json:"source"` // gateway name or id
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// QueryEvents handles GET /api/history/events?start={iso}&end={iso}
// @Summary Query historical events
// @Description Query system events (connections, etc.) from PostgreSQL - only for existing gateways
// @Tags history
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param start query string true "Start time (ISO 8601)"
// @Param end query string true "End time (ISO 8601)"
// @Success 200 {array} HistoryEvent
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/history/events [get]
func (h *HistoryHandler) QueryEvents(c *gin.Context) {
	// Get organization ID from context
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Get list of active gateway IDs for this organization (to filter out deleted gateways)
	activeGatewayIDs, err := h.getActiveGatewayIDs(orgID)
	if err != nil {
		log.Printf("[HISTORY] Failed to get active gateways: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get active gateways"})
		return
	}

	startStr := c.Query("start")
	if startStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start query parameter is required (ISO 8601 format)"})
		return
	}

	endStr := c.Query("end")
	if endStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end query parameter is required (ISO 8601 format)"})
		return
	}

	// Parse start and end times
	startTime, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start parameter format, use ISO 8601 format"})
		return
	}

	endTime, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end parameter format, use ISO 8601 format"})
		return
	}

	if endTime.Before(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end time must be after start time"})
		return
	}

	log.Printf("[HISTORY] Querying events for org=%d, start=%s, end=%s", orgID, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Query system_events table - join with gateways to get the name
	// This ensures we show current gateway names
	query := `
		SELECT
			EXTRACT(EPOCH FROM e.time)::BIGINT * 1000 as ts,
			e.status,
			e.message,
			e.gateway_id,
			COALESCE(g.name, '[deleted gateway]') as gateway_name
		FROM system_events e
		LEFT JOIN gateways g ON e.gateway_id = g.id
		WHERE e.time >= $1 AND e.time <= $2
		ORDER BY e.time ASC
	`

	rows, err := h.db.Query(query, startTime, endTime)
	if err != nil {
		log.Printf("[HISTORY] Failed to query system_events: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query events: %v", err)})
		return
	}
	defer rows.Close()

	// Parse results - filter to only include events from active gateways
	var events []HistoryEvent
	for rows.Next() {
		var ts int64
		var status, message string
		var gatewayID int
		var gatewayName string

		if err := rows.Scan(&ts, &status, &message, &gatewayID, &gatewayName); err != nil {
			log.Printf("[HISTORY] Error scanning event row: %v", err)
			continue
		}

		// Skip events from deleted gateways
		if !activeGatewayIDs[gatewayID] {
			continue
		}

		events = append(events, HistoryEvent{
			Timestamp: ts,
			Type:      "connection",
			Source:    gatewayName,
			Status:    status,
			Message:   message,
		})
	}

	if err := rows.Err(); err != nil {
		log.Printf("[HISTORY] Query error after iteration: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Query error: %v", err)})
		return
	}

	if events == nil {
		events = []HistoryEvent{}
	}

	c.JSON(http.StatusOK, events)
}

// getActiveGatewayIDs returns a map of active gateway IDs for the organization
func (h *HistoryHandler) getActiveGatewayIDs(orgID int) (map[int]bool, error) {
	query := `
		SELECT g.id
		FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE s.org_id = $1
	`

	rows, err := h.db.Query(query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[int]bool)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}

	return ids, rows.Err()
}
