package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// Tag forecasts (docs/FORECASTING.md): read them, recompute one, and set the
// profiles that shape them at any level — organization, site, area, gateway
// or tag — where an unset field inherits from the level above.

var forecastCmd = &cobra.Command{
	Use:   "forecast",
	Short: "Tag forecasts: overview, one tag, recompute, settings by level",
}

var forecastListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every forecastable tag of the organization with the state of its forecast",
	Run:   runForecastList,
}

var forecastShowCmd = &cobra.Command{
	Use:   "show",
	Short: "One tag's forecast: status, how reliable, and the forecast steps",
	Run:   runForecastShow,
}

var forecastRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Forecast a tag now (admin)",
	Run:   runForecastRefresh,
}

var forecastSettingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Forecast settings of one level (get, or change with flags)",
	Long: `Forecast settings of one level of the hierarchy.

Without change flags it prints the level's own settings, what it inherits and
the result. With --enabled, --kind or --horizon it changes those fields (admin):
"inherit" (or --horizon 0) puts a field back to inheriting. --clear removes the
level's profile entirely.

  openedge forecast settings --scope org --id 1 --horizon 48
  openedge forecast settings --scope site --id 3 --enabled off
  openedge forecast settings --scope tag --id 120 --kind degradation
  openedge forecast settings --scope gateway --id 7 --clear`,
	Run: runForecastSettings,
}

var (
	fcStatus  string
	fcSite    string
	fcTagID   int
	fcScope   string
	fcScopeID int
	fcEnabled string
	fcKind    string
	fcHorizon int
	fcClear   bool
)

func init() {
	forecastListCmd.Flags().StringVar(&fcStatus, "status", "", "Only this status: ok, unreliable, learning, disabled, error, pending, or at_risk")
	forecastListCmd.Flags().StringVar(&fcSite, "site", "", "Only tags of this site (name)")

	for _, c := range []*cobra.Command{forecastShowCmd, forecastRefreshCmd} {
		c.Flags().IntVar(&fcTagID, "tag", 0, "Tag ID (required)")
		_ = c.MarkFlagRequired("tag")
	}

	forecastSettingsCmd.Flags().StringVar(&fcScope, "scope", "", "org, site, area, gateway or tag (required)")
	forecastSettingsCmd.Flags().IntVar(&fcScopeID, "id", 0, "ID at that level (required)")
	forecastSettingsCmd.Flags().StringVar(&fcEnabled, "enabled", "", "on, off or inherit")
	forecastSettingsCmd.Flags().StringVar(&fcKind, "kind", "", "auto, process, cyclic, degradation, counter, state, or inherit")
	forecastSettingsCmd.Flags().IntVar(&fcHorizon, "horizon", -1, "Horizon in hours (0 = inherit)")
	forecastSettingsCmd.Flags().BoolVar(&fcClear, "clear", false, "Remove the level's profile: inherit everything")
	_ = forecastSettingsCmd.MarkFlagRequired("scope")
	_ = forecastSettingsCmd.MarkFlagRequired("id")

	forecastCmd.AddCommand(forecastListCmd, forecastShowCmd, forecastRefreshCmd, forecastSettingsCmd)
}

type forecastOverview struct {
	Engines []string         `json:"engines"`
	Summary map[string]int   `json:"summary"`
	Tags    []forecastRowOut `json:"tags"`
}

type forecastPoint struct {
	T   string   `json:"t"`
	Q10 *float64 `json:"q10"`
	Q50 *float64 `json:"q50"`
	Q90 *float64 `json:"q90"`
}

type forecastRowOut struct {
	TagID        int             `json:"tag_id"`
	Alias        string          `json:"alias"`
	Unit         string          `json:"unit,omitempty"`
	GatewayName  string          `json:"gateway_name"`
	AreaName     string          `json:"area_name"`
	SiteName     string          `json:"site_name"`
	Status       string          `json:"status"`
	Reason       string          `json:"reason,omitempty"`
	Kind         string          `json:"kind,omitempty"`
	Engine       string          `json:"engine,omitempty"`
	Skill        *float64        `json:"skill,omitempty"`
	HorizonHours int             `json:"horizon_hours"`
	End          *forecastPoint  `json:"end,omitempty"`
	Risks        []forecastRisk  `json:"risks,omitempty"`
	Anomaly      json.RawMessage `json:"anomaly,omitempty"`
}

// forecastRisk is an alarm rule the forecast may trip.
type forecastRisk struct {
	RuleID       int     `json:"rule_id"`
	Type         string  `json:"type"`
	Threshold    float64 `json:"threshold"`
	Severity     string  `json:"severity,omitempty"`
	Message      string  `json:"message,omitempty"`
	Likelihood   string  `json:"likelihood"`
	At           string  `json:"at"`
	Extrapolated bool    `json:"extrapolated,omitempty"`
}

func (r forecastRowOut) serious() bool {
	for _, k := range r.Risks {
		if k.Likelihood == "likely" || k.Likelihood == "almost_certain" {
			return true
		}
	}
	return false
}

// filterForecastOverview keeps the rows of one status and/or site (by name,
// case-insensitive). Empty filters keep everything. status "at_risk" keeps the
// tags with an alarm rule likely or almost certain to trip.
func filterForecastOverview(rows []forecastRowOut, status, site string) []forecastRowOut {
	out := make([]forecastRowOut, 0, len(rows))
	for _, r := range rows {
		if status == "at_risk" {
			if !r.serious() {
				continue
			}
		} else if status != "" && r.Status != status {
			continue
		}
		if site != "" && !strings.EqualFold(r.SiteName, site) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func fmtNum(p *float64) string {
	if p == nil {
		return "—"
	}
	return strconv.FormatFloat(*p, 'g', 4, 64)
}

func runForecastList(_ *cobra.Command, _ []string) {
	var ov forecastOverview
	if err := GetClient().Get("/api/forecast/overview", &ov); err != nil {
		PrintError("%v", err)
	}
	rows := filterForecastOverview(ov.Tags, fcStatus, fcSite)
	if flagJSON {
		data, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(data))
		return
	}
	fmt.Printf("Engines: %s · reliable %d · unreliable %d · learning %d · total %d\n\n",
		strings.Join(ov.Engines, ", "), ov.Summary["ok"], ov.Summary["unreliable"], ov.Summary["learning"], ov.Summary["total"])
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tTAG\tWHERE\tKIND\tSTATUS\tSKILL\tEXPECTED\tRISK"+ColorReset)
	for _, r := range rows {
		expected := "—"
		if r.Status == "ok" && r.End != nil {
			expected = fmt.Sprintf("%s %s (%s–%s) in %d h", fmtNum(r.End.Q50), r.Unit, fmtNum(r.End.Q10), fmtNum(r.End.Q90), r.HorizonHours)
		}
		risk := "—"
		if len(r.Risks) > 0 {
			k := r.Risks[0]
			risk = fmt.Sprintf("%s %s %g at %s", k.Likelihood, k.Type, k.Threshold, k.At)
		}
		fmt.Fprintf(w, "%d\t%s\t%s / %s / %s\t%s\t%s\t%s\t%s\t%s\n", r.TagID, r.Alias, r.SiteName, r.AreaName, r.GatewayName,
			orDash(r.Kind), r.Status, fmtNum(r.Skill), expected, risk)
	}
	_ = w.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func printForecast(raw []byte) {
	if flagJSON {
		fmt.Println(prettyJSON(raw))
		return
	}
	var f struct {
		TagID    int             `json:"tag_id"`
		Status   string          `json:"status"`
		Reason   string          `json:"reason"`
		Kind     string          `json:"kind"`
		Engine   string          `json:"engine"`
		Model    string          `json:"model"`
		Skill    *float64        `json:"skill"`
		Coverage *float64        `json:"coverage"`
		Points   []forecastPoint `json:"points"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		fmt.Println(string(raw))
		return
	}
	fmt.Printf("Tag %d: %s", f.TagID, f.Status)
	if f.Reason != "" {
		fmt.Printf(" (%s)", f.Reason)
	}
	fmt.Println()
	if f.Kind != "" {
		fmt.Printf("Kind %s · engine %s %s · skill %s · coverage %s\n", f.Kind, f.Engine, f.Model, fmtNum(f.Skill), fmtNum(f.Coverage))
	}
	if len(f.Points) == 0 {
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"TIME\tP10\tP50\tP90"+ColorReset)
	// The shape is enough on a terminal: a dozen steps, evenly spread.
	stride := (len(f.Points) + 11) / 12
	for i := 0; i < len(f.Points); i += stride {
		p := f.Points[i]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.T, fmtNum(p.Q10), fmtNum(p.Q50), fmtNum(p.Q90))
	}
	_ = w.Flush()
}

func runForecastShow(_ *cobra.Command, _ []string) {
	raw, err := GetClient().RawGet(fmt.Sprintf("/api/tags/%d/forecast", fcTagID))
	if err != nil {
		PrintError("%v", err)
	}
	printForecast(raw)
}

func runForecastRefresh(_ *cobra.Command, _ []string) {
	raw, err := GetClient().RawPost(fmt.Sprintf("/api/tags/%d/forecast/refresh", fcTagID), nil)
	if err != nil {
		PrintError("%v", err)
	}
	printForecast(raw)
}

func forecastProfilePath(scope string, id int) string {
	return "/api/forecast/profiles?scope=" + url.QueryEscape(scope) + "&scope_id=" + strconv.Itoa(id)
}

// mergeForecastSettings applies changes to a level's own settings. A change
// whose value is "inherit" (or nil, or a horizon of 0) removes the field, so
// it inherits again; a field not mentioned keeps its value. enabled accepts
// on/off/true/false.
func mergeForecastSettings(own map[string]interface{}, changes map[string]interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	for k, v := range own {
		out[k] = v
	}
	for k, v := range changes {
		if v == nil || v == "inherit" || v == "" {
			delete(out, k)
			continue
		}
		switch k {
		case "enabled":
			switch x := v.(type) {
			case bool:
				out[k] = x
			case string:
				switch strings.ToLower(x) {
				case "on", "true", "yes":
					out[k] = true
				case "off", "false", "no":
					out[k] = false
				default:
					return nil, fmt.Errorf("enabled must be on, off or inherit, not %q", x)
				}
			default:
				return nil, fmt.Errorf("enabled must be on, off or inherit")
			}
		case "kind":
			out[k] = v
		case "horizon_hours", "context_days":
			n, ok := toInt(v)
			if !ok {
				return nil, fmt.Errorf("%s must be a number", k)
			}
			if n == 0 {
				delete(out, k)
			} else {
				out[k] = n
			}
		default:
			return nil, fmt.Errorf("unknown setting %q", k)
		}
	}
	return out, nil
}

func toInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case float64:
		return int(x), x == float64(int(x))
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	}
	return 0, false
}

// setForecastSettings reads a level's profile, applies the changes and saves
// it. clear saves it empty, which removes it.
func setForecastSettings(raw func(string) ([]byte, error), put func(string, interface{}) ([]byte, error),
	scope string, id int, changes map[string]interface{}, clear bool) ([]byte, error) {
	cur, err := raw(forecastProfilePath(scope, id))
	if err != nil {
		return nil, err
	}
	var p struct {
		Settings map[string]interface{} `json:"settings"`
	}
	if err := json.Unmarshal(cur, &p); err != nil {
		return nil, fmt.Errorf("unexpected profile: %w", err)
	}
	settings := map[string]interface{}{}
	if !clear {
		if settings, err = mergeForecastSettings(p.Settings, changes); err != nil {
			return nil, err
		}
	}
	return put("/api/forecast/profiles", map[string]interface{}{"scope": scope, "scope_id": id, "settings": settings})
}

func runForecastSettings(_ *cobra.Command, _ []string) {
	c := GetClient()
	changes := map[string]interface{}{}
	if fcEnabled != "" {
		changes["enabled"] = fcEnabled
	}
	if fcKind != "" {
		changes["kind"] = fcKind
	}
	if fcHorizon >= 0 {
		changes["horizon_hours"] = fcHorizon
	}
	var raw []byte
	var err error
	if len(changes) == 0 && !fcClear {
		raw, err = c.RawGet(forecastProfilePath(fcScope, fcScopeID))
	} else {
		raw, err = setForecastSettings(c.RawGet, func(path string, body interface{}) ([]byte, error) {
			var out interface{}
			if err := c.Put(path, body, &out); err != nil {
				return nil, err
			}
			return json.Marshal(out)
		}, fcScope, fcScopeID, changes, fcClear)
	}
	if err != nil {
		PrintError("%v", err)
	}
	fmt.Println(prettyJSON(raw))
}
