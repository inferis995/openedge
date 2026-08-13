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
		// Also drop rollup retention so nothing is purged when retention is off.
		h.db.Exec(`SELECT remove_retention_policy('tag_history_1m', if_exists => true)`)
		h.db.Exec(`SELECT remove_retention_policy('tag_history_1h', if_exists => true)`)
		return
	}

	// Validate retentionDays is within reasonable bounds (1 day to 10 years)
	if retentionDays < 1 || retentionDays > 3650 {
		log.Printf("[TIMESCALEDB] WARNING: Invalid retention days value: %d (valid range: 1-3650). Using default: 30 days", retentionDays)
		retentionDays = 30
	}

	// Remove existing policies first to avoid duplicate errors, then add the new ones
	h.db.Exec(`SELECT remove_retention_policy('tag_history', if_exists => true)`)
	h.db.Exec(`SELECT remove_retention_policy('system_events', if_exists => true)`)

	log.Printf("[TIMESCALEDB] Configuring data retention policy: keeping %d days of history", retentionDays)

	_, err1 := h.db.Exec(`SELECT add_retention_policy('tag_history', make_interval(days => $1::int), if_not_exists => true)`, retentionDays)
	if err1 != nil {
		log.Printf("[TIMESCALEDB] Error setting retention policy for tag_history: %v", err1)
	}

	_, err2 := h.db.Exec(`SELECT add_retention_policy('system_events', make_interval(days => $1::int), if_not_exists => true)`, retentionDays)
	if err2 != nil {
		log.Printf("[TIMESCALEDB] Error setting retention policy for system_events: %v", err2)
	}

	// Rollup retention: keep aggregates LONGER than raw data so long-range
	// trend charts stay complete after raw rows are purged. The 1-minute
	// rollup is the main disk consumer (~1440 rows/tag/day), so we cap it;
	// the 1-hour rollup is kept much longer; the 1-day rollup is tiny
	// (~1 row/tag/day) and intentionally left unbounded.
	rollups := []struct {
		view string
		days int
	}{
		{"tag_history_1m", retentionDays * 3},
		{"tag_history_1h", retentionDays * 12},
	}
	for _, r := range rollups {
		h.db.Exec(`SELECT remove_retention_policy($1, if_exists => true)`, r.view)
		if _, err := h.db.Exec(
			`SELECT add_retention_policy($1, make_interval(days => $2::int), if_not_exists => true)`,
			r.view, r.days,
		); err != nil {
			// Best-effort: the continuous aggregate may not exist yet on a
			// fresh database; it will be picked up on the next call.
			log.Printf("[TIMESCALEDB] Note: could not set retention for %s (may not exist yet): %v", r.view, err)
		}
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
	// Check if user is global admin
	isGlobalAdmin := middleware.IsGlobalAdmin(c)

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
	var dataType string
	if isGlobalAdmin {
		// Global admin can access any tag, no org filter needed
		dataType, err = h.getTagDetailsNoOrg(tagID)
		if err != nil {
			log.Printf("[HISTORY] Tag %d not found: %v", tagID, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found", "detail": err.Error()})
			return
		}
	} else {
		// Regular user: verify tag belongs to their organization
		orgID, ok := middleware.GetOrganizationID(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
			return
		}

		dataType, err = h.getTagDetails(tagID, orgID)
		if err != nil {
			// Tag not found or doesn't belong to this organization
			log.Printf("[HISTORY] Tag %d not found or access denied for org %d: %v", tagID, orgID, err)
			c.JSON(http.StatusForbidden, gin.H{"error": "Tag not found or access denied", "detail": err.Error()})
			return
		}
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

	// Whitelist agg parameter — only these values are accepted.
	// aggFunc is later interpolated into SQL via fmt.Sprintf; any unlisted
	// value must be rejected here before it reaches the query builders.
	validAgg := map[string]string{
		"max":  "max",
		"min":  "min",
		"last": "last",
		"mean": "avg",
		"avg":  "avg",
		"":     "avg",
	}
	aggFunc, ok := validAgg[agg]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agg parameter: allowed values are avg, mean, min, max, last"})
		return
	}

	// Determine which aggregate level to use
	aggLevel := determineAggregationLevel(startTime, endTime)
	if forceRaw {
		aggLevel.Source = "raw"
	}

	log.Printf("[HISTORY] Querying tag_id=%d, type=%s, start=%s, end=%s, agg=%s, interval=%s, source=%s",
		tagID, dataType, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), agg, interval, aggLevel.Source)

	autoIntvl := interval == ""

	dataPoints, source, err := h.seriesForTag(tagID, startTime, endTime, aggFunc, interval, aggLevel.Source)
	if err != nil {
		log.Printf("[HISTORY] Query failed for tag %d: %v", tagID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query history"})
		return
	}

	log.Printf("[HISTORY] Query successful: returned %d data points for tag_id=%d from source=%s", len(dataPoints), tagID, source)

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

// seriesForTag is one tag's series, from picking the aggregate level through to
// the seed point. Both the single-tag query and the batch query go through it.
//
// It exists because the trend chart draws several tags on one axis: if the
// batch path built its series even slightly differently — a different
// aggregate, no seed — the same tag would render one way alone and another way
// beside its neighbors, and the difference would look like the plant.
func (h *HistoryHandler) seriesForTag(
	tagID int, start, end time.Time, aggFunc, interval, level string,
) ([]HistoryDataPoint, string, error) {
	var points []HistoryDataPoint
	var err error
	source := level

	switch level {
	case "1m":
		points, err = h.query1mAggregate(tagID, start, end, aggFunc)
	case "1h":
		points, err = h.query1hAggregate(tagID, start, end, aggFunc)
	case "1d":
		points, err = h.query1dAggregate(tagID, start, end, aggFunc)
	default:
		// Raw query with optional downsampling
		if interval != "" {
			points, err = h.queryRawWithInterval(tagID, start, end, aggFunc, interval)
		} else {
			points, err = h.queryRaw(tagID, start, end)
		}
		source = "raw"
	}
	if err != nil {
		return nil, source, err
	}
	if points == nil {
		points = []HistoryDataPoint{}
	}

	// ── SEED pattern ──────────────────────────────────────────────────────
	// For on-change (RBE) drivers like Sparkplug B, Modbus, OPC-UA, etc.
	// the tag may not publish during the query range if its value hasn't
	// changed. Prepend the last known GOOD value before range start so the
	// chart always has an initial state.
	startMs := start.UnixMilli()
	if len(points) == 0 || (points[0].Value != nil && points[0].Timestamp > startMs+500) {
		if seed, seedErr := h.getSeedValue(tagID, start); seedErr == nil && seed != nil {
			points = append([]HistoryDataPoint{*seed}, points...)
			log.Printf("[HISTORY] SEED injected for tag_id=%d at ts=%d", tagID, seed.Timestamp)
		}
	}
	return points, source, nil
}

// BatchQueryRequest is what the trend chart asks for: several tags over one
// window, so the series arrive together and share an axis.
type BatchQueryRequest struct {
	TagIDs   []int  `json:"tag_ids"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Agg      string `json:"agg"`
	Interval string `json:"interval"`
}

// maxBatchTags bounds one request. The chart draws a handful of pens; a caller
// asking for thousands is either confused or probing, and either way it should
// not turn into thousands of queries.
const maxBatchTags = 50

// BatchQuery answers POST /api/history/batch.
//
// The trend page has always called this endpoint and it has never existed:
// selecting a tag to chart produced a 404 and an empty graph. Nothing failed
// loudly, because a chart with no data looks like a plant with no data.
//
// Ownership is checked per tag, not once for the request. A batch is not a
// reason to trust a list of ids: one id from another tenant among twenty of
// your own must be refused like any other.
func (h *HistoryHandler) BatchQuery(c *gin.Context) {
	var req BatchQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.TagIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag_ids is required"})
		return
	}
	if len(req.TagIDs) > maxBatchTags {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("too many tags: %d requested, %d is the maximum", len(req.TagIDs), maxBatchTags)})
		return
	}

	startTime, err := time.Parse(time.RFC3339, req.Start)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start, use ISO 8601 (e.g. 2024-01-01T00:00:00Z)"})
		return
	}
	endTime, err := time.Parse(time.RFC3339, req.End)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end, use ISO 8601 (e.g. 2024-01-01T23:59:59Z)"})
		return
	}
	if !endTime.After(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end time must be after start time"})
		return
	}

	// The same whitelist as the single-tag query: aggFunc reaches SQL through
	// fmt.Sprintf, so anything unlisted has to be refused here.
	validAgg := map[string]string{
		"max": "max", "min": "min", "last": "last",
		"mean": "avg", "avg": "avg", "": "avg",
	}
	aggFunc, ok := validAgg[req.Agg]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agg parameter: allowed values are avg, mean, min, max, last"})
		return
	}

	isGlobalAdmin := middleware.IsGlobalAdmin(c)
	orgID, hasOrg := middleware.GetOrganizationID(c)
	if !isGlobalAdmin && !hasOrg {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	level := determineAggregationLevel(startTime, endTime).Source

	// Keyed by tag id, which is what the chart indexes by. Always an object,
	// never null: the frontend reads it directly.
	out := make(map[string][]HistoryDataPoint, len(req.TagIDs))
	for _, tagID := range req.TagIDs {
		if isGlobalAdmin {
			if _, err := h.getTagDetailsNoOrg(tagID); err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("tag %d not found", tagID)})
				return
			}
		} else if _, err := h.getTagDetails(tagID, orgID); err != nil {
			log.Printf("[HISTORY] batch: tag %d not found or denied for org %d: %v", tagID, orgID, err)
			c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("tag %d not found or access denied", tagID)})
			return
		}

		points, _, err := h.seriesForTag(tagID, startTime, endTime, aggFunc, req.Interval, level)
		if err != nil {
			log.Printf("[HISTORY] batch: query failed for tag %d: %v", tagID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query history"})
			return
		}
		out[strconv.Itoa(tagID)] = points
	}

	c.JSON(http.StatusOK, out)
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

	// Truncate start to minute boundary so generate_series aligns with 1m buckets
	alignedStart := start.Truncate(time.Minute)

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

	rows, err := h.db.Query(query, tagID, alignedStart, end)
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

	// Truncate start to hour boundary so generate_series aligns with 1h buckets
	alignedStart := start.Truncate(time.Hour)

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

	rows, err := h.db.Query(query, tagID, alignedStart, end)
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

	// Truncate start to day boundary so generate_series aligns with 1d buckets
	alignedStart := start.Truncate(24 * time.Hour)

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

	rows, err := h.db.Query(query, tagID, alignedStart, end)
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
// For RBE (Report by Exception) drivers, if no new value was published within
// the query range, it means the value hasn't changed - the seed represents the
// actual current state and should be displayed as GOOD quality.
//
// Offline states are tracked separately via:
//   - Gateway health events (markTagOffline in historian)
//   - NULL value markers in tag_history with source='offline'
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
	// Quality is GOOD because this represents the actual tag state.
	// For RBE data, if value didn't change, it should display normally.
	// Only PLC disconnection should show N/A (handled by offline markers).
	startMs := start.UnixMilli()
	return &HistoryDataPoint{
		Timestamp:   startMs,
		Value:       &value,
		Quality:     QualityGood, // GOOD - valid tag state for RBE
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

// getTagDetailsNoOrg retrieves the data type for a tag without organization check
// Used by global admin who can access all tags
func (h *HistoryHandler) getTagDetailsNoOrg(tagID int) (string, error) {
	var dataType string

	query := `SELECT data_type FROM tags WHERE id = $1`

	err := h.db.QueryRow(query, tagID).Scan(&dataType)
	if err != nil {
		return "", fmt.Errorf("failed to get tag details: %w", err)
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
	// Check if user is global admin
	isGlobalAdmin := middleware.IsGlobalAdmin(c)

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

	// Verify tag ownership (global admin can access any tag)
	if !isGlobalAdmin {
		orgID, ok := middleware.GetOrganizationID(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
			return
		}

		_, err = h.getTagDetails(tagID, orgID)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "Tag not found or access denied"})
			return
		}
	} else {
		// Global admin: just verify tag exists
		_, err = h.getTagDetailsNoOrg(tagID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
			return
		}
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
		log.Printf("[HISTORY] Failed to get stats for tag %d: %v", tagID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get stats"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query events"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query error"})
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
