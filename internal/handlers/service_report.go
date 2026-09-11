package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/servicereport"
)

// ServiceReportHandler serves the end-of-month document.
type ServiceReportHandler struct {
	db *sql.DB
}

func NewServiceReportHandler(db *sql.DB) *ServiceReportHandler {
	return &ServiceReportHandler{db: db}
}

// reportScope resolves which organization the caller may report on, where zero
// means all of them. Taken from the token, never from a parameter.
func reportScope(c *gin.Context) (int, bool) {
	if middleware.IsGlobalAdmin(c) {
		return 0, true
	}
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		return 0, false
	}
	return orgID, true
}

// reportPeriod resolves ?month=YYYY-MM, defaulting to the calendar month that
// has just finished.
//
// A whole month, aligned to midnight, and not "the last thirty days": the
// document is filed, compared with the one before it, and attached to an
// invoice. Two reports that overlap by a few hours cannot be compared, and
// nobody reading them would know why the numbers moved.
func reportPeriod(c *gin.Context) (time.Time, time.Time, error) {
	month := c.Query("month")
	if month == "" {
		from, to := previousMonth(time.Now().UTC())
		return from, to, nil
	}
	return parseMonth(month)
}

// previousMonth is the half-open range covering the calendar month before the
// one containing now.
//
// Built from the year and the month rather than by subtracting days: "thirty
// days ago" lands mid-month, and rounding a timestamp down to midnight depends
// on the zone it is rounded in. Both are the kind of arithmetic that works for
// eleven months and then produces a wrong report in March.
func previousMonth(now time.Time) (from, to time.Time) {
	firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return firstOfThisMonth.AddDate(0, -1, 0), firstOfThisMonth
}

// parseMonth turns "2026-08" into the half-open range covering August 2026.
func parseMonth(month string) (from, to time.Time, err error) {
	from, err = time.ParseInLocation("2006-01", month, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("il mese deve essere nella forma 2026-08")
	}
	return from, from.AddDate(0, 1, 0), nil
}

// GetServiceReport handles GET /api/reports/service-report — the data.
func (h *ServiceReportHandler) GetServiceReport(c *gin.Context) {
	report, ok := h.build(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, report)
}

// DownloadServiceReport handles GET /api/reports/service-report.html — the
// document, self-contained, ready to print or attach to an email.
func (h *ServiceReportHandler) DownloadServiceReport(c *gin.Context) {
	report, ok := h.build(c)
	if !ok {
		return
	}

	filename := fmt.Sprintf("rapporto-servizio-%s.html", report.From.Format("2006-01"))
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)

	if err := servicereport.Render(c.Writer, report); err != nil {
		// The headers are already on the wire; there is no status left to set.
		_ = c.Error(err)
	}
}

// build resolves the scope and the period and assembles the report, writing the
// error response itself when something is wrong.
func (h *ServiceReportHandler) build(c *gin.Context) (*servicereport.Report, bool) {
	orgID, allowed := reportScope(c)
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "no organization in this session"})
		return nil, false
	}

	from, to, err := reportPeriod(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, false
	}

	report, err := servicereport.Build(c.Request.Context(), h.db, orgID, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not build the report"})
		return nil, false
	}
	return report, true
}
