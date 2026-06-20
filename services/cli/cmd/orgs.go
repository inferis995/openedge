package cmd

import (
	"encoding/json"
	"fmt"
	"os"
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

var (
	inviteEmail string
	inviteRole  string
)

func init() {
	orgsInviteCmd.Flags().StringVar(&inviteEmail, "email", "", "Email address to invite (required)")
	orgsInviteCmd.Flags().StringVar(&inviteRole, "role", "viewer", "Role: admin, operator, viewer")
	_ = orgsInviteCmd.MarkFlagRequired("email")

	orgsCmd.AddCommand(orgsListCmd)
	orgsCmd.AddCommand(orgsGetCmd)
	orgsCmd.AddCommand(orgsInviteCmd)
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
