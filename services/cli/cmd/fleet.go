package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var fleetCmd = &cobra.Command{
	Use:   "fleet",
	Short: "Manage edge fleet",
}

var fleetStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show fleet status",
	Run:   runFleetStatus,
}

var fleetRestartCmd = &cobra.Command{
	Use:   "restart <org-id>",
	Short: "Restart edge for an organization",
	Args:  cobra.ExactArgs(1),
	Run:   runFleetRestart,
}

var fleetUpdateCmd = &cobra.Command{
	Use:   "update <org-id>",
	Short: "Update edge software for an organization",
	Args:  cobra.ExactArgs(1),
	Run:   runFleetUpdate,
}

var fleetUpdateVersion string

func init() {
	fleetUpdateCmd.Flags().StringVar(&fleetUpdateVersion, "version", "", "Version to update to (e.g. v1.2.0)")
	_ = fleetUpdateCmd.MarkFlagRequired("version")

	fleetCmd.AddCommand(fleetStatusCmd)
	fleetCmd.AddCommand(fleetRestartCmd)
	fleetCmd.AddCommand(fleetUpdateCmd)
}

type FleetEntry struct {
	OrgID    int    `json:"org_id"`
	OrgName  string `json:"org_name"`
	Online   bool   `json:"online"`
	LastPing string `json:"last_ping"`
	Version  string `json:"version"`
	Status   string `json:"status"`
}

func runFleetStatus(cmd *cobra.Command, args []string) {
	client := GetClient()

	var fleet []FleetEntry
	if err := client.Get("/api/fleet/status", &fleet); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(fleet, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(fleet) == 0 {
		fmt.Println("No fleet entries found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ORG ID\tORG NAME\tONLINE\tLAST PING\tVERSION"+ColorReset)
	for _, f := range fleet {
		onlineStr := ColorGreen + "online" + ColorReset
		if !f.Online {
			onlineStr = ColorRed + "offline" + ColorReset
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
			f.OrgID, f.OrgName, onlineStr, f.LastPing, f.Version)
	}
	w.Flush()
}

func runFleetRestart(cmd *cobra.Command, args []string) {
	client := GetClient()

	var result map[string]interface{}
	if err := client.Post("/api/organizations/"+args[0]+"/edge-restart", nil, &result); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	PrintSuccess("Edge restart requested for org %s", args[0])
	if msg, ok := result["message"].(string); ok {
		fmt.Printf("  %s\n", msg)
	}
}

func runFleetUpdate(cmd *cobra.Command, args []string) {
	client := GetClient()

	body := map[string]interface{}{
		"version": fleetUpdateVersion,
	}

	var result map[string]interface{}
	if err := client.Post("/api/organizations/"+args[0]+"/edge-update", body, &result); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	PrintSuccess("Edge update to %s requested for org %s", fleetUpdateVersion, args[0])
	if msg, ok := result["message"].(string); ok {
		fmt.Printf("  %s\n", msg)
	}
}
