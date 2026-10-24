package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// Shifts, planned maintenance and OEE profiles: what forecasts (and the OEE
// page) know about how the plant works. Each is a list with create, update
// and delete; the commands and the MCP tools are made from one description so
// they say the same thing.

type resField struct {
	key, flag, kind, help string // kind: str, int, float, bool, ints, time
}

type resource struct {
	name, singular, short string
	path                  string // list and create; path/{id} to update and delete
	fields                []resField
	required              []string // keys a new one needs
	columns               []string // keys shown by list
}

var shiftsRes = resource{
	name: "shifts", singular: "shift", path: "/api/shifts",
	short: "Work shifts: end-of-shift totals and OEE, and what the forecasts know about working hours",
	fields: []resField{
		{"name", "name", "str", "Name (e.g. Mattino)"},
		{"start_time", "start", "str", "Start, HH:MM local time"},
		{"end_time", "end", "str", "End, HH:MM (before the start: runs past midnight)"},
		{"weekdays", "days", "ints", "Days, comma separated, 0 = Sunday … 6 = Saturday (default 1-5)"},
		{"active", "active", "bool", "Active (true/false)"},
	},
	required: []string{"name", "start_time", "end_time"},
	columns:  []string{"id", "name", "start_time", "end_time", "weekdays", "active", "platform"},
}

var maintenanceRes = resource{
	name: "maintenance", singular: "maintenance window", path: "/api/maintenance",
	short: "Planned maintenance windows: the plant is known to be still then",
	fields: []resField{
		{"title", "title", "str", "Title"},
		{"start_at", "start", "time", "Start: RFC3339 or \"2026-10-25 08:00\" (local time)"},
		{"end_at", "end", "time", "End, same formats"},
		{"reason", "reason", "str", "Reason"},
	},
	required: []string{"title", "start_at", "end_at"},
	columns:  []string{"id", "title", "start_at", "end_at", "active"},
}

var oeeProfilesRes = resource{
	name: "oee-profiles", singular: "OEE profile", path: "/api/oee/profiles",
	short: "OEE profiles: a line's run, pieces and good-pieces tags, and its targets",
	fields: []resField{
		{"name", "name", "str", "Name"},
		{"description", "description", "str", "Description"},
		{"run_time_tag_id", "run-tag", "int", "Tag that is on while the line runs"},
		{"produced_tag_id", "produced-tag", "int", "Pieces counter tag"},
		{"good_tag_id", "good-tag", "int", "Good pieces counter tag"},
		{"target_pieces_per_hour", "target-pph", "float", "Target pieces per hour"},
		{"target_oee", "target-oee", "float", "Target OEE, % (default 85)"},
		{"window_minutes", "window", "int", "Rolling window of the OEE page, minutes (default 480)"},
		{"enabled", "enabled", "bool", "Enabled (true/false)"},
		{"area_id", "area", "int", "Area it belongs to, for grouping"},
		{"gateway_id", "gateway", "int", "Gateway it belongs to, for grouping"},
		{"display_order", "order", "int", "Position in lists"},
		{"respect_shifts", "respect-shifts", "bool", "Count only shift time as planned (true/false)"},
		{"respect_maintenance", "respect-maintenance", "bool", "Leave planned maintenance out of planned time (true/false)"},
		{"availability_mode", "availability-mode", "str", "How availability is measured: values (the run tag, default) or alarms (critical downtime)"},
	},
	required: []string{"name"},
	columns:  []string{"id", "name", "run_time_tag_id", "produced_tag_id", "good_tag_id", "target_pieces_per_hour", "target_oee", "enabled"},
}

// plantResources are the resources with commands and MCP tools.
var plantResources = []*resource{&shiftsRes, &maintenanceRes, &oeeProfilesRes}

// parseResValue reads one field's value from text (a flag, or an MCP
// argument given as a string).
func parseResValue(f *resField, s string) (interface{}, error) {
	s = strings.TrimSpace(s)
	switch f.kind {
	case "int":
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a whole number", f.key, s)
		}
		if n == 0 {
			return nil, nil // 0 clears a reference
		}
		return n, nil
	case "float":
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a number", f.key, s)
		}
		return x, nil
	case "bool":
		switch strings.ToLower(s) {
		case "true", "on", "yes", "1", "si", "sì":
			return true, nil
		case "false", "off", "no", "0":
			return false, nil
		}
		return nil, fmt.Errorf("%s: %q is not true or false", f.key, s)
	case "ints":
		out := []int{}
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a whole number", f.key, p)
			}
			out = append(out, n)
		}
		return out, nil
	case "time":
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Format(time.RFC3339), nil
		}
		for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
				return t.Format(time.RFC3339), nil
			}
		}
		return nil, fmt.Errorf("%s: %q is not a date and time (RFC3339 or 2026-10-25 08:00)", f.key, s)
	}
	return s, nil
}

// resValue reads an MCP argument: JSON numbers, booleans and lists come as
// they are, text is parsed like a flag.
func resValue(f *resField, v interface{}) (interface{}, error) {
	switch x := v.(type) {
	case string:
		return parseResValue(f, x)
	case float64:
		if f.kind == "int" {
			if x == 0 {
				return nil, nil
			}
			return int(x), nil
		}
		if f.kind == "float" {
			return x, nil
		}
	case bool:
		if f.kind == "bool" {
			return x, nil
		}
	case []interface{}:
		if f.kind == "ints" {
			out := []int{}
			for _, e := range x {
				n, ok := e.(float64)
				if !ok {
					return nil, fmt.Errorf("%s: %v is not a whole number", f.key, e)
				}
				out = append(out, int(n))
			}
			return out, nil
		}
	}
	return parseResValue(f, fmt.Sprint(v))
}

// saveResource creates (id 0) or updates one item. An update reads the
// current one from the list and changes only the given fields: the API
// replaces the whole item.
func saveResource(r *resource, raw func(string) ([]byte, error), post, put func(string, interface{}) ([]byte, error),
	id int, changes map[string]interface{}) ([]byte, error) {
	if id == 0 {
		for _, k := range r.required {
			if v, ok := changes[k]; !ok || v == nil || v == "" {
				return nil, fmt.Errorf("a new %s needs %s", r.singular, strings.Join(r.required, ", "))
			}
		}
		return post(r.path, changes)
	}
	data, err := raw(r.path)
	if err != nil {
		return nil, err
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unexpected answer for %s: %w", r.path, err)
	}
	for _, it := range items {
		if n, ok := it["id"].(float64); ok && int(n) == id {
			body := map[string]interface{}{}
			for _, f := range r.fields {
				if v, ok := it[f.key]; ok {
					body[f.key] = v
				}
			}
			for k, v := range changes {
				body[k] = v
			}
			return put(fmt.Sprintf("%s/%d", r.path, id), body)
		}
	}
	return nil, fmt.Errorf("%s %d not found", r.singular, id)
}

// ── Commands ────────────────────────────────────────────────────────────────

func init() {
	for _, r := range plantResources {
		rootCmd.AddCommand(resourceCommand(r))
	}
}

func resourceCommand(r *resource) *cobra.Command {
	root := &cobra.Command{Use: r.name, Short: r.short}
	var id int
	values := map[string]*string{}

	list := &cobra.Command{Use: "list", Short: "Every " + r.singular, Run: func(_ *cobra.Command, _ []string) {
		data, err := GetClient().RawGet(r.path)
		if err != nil {
			PrintError("%v", err)
		}
		printResourceList(r, data)
	}}

	save := func(update bool) func(*cobra.Command, []string) {
		return func(cmd *cobra.Command, _ []string) {
			changes := map[string]interface{}{}
			for i := range r.fields {
				f := &r.fields[i]
				if !cmd.Flags().Changed(f.flag) {
					continue
				}
				v, err := parseResValue(f, *values[f.flag])
				if err != nil {
					PrintError("%v", err)
				}
				changes[f.key] = v
			}
			target := 0
			if update {
				target = id
			}
			c := GetClient()
			data, err := saveResource(r, c.RawGet, c.RawPost, func(path string, body interface{}) ([]byte, error) {
				var out json.RawMessage
				perr := c.Put(path, body, &out)
				return out, perr
			}, target, changes)
			if err != nil {
				PrintError("%v", err)
			}
			if flagJSON {
				fmt.Println(prettyJSON(data))
				return
			}
			PrintSuccess("%s saved", strings.ToUpper(r.singular[:1])+r.singular[1:])
		}
	}
	create := &cobra.Command{Use: "create", Short: "Create a " + r.singular, Run: save(false)}
	update := &cobra.Command{Use: "update", Short: "Change a " + r.singular + "; fields not given keep their value", Run: save(true)}
	del := &cobra.Command{Use: "delete", Short: "Delete a " + r.singular, Run: func(_ *cobra.Command, _ []string) {
		if _, err := GetClient().RawDelete(fmt.Sprintf("%s/%d", r.path, id)); err != nil {
			PrintError("%v", err)
		}
		PrintSuccess("Deleted")
	}}
	for _, c := range []*cobra.Command{create, update} {
		for i := range r.fields {
			f := &r.fields[i]
			if values[f.flag] == nil {
				values[f.flag] = new(string)
			}
			c.Flags().StringVar(values[f.flag], f.flag, "", f.help)
		}
	}
	for _, c := range []*cobra.Command{update, del} {
		c.Flags().IntVar(&id, "id", 0, "ID (required)")
		_ = c.MarkFlagRequired("id")
	}
	root.AddCommand(list, create, update, del)
	return root
}

func printResourceList(r *resource, data []byte) {
	if flagJSON {
		fmt.Println(prettyJSON(data))
		return
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(data, &items); err != nil {
		fmt.Println(string(data))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, ColorBold+strings.ToUpper(strings.Join(r.columns, "\t"))+ColorReset)
	for _, it := range items {
		cells := make([]string, len(r.columns))
		for i, k := range r.columns {
			switch v := it[k].(type) {
			case nil:
				cells[i] = "—"
			case float64:
				cells[i] = strconv.FormatFloat(v, 'f', -1, 64)
			default:
				cells[i] = fmt.Sprint(v)
			}
		}
		_, _ = fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush()
}

// ── MCP ─────────────────────────────────────────────────────────────────────

// resourceTools are list_<x>, save_<x> and delete_<x> for each resource.
func resourceTools() []mcpTool {
	var out []mcpTool
	for _, r := range plantResources {
		n := mcpName(r)
		props := map[string]interface{}{"id": propInt("The " + r.singular + " to change; omit to create one")}
		for _, f := range r.fields {
			switch f.kind {
			case "int":
				props[f.key] = propInt(f.help + " (0 removes it)")
			case "bool":
				props[f.key] = propBool(f.help)
			default:
				props[f.key] = propStr(f.help)
			}
		}
		out = append(out,
			mcpTool{Name: "list_" + n, Description: "Every " + r.singular + " of the current organization. " + r.short,
				InputSchema: jsonSchema(map[string]interface{}{})},
			mcpTool{Name: "save_" + n, Description: "Create a " + r.singular + " (no id: " + strings.Join(r.required, ", ") +
				" required) or change one (fields not given keep their value). Admin.", InputSchema: jsonSchema(props)},
			mcpTool{Name: "delete_" + n, Description: "Delete a " + r.singular + ". Admin.",
				InputSchema: jsonSchemaRequired(map[string]interface{}{"id": propInt("The ID")}, []string{"id"})},
		)
	}
	return out
}

func mcpName(r *resource) string {
	return map[string]string{"shifts": "shifts", "maintenance": "maintenance", "oee-profiles": "oee_profiles"}[r.name]
}

// callResourceTool runs a resource tool; ok is false for any other name.
func (s *mcpServer) callResourceTool(name string, args map[string]interface{}) (string, bool, error) {
	for _, r := range plantResources {
		n := mcpName(r)
		switch name {
		case "list_" + n:
			out, err := s.toolGet(r.path)
			return out, true, err
		case "delete_" + n:
			id := intArg(args, "id")
			if id == 0 {
				return "", true, fmt.Errorf("id is required")
			}
			if _, err := s.toolDelete(fmt.Sprintf("%s/%d", r.path, id)); err != nil {
				return "", true, err
			}
			return fmt.Sprintf("%s %d deleted", r.singular, id), true, nil
		case "save_" + n:
			changes := map[string]interface{}{}
			for i := range r.fields {
				f := &r.fields[i]
				v, given := args[f.key]
				if !given {
					continue
				}
				val, err := resValue(f, v)
				if err != nil {
					return "", true, err
				}
				changes[f.key] = val
			}
			data, err := saveResource(r, s.client.RawGet, s.client.RawPost, func(path string, body interface{}) ([]byte, error) {
				out, perr := s.toolPut(path, body)
				return []byte(out), perr
			}, intArg(args, "id"), changes)
			if err != nil {
				return "", true, err
			}
			return prettyJSON(data), true, nil
		}
	}
	return "", false, nil
}
