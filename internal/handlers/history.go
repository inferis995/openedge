package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/influxdata/influxdb-client-go/v2/api"
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
}

// NewHistoryHandler creates a new history handler
func NewHistoryHandler(influxClient InfluxClient, influxOrg, influxBucket string) *HistoryHandler {
	return &HistoryHandler{
		influxClient: influxClient,
		influxOrg:    influxOrg,
		influxBucket: influxBucket,
	}
}

// HistoryDataPoint represents a single historical data point
type HistoryDataPoint struct {
	Timestamp int64       `json:"timestamp"`
	Value     interface{} `json:"value"`
	Quality   int         `json:"quality"`
}

// Query handles GET /api/history?tag_id={id}&start={iso}&end={iso}&agg={agg}&interval={interval}
func (h *HistoryHandler) Query(c *gin.Context) {
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
	if endTime.Before(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end time must be after start time"})
		return
	}

	// Get optional aggregation parameters
	agg := c.Query("agg")     // e.g., "mean", "max", "min", "sum", "first", "last"
	interval := c.Query("interval") // e.g., "1m", "5m", "1h", "1d"

	// Build Flux query
	query := h.buildFluxQuery(tagID, startTime, endTime, agg, interval)

	// Execute query
	queryAPI := h.influxClient.QueryAPI(h.influxOrg)
	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query InfluxDB: %v", err)})
		return
	}
	defer result.Close()

	// Parse results
	var dataPoints []HistoryDataPoint
	for result.Next() {
		record := result.Record()

		timestamp := record.Time().Unix()
		value := record.Value()
		quality := 0 // Default quality to good

		// Try to get quality from record if available
		if qVal, ok := record.ValueByKey("quality").(int64); ok {
			quality = int(qVal)
		}

		dataPoints = append(dataPoints, HistoryDataPoint{
			Timestamp: timestamp,
			Value:     value,
			Quality:   quality,
		})
	}

	// Check for query errors
	if result.Err() != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Query error: %v", result.Err())})
		return
	}

	c.JSON(http.StatusOK, dataPoints)
}

// buildFluxQuery constructs a Flux query for InfluxDB
func (h *HistoryHandler) buildFluxQuery(tagID int, start, end time.Time, agg, interval string) string {
	// Base query to fetch data for the tag
	baseQuery := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: %s, stop: %s)
			|> filter(fn: (r) => r._measurement == "tag_data")
			|> filter(fn: (r) => r.tag_id == "%d")
			|> filter(fn: (r) => r._field == "value")
	`, h.influxBucket, start.Format(time.RFC3339), end.Format(time.RFC3339), tagID)

	// Add aggregation if specified
	if agg != "" && interval != "" {
		// Map common aggregation functions to Flux window functions
		var aggFunc string
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
		default:
			aggFunc = "mean" // Default to mean if unknown
		}

		aggregationQuery := fmt.Sprintf(`
			|> aggregateWindow(every: %s, fn: %s, createEmpty: false)
			|> yield(name: "aggregated")
		`, interval, aggFunc)

		return baseQuery + aggregationQuery
	}

	// Without aggregation, just return raw data
	return baseQuery + `|> yield(name: "raw")`
}
