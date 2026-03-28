package handlers

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// AIopsHandler serves read-only AI-Ops endpoints consumed by Paperclip agents.
// All endpoints are safe: no INSERT/UPDATE/DELETE, all queries org-scoped,
// all inputs validated, all nullable columns scanned into pointers.
type AIopsHandler struct {
	db *sql.DB
}

func NewAIopsHandler(db *sql.DB) *AIopsHandler {
	return &AIopsHandler{db: db}
}

// --- Response types ---

type aiopsTagSummary struct {
	TagID       int      `json:"tag_id"`
	Alias       string   `json:"alias"`
	DataType    string   `json:"data_type"`
	GatewayName string   `json:"gateway_name"`
	SiteName    string   `json:"site_name"`
	AvgValue    *float64 `json:"avg_value"`
	MinValue    *float64 `json:"min_value"`
	MaxValue    *float64 `json:"max_value"`
	SampleCount int64    `json:"sample_count"`
	HasAlarm    bool     `json:"has_alarm"`
	AlarmCount  int64    `json:"alarm_count"`
}

type aiopsOrgSummary struct {
	OrgID               int               `json:"org_id"`
	OrgName             string            `json:"org_name"`
	PeriodHours         int               `json:"period_hours"`
	GeneratedAt         string            `json:"generated_at"`
	ActiveAlarmsCount   int64             `json:"active_alarms_count"`
	CriticalAlarmsCount int64             `json:"critical_alarms_count"`
	TotalGatewaysCount  int64             `json:"total_gateways_count"`
	Tags                []aiopsTagSummary `json:"tags"`
}

type aiopsAnomaly struct {
	Bucket    string  `json:"bucket"`
	Value     float64 `json:"value"`
	ZScore    float64 `json:"z_score"`
	Direction string  `json:"direction"`
}

type aiopsAnomaliesResponse struct {
	TagID          int            `json:"tag_id"`
	TagAlias       string         `json:"tag_alias"`
	BaselineMean   float64        `json:"baseline_mean"`
	BaselineStdDev float64        `json:"baseline_std_dev"`
	AnomalyCount   int            `json:"anomaly_count"`
	Anomalies      []aiopsAnomaly `json:"anomalies"`
	Note           string         `json:"note,omitempty"`
}

type aiopsAlarmItem struct {
	Severity        string   `json:"severity"`
	TagID           int      `json:"tag_id"`
	TagAlias        string   `json:"tag_alias"`
	AlarmType       string   `json:"alarm_type"`
	Message         *string  `json:"message"`
	ValueAtTrigger  *float64 `json:"value_at_trigger"`
	TriggerTime     string   `json:"trigger_time"`
	Status          string   `json:"status"`
	ClearTime       *string  `json:"clear_time"`
}

type aiopsAlarmDigest struct {
	PeriodHours int                `json:"period_hours"`
	TotalFired  int                `json:"total_fired"`
	StillActive int                `json:"still_active"`
	Cleared     int                `json:"cleared"`
	BySeverity  map[string]int     `json:"by_severity"`
	Alarms      []aiopsAlarmItem   `json:"alarms"`
}

// --- Helper: parse and clamp int query param ---

func parseIntParam(c *gin.Context, key string, defaultVal, minVal, maxVal int) int {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

// --- Handler 1: GET /api/aiops/summary?hours=24 ---
//
// Returns a full org snapshot: all tags with 1h aggregate stats for the period,
// active alarm counts, and gateway count. Consumed by Daily Report and Plant Director agents.

func (h *AIopsHandler) GetOrgSummary(c *gin.Context) {
	hours := parseIntParam(c, "hours", 24, 1, 720)
	orgFilter := middleware.GetOrgFilterForQuery(c)

	endTime := time.Now()
	startTime := endTime.Add(-time.Duration(hours) * time.Hour)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// --- 1. Org name ---
	orgID := 0
	orgName := "All Organizations"
	if orgFilter != nil {
		orgID = *orgFilter
		err := h.db.QueryRowContext(ctx,
			`SELECT id, name FROM organizations WHERE id = $1`, orgID,
		).Scan(&orgID, &orgName)
		if err != nil && err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load organization"})
			return
		}
	}

	// --- 2. Tags with 1h aggregate stats (single batch query, no N+1) ---
	var tagRows *sql.Rows
	var err error

	if orgFilter == nil {
		// Global admin: no org filter
		tagRows, err = h.db.QueryContext(ctx, `
			SELECT
				t.id,
				t.alias,
				t.data_type,
				g.name   AS gateway_name,
				s.name   AS site_name,
				AVG(h.avg_value)              AS avg_v,
				MIN(h.min_value)              AS min_v,
				MAX(h.max_value)              AS max_v,
				COALESCE(SUM(h.sample_count), 0) AS cnt,
				s.org_id
			FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas    a ON g.area_id = a.id
			JOIN sites    s ON a.site_id  = s.id
			LEFT JOIN tag_history_1h h
				ON h.tag_id = t.id
				AND h.bucket >= $1
				AND h.bucket <  $2
			GROUP BY t.id, t.alias, t.data_type, g.name, s.name, s.org_id
			ORDER BY t.id
		`, startTime, endTime)
	} else {
		tagRows, err = h.db.QueryContext(ctx, `
			SELECT
				t.id,
				t.alias,
				t.data_type,
				g.name   AS gateway_name,
				s.name   AS site_name,
				AVG(h.avg_value)              AS avg_v,
				MIN(h.min_value)              AS min_v,
				MAX(h.max_value)              AS max_v,
				COALESCE(SUM(h.sample_count), 0) AS cnt,
				s.org_id
			FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas    a ON g.area_id = a.id
			JOIN sites    s ON a.site_id  = s.id
			LEFT JOIN tag_history_1h h
				ON h.tag_id = t.id
				AND h.bucket >= $2
				AND h.bucket <  $3
			WHERE s.org_id = $1
			GROUP BY t.id, t.alias, t.data_type, g.name, s.name, s.org_id
			ORDER BY t.id
		`, orgID, startTime, endTime)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query tags"})
		return
	}
	defer tagRows.Close()

	tags := make([]aiopsTagSummary, 0)
	tagIDs := make([]int, 0)

	for tagRows.Next() {
		var ts aiopsTagSummary
		var scanOrgID int
		if err := tagRows.Scan(
			&ts.TagID, &ts.Alias, &ts.DataType,
			&ts.GatewayName, &ts.SiteName,
			&ts.AvgValue, &ts.MinValue, &ts.MaxValue, &ts.SampleCount,
			&scanOrgID,
		); err != nil {
			continue
		}
		tags = append(tags, ts)
		tagIDs = append(tagIDs, ts.TagID)
	}
	if err := tagRows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error reading tags"})
		return
	}

	// --- 3. Alarm counts per tag for the period ---
	alarmCountMap := make(map[int]int64)
	if len(tagIDs) > 0 {
		var alarmRows *sql.Rows
		if orgFilter == nil {
			alarmRows, err = h.db.QueryContext(ctx, `
				SELECT ae.tag_id, COUNT(*)
				FROM alarm_events ae
				WHERE ae.trigger_time >= $1 AND ae.trigger_time < $2
				GROUP BY ae.tag_id
			`, startTime, endTime)
		} else {
			alarmRows, err = h.db.QueryContext(ctx, `
				SELECT ae.tag_id, COUNT(*)
				FROM alarm_events ae
				JOIN tags      t ON ae.tag_id   = t.id
				JOIN gateways  g ON t.gateway_id = g.id
				JOIN areas     a ON g.area_id    = a.id
				JOIN sites     s ON a.site_id    = s.id
				WHERE s.org_id = $1
				  AND ae.trigger_time >= $2
				  AND ae.trigger_time <  $3
				GROUP BY ae.tag_id
			`, orgID, startTime, endTime)
		}
		if err == nil {
			defer alarmRows.Close()
			for alarmRows.Next() {
				var tid int
				var cnt int64
				if err := alarmRows.Scan(&tid, &cnt); err == nil {
					alarmCountMap[tid] = cnt
				}
			}
		}
	}

	// Merge alarm counts into tags
	for i := range tags {
		cnt := alarmCountMap[tags[i].TagID]
		tags[i].AlarmCount = cnt
		tags[i].HasAlarm = cnt > 0
	}

	// --- 4. Active alarms total + critical ---
	var activeTotal, criticalTotal int64
	if orgFilter == nil {
		h.db.QueryRowContext(ctx, `
			SELECT
				COALESCE(COUNT(*), 0),
				COALESCE(SUM(CASE WHEN ae.severity = 'critical' THEN 1 ELSE 0 END), 0)
			FROM alarm_events ae
			WHERE ae.status IN ('ACTIVE', 'ACKNOWLEDGED')
		`).Scan(&activeTotal, &criticalTotal)
	} else {
		h.db.QueryRowContext(ctx, `
			SELECT
				COALESCE(COUNT(*), 0),
				COALESCE(SUM(CASE WHEN ae.severity = 'critical' THEN 1 ELSE 0 END), 0)
			FROM alarm_events ae
			JOIN tags      t ON ae.tag_id   = t.id
			JOIN gateways  g ON t.gateway_id = g.id
			JOIN areas     a ON g.area_id    = a.id
			JOIN sites     s ON a.site_id    = s.id
			WHERE s.org_id = $1
			  AND ae.status IN ('ACTIVE', 'ACKNOWLEDGED')
		`, orgID).Scan(&activeTotal, &criticalTotal)
	}

	// --- 5. Gateway count ---
	var gatewayCount int64
	if orgFilter == nil {
		h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gateways`).Scan(&gatewayCount)
	} else {
		h.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM gateways g
			JOIN areas a ON g.area_id = a.id
			JOIN sites s ON a.site_id = s.id
			WHERE s.org_id = $1
		`, orgID).Scan(&gatewayCount)
	}

	c.JSON(http.StatusOK, aiopsOrgSummary{
		OrgID:               orgID,
		OrgName:             orgName,
		PeriodHours:         hours,
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
		ActiveAlarmsCount:   activeTotal,
		CriticalAlarmsCount: criticalTotal,
		TotalGatewaysCount:  gatewayCount,
		Tags:                tags,
	})
}

// --- Handler 2: GET /api/aiops/anomalies?tag_id=5&window_hours=168&baseline_days=30 ---
//
// Z-score anomaly detection on tag_history_1h.
// Baseline: mean and stddev over baseline_days (excluding the current window).
// Window: hourly buckets over window_hours.
// Threshold: |z_score| >= 2.5

func (h *AIopsHandler) GetTagAnomalies(c *gin.Context) {
	// --- Validate required tag_id ---
	tagIDStr := c.Query("tag_id")
	if tagIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag_id is required"})
		return
	}
	tagID, err := strconv.Atoi(tagIDStr)
	if err != nil || tagID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag_id must be a positive integer"})
		return
	}

	windowHours   := parseIntParam(c, "window_hours",   168, 1,  720)
	baselineDays  := parseIntParam(c, "baseline_days",   30, 7,  365)

	orgFilter := middleware.GetOrgFilterForQuery(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// --- Ownership check (skip for global admin) ---
	tagAlias := ""
	if orgFilter != nil {
		var count int
		err := h.db.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(MAX(t.alias), '')
			FROM tags t
			JOIN gateways g ON t.gateway_id = g.id
			JOIN areas    a ON g.area_id    = a.id
			JOIN sites    s ON a.site_id    = s.id
			WHERE t.id = $1 AND s.org_id = $2
		`, tagID, *orgFilter).Scan(&count, &tagAlias)
		if err != nil || count == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "tag not found or access denied"})
			return
		}
	} else {
		// Global admin: just fetch alias
		h.db.QueryRowContext(ctx, `SELECT COALESCE(alias, '') FROM tags WHERE id = $1`, tagID).Scan(&tagAlias)
	}

	// --- Baseline stats (excludes the current window) ---
	var baselineMeanPtr  *float64
	var baselineStdDevPtr *float64

	baselineDaysStr  := strconv.Itoa(baselineDays)
	windowHoursStr   := strconv.Itoa(windowHours)

	h.db.QueryRowContext(ctx, `
		SELECT
			AVG(avg_value),
			STDDEV(avg_value)
		FROM tag_history_1h
		WHERE tag_id = $1
		  AND bucket >= NOW() - ($2 || ' days')::INTERVAL
		  AND bucket <  NOW() - ($3 || ' hours')::INTERVAL
		  AND avg_value IS NOT NULL
	`, tagID, baselineDaysStr, windowHoursStr).Scan(&baselineMeanPtr, &baselineStdDevPtr)

	// Safe defaults for nil
	baselineMean := 0.0
	if baselineMeanPtr != nil {
		baselineMean = *baselineMeanPtr
	}

	// --- Guard: insufficient baseline ---
	anomalies := make([]aiopsAnomaly, 0)
	if baselineStdDevPtr == nil || *baselineStdDevPtr < 0.0001 {
		note := "baseline stddev too low or no baseline data — anomaly detection not applicable"
		c.JSON(http.StatusOK, aiopsAnomaliesResponse{
			TagID:          tagID,
			TagAlias:       tagAlias,
			BaselineMean:   baselineMean,
			BaselineStdDev: 0,
			AnomalyCount:   0,
			Anomalies:      anomalies,
			Note:           note,
		})
		return
	}
	baselineStdDev := *baselineStdDevPtr

	// --- Window data ---
	windowRows, err := h.db.QueryContext(ctx, `
		SELECT bucket, avg_value
		FROM tag_history_1h
		WHERE tag_id = $1
		  AND bucket >= NOW() - ($2 || ' hours')::INTERVAL
		  AND avg_value IS NOT NULL
		ORDER BY bucket ASC
	`, tagID, windowHoursStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query window data"})
		return
	}
	defer windowRows.Close()

	// --- Z-score calculation ---
	for windowRows.Next() {
		var bucket time.Time
		var value float64
		if err := windowRows.Scan(&bucket, &value); err != nil {
			continue
		}
		zScore := (value - baselineMean) / baselineStdDev
		if math.Abs(zScore) >= 2.5 {
			direction := "high"
			if zScore < 0 {
				direction = "low"
			}
			anomalies = append(anomalies, aiopsAnomaly{
				Bucket:    bucket.UTC().Format(time.RFC3339),
				Value:     math.Round(value*10000) / 10000,
				ZScore:    math.Round(zScore*100) / 100,
				Direction: direction,
			})
		}
	}
	if err := windowRows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error reading window data"})
		return
	}

	c.JSON(http.StatusOK, aiopsAnomaliesResponse{
		TagID:          tagID,
		TagAlias:       tagAlias,
		BaselineMean:   math.Round(baselineMean*10000) / 10000,
		BaselineStdDev: math.Round(baselineStdDev*10000) / 10000,
		AnomalyCount:   len(anomalies),
		Anomalies:      anomalies,
	})
}

// --- Handler 3: GET /api/aiops/alarms/digest?hours=24 ---
//
// Returns a full alarm digest for the period: totals, by-severity breakdown, and detail list.
// Consumed by Daily Report agent to generate PDF reports.

func (h *AIopsHandler) GetAlarmDigest(c *gin.Context) {
	hours := parseIntParam(c, "hours", 24, 1, 720)
	orgFilter := middleware.GetOrgFilterForQuery(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	hoursStr := strconv.Itoa(hours)

	var rows *sql.Rows
	var err error

	if orgFilter == nil {
		rows, err = h.db.QueryContext(ctx, `
			SELECT
				ae.severity,
				ae.tag_id,
				t.alias          AS tag_alias,
				ae.alarm_type,
				ae.message,
				ae.value_at_trigger,
				ae.trigger_time,
				ae.status,
				ae.clear_time
			FROM alarm_events ae
			JOIN tags t ON ae.tag_id = t.id
			WHERE ae.trigger_time >= NOW() - ($1 || ' hours')::INTERVAL
			ORDER BY ae.trigger_time DESC
			LIMIT 1000
		`, hoursStr)
	} else {
		rows, err = h.db.QueryContext(ctx, `
			SELECT
				ae.severity,
				ae.tag_id,
				t.alias          AS tag_alias,
				ae.alarm_type,
				ae.message,
				ae.value_at_trigger,
				ae.trigger_time,
				ae.status,
				ae.clear_time
			FROM alarm_events ae
			JOIN tags      t ON ae.tag_id   = t.id
			JOIN gateways  g ON t.gateway_id = g.id
			JOIN areas     a ON g.area_id    = a.id
			JOIN sites     s ON a.site_id    = s.id
			WHERE s.org_id = $1
			  AND ae.trigger_time >= NOW() - ($2 || ' hours')::INTERVAL
			ORDER BY ae.trigger_time DESC
			LIMIT 1000
		`, *orgFilter, hoursStr)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query alarm events"})
		return
	}
	defer rows.Close()

	alarms := make([]aiopsAlarmItem, 0)
	for rows.Next() {
		var item aiopsAlarmItem
		var triggerTime time.Time
		var clearTime *time.Time

		if err := rows.Scan(
			&item.Severity,
			&item.TagID,
			&item.TagAlias,
			&item.AlarmType,
			&item.Message,
			&item.ValueAtTrigger,
			&triggerTime,
			&item.Status,
			&clearTime,
		); err != nil {
			continue
		}
		item.TriggerTime = triggerTime.UTC().Format(time.RFC3339)
		if clearTime != nil {
			s := clearTime.UTC().Format(time.RFC3339)
			item.ClearTime = &s
		}
		alarms = append(alarms, item)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error reading alarm events"})
		return
	}

	// --- Aggregation in Go ---
	stillActive := 0
	cleared := 0
	bySeverity := map[string]int{"info": 0, "warning": 0, "critical": 0}

	for _, a := range alarms {
		switch a.Status {
		case "ACTIVE", "ACKNOWLEDGED":
			stillActive++
		case "CLEARED":
			cleared++
		}
		if _, ok := bySeverity[a.Severity]; ok {
			bySeverity[a.Severity]++
		} else {
			bySeverity[a.Severity] = 1
		}
	}

	c.JSON(http.StatusOK, aiopsAlarmDigest{
		PeriodHours: hours,
		TotalFired:  len(alarms),
		StillActive: stillActive,
		Cleared:     cleared,
		BySeverity:  bySeverity,
		Alarms:      alarms,
	})
}
