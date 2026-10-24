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

// Machines (assets): a named group of the organization's tags, with an
// optional BOOL tag that says the machine is running. Their health, 0–100,
// comes from the members' forecasts: the weakest part sets it.

var forecastMachinesCmd = &cobra.Command{
	Use:   "machines",
	Short: "Machines: tags grouped by equipment, with a health score from their forecasts",
	Long: `Machines group the tags of one piece of equipment. Their health (0–100) is
100 minus the worst problem among their tags: an alarm rule the forecast may
trip, or recent values outside the forecast. With a state tag (running/stopped)
the forecasts learn from running time only.

  openedge forecast machines list
  openedge forecast machines show --id 4
  openedge forecast machines create --name "Compressore 3" --tags 120,121,122 --state-tag 119
  openedge forecast machines update --id 4 --tags 120,121 --state-tag 0
  openedge forecast machines delete --id 4
  openedge forecast machines from-gateways`,
}

var forecastMachinesListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every machine of the organization with its health",
	Run:   runMachinesList,
}

var forecastMachinesShowCmd = &cobra.Command{
	Use:   "show",
	Short: "One machine: health, issues and every tag with its forecast state",
	Run:   runMachinesShow,
}

var forecastMachinesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a machine (admin)",
	Run:   runMachinesSave,
}

var forecastMachinesUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Change a machine (admin); fields not given keep their value",
	Run:   runMachinesSave,
}

var forecastMachinesDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a machine (admin); its tags and forecasts stay",
	Run:   runMachinesDelete,
}

var forecastMachinesFromGatewaysCmd = &cobra.Command{
	Use:   "from-gateways",
	Short: "One machine per gateway, with its historized tags (admin); existing names are left alone",
	Run:   runMachinesFromGateways,
}

var (
	fmID          int
	fmName        string
	fmDescription string
	fmTags        string
	fmStateTag    int
)

func init() {
	for _, c := range []*cobra.Command{forecastMachinesShowCmd, forecastMachinesUpdateCmd, forecastMachinesDeleteCmd} {
		c.Flags().IntVar(&fmID, "id", 0, "Machine ID (required)")
		_ = c.MarkFlagRequired("id")
	}
	for _, c := range []*cobra.Command{forecastMachinesCreateCmd, forecastMachinesUpdateCmd} {
		c.Flags().StringVar(&fmName, "name", "", "Name, unique in the organization")
		c.Flags().StringVar(&fmDescription, "description", "", "Description")
		c.Flags().StringVar(&fmTags, "tags", "", "Member tag IDs, comma separated")
		c.Flags().IntVar(&fmStateTag, "state-tag", -1, "BOOL tag that is true while the machine runs (0 = none)")
	}
	_ = forecastMachinesCreateCmd.MarkFlagRequired("name")
	_ = forecastMachinesCreateCmd.MarkFlagRequired("tags")

	forecastMachinesCmd.AddCommand(forecastMachinesListCmd, forecastMachinesShowCmd, forecastMachinesCreateCmd,
		forecastMachinesUpdateCmd, forecastMachinesDeleteCmd, forecastMachinesFromGatewaysCmd)
	forecastCmd.AddCommand(forecastMachinesCmd)
}

type machineIssue struct {
	TagID   int           `json:"tag_id"`
	Alias   string        `json:"alias"`
	Kind    string        `json:"kind"`
	Points  int           `json:"points"`
	Risk    *forecastRisk `json:"risk,omitempty"`
	Outside int           `json:"outside,omitempty"`
	Checked int           `json:"checked,omitempty"`
}

type machineOut struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	StateTagID  *int   `json:"state_tag_id"`
	StateAlias  string `json:"state_alias,omitempty"`
	Members     []struct {
		TagID       int    `json:"tag_id"`
		Alias       string `json:"alias"`
		Status      string `json:"status"`
		Unit        string `json:"unit,omitempty"`
		GatewayName string `json:"gateway_name"`
		Kind        string `json:"kind,omitempty"`
	} `json:"members"`
	Health struct {
		Score   *int           `json:"score"`
		Level   string         `json:"level"`
		Issues  []machineIssue `json:"issues"`
		Known   int            `json:"known"`
		Members int            `json:"members"`
	} `json:"health"`
}

// machineInput is the body of POST and PUT /api/forecast/assets.
type machineInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StateTagID  *int   `json:"state_tag_id"`
	TagIDs      []int  `json:"tag_ids"`
}

// machineChanges are the fields given on the command line or by an agent;
// nil keeps the machine's current value.
type machineChanges struct {
	Name        *string
	Description *string
	Tags        []int // nil: keep
	StateTag    *int  // 0: none
}

// parseIDList reads "1, 2,3" into IDs.
func parseIDList(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%q is not a tag ID", f)
		}
		out = append(out, n)
	}
	return out, nil
}

// applyMachineChanges merges changes onto a machine (nil for a new one).
func applyMachineChanges(m *machineOut, ch *machineChanges) machineInput {
	var in machineInput
	if m != nil {
		in = machineInput{Name: m.Name, Description: m.Description, StateTagID: m.StateTagID}
		for _, t := range m.Members {
			in.TagIDs = append(in.TagIDs, t.TagID)
		}
	}
	if ch.Name != nil {
		in.Name = *ch.Name
	}
	if ch.Description != nil {
		in.Description = *ch.Description
	}
	if ch.Tags != nil {
		in.TagIDs = ch.Tags
	}
	if ch.StateTag != nil {
		if *ch.StateTag > 0 {
			id := *ch.StateTag
			in.StateTagID = &id
		} else {
			in.StateTagID = nil
		}
	}
	if in.TagIDs == nil {
		in.TagIDs = []int{}
	}
	return in
}

// saveMachine creates (id 0) or updates a machine, reading the current one
// first so that what is not changed stays as it is.
func saveMachine(raw func(string) ([]byte, error), post, put func(string, interface{}) ([]byte, error),
	id int, ch *machineChanges) ([]byte, error) {
	if id == 0 {
		if ch.Name == nil || *ch.Name == "" || len(ch.Tags) == 0 {
			return nil, fmt.Errorf("a new machine needs a name and at least one tag")
		}
		return post("/api/forecast/assets", applyMachineChanges(nil, ch))
	}
	cur, err := raw(fmt.Sprintf("/api/forecast/assets/%d", id))
	if err != nil {
		return nil, err
	}
	var m machineOut
	if err := json.Unmarshal(cur, &m); err != nil {
		return nil, fmt.Errorf("unexpected answer for machine %d: %w", id, err)
	}
	return put(fmt.Sprintf("/api/forecast/assets/%d", id), applyMachineChanges(&m, ch))
}

func healthText(m *machineOut) string {
	if m.Health.Score == nil {
		return "— " + m.Health.Level
	}
	return fmt.Sprintf("%d %s", *m.Health.Score, m.Health.Level)
}

func issueText(is *machineIssue) string {
	if is.Risk != nil {
		return fmt.Sprintf("%s: %s %s %g at %s", is.Alias, is.Risk.Likelihood, is.Risk.Type, is.Risk.Threshold, is.Risk.At)
	}
	return fmt.Sprintf("%s: %d of %d values outside the forecast", is.Alias, is.Outside, is.Checked)
}

func runMachinesList(_ *cobra.Command, _ []string) {
	var out struct {
		Assets []machineOut `json:"assets"`
	}
	if err := GetClient().Get("/api/forecast/assets", &out); err != nil {
		PrintError("%v", err)
	}
	if flagJSON {
		data, _ := json.MarshalIndent(out.Assets, "", "  ")
		fmt.Println(string(data))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tMACHINE\tHEALTH\tTAGS\tRELIABLE\tWORST"+ColorReset)
	for i := range out.Assets {
		m := &out.Assets[i]
		worst := "—"
		if len(m.Health.Issues) > 0 {
			worst = issueText(&m.Health.Issues[0])
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%d\t%s\n", m.ID, m.Name, healthText(m), m.Health.Members, m.Health.Known, worst)
	}
	_ = w.Flush()
}

func printMachine(raw []byte) {
	if flagJSON {
		fmt.Println(prettyJSON(raw))
		return
	}
	var m machineOut
	if err := json.Unmarshal(raw, &m); err != nil {
		fmt.Println(string(raw))
		return
	}
	fmt.Printf("Machine %d: %s · health %s\n", m.ID, m.Name, healthText(&m))
	if m.Description != "" {
		fmt.Println(m.Description)
	}
	if m.StateAlias != "" {
		fmt.Printf("Running state: %s (forecasts learn from running time only)\n", m.StateAlias)
	}
	for i := range m.Health.Issues {
		fmt.Printf("  - %s (−%d)\n", issueText(&m.Health.Issues[i]), m.Health.Issues[i].Points)
	}
	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tTAG\tGATEWAY\tKIND\tSTATUS"+ColorReset)
	for _, t := range m.Members {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", t.TagID, t.Alias, t.GatewayName, orDash(t.Kind), t.Status)
	}
	_ = w.Flush()
}

func runMachinesShow(_ *cobra.Command, _ []string) {
	raw, err := GetClient().RawGet(fmt.Sprintf("/api/forecast/assets/%d", fmID))
	if err != nil {
		PrintError("%v", err)
	}
	printMachine(raw)
}

func runMachinesSave(cmd *cobra.Command, _ []string) {
	var ch machineChanges
	if cmd.Flags().Changed("name") {
		ch.Name = &fmName
	}
	if cmd.Flags().Changed("description") {
		ch.Description = &fmDescription
	}
	if cmd.Flags().Changed("tags") {
		tags, err := parseIDList(fmTags)
		if err != nil {
			PrintError("%v", err)
		}
		ch.Tags = tags
		if ch.Tags == nil {
			ch.Tags = []int{}
		}
	}
	if cmd.Flags().Changed("state-tag") {
		ch.StateTag = &fmStateTag
	}
	id := 0
	if cmd.Name() == "update" {
		id = fmID
	}
	client := GetClient()
	raw, err := saveMachine(client.RawGet, client.RawPost, func(path string, body interface{}) ([]byte, error) {
		var out json.RawMessage
		err := client.Put(path, body, &out)
		return out, err
	}, id, &ch)
	if err != nil {
		PrintError("%v", err)
	}
	printMachine(raw)
}

func runMachinesDelete(_ *cobra.Command, _ []string) {
	if _, err := GetClient().RawDelete(fmt.Sprintf("/api/forecast/assets/%d", fmID)); err != nil {
		PrintError("%v", err)
	}
	PrintSuccess("Machine %d deleted", fmID)
}

func runMachinesFromGateways(_ *cobra.Command, _ []string) {
	var out struct {
		Created int `json:"created"`
	}
	raw, err := GetClient().RawPost("/api/forecast/assets/from-gateways", map[string]interface{}{})
	if err != nil {
		PrintError("%v", err)
	}
	if flagJSON {
		fmt.Println(prettyJSON(raw))
		return
	}
	_ = json.Unmarshal(raw, &out)
	PrintSuccess("%d machines created", out.Created)
}

// machineChangesFromArgs reads save_machine's arguments; tag_ids may be a
// comma-separated string or a list of numbers.
func machineChangesFromArgs(args map[string]interface{}) (*machineChanges, error) {
	var ch machineChanges
	if _, ok := args["name"]; ok {
		v := strArg(args, "name")
		ch.Name = &v
	}
	if _, ok := args["description"]; ok {
		v := strArg(args, "description")
		ch.Description = &v
	}
	if v, ok := args["state_tag_id"]; ok && v != nil {
		n := intArg(args, "state_tag_id")
		ch.StateTag = &n
	}
	switch v := args["tag_ids"].(type) {
	case nil:
	case []interface{}:
		ch.Tags = []int{}
		for _, x := range v {
			n, ok := x.(float64)
			if !ok || n <= 0 || n != float64(int(n)) {
				return nil, fmt.Errorf("tag_ids: %v is not a tag ID", x)
			}
			ch.Tags = append(ch.Tags, int(n))
		}
	default:
		tags, err := parseIDList(strArg(args, "tag_ids"))
		if err != nil {
			return nil, fmt.Errorf("tag_ids: %w", err)
		}
		ch.Tags = tags
		if ch.Tags == nil {
			ch.Tags = []int{}
		}
	}
	return &ch, nil
}
