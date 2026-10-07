package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// ── openedge gateways configure ─────────────────────────────────────────────
//
// One device per gateway means a setting such as stale_after_minutes lives on
// every gateway of a site, and changing it in the UI is one form per device.
// This merges the given keys into the connection_config of every gateway the
// filters select, leaving the rest of each configuration as it is.

var (
	gwCfgSet    string
	gwCfgUnset  []string
	gwCfgSite   int
	gwCfgAreas  []int
	gwCfgDriver string
	gwCfgName   string
	gwCfgAll    bool
	gwCfgApply  bool
)

type gatewayForConfig struct {
	ID               int                    `json:"id"`
	AreaID           int                    `json:"area_id"`
	Name             string                 `json:"name"`
	DriverType       string                 `json:"driver_type"`
	ConnectionConfig map[string]interface{} `json:"connection_config"`
}

var gatewaysConfigureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Change connection settings on many gateways at once",
	Long: `Merges --set into the connection_config of every gateway the filters
select; keys not named are left as they are. Previews unless --apply.

Every MiDo gateway of site 1 offline after a day without data:
  openedge gateways configure --site 1 --driver MQTT \
    --set '{"stale_after_minutes":1440,"alarms_on_history":true}'
  openedge gateways configure --site 1 --driver MQTT \
    --set '{"stale_after_minutes":1440,"alarms_on_history":true}' --apply

Filters combine: --site, --area (repeatable), --driver, --name (substring).
With none of them, --all is required.`,
	Run: runGatewaysConfigure,
}

func runGatewaysConfigure(cmd *cobra.Command, args []string) {
	var set map[string]interface{}
	if gwCfgSet != "" {
		if err := json.Unmarshal([]byte(gwCfgSet), &set); err != nil {
			PrintError("--set is not a JSON object: %v", err)
		}
	}
	if len(set) == 0 && len(gwCfgUnset) == 0 {
		PrintError("nothing to change: give --set and/or --unset")
	}
	if gwCfgSite == 0 && len(gwCfgAreas) == 0 && gwCfgDriver == "" && gwCfgName == "" && !gwCfgAll {
		PrintError("no filter: give --site, --area, --driver or --name, or --all for every gateway")
	}

	selected := selectGatewaysForConfig()
	if len(selected) == 0 {
		PrintError("no gateway matches the filters")
	}

	changed := 0
	for _, g := range selected {
		next, diff := mergeGatewayConfig(g.ConnectionConfig, set, gwCfgUnset)
		if len(diff) == 0 {
			fmt.Printf("  %4d  %-40s  unchanged\n", g.ID, g.Name)
			continue
		}
		changed++
		fmt.Printf("  %4d  %-40s  %s\n", g.ID, g.Name, strings.Join(diff, ", "))
		if !gwCfgApply {
			continue
		}
		if err := GetClient().Put("/api/gateways/"+strconv.Itoa(g.ID),
			map[string]interface{}{"connection_config": next}, nil); err != nil {
			PrintError("gateway %d (%s): %v — gateways listed above it were updated", g.ID, g.Name, err)
		}
	}

	switch {
	case changed == 0:
		PrintSuccess("%d gateways, nothing to change", len(selected))
	case gwCfgApply:
		PrintSuccess("%d of %d gateways updated; each driver reloads its settings", changed, len(selected))
	default:
		fmt.Printf("\n%d of %d gateways would change. Run again with --apply to write.\n", changed, len(selected))
	}
}

// selectGatewaysForConfig lists the organization's gateways and keeps those
// the filters select.
func selectGatewaysForConfig() []gatewayForConfig {
	var all []gatewayForConfig
	if err := GetClient().Get("/api/gateways", &all); err != nil {
		PrintError("%v", err)
	}

	areas := map[int]bool{}
	for _, a := range gwCfgAreas {
		areas[a] = true
	}
	if gwCfgSite > 0 {
		var siteAreas []struct {
			ID int `json:"id"`
		}
		if err := GetClient().Get("/api/areas?site_id="+strconv.Itoa(gwCfgSite), &siteAreas); err != nil {
			PrintError("%v", err)
		}
		inSite := map[int]bool{}
		for _, a := range siteAreas {
			inSite[a.ID] = true
		}
		if len(areas) == 0 {
			areas = inSite
		} else {
			for a := range areas {
				if !inSite[a] {
					delete(areas, a)
				}
			}
		}
		if len(areas) == 0 {
			PrintError("site %d has no areas (or none of the --area given)", gwCfgSite)
		}
	}

	var out []gatewayForConfig
	for _, g := range all {
		if len(areas) > 0 && !areas[g.AreaID] {
			continue
		}
		if gwCfgDriver != "" && !strings.EqualFold(g.DriverType, gwCfgDriver) {
			continue
		}
		if gwCfgName != "" && !strings.Contains(strings.ToLower(g.Name), strings.ToLower(gwCfgName)) {
			continue
		}
		out = append(out, g)
	}
	return out
}

// mergeGatewayConfig returns cur with set applied and unset removed, and a
// description of each key that actually changes.
func mergeGatewayConfig(cur, set map[string]interface{}, unset []string) (map[string]interface{}, []string) {
	next := make(map[string]interface{}, len(cur)+len(set))
	for k, v := range cur {
		next[k] = v
	}
	var diff []string
	for k, v := range set {
		old, had := cur[k]
		if had && jsonEqual(old, v) {
			continue
		}
		next[k] = v
		if had {
			diff = append(diff, fmt.Sprintf("%s: %s → %s", k, jsonText(old), jsonText(v)))
		} else {
			diff = append(diff, fmt.Sprintf("%s: %s", k, jsonText(v)))
		}
	}
	for _, k := range unset {
		if _, had := next[k]; had {
			delete(next, k)
			diff = append(diff, k+" removed")
		}
	}
	return next, diff
}

func jsonText(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// jsonEqual compares as JSON, so 1440 read back from the server (a float64)
// equals 1440 given on the command line.
func jsonEqual(a, b interface{}) bool { return jsonText(a) == jsonText(b) }

func init() {
	f := gatewaysConfigureCmd.Flags()
	f.StringVar(&gwCfgSet, "set", "", `Keys to set, as a JSON object (e.g. '{"stale_after_minutes":1440}')`)
	f.StringSliceVar(&gwCfgUnset, "unset", nil, "Keys to remove (comma-separated)")
	f.IntVar(&gwCfgSite, "site", 0, "Only gateways in this site")
	f.IntSliceVar(&gwCfgAreas, "area", nil, "Only gateways in this area (repeatable)")
	f.StringVar(&gwCfgDriver, "driver", "", "Only gateways of this driver type (e.g. MQTT)")
	f.StringVar(&gwCfgName, "name", "", "Only gateways whose name contains this")
	f.BoolVar(&gwCfgAll, "all", false, "Every gateway of the organization (when no other filter is given)")
	f.BoolVar(&gwCfgApply, "apply", false, "Write the changes (default: preview only)")
	gatewaysCmd.AddCommand(gatewaysConfigureCmd)
}
