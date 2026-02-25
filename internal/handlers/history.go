package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/influxdata/influxdb-client-go/v2/api"
	_ "github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// InfluxClient interface for InfluxDB operations
type InfluxClient interface {
	QueryAPI(org string) api.QueryAPI
}

// HistoryHandler handles historical data query requests
type HistoryHandler struct {
	influxClient InfluxClient
	influxOrg    string
	influxBucket string
	db           *sql.DB
}

// NewHistoryHandler creates a new history handler
func NewHistoryHandler(influxClient InfluxClient, influxOrg, influxBucket string, db *sql.DB) *HistoryHandler {
	return &HistoryHandler{
		influxClient: influxClient,
		influxOrg:    influxOrg,
		influxBucket: influxBucket,
		db:           db,
	}
}

// HistoryDataPoint represents a single historical data point
type HistoryDataPoint struct {
	Timestamp int64       `json:"timestamp"`
	Value     interface{} `json:"value"`
	Quality   int         `json:"quality"`
}

// Query handles GET /api/history?tag_id={id}&start={iso}&end={iso}&agg={agg}&interval={interval}
// @Summary Query historical data
// @Description Query historical data for a tag from InfluxDB
// @Tags history
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param tag_id query int true "Tag ID"
// @Param start query string true "Start time (ISO 8601)"
// @Param end query string true "End time (ISO 8601)"
// @Param agg query string false "Aggregation function (mean, max, min, sum, first, last, count, median, stddev)"
// @Param interval query string false "Aggregation interval (e.g., 1m, 5m, 1h, 1d)"
// @Success 200 {array} HistoryDataPoint
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

	// Verify tag ownership and get organization name and data type
	orgName, dataType, err := h.getTagDetails(tagID, orgID)
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
	agg := c.Query("agg")           // e.g., "mean", "max", "min", "sum", "first", "last"
	interval := c.Query("interval") // e.g., "1m", "5m", "1h", "1d"

	log.Printf("[HISTORY] Querying tag_id=%d, org=%q, type=%s, start=%s, end=%s, agg=%s, interval=%s",
		tagID, orgName, dataType, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), agg, interval)

	queryAPI := h.influxClient.QueryAPI(h.influxOrg)

	var dataPoints []HistoryDataPoint

	if dataType == "BOOL" && agg != "" && interval != "" {
		// BOOL with aggregation: seed + quality-aware forward-fill
		var err error
		dataPoints, err = h.queryBoolWithSeed(queryAPI, tagID, startTime, endTime, agg, interval)
		if err != nil {
			log.Printf("[HISTORY] BOOL query failed for tag %d: %v", tagID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query BOOL history: %v", err)})
			return
		}
	} else if agg != "" && interval != "" {
		// Numeric types (INT, REAL, DINT) with aggregation: quality-aware query
		// BAD quality windows returned as null → visible gaps in chart
		var err error
		dataPoints, err = h.queryNumericWithQuality(queryAPI, tagID, startTime, endTime, agg, interval)
		if err != nil {
			log.Printf("[HISTORY] Numeric quality query failed for tag %d: %v", tagID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query history: %v", err)})
			return
		}
	} else {
		// Raw (no aggregation) — single Flux query, quality returned as-is
		query := h.buildFluxQuery(tagID, orgName, dataType, startTime, endTime, agg, interval)
		result, err := queryAPI.Query(context.Background(), query)
		if err != nil {
			log.Printf("[HISTORY] Failed to query InfluxDB: %v\nQuery was: %s", err, query)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query InfluxDB: %v", err), "query": query})
			return
		}
		defer result.Close()

		for result.Next() {
			record := result.Record()
			dataPoints = append(dataPoints, HistoryDataPoint{
				Timestamp: record.Time().Unix(),
				Value:     record.Value(),
				Quality:   0,
			})
		}
		if result.Err() != nil {
			log.Printf("[HISTORY] Query error after iteration: %v", result.Err())
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Query error: %v", result.Err())})
			return
		}
	}

	log.Printf("[HISTORY] Query successful: returned %d data points for tag_id=%d", len(dataPoints), tagID)

	if dataPoints == nil {
		dataPoints = []HistoryDataPoint{}
	}

	c.JSON(http.StatusOK, dataPoints)
}

// queryBoolWithSeed executes a two-phase query for BOOL tags:
//  1. Seed: last known value in the 30-day window before rangeStart (fast indexed scan)
//  2. Main: aggregateWindow over [rangeStart, rangeEnd] with createEmpty:true
//  3. Go forward-fill: propagate the seed value through null windows
//
// This avoids the massive 30-day aggregateWindow(createEmpty:true) approach
// that created tens of thousands of empty windows.
func (h *HistoryHandler) queryBoolWithSeed(queryAPI api.QueryAPI, tagID int, start, end time.Time, agg, interval string) ([]HistoryDataPoint, error) {
	startStr := start.Format(time.RFC3339)
	endStr := end.Format(time.RFC3339)
	seedStartStr := start.Add(-30 * 24 * time.Hour).Format(time.RFC3339)

	// Resolve aggregation function (BOOL never uses mean)
	aggFunc := "max"
	switch agg {
	case "max", "min", "first", "last":
		aggFunc = agg
	}

	// ── Phase 1: Seed (last known value before rangeStart) ───────────────────
	// Uses last() which is an indexed O(1)-ish operation — fast even over 30 days.
	seedQuery := fmt.Sprintf(
		`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r._measurement == "tag_data")
  |> filter(fn: (r) => r.tag_id == "%d")
  |> filter(fn: (r) => r._field == "value")
  |> last()
  |> toFloat()
  |> yield(name: "seed")`,
		h.influxBucket, seedStartStr, startStr, tagID)

	var seedValue *float64
	seedRes, err := queryAPI.Query(context.Background(), seedQuery)
	if err == nil {
		defer seedRes.Close()
		for seedRes.Next() {
			if v, ok := seedRes.Record().Value().(float64); ok {
				seedValue = &v
				log.Printf("[HISTORY] BOOL seed for tag %d = %.1f", tagID, v)
			}
		}
		// Seed errors are non-fatal — we just start without a prior value
		if seedRes.Err() != nil {
			log.Printf("[HISTORY] Seed query warning for tag %d: %v", tagID, seedRes.Err())
		}
	} else {
		log.Printf("[HISTORY] Seed query failed for tag %d (non-fatal): %v", tagID, err)
	}

	// ── Phase 2: Main aggregation over requested range only ───────────────────
	mainQuery := fmt.Sprintf(
		`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r._measurement == "tag_data")
  |> filter(fn: (r) => r.tag_id == "%d")
  |> filter(fn: (r) => r._field == "value")
  |> toFloat()
  |> aggregateWindow(every: %s, fn: %s, createEmpty: true)
  |> yield(name: "aggregated")`,
		h.influxBucket, startStr, endStr, tagID, interval, aggFunc)

	mainRes, err := queryAPI.Query(context.Background(), mainQuery)
	if err != nil {
		return nil, fmt.Errorf("BOOL main query: %w", err)
	}
	defer mainRes.Close()

	// ── Phase 2b: Quality aggregation — find BAD quality windows ─────────────
	// max(quality) > 0 in a window means at least one BAD sample → show as gap.
	badQualityWindows := make(map[int64]bool)
	qualityQuery := fmt.Sprintf(
		`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r._measurement == "tag_data")
  |> filter(fn: (r) => r.tag_id == "%d")
  |> filter(fn: (r) => r._field == "quality")
  |> toFloat()
  |> aggregateWindow(every: %s, fn: max, createEmpty: false)
  |> yield(name: "quality")`,
		h.influxBucket, startStr, endStr, tagID, interval)
	if qRes, qErr := queryAPI.Query(context.Background(), qualityQuery); qErr == nil {
		for qRes.Next() {
			if q, ok := qRes.Record().Value().(float64); ok && q > 0 {
				badQualityWindows[qRes.Record().Time().Unix()] = true
			}
		}
		qRes.Close()
	} else {
		log.Printf("[HISTORY] BOOL quality query failed for tag %d (non-fatal): %v", tagID, qErr)
	}

	// ── Phase 3: Read results + Go forward-fill (skipping BAD quality) ────────
	var points []HistoryDataPoint
	prevValue := seedValue

	for mainRes.Next() {
		record := mainRes.Record()
		ts := record.Time().Unix()

		// BAD quality window → null point (visible gap), reset forward-fill
		if badQualityWindows[ts] {
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     nil,
				Quality:   1,
			})
			prevValue = nil
			continue
		}

		raw := record.Value()
		var floatPtr *float64
		if raw != nil {
			if f, ok := raw.(float64); ok {
				floatPtr = &f
				prevValue = &f
			}
		}

		// Null window (stable GOOD) → forward-fill with last known GOOD value
		if floatPtr == nil && prevValue != nil {
			pv := *prevValue
			floatPtr = &pv
		}

		if floatPtr != nil {
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     *floatPtr,
				Quality:   0,
			})
		}
	}

	if err := mainRes.Err(); err != nil {
		return nil, err
	}

	// Prepend the seed as the very first point so the chart starts with the
	// correct state even when the first aggregation window has no data.
	if seedValue != nil {
		seedTS := start.Unix()
		if len(points) == 0 || points[0].Timestamp > seedTS {
			points = append([]HistoryDataPoint{{
				Timestamp: seedTS,
				Value:     *seedValue,
				Quality:   0,
			}}, points...)
		}
	}

	if points == nil {
		return []HistoryDataPoint{}, nil
	}
	return points, nil
}

// queryNumericWithQuality executes a quality-aware historical query for numeric tags (INT, REAL, DINT).
// Uses three sub-queries:
//  1. Seed: last known value before rangeStart (initialises forward-fill)
//  2. Value: aggregated per window with createEmpty:true (covers stable periods)
//  3. Quality: max quality per window (detects BAD-quality windows)
//
// Result per window:
//   - BAD quality  → {Value:nil, Quality:1}  → visible gap in chart
//   - Stable GOOD  → forward-filled with last known GOOD value
//   - Changed GOOD → actual aggregated value
func (h *HistoryHandler) queryNumericWithQuality(
	queryAPI api.QueryAPI,
	tagID int,
	start, end time.Time,
	agg, interval string,
) ([]HistoryDataPoint, error) {
	startStr := start.Format(time.RFC3339)
	endStr := end.Format(time.RFC3339)
	seedStartStr := start.Add(-30 * 24 * time.Hour).Format(time.RFC3339)

	aggFunc := "mean"
	switch agg {
	case "max":
		aggFunc = "max"
	case "min":
		aggFunc = "min"
	case "sum":
		aggFunc = "sum"
	case "first":
		aggFunc = "first"
	case "last":
		aggFunc = "last"
	case "count":
		aggFunc = "count"
	case "median":
		aggFunc = "median"
	case "stddev":
		aggFunc = "sd"
	}

	// ── Phase 1: Seed ────────────────────────────────────────────────────────
	seedQuery := fmt.Sprintf(
		`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r._measurement == "tag_data")
  |> filter(fn: (r) => r.tag_id == "%d")
  |> filter(fn: (r) => r._field == "value")
  |> toFloat()
  |> last()
  |> yield(name: "seed")`,
		h.influxBucket, seedStartStr, startStr, tagID)

	var seedValue *float64
	if seedRes, seedErr := queryAPI.Query(context.Background(), seedQuery); seedErr == nil {
		for seedRes.Next() {
			if v, ok := seedRes.Record().Value().(float64); ok {
				seedValue = &v
			}
		}
		seedRes.Close()
	} else {
		log.Printf("[HISTORY] Numeric seed query failed for tag %d (non-fatal): %v", tagID, seedErr)
	}

	// ── Phase 2: Aggregated values (createEmpty:true → stable windows visible) ─
	valueQuery := fmt.Sprintf(
		`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r._measurement == "tag_data")
  |> filter(fn: (r) => r.tag_id == "%d")
  |> filter(fn: (r) => r._field == "value")
  |> toFloat()
  |> aggregateWindow(every: %s, fn: %s, createEmpty: true)
  |> yield(name: "values")`,
		h.influxBucket, startStr, endStr, tagID, interval, aggFunc)

	type winState struct {
		value        *float64
		hasBadQuality bool
	}
	windows := make(map[int64]*winState)
	var orderedTS []int64

	vRes, err := queryAPI.Query(context.Background(), valueQuery)
	if err != nil {
		return nil, fmt.Errorf("numeric value query: %w", err)
	}
	defer vRes.Close()

	for vRes.Next() {
		record := vRes.Record()
		ts := record.Time().Unix()
		ws := &winState{}
		if record.Value() != nil {
			if v, ok := record.Value().(float64); ok {
				ws.value = &v
			}
		}
		if _, exists := windows[ts]; !exists {
			windows[ts] = ws
			orderedTS = append(orderedTS, ts)
		}
	}
	if err := vRes.Err(); err != nil {
		return nil, fmt.Errorf("numeric value result error: %w", err)
	}

	// ── Phase 3: Max quality per window (createEmpty:false — only where data exists) ─
	qualityQuery := fmt.Sprintf(
		`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r._measurement == "tag_data")
  |> filter(fn: (r) => r.tag_id == "%d")
  |> filter(fn: (r) => r._field == "quality")
  |> toFloat()
  |> aggregateWindow(every: %s, fn: max, createEmpty: false)
  |> yield(name: "quality")`,
		h.influxBucket, startStr, endStr, tagID, interval)

	if qRes, qErr := queryAPI.Query(context.Background(), qualityQuery); qErr == nil {
		for qRes.Next() {
			record := qRes.Record()
			ts := record.Time().Unix()
			if record.Value() != nil {
				if q, ok := record.Value().(float64); ok && q > 0 {
					if ws, exists := windows[ts]; exists {
						ws.hasBadQuality = true
					}
				}
			}
		}
		qRes.Close()
	} else {
		log.Printf("[HISTORY] Numeric quality query failed for tag %d (non-fatal): %v", tagID, qErr)
	}

	// ── Phase 4: Sort and build result ───────────────────────────────────────
	sort.Slice(orderedTS, func(i, j int) bool { return orderedTS[i] < orderedTS[j] })

	var points []HistoryDataPoint
	lastGoodValue := seedValue

	for _, ts := range orderedTS {
		ws := windows[ts]
		switch {
		case ws.hasBadQuality:
			// BAD quality → null point (visible gap), reset forward-fill
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     nil,
				Quality:   1,
			})
			lastGoodValue = nil

		case ws.value != nil:
			// GOOD quality with actual data
			lastGoodValue = ws.value
			points = append(points, HistoryDataPoint{
				Timestamp: ts,
				Value:     *ws.value,
				Quality:   0,
			})

		default:
			// Empty window (stable GOOD) → forward-fill
			if lastGoodValue != nil {
				points = append(points, HistoryDataPoint{
					Timestamp: ts,
					Value:     *lastGoodValue,
					Quality:   0,
				})
			}
		}
	}

	// Prepend seed as first point so chart starts at the correct value
	if seedValue != nil {
		seedTS := start.Unix()
		if len(points) == 0 || points[0].Timestamp > seedTS {
			points = append([]HistoryDataPoint{{
				Timestamp: seedTS,
				Value:     *seedValue,
				Quality:   0,
			}}, points...)
		}
	}

	if points == nil {
		return []HistoryDataPoint{}, nil
	}
	return points, nil
}

// buildFluxQuery constructs a Flux query for InfluxDB for numeric types (INT, REAL, DINT)
// and raw (no-aggregation) queries.
//
// BOOL tags with aggregation are handled separately by queryBoolWithSeed() which uses
// a two-phase approach (fast last() seed + range aggregation + Go forward-fill).
func (h *HistoryHandler) buildFluxQuery(tagID int, orgName, dataType string, start, end time.Time, agg, interval string) string {
	startStr := start.Format(time.RFC3339)
	endStr := end.Format(time.RFC3339)

	tagFilters := fmt.Sprintf(
		`  |> filter(fn: (r) => r._measurement == "tag_data")
  |> filter(fn: (r) => r.tag_id == "%d")
  |> filter(fn: (r) => r._field == "value")
  |> toFloat()`,
		tagID)

	if agg != "" && interval != "" {
		aggFunc := "mean"
		switch agg {
		case "mean":
			aggFunc = "mean"
		case "max":
			aggFunc = "max"
		case "min":
			aggFunc = "min"
		case "sum":
			aggFunc = "sum"
		case "first":
			aggFunc = "first"
		case "last":
			aggFunc = "last"
		case "count":
			aggFunc = "count"
		case "median":
			aggFunc = "median"
		case "stddev":
			aggFunc = "sd"
		}

		// Numeric: sparse aggregation windows, no fill — frontend interpolates gaps
		return fmt.Sprintf(
			`from(bucket: "%s")
  |> range(start: %s, stop: %s)
%s
  |> aggregateWindow(every: %s, fn: %s, createEmpty: false)
  |> yield(name: "aggregated")
`,
			h.influxBucket, startStr, endStr, tagFilters, interval, aggFunc)
	}

	// Raw (no aggregation): all data points within the requested range
	return fmt.Sprintf(
		`from(bucket: "%s")
  |> range(start: %s, stop: %s)
%s
  |> yield(name: "raw")
`,
		h.influxBucket, startStr, endStr, tagFilters)
}

// getTagDetails retrieves the organization name and data type for a tag and verifies ownership
func (h *HistoryHandler) getTagDetails(tagID, orgID int) (string, string, error) {
	var orgName string
	var dataType string

	query := `
		SELECT o.name, t.data_type
		FROM tags t
		JOIN gateways g ON t.gateway_id = g.id
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE t.id = $1 AND s.org_id = $2
	`

	err := h.db.QueryRow(query, tagID, orgID).Scan(&orgName, &dataType)
	if err != nil {
		return "", "", fmt.Errorf("failed to verify tag ownership: %w", err)
	}

	return orgName, dataType, nil
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
// @Description Query system events (connections, etc.) from InfluxDB - only for existing gateways
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

	// Resolve Organization Name for filtering
	orgName, err := h.getOrganizationName(orgID)
	if err != nil {
		log.Printf("[HISTORY] Failed to resolve organization name for ID %d: %v", orgID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve organization context"})
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

	// Build Flux query for events
	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: %s, stop: %s)
			|> filter(fn: (r) => r._measurement == "system_events")
			|> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
	`, h.influxBucket, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	log.Printf("[HISTORY] Querying events for org=%q, start=%s, end=%s", orgName, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Execute query
	queryAPI := h.influxClient.QueryAPI(h.influxOrg)
	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		log.Printf("[HISTORY] Failed to query InfluxDB for events: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query events: %v", err)})
		return
	}
	defer result.Close()

	// Parse results - filter to only include events from active gateways
	var events []HistoryEvent
	for result.Next() {
		record := result.Record()

		// Get gateway_id from the record
		gatewayIDStr, _ := record.ValueByKey("gateway_id").(string)

		// Skip events from deleted gateways (gateway_id must be in active list)
		if gatewayIDStr != "" {
			gatewayID, convErr := strconv.Atoi(gatewayIDStr)
			if convErr == nil {
				// Check if this gateway still exists
				if !activeGatewayIDs[gatewayID] {
					continue // Skip events from deleted gateways
				}
			}
		}

		timestamp := record.Time().Unix()

		// Pivot puts fields as columns in the record values
		// Access them safely
		status, _ := record.ValueByKey("status").(string)
		message, _ := record.ValueByKey("message").(string)

		// Tags are also available
		typeTag, _ := record.ValueByKey("type").(string)
		source, _ := record.ValueByKey("gateway").(string) // Use gateway name as source
		if source == "" {
			source, _ = record.ValueByKey("gateway_id").(string)
		}

		events = append(events, HistoryEvent{
			Timestamp: timestamp,
			Type:      typeTag,
			Source:    source,
			Status:    status,
			Message:   message,
		})
	}

	if result.Err() != nil {
		log.Printf("[HISTORY] Query error after iteration: %v", result.Err())
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Query error: %v", result.Err())})
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

func (h *HistoryHandler) getOrganizationName(orgID int) (string, error) {
	var name string
	err := h.db.QueryRow("SELECT name FROM organizations WHERE id = $1", orgID).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}
