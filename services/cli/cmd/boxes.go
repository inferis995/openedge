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

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
)

// The same decisions the web UI offers under Organization → Edge, for anyone
// who would rather script them: which PLCs a box polls, and which the server.
//
// The rule behind every command here: each gateway is polled by the box it is
// assigned to, or by the server. Never two, never none.

var boxesCmd = &cobra.Command{
	Use:   "boxes",
	Short: "Edge boxes: who polls which PLC",
	Long: `Edge boxes sit next to PLCs the server cannot reach.

Each gateway is polled by the box it is assigned to, or by the server —
never two, never none. A new box polls nothing until a gateway is assigned
to it; a box with scope "all" also polls every gateway assigned to no box,
for installations where the server is in the cloud and polls nothing.`,
}

var boxesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List boxes and how many gateways each polls",
	Run:   runBoxesList,
}

var boxesScopeCmd = &cobra.Command{
	Use:   "scope <box-id> <assigned|all>",
	Short: "Set what a box polls besides the gateways assigned to it",
	Args:  cobra.ExactArgs(2),
	Run:   runBoxesScope,
}

var boxesRenameCmd = &cobra.Command{
	Use:   "rename <box-id> <name>",
	Short: "Rename a box",
	Args:  cobra.ExactArgs(2),
	Run:   runBoxesRename,
}

var boxesRemoveCmd = &cobra.Command{
	Use:   "remove <box-id>",
	Short: "Remove a box: revokes its key, its gateways go back to the server",
	Args:  cobra.ExactArgs(1),
	Run:   runBoxesRemove,
}

var gatewaysAssignCmd = &cobra.Command{
	Use:   "assign <gateway-id> <server|box-id>",
	Short: "Choose who polls a gateway: the server, or a box",
	Args:  cobra.ExactArgs(2),
	Run:   runGatewaysAssign,
}

var boxesRemoveYes bool

func init() {
	boxesRemoveCmd.Flags().BoolVar(&boxesRemoveYes, "yes", false, "Do not ask for confirmation")
	boxesCmd.AddCommand(boxesListCmd, boxesScopeCmd, boxesRenameCmd, boxesRemoveCmd)
	gatewaysCmd.AddCommand(gatewaysAssignCmd)
	rootCmd.AddCommand(boxesCmd)
}

type edgeBox struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	Scope        string     `json:"scope"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	AgentVersion string     `json:"agent_version"`
	Gateways     int        `json:"gateways"`
}

type edgeBoxes struct {
	Agents           []edgeBox `json:"agents"`
	ServerGateways   int       `json:"server_gateways"`
	ServerPolls      bool      `json:"server_polls"`
	UnpolledGateways int       `json:"unpolled_gateways"`
}

func orgOrExit(client *api.Client) int {
	org := client.OrgID()
	if org == 0 {
		PrintError("no organization selected: pass --org <id> or set OPENEDGE_ORG_ID")
	}
	return org
}

// Written out in full rather than composed, so test/config/routes_test.go can
// read them and check each one reaches a route.
func boxesListPath(org int) string {
	return fmt.Sprintf("/api/organizations/%d/edge-agents", org)
}

func boxPath(org, id int) string {
	return fmt.Sprintf("/api/organizations/%d/edge-agents/%d", org, id)
}

func runBoxesList(cmd *cobra.Command, args []string) {
	client := GetClient()
	org := orgOrExit(client)

	var out edgeBoxes
	if err := client.Get(boxesListPath(org), &out); err != nil {
		PrintError("%v", err)
	}
	if flagJSON {
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	if out.ServerPolls {
		fmt.Printf("Server: polls %d gateway(s) directly — those assigned to no box.\n\n", out.ServerGateways)
	} else {
		fmt.Printf("Server: polls no PLCs (no driver-manager running on it).\n\n")
	}
	if out.UnpolledGateways > 0 {
		PrintWarning("%d gateway(s) are polled by nobody: assign them to a box, or set one box to scope \"all\"",
			out.UnpolledGateways)
		fmt.Println()
	}
	if len(out.Agents) == 0 {
		fmt.Println("No boxes. None are needed where the server reaches every PLC.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, ColorBold+"ID\tNAME\tSTATE\tSCOPE\tGATEWAYS\tVERSION"+ColorReset)
	for _, b := range out.Agents {
		state := ColorYellow + "never connected" + ColorReset
		if b.LastSeenAt != nil {
			if time.Since(*b.LastSeenAt) < 2*time.Minute {
				state = ColorGreen + "online" + ColorReset
			} else {
				state = ColorRed + "offline" + ColorReset
			}
		}
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%s\n", b.ID, b.Name, state, b.Scope, b.Gateways, b.AgentVersion)
	}
	_ = w.Flush()
}

func parseID(s, what string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		PrintError("%s must be a positive number, got %q", what, s)
	}
	return n
}

func runBoxesScope(cmd *cobra.Command, args []string) {
	client := GetClient()
	org := orgOrExit(client)
	id := parseID(args[0], "box id")
	scope := strings.ToLower(args[1])
	if scope != "assigned" && scope != "all" {
		PrintError(`scope must be "assigned" (only the gateways given to the box) or "all" (also every gateway given to no box)`)
	}
	if err := client.Put(boxPath(org, id), map[string]string{"scope": scope}, nil); err != nil {
		PrintError("%v", err)
	}
	PrintSuccess("box %d now polls: %s", id, map[string]string{
		"assigned": "only the gateways assigned to it",
		"all":      "its gateways and every gateway assigned to no box",
	}[scope])
}

func runBoxesRename(cmd *cobra.Command, args []string) {
	client := GetClient()
	org := orgOrExit(client)
	id := parseID(args[0], "box id")
	if err := client.Put(boxPath(org, id), map[string]string{"name": args[1]}, nil); err != nil {
		PrintError("%v", err)
	}
	PrintSuccess("box %d renamed to %q", id, args[1])
}

func runBoxesRemove(cmd *cobra.Command, args []string) {
	client := GetClient()
	org := orgOrExit(client)
	id := parseID(args[0], "box id")
	if !boxesRemoveYes {
		fmt.Printf("Remove box %d? Its key is revoked at once and its gateways go back to the server. [y/N] ", id)
		var answer string
		_, _ = fmt.Scanln(&answer)
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" && a != "s" && a != "si" {
			fmt.Println("Not removed.")
			return
		}
	}
	if _, err := client.RawDelete(boxPath(org, id)); err != nil {
		PrintError("%v", err)
	}
	PrintSuccess("box %d removed; its key is revoked and its gateways are the server's again", id)
}

func runGatewaysAssign(cmd *cobra.Command, args []string) {
	client := GetClient()
	gw := parseID(args[0], "gateway id")
	to := 0
	if !strings.EqualFold(args[1], "server") {
		to = parseID(args[1], `box id (or "server")`)
	}
	if err := client.Put("/api/gateways/"+strconv.Itoa(gw), map[string]int{"edge_agent_id": to}, nil); err != nil {
		PrintError("%v", err)
	}
	if to == 0 {
		PrintSuccess("gateway %d is polled by the server", gw)
	} else {
		PrintSuccess("gateway %d is polled by box %d; the server stops polling it", gw, to)
	}
}
