package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The forecast setup checklist, as on the Forecasts page: what forecasts
// have to work with, what is missing, and which command or tool sets it up.

var forecastSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "What forecasts have to work with and what to set up next, step by step",
	Run:   runForecastSetup,
}

func init() { forecastCmd.AddCommand(forecastSetupCmd) }

type setupStep struct {
	Key    string `json:"key"`
	State  string `json:"state"` // done, todo, wait, optional
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// How: the CLI command and the MCP tool that set it up.
	Command string `json:"command,omitempty"`
	Tool    string `json:"tool,omitempty"`
}

// getter reads one API path into v; false when it cannot (a module off).
type getter func(path string, v interface{}) bool

// forecastSetup reads what the organization has. A list it cannot read (a
// module switched off) leaves its step out.
func forecastSetup(raw func(string) ([]byte, error)) ([]setupStep, error) {
	get := func(path string, v interface{}) bool {
		data, err := raw(path)
		return err == nil && json.Unmarshal(data, v) == nil
	}
	var ov forecastOverview
	if !get("/api/forecast/overview", &ov) {
		return nil, fmt.Errorf("cannot read /api/forecast/overview: is the forecast module on for this organization?")
	}
	steps := []setupStep{setupTags(&ov), setupModel(&ov)}
	steps = append(steps, setupMachines(get)...)
	for _, f := range []func(getter) (setupStep, bool){setupShifts, setupMaintenance, setupOEE} {
		if st, ok := f(get); ok {
			steps = append(steps, st)
		}
	}
	return append(steps, setupLevels(get)), nil
}

func setupTags(ov *forecastOverview) setupStep {
	ok, learning := 0, 0
	for i := range ov.Tags {
		switch ov.Tags[i].Status {
		case "ok":
			ok++
		case "learning", "pending":
			learning++
		}
	}
	st := setupStep{Key: "tags", Title: "Tags with history", Command: "openedge forecast list", Tool: "get_forecast_overview"}
	switch {
	case len(ov.Tags) == 0:
		st.State, st.Detail = "todo", "no numeric or digital tag has history on: forecasts learn from history"
		st.Command, st.Tool = "openedge tags …", "create_tag (historize: true)"
	case ok == 0:
		st.State, st.Detail = "wait", fmt.Sprintf("%d tags learning: each needs 24 values before its first forecast", len(ov.Tags))
	default:
		st.State, st.Detail = "done", fmt.Sprintf("%d of %d tags have a reliable forecast, %d still learning", ok, len(ov.Tags), learning)
	}
	return st
}

func setupModel(ov *forecastOverview) setupStep {
	st := setupStep{Key: "model", Title: "Timestral model", State: "optional",
		Detail: "statistical engine only (enough to start); Timestral is turned on on the server: make forecast-up"}
	for _, e := range ov.Engines {
		if e != "statistical" {
			st.State, st.Detail = "done", "on: "+strings.Join(ov.Engines, ", ")
		}
	}
	return st
}

func setupMachines(get getter) []setupStep {
	var assets struct {
		Assets []machineOut `json:"assets"`
	}
	if !get("/api/forecast/assets", &assets) {
		return nil
	}
	n := len(assets.Assets)
	if n == 0 {
		return []setupStep{{Key: "machines", Title: "Machines", State: "todo",
			Detail:  "group each piece of equipment's tags to get a health score; start with one per gateway",
			Command: "openedge forecast machines from-gateways", Tool: "create_machines_from_gateways"}}
	}
	with := 0
	for i := range assets.Assets {
		if assets.Assets[i].StateTagID != nil {
			with++
		}
	}
	state := setupStep{Key: "state", Title: "Running tags", State: "optional",
		Command: "openedge forecast machines update --id N --state-tag T", Tool: "save_machine (state_tag_id)",
		Detail: fmt.Sprintf("%d of %d machines have a running tag; only needed for machines that stop", with, n)}
	if with > 0 {
		state.State = "done"
	}
	return []setupStep{{Key: "machines", Title: "Machines", State: "done", Detail: fmt.Sprintf("%d machines, each with its 0–100 health", n),
		Command: "openedge forecast machines list", Tool: "list_machines"}, state}
}

func setupShifts(get getter) (setupStep, bool) {
	var shifts []struct {
		Name      string `json:"name"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Active    bool   `json:"active"`
	}
	if !get("/api/shifts", &shifts) {
		return setupStep{}, false
	}
	var on []string
	for _, s := range shifts {
		if s.Active {
			on = append(on, fmt.Sprintf("%s %s–%s", s.Name, s.StartTime, s.EndTime))
		}
	}
	st := setupStep{Key: "shifts", Title: "Shifts", Command: "openedge shifts create --name Mattino --start 06:00 --end 14:00", Tool: "save_shifts",
		State: "todo", Detail: "without shifts, totals and OEE go to the end of the day and month only"}
	if len(on) > 0 {
		st.State, st.Detail = "done", strings.Join(on, " · ")+": used for end-of-shift totals and OEE"
	}
	return st, true
}

func setupMaintenance(get getter) (setupStep, bool) {
	var maint []struct {
		EndAt string `json:"end_at"`
	}
	if !get("/api/maintenance", &maint) {
		return setupStep{}, false
	}
	ahead := 0
	for _, m := range maint {
		if t, err := time.Parse(time.RFC3339, m.EndAt); err == nil && t.After(time.Now()) {
			ahead++
		}
	}
	st := setupStep{Key: "maintenance", Title: "Planned maintenance", State: "optional",
		Command: "openedge maintenance create --title … --start \"2026-11-03 08:00\" --end \"2026-11-03 12:00\"", Tool: "save_maintenance",
		Detail: "none planned: entering them keeps the model from taking a planned stop for a fault"}
	if ahead > 0 {
		st.State, st.Detail = "done", fmt.Sprintf("%d windows planned", ahead)
	}
	return st, true
}

func setupOEE(get getter) (setupStep, bool) {
	var oee struct {
		Profiles []oeeForecastOut `json:"profiles"`
	}
	if !get("/api/forecast/oee", &oee) {
		return setupStep{}, false
	}
	st := setupStep{Key: "oee", Title: "OEE profiles", Command: "openedge oee-profiles create --name … --produced-tag N --target-pph X", Tool: "save_oee_profiles"}
	ready := 0
	for i := range oee.Profiles {
		if oee.Profiles[i].OEE != nil {
			ready++
		}
	}
	switch {
	case len(oee.Profiles) == 0:
		st.State, st.Detail = "optional", "no OEE profile: only where pieces are counted"
	case ready == len(oee.Profiles):
		st.State, st.Detail = "done", fmt.Sprintf("%d profiles with an end-of-shift OEE forecast", ready)
		st.Command, st.Tool = "openedge forecast production", "get_production_forecast"
	default:
		st.State, st.Detail = "todo", fmt.Sprintf("%d of %d profiles have an OEE forecast: the others need a reliable forecast on their pieces counter and a pieces-per-hour target", ready, len(oee.Profiles))
	}
	return st, true
}

func setupLevels(get getter) setupStep {
	var custom struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	st := setupStep{Key: "levels", Title: "Horizon and history", State: "done", Command: "openedge forecast settings --scope … --id …", Tool: "set_forecast_settings",
		Detail: "defaults for the whole organization: 24 h ahead, 28 days of history"}
	if get("/api/forecast/profiles/custom", &custom) && len(custom.Profiles) > 0 {
		st.Detail = fmt.Sprintf("%d levels have their own settings, the others inherit", len(custom.Profiles))
	}
	return st
}

func runForecastSetup(_ *cobra.Command, _ []string) {
	steps, err := forecastSetup(GetClient().RawGet)
	if err != nil {
		PrintError("%v", err)
	}
	if flagJSON {
		data, _ := json.MarshalIndent(steps, "", "  ")
		fmt.Println(string(data))
		return
	}
	mark := map[string]string{"done": ColorGreen + "✓" + ColorReset, "todo": ColorYellow + "○" + ColorReset, "wait": "…", "optional": "·"}
	done, total := 0, 0
	for _, s := range steps {
		if s.State != "optional" {
			total++
			if s.State != "todo" {
				done++
			}
		}
	}
	fmt.Printf("Forecast setup: %d of %d\n\n", done, total)
	for _, s := range steps {
		opt := ""
		if s.State == "optional" {
			opt = " (optional)"
		}
		fmt.Printf("%s %s%s\n  %s\n", mark[s.State], ColorBold+s.Title+ColorReset, opt, s.Detail)
		if s.State != "done" && s.Command != "" {
			fmt.Printf("  → %s\n", s.Command)
		}
		fmt.Println()
	}
}
