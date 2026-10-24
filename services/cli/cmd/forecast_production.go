package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// Production forecasts: OEE by the end of the shift, and the totals of every
// counter (or state, in hours on) by the end of the shift, the day and the
// month — measured so far, forecast for the rest.

var forecastProductionCmd = &cobra.Command{
	Use:   "production",
	Short: "OEE at the end of the shift and counter totals by shift, day and month",
	Run:   runForecastProduction,
}

var fpSite string

func init() {
	forecastProductionCmd.Flags().StringVar(&fpSite, "site", "", "Only counters of this site (name)")
}

type periodTotal struct {
	Period       string  `json:"period"`
	Name         string  `json:"name,omitempty"`
	Start        string  `json:"start"`
	End          string  `json:"end"`
	SoFar        float64 `json:"so_far"`
	Q10          float64 `json:"q10"`
	Q50          float64 `json:"q50"`
	Q90          float64 `json:"q90"`
	Extrapolated bool    `json:"extrapolated,omitempty"`
}

type band struct {
	Q10 float64 `json:"q10"`
	Q50 float64 `json:"q50"`
	Q90 float64 `json:"q90"`
}

type oeeForecastOut struct {
	ProfileID    int      `json:"profile_id"`
	Name         string   `json:"name"`
	TargetOEE    float64  `json:"target_oee"`
	Period       string   `json:"period,omitempty"`
	PeriodName   string   `json:"period_name,omitempty"`
	End          string   `json:"end,omitempty"`
	OEE          *band    `json:"oee,omitempty"`
	Availability *band    `json:"availability,omitempty"`
	Performance  *band    `json:"performance,omitempty"`
	Quality      *band    `json:"quality,omitempty"`
	Produced     *band    `json:"produced,omitempty"`
	Good         *band    `json:"good,omitempty"`
	Missing      []string `json:"missing"`
	Extrapolated bool     `json:"extrapolated,omitempty"`
}

type productionOut struct {
	OEE    []oeeForecastOut `json:"oee"`
	Totals []forecastRowOut `json:"totals"`
}

// productionForecast reads the OEE forecasts and the tags that have period
// totals (of one site, when given).
func productionForecast(raw func(string) ([]byte, error), site string) (productionOut, error) {
	out := productionOut{OEE: []oeeForecastOut{}, Totals: []forecastRowOut{}}
	data, err := raw("/api/forecast/oee")
	if err != nil {
		return out, err
	}
	var oee struct {
		Profiles []oeeForecastOut `json:"profiles"`
	}
	if err = json.Unmarshal(data, &oee); err != nil {
		return out, fmt.Errorf("unexpected OEE forecast answer: %w", err)
	}
	if oee.Profiles != nil {
		out.OEE = oee.Profiles
	}
	if data, err = raw("/api/forecast/overview"); err != nil {
		return out, err
	}
	var ov forecastOverview
	if err = json.Unmarshal(data, &ov); err != nil {
		return out, fmt.Errorf("unexpected overview answer: %w", err)
	}
	for _, r := range filterForecastOverview(ov.Tags, "", site) {
		if len(r.Periods) > 0 {
			out.Totals = append(out.Totals, r)
		}
	}
	return out, nil
}

func fmtTotal(p *periodTotal, unit string) string {
	v := strconv.FormatFloat(p.Q50, 'f', 0, 64)
	if p.Q50 < 100 {
		v = strconv.FormatFloat(p.Q50, 'g', 3, 64)
	}
	s := strings.TrimSpace(v + " " + unit)
	if p.Extrapolated {
		s += " (est.)"
	}
	return s
}

func runForecastProduction(_ *cobra.Command, _ []string) {
	data, err := productionForecast(GetClient().RawGet, fpSite)
	if err != nil {
		PrintError("%v", err)
	}
	if flagJSON {
		b, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(b))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"OEE PROFILE\tUNTIL\tOEE\tBAND\tTARGET\tA / P / Q\tMISSING"+ColorReset)
	for i := range data.OEE {
		o := &data.OEE[i]
		if o.OEE == nil {
			fmt.Fprintf(w, "%s\t—\t—\t—\t%g\t—\t%s\n", o.Name, o.TargetOEE, strings.Join(o.Missing, ", "))
			continue
		}
		fmt.Fprintf(w, "%s\t%s %s\t%.0f%%\t%.0f–%.0f%%\t%g%%\t%.0f / %.0f / %.0f\t%s\n", o.Name, o.Period, o.End,
			o.OEE.Q50, o.OEE.Q10, o.OEE.Q90, o.TargetOEE, o.Availability.Q50, o.Performance.Q50, o.Quality.Q50,
			orDash(strings.Join(o.Missing, ", ")))
	}
	_ = w.Flush()
	fmt.Println()
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tTAG\tKIND\tSHIFT\tDAY\tMONTH"+ColorReset)
	for i := range data.Totals {
		r := &data.Totals[i]
		unit := r.Unit
		if r.Kind == "state" {
			unit = "h"
		}
		cells := map[string]string{"shift": "—", "day": "—", "month": "—"}
		for k := range r.Periods {
			cells[r.Periods[k].Period] = fmtTotal(&r.Periods[k], unit)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", r.TagID, r.Alias, r.Kind, cells["shift"], cells["day"], cells["month"])
	}
	_ = w.Flush()
}
