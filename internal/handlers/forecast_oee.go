package handlers

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/forecast"
)

// OEE at the end of the shift (or of the day, with no shift on), from the
// forecasts of an OEE profile's tags: hours running from its run tag, pieces
// from its counters. The same formula as the OEE page, over the period:
// availability = hours running / hours of the period, performance = pieces /
// (target per hour × hours of the period), quality = good / pieces.

// Band is a forecast value with its P10–P90.
type Band struct {
	Q10 float64 `json:"q10"`
	Q50 float64 `json:"q50"`
	Q90 float64 `json:"q90"`
}

// OEEForecast is one OEE profile's expected OEE by the end of the period.
type OEEForecast struct {
	ProfileID int     `json:"profile_id"`
	Name      string  `json:"name"`
	TargetOEE float64 `json:"target_oee"`
	Period    string  `json:"period,omitempty"` // shift or day
	// PeriodName is the shift's name.
	PeriodName string     `json:"period_name,omitempty"`
	Start      *time.Time `json:"start,omitempty"`
	End        *time.Time `json:"end,omitempty"`
	// OEE, Availability, Performance, Quality in %. The band comes from
	// running time and pieces; quality is at its median.
	OEE          *Band `json:"oee,omitempty"`
	Availability *Band `json:"availability,omitempty"`
	Performance  *Band `json:"performance,omitempty"`
	Quality      *Band `json:"quality,omitempty"`
	Produced     *Band `json:"produced,omitempty"`
	Good         *Band `json:"good,omitempty"`
	RunHours     *Band `json:"run_hours,omitempty"`
	// Missing: the profile's inputs with no reliable forecast for the period
	// (run_time, produced, good). Run time and good pieces missing count as
	// 100%, as on the OEE page; with no pieces there is no forecast.
	Missing []string `json:"missing"`
	// Extrapolated: the period ends past the forecasts' horizon.
	Extrapolated bool `json:"extrapolated,omitempty"`
}

type oeeProfileRow struct {
	id                   int
	name                 string
	run, produced, good  int
	targetPPH, targetOEE float64
}

// projectOEE works out a profile's OEE from its tags' period totals.
func projectOEE(p *oeeProfileRow, periods map[int][]forecast.PeriodTotal) OEEForecast {
	out := OEEForecast{ProfileID: p.id, Name: p.name, TargetOEE: p.targetOEE, Missing: []string{}}
	pick := func(tag int, w *forecast.PeriodTotal) *forecast.PeriodTotal {
		for i := range periods[tag] {
			x := &periods[tag][i]
			if w == nil && (x.Period == forecast.PeriodShift || x.Period == forecast.PeriodDay) {
				return x
			}
			if w != nil && x.Period == w.Period && x.Start.Equal(w.Start) {
				return x
			}
		}
		return nil
	}
	if p.produced == 0 {
		out.Missing = append(out.Missing, "produced")
		return out
	}
	// The shift when one is on (it comes first), the day otherwise.
	prod := pick(p.produced, nil)
	if prod == nil || p.targetPPH <= 0 {
		out.Missing = append(out.Missing, "produced")
		return out
	}
	start, end := prod.Start, prod.End
	out.Period, out.PeriodName, out.Start, out.End = prod.Period, prod.Name, &start, &end
	hours := end.Sub(start).Hours()
	expected := p.targetPPH * hours
	out.Produced = &Band{prod.Q10, prod.Q50, prod.Q90}
	out.Extrapolated = prod.Extrapolated
	pct := func(x, of float64) float64 { return math.Min(100, math.Max(0, x/of*100)) }

	a := Band{100, 100, 100}
	if run := pick(p.run, prod); p.run > 0 && run != nil {
		out.RunHours = &Band{run.Q10, run.Q50, run.Q90}
		a = Band{pct(run.Q10, hours), pct(run.Q50, hours), pct(run.Q90, hours)}
		out.Extrapolated = out.Extrapolated || run.Extrapolated
	} else {
		out.Missing = append(out.Missing, "run_time")
	}
	perf := Band{pct(prod.Q10, expected), pct(prod.Q50, expected), pct(prod.Q90, expected)}
	q := 100.0
	if good := pick(p.good, prod); p.good > 0 && good != nil {
		out.Good = &Band{good.Q10, good.Q50, good.Q90}
		if prod.Q50 > 0 {
			q = pct(good.Q50, prod.Q50)
		}
		out.Extrapolated = out.Extrapolated || good.Extrapolated
	} else {
		out.Missing = append(out.Missing, "good")
	}
	out.Availability, out.Performance, out.Quality = &a, &perf, &Band{q, q, q}
	out.OEE = &Band{a.Q10 * perf.Q10 * q / 1e4, a.Q50 * perf.Q50 * q / 1e4, a.Q90 * perf.Q90 * q / 1e4}
	return out
}

// OEEForecasts is GET /api/forecast/oee: every enabled OEE profile of the
// organization with its OEE expected by the end of the shift.
func (h *ForecastHandler) OEEForecasts(c *gin.Context) {
	org, ok := orgFromContext(c)
	if !ok {
		return
	}
	out, err := h.oeeForecasts(c.Request.Context(), org)
	if err != nil {
		log.Printf("[FORECAST] oee: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read OEE forecasts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profiles": out})
}

func (h *ForecastHandler) oeeForecasts(ctx context.Context, org int) ([]OEEForecast, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(run_time_tag_id, 0), COALESCE(produced_tag_id, 0), COALESCE(good_tag_id, 0),
		       COALESCE(target_pieces_per_hour, 0), COALESCE(target_oee, 0)
		FROM oee_profiles WHERE org_id = $1 AND enabled ORDER BY display_order, name`, org)
	if err != nil {
		return nil, err
	}
	var profiles []oeeProfileRow
	var tags []int64
	for rows.Next() {
		var p oeeProfileRow
		if err = rows.Scan(&p.id, &p.name, &p.run, &p.produced, &p.good, &p.targetPPH, &p.targetOEE); err != nil {
			_ = rows.Close()
			return nil, err
		}
		profiles = append(profiles, p)
		for _, t := range []int{p.run, p.produced, p.good} {
			if t > 0 {
				tags = append(tags, int64(t))
			}
		}
	}
	_ = rows.Close()
	periods := map[int][]forecast.PeriodTotal{}
	if len(tags) > 0 {
		// Only reliable forecasts have periods; the tags were checked to be
		// the organization's when the profile was saved.
		prow, qerr := h.db.QueryContext(ctx, `
			SELECT tag_id, periods FROM tag_forecasts WHERE tag_id = ANY($1) AND status = 'ok'`, pq.Array(tags))
		if qerr != nil {
			return nil, qerr
		}
		for prow.Next() {
			var id int
			var raw []byte
			if prow.Scan(&id, &raw) == nil {
				var ps []forecast.PeriodTotal
				if json.Unmarshal(raw, &ps) == nil {
					periods[id] = ps
				}
			}
		}
		_ = prow.Close()
	}
	out := make([]OEEForecast, 0, len(profiles))
	for i := range profiles {
		out = append(out, projectOEE(&profiles[i], periods))
	}
	return out, nil
}
