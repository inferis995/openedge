package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var orgsCmd = &cobra.Command{
	Use:   "orgs",
	Short: "Manage organizations",
}

var orgsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all organizations",
	Run:   runOrgsList,
}

var orgsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get organization details",
	Args:  cobra.ExactArgs(1),
	Run:   runOrgsGet,
}

var orgsInviteCmd = &cobra.Command{
	Use:   "invite <org-id>",
	Short: "Invite a user to an organization",
	Args:  cobra.ExactArgs(1),
	Run:   runOrgsInvite,
}

var orgsModulesCmd = &cobra.Command{
	Use:   "modules <org-id>",
	Short: "Show or change which pages an organization may use",
	Long: `Shows the module list with the effective state. With --disable or
--enable, replaces the disabled set (previews unless --apply).

  openedge orgs modules 2
  openedge orgs modules 2 --disable oee,synoptics,recipes,shifts,kpis --apply`,
	Args: cobra.ExactArgs(1),
	Run:  runOrgsModules,
}

var (
	orgModulesDisable string
	orgModulesEnable  string
	orgModulesApply   bool
)

var (
	inviteEmail string
	inviteRole  string
)

func init() {
	orgsInviteCmd.Flags().StringVar(&inviteEmail, "email", "", "Email address to invite (required)")
	orgsInviteCmd.Flags().StringVar(&inviteRole, "role", "user", "Role: user (read-only) or admin (of the organization)")
	_ = orgsInviteCmd.MarkFlagRequired("email")
	orgsModulesCmd.Flags().StringVar(&orgModulesDisable, "disable", "", "Comma-separated module keys to turn off")
	orgsModulesCmd.Flags().StringVar(&orgModulesEnable, "enable", "", "Comma-separated module keys to turn back on")
	orgsModulesCmd.Flags().BoolVar(&orgModulesApply, "apply", false, "Write the change (otherwise preview only)")

	orgsCmd.AddCommand(orgsListCmd)
	orgsCmd.AddCommand(orgsGetCmd)
	orgsCmd.AddCommand(orgsInviteCmd)
	orgsCmd.AddCommand(orgsModulesCmd)
}

func splitKeys(s string) []string {
	var out []string
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

type Organization struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Plan        string `json:"plan"`
	Active      bool   `json:"active"`
}

func runOrgsList(cmd *cobra.Command, args []string) {
	client := GetClient()

	var orgs []Organization
	if err := client.Get("/api/organizations", &orgs); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(orgs, "", "  ")
		fmt.Println(string(data))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tNAME\tDESCRIPTION\tPLAN\tACTIVE"+ColorReset)
	for _, o := range orgs {
		active := "yes"
		if !o.Active {
			active = ColorYellow + "no" + ColorReset
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", o.ID, o.Name, o.Description, o.Plan, active)
	}
	w.Flush()
}

func runOrgsGet(cmd *cobra.Command, args []string) {
	client := GetClient()

	var org map[string]interface{}
	if err := client.Get("/api/organizations/"+args[0], &org); err != nil {
		PrintError("%v", err)
	}

	data, _ := json.MarshalIndent(org, "", "  ")
	fmt.Println(string(data))
}

func runOrgsInvite(cmd *cobra.Command, args []string) {
	client := GetClient()

	body := map[string]interface{}{
		"email": inviteEmail,
		"role":  inviteRole,
	}

	var result map[string]interface{}
	if err := client.Post("/api/organizations/"+args[0]+"/invites", body, &result); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	PrintSuccess("Invite sent to %s with role %s", inviteEmail, inviteRole)
}

type orgModuleState struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

func runOrgsModules(cmd *cobra.Command, args []string) {
	client := GetClient()
	path := "/api/organizations/" + args[0] + "/modules"

	var current struct {
		Modules []orgModuleState `json:"modules"`
	}
	if err := client.Get(path, &current); err != nil {
		PrintError("%v", err)
	}

	state := map[string]bool{}
	for _, m := range current.Modules {
		state[m.Key] = m.Enabled
	}
	for _, k := range splitKeys(orgModulesDisable) {
		state[k] = false
	}
	for _, k := range splitKeys(orgModulesEnable) {
		state[k] = true
	}

	// Unknown keys are user typos, not modules: say so before writing.
	for _, k := range append(splitKeys(orgModulesDisable), splitKeys(orgModulesEnable)...) {
		if _, ok := state[k]; !ok {
			PrintError("unknown module %q", k)
		}
	}

	var disabled []string
	var enabled []string
	for _, m := range current.Modules {
		if state[m.Key] {
			enabled = append(enabled, m.Key)
		} else {
			disabled = append(disabled, m.Key)
		}
	}

	if flagJSON {
		data, _ := json.MarshalIndent(map[string]interface{}{"disabled": disabled, "enabled": enabled}, "", "  ")
		fmt.Println(string(data))
		return
	}

	if orgModulesDisable == "" && orgModulesEnable == "" {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MODULE\tSTATE")
		for _, m := range current.Modules {
			s := "on"
			if !m.Enabled {
				s = "off"
			}
			fmt.Fprintf(w, "%s\t%s\n", m.Key, s)
		}
		_ = w.Flush()
		return
	}

	fmt.Printf("would disable: %s\n", strings.Join(disabled, ", "))
	if !orgModulesApply {
		fmt.Println("Preview only — run again with --apply to write.")
		return
	}
	if err := client.Put(path, map[string]interface{}{"disabled": disabled}, nil); err != nil {
		PrintError("%v", err)
	}
	PrintSuccess("organization %s pages updated (%d disabled)", args[0], len(disabled))
}
