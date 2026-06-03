// Package handlers — reports.
//
// Operator-facing CSV exports. CSV (not PDF) is deliberate: every
// shop-floor PC has Excel / LibreOffice; CSVs survive email, fit in
// spreadsheets, and can be re-imported into BI tools. PDF reports come
// in a separate iteration once we wire a headless chromium renderer.
//
// All endpoints are org-scoped via the existing middleware chain.
// Range is required (start + end) so we never accidentally stream the
// whole DB when an operator forgets a filter.
package handlers

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// ReportsHandler exposes the CSV export endpoints.
type ReportsHandler struct {
	db *sql.DB
}

func NewReportsHandler(db *sql.DB) *ReportsHandler {
	return &ReportsHandler{db: db}
}

// parseRange resolves ?start=...&end=... ISO-8601 (RFC3339) timestamps
// and applies sensible bounds: end defaults to now, start to end-24h,
// max range 90 days to keep the export bounded.
func parseRange(c *gin.Context) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	end := now
	if e := c.Query("end"); e != "" {
		t, err := time.Parse(time.RFC3339, e)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end (RFC3339): %w", err)
		}
		end = t.UTC()
	}
	start := end.Add(-24 * time.Hour)
	if s := c.Query("start"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start (RFC3339): %w", err)
		}
		start = t.UTC()
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start must be before end")
	}
	if end.Sub(start) > 90*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("range cannot exceed 90 days")
	}
	return start, end, nil
}

// HistoryCSV streams tag history for the caller's org, optionally
// filtered by tag id list (?tag_ids=1,2,3).
func (h *ReportsHandler) HistoryCSV(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	start, end, err := parseRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	args := []interface{}{orgID, start, end}
	tagFilter := ""
	if ids := c.Query("tag_ids"); ids != "" {
		ids = sanitizeIDList(ids)
		if ids == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tag_ids must be a comma-separated list of integers"})
			return
		}
		tagFilter = fmt.Sprintf(" AND t.id = ANY('{%s}'::int[])", ids)
	}

	q := `
		SELECT th.time, t.id, t.alias, t.code, g.name AS gateway_name, th.value
		FROM tag_history th
		JOIN tags t ON t.id = th.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 AND th.time BETWEEN $2 AND $3` + tagFilter + `
		ORDER BY th.time`

	rows, err := h.db.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query history"})
		return
	}
	defer rows.Close()

	filename := fmt.Sprintf("history-%s.csv", time.Now().UTC().Format("20060102-150405"))
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Content-Type", "text/csv; charset=utf-8")

	w := csv.NewWriter(c.Writer)
	defer w.Flush()
	_ = w.Write([]string{"time_utc", "tag_id", "alias", "code", "gateway", "value"})

	for rows.Next() {
		var (
			t     time.Time
			tagID int
			alias sql.NullString
			code  string
			gw    string
			value sql.NullFloat64
		)
		if err := rows.Scan(&t, &tagID, &alias, &code, &gw, &value); err != nil {
			_ = w.Write([]string{"ERROR", err.Error()})
			return
		}
		valStr := ""
		if value.Valid {
			valStr = strconv.FormatFloat(value.Float64, 'f', -1, 64)
		}
		_ = w.Write([]string{
			t.UTC().Format(time.RFC3339Nano),
			strconv.Itoa(tagID),
			alias.String,
			code,
			gw,
			valStr,
		})
	}
}

// AlarmsCSV exports alarm_events for the caller's org over the range.
func (h *ReportsHandler) AlarmsCSV(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}
	start, end, err := parseRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rows, err := h.db.Query(`
		SELECT e.id, e.trigger_time, e.clear_time, e.status, e.severity, e.alarm_type,
		       COALESCE(e.message,''), COALESCE(t.alias,''), COALESCE(t.code,''), e.value_at_trigger
		FROM alarm_events e
		JOIN tags t ON t.id = e.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		WHERE s.org_id = $1 AND e.trigger_time BETWEEN $2 AND $3
		ORDER BY e.trigger_time DESC`, orgID, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query alarms"})
		return
	}
	defer rows.Close()

	c.Header("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q",
			fmt.Sprintf("alarms-%s.csv", time.Now().UTC().Format("20060102-150405"))))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	w := csv.NewWriter(c.Writer)
	defer w.Flush()
	_ = w.Write([]string{"id", "trigger_utc", "clear_utc", "status", "severity", "type",
		"message", "tag_alias", "tag_code", "value_at_trigger"})

	for rows.Next() {
		var (
			id           int64
			trig         time.Time
			clear        sql.NullTime
			status       string
			severity     string
			alarmType    string
			msg          string
			alias, code  string
			val          sql.NullFloat64
		)
		if err := rows.Scan(&id, &trig, &clear, &status, &severity, &alarmType, &msg, &alias, &code, &val); err != nil {
			_ = w.Write([]string{"ERROR", err.Error()})
			return
		}
		clearStr := ""
		if clear.Valid {
			clearStr = clear.Time.UTC().Format(time.RFC3339)
		}
		valStr := ""
		if val.Valid {
			valStr = strconv.FormatFloat(val.Float64, 'f', -1, 64)
		}
		_ = w.Write([]string{
			strconv.FormatInt(id, 10),
			trig.UTC().Format(time.RFC3339),
			clearStr,
			status, severity, alarmType, msg, alias, code, valStr,
		})
	}
}

// AuditCSV exports audit_logs for the caller's org. Global-admin only —
// the audit log spans every action and may carry sensitive details
// (failed login attempts, write values). Org admins see only their own
// scoped data through other UI surfaces.
func (h *ReportsHandler) AuditCSV(c *gin.Context) {
	start, end, err := parseRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rows, err := h.db.Query(`
		SELECT id, COALESCE(user_id,0), COALESCE(username,''), action,
		       COALESCE(ip_address,''), COALESCE(user_agent,''), COALESCE(details::text,''), success, created_at
		FROM audit_logs WHERE created_at BETWEEN $1 AND $2
		ORDER BY created_at DESC`, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query audit"})
		return
	}
	defer rows.Close()

	c.Header("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q",
			fmt.Sprintf("audit-%s.csv", time.Now().UTC().Format("20060102-150405"))))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	w := csv.NewWriter(c.Writer)
	defer w.Flush()
	_ = w.Write([]string{"id", "user_id", "username", "action", "ip", "user_agent", "details", "success", "created_utc"})

	for rows.Next() {
		var (
			id        int64
			uid       int
			uname     string
			action    string
			ip        string
			ua        string
			details   string
			success   bool
			created   time.Time
		)
		if err := rows.Scan(&id, &uid, &uname, &action, &ip, &ua, &details, &success, &created); err != nil {
			_ = w.Write([]string{"ERROR", err.Error()})
			return
		}
		_ = w.Write([]string{
			strconv.FormatInt(id, 10), strconv.Itoa(uid), uname, action, ip, ua,
			details, strconv.FormatBool(success), created.UTC().Format(time.RFC3339),
		})
	}
}

// sanitizeIDList trims and validates that every token in a
// comma-separated string is a positive integer, returning the cleaned
// string. Returns "" on any non-integer token so the caller can reject
// the request cleanly. Used to safely interpolate into the
// `ANY('{...}'::int[])` clause.
func sanitizeIDList(s string) string {
	out := []string{}
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n <= 0 {
			return ""
		}
		out = append(out, strconv.Itoa(n))
	}
	return strings.Join(out, ",")
}
