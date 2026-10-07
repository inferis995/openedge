package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"
	_ "time/tzdata" // the user's time zone, whatever the container carries

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// An Excel workbook of a trend: the real samples when there are not too many,
// buckets with their min and max otherwise, and a sheet saying what it holds.
// Built on the server so it follows the same tenant checks as the chart and
// is not limited to what the screen shows.

const (
	maxExportRows    = 200_000
	exportBucketRows = 10_000
)

type exportRequest struct {
	TagIDs []int  `json:"tag_ids"`
	Start  string `json:"start"`
	End    string `json:"end"`
	Agg    string `json:"agg"`
	// IANA time zone the times are written in (the user's); UTC if unknown.
	TZ string `json:"tz"`
}

type exportTag struct {
	ID      int
	Alias   string
	Unit    string
	Gateway string
	points  []HistoryDataPoint
}

type parsedExport struct {
	req        exportRequest
	start, end time.Time
	agg        string
	loc        *time.Location
}

// parseExport reads and checks the request, answering 400 itself when it is not usable.
func parseExport(c *gin.Context) (parsedExport, bool) {
	var p parsedExport
	if err := c.ShouldBindJSON(&p.req); err != nil || len(p.req.TagIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag_ids, start and end are required"})
		return p, false
	}
	if len(p.req.TagIDs) > maxBatchTags {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("at most %d tags", maxBatchTags)})
		return p, false
	}
	var err1, err2 error
	p.start, err1 = time.Parse(time.RFC3339, p.req.Start)
	p.end, err2 = time.Parse(time.RFC3339, p.req.End)
	if err1 != nil || err2 != nil || !p.end.After(p.start) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start and end must be RFC 3339, end after start"})
		return p, false
	}
	var ok bool
	p.agg, ok = map[string]string{"": "avg", "mean": "avg", "avg": "avg", "min": "min", "max": "max", "last": "last"}[p.req.Agg]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agg: avg, min, max or last"})
		return p, false
	}
	p.loc = time.UTC
	if p.req.TZ != "" {
		if l, err := time.LoadLocation(p.req.TZ); err == nil {
			p.loc = l
		}
	}
	return p, true
}

// ExportXLSX handles POST /api/history/export.xlsx.
func (h *HistoryHandler) ExportXLSX(c *gin.Context) {
	p, ok := parseExport(c)
	if !ok {
		return
	}
	req, start, end, agg, loc := p.req, p.start, p.end, p.agg, p.loc

	ctx := c.Request.Context()
	isGlobalAdmin := middleware.IsGlobalAdmin(c)
	orgID, hasOrg := middleware.GetOrganizationID(c)
	if !isGlobalAdmin && !hasOrg {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Access first, for every tag, as the batch query does.
	tags := make([]*exportTag, 0, len(req.TagIDs))
	for _, id := range req.TagIDs {
		var accessErr error
		if isGlobalAdmin {
			_, accessErr = h.getTagDetailsNoOrg(id)
		} else {
			_, accessErr = h.getTagDetails(id, orgID)
		}
		if accessErr != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("tag %d not found or access denied", id)})
			return
		}
		t := &exportTag{ID: id}
		_ = h.db.QueryRowContext(ctx, `
			SELECT COALESCE(NULLIF(t.alias, ''), t.code), COALESCE(t.eu_unit, ''), COALESCE(g.name, '')
			FROM tags t LEFT JOIN gateways g ON g.id = t.gateway_id WHERE t.id = $1`, id).
			Scan(&t.Alias, &t.Unit, &t.Gateway)
		tags = append(tags, t)
	}

	raw, bucket, readErr := h.loadExportPoints(ctx, tags, start, end, agg)
	if readErr != nil {
		log.Printf("[HISTORY] export: %v", readErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read history"})
		return
	}

	f, buildErr := buildExportWorkbook(tags, start, end, loc, raw, bucket, req.Agg)
	if buildErr != nil {
		log.Printf("[HISTORY] export: %v", buildErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build the workbook"})
		return
	}
	defer func() { _ = f.Close() }()
	name := fmt.Sprintf("trend_%s.xlsx", start.In(loc).Format("2006-01-02_1504"))
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Status(http.StatusOK)
	if err := f.Write(c.Writer); err != nil {
		log.Printf("[HISTORY] export write: %v", err)
	}
}

// loadExportPoints fills each tag's points: the samples if they all fit,
// buckets with min and max otherwise — decided for the whole workbook.
func (h *HistoryHandler) loadExportPoints(ctx context.Context, tags []*exportTag, start, end time.Time, agg string) (bool, time.Duration, error) {
	raw := true
	for _, t := range tags {
		var n int
		if err := h.db.QueryRowContext(ctx, `SELECT count(*) FROM (
			SELECT 1 FROM tag_history WHERE tag_id = $1 AND time >= $2 AND time <= $3 LIMIT $4) s`,
			t.ID, start, end, maxExportRows+1).Scan(&n); err != nil {
			return false, 0, err
		}
		if n > maxExportRows/len(tags) {
			raw = false
		}
	}
	bucket := chartBucket(start, end, exportBucketRows)
	for _, t := range tags {
		var err error
		if raw {
			t.points, err = h.queryRaw(t.ID, start, end)
		} else {
			t.points, err = h.exportBuckets(ctx, t.ID, start, end, bucket, agg)
		}
		if err != nil {
			return false, 0, fmt.Errorf("tag %d: %w", t.ID, err)
		}
	}
	return raw, bucket, nil
}

func (h *HistoryHandler) exportBuckets(ctx context.Context, tagID int, start, end time.Time, bucket time.Duration, agg string) ([]HistoryDataPoint, error) {
	if table, level := aggregateSource(bucket); table != "tag_history" {
		if p, err := h.bucketsFromAggregate(ctx, tagID, start, end, bucket, agg, table, level); err == nil {
			return p, nil
		}
	}
	return h.bucketsFromRaw(ctx, tagID, start, end, bucket, agg)
}

// buildExportWorkbook writes the "Data" sheet (time, then each tag — and its
// min and max when bucketed) and an "Info" sheet.
func buildExportWorkbook(tags []*exportTag, start, end time.Time, loc *time.Location, raw bool, bucket time.Duration, agg string) (*excelize.File, error) {
	f := excelize.NewFile()
	const data = "Data"
	if err := f.SetSheetName("Sheet1", data); err != nil {
		return nil, err
	}

	// One row per instant any tag has a sample at.
	byTs := map[int64][]*HistoryDataPoint{}
	for i, t := range tags {
		for j := range t.points {
			p := &t.points[j]
			row := byTs[p.Timestamp]
			if row == nil {
				row = make([]*HistoryDataPoint, len(tags))
				byTs[p.Timestamp] = row
			}
			row[i] = p
		}
	}
	instants := make([]int64, 0, len(byTs))
	for ts := range byTs {
		instants = append(instants, ts)
	}
	sort.Slice(instants, func(a, b int) bool { return instants[a] < instants[b] })

	sw, err := f.NewStreamWriter(data)
	if err != nil {
		return nil, err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E7E6E6"}}})
	dateStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr("yyyy-mm-dd hh:mm:ss")})

	header := []interface{}{excelize.Cell{StyleID: bold, Value: "Time (" + loc.String() + ")"}}
	for _, t := range tags {
		label := t.Alias
		if t.Unit != "" {
			label += " (" + t.Unit + ")"
		}
		header = append(header, excelize.Cell{StyleID: bold, Value: label})
		if !raw {
			header = append(header, excelize.Cell{StyleID: bold, Value: t.Alias + " min"}, excelize.Cell{StyleID: bold, Value: t.Alias + " max"})
		}
	}
	if err := sw.SetRow("A1", header, excelize.RowOpts{}); err != nil {
		return nil, err
	}
	_ = sw.SetColWidth(1, 1, 20)
	for i, ts := range instants {
		local := time.UnixMilli(ts).In(loc)
		// Excel has no time zones: the wall-clock time, as a date value.
		wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), time.UTC)
		row := []interface{}{excelize.Cell{StyleID: dateStyle, Value: wall}}
		for _, p := range byTs[ts] {
			row = append(row, cellValue(p, func(p *HistoryDataPoint) *float64 { return p.Value }))
			if !raw {
				row = append(row,
					cellValue(p, func(p *HistoryDataPoint) *float64 { return p.Min }),
					cellValue(p, func(p *HistoryDataPoint) *float64 { return p.Max }))
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := sw.SetRow(cell, row); err != nil {
			return nil, err
		}
	}
	if err := sw.Flush(); err != nil {
		return nil, err
	}
	_ = f.SetPanes(data, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	if err := writeExportInfo(f, tags, start, end, loc, raw, bucket, agg, len(instants)); err != nil {
		return nil, err
	}
	return f, nil
}

// writeExportInfo writes the "Info" sheet: the period, what the values are, the tags.
func writeExportInfo(f *excelize.File, tags []*exportTag, start, end time.Time, loc *time.Location, raw bool, bucket time.Duration, agg string, nRows int) error {
	const info = "Info"
	if _, err := f.NewSheet(info); err != nil {
		return err
	}
	source := "samples (raw)"
	if !raw {
		if agg == "" {
			agg = "mean"
		}
		source = fmt.Sprintf("%s per %s bucket, with min and max", agg, bucket)
	}
	rows := [][]interface{}{
		{"From", start.In(loc).Format("2006-01-02 15:04:05 MST")},
		{"To", end.In(loc).Format("2006-01-02 15:04:05 MST")},
		{"Values", source},
		{"Rows", nRows},
		{"Generated", time.Now().In(loc).Format("2006-01-02 15:04:05 MST")},
		{},
		{"Tag id", "Name", "Unit", "Gateway", "Values in period"},
	}
	for _, t := range tags {
		rows = append(rows, []interface{}{t.ID, t.Alias, t.Unit, t.Gateway, len(t.points)})
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow(info, cell, &r); err != nil {
			return err
		}
	}
	return f.SetColWidth(info, "A", "E", 18)
}

func cellValue(p *HistoryDataPoint, pick func(*HistoryDataPoint) *float64) interface{} {
	if p == nil {
		return nil
	}
	if v := pick(p); v != nil {
		return *v
	}
	return nil
}

func strPtr(s string) *string { return &s }
