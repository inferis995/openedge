package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
	"github.com/ralph/industrial-edge-middleware/services/cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	loginURL      string
	loginUsername string
	loginPassword string
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with OpenEdge and save credentials",
	Long: `Log in to OpenEdge with your username and password.
The JWT token is saved to ~/.openedge/config.json for subsequent commands.

Example:
  openedge login --url https://app.example.com --username admin`,
	Run: runLogin,
}

func init() {
	loginCmd.Flags().StringVar(&loginURL, "url", "", "OpenEdge API URL (required)")
	loginCmd.Flags().StringVar(&loginUsername, "username", "", "Username")
	loginCmd.Flags().StringVar(&loginPassword, "password", "", "Password (omit to be prompted)")
	_ = loginCmd.MarkFlagRequired("url")
}

func runLogin(cmd *cobra.Command, args []string) {
	reader := bufio.NewReader(os.Stdin)

	username := loginUsername
	if username == "" {
		fmt.Print("Username: ")
		u, err := reader.ReadString('\n')
		if err != nil {
			PrintError("reading username: %v", err)
		}
		username = strings.TrimSpace(u)
	}

	password := loginPassword
	if password == "" {
		fmt.Print("Password: ")
		// Try to read password silently; fall back to plain read if not a terminal.
		if term.IsTerminal(int(syscall.Stdin)) {
			pw, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				PrintError("reading password: %v", err)
			}
			fmt.Println()
			password = string(pw)
		} else {
			pw, err := reader.ReadString('\n')
			if err != nil {
				PrintError("reading password: %v", err)
			}
			password = strings.TrimSpace(pw)
		}
	}

	resp, err := api.LoginWithOptions(loginURL, username, password, flagInsecure)
	if err != nil {
		PrintError("%v", err)
	}

	cfg := config.Config{
		URL:   loginURL,
		Token: resp.Token,
	}
	if resp.User.OrgID != nil {
		cfg.OrgID = *resp.User.OrgID
	}

	if err := config.Save(cfg); err != nil {
		PrintError("saving config: %v", err)
	}

	orgLabel := "global admin"
	if resp.User.OrgID != nil {
		orgLabel = fmt.Sprintf("org %d", *resp.User.OrgID)
	}

	PrintSuccess("Logged in as %s (%s)", resp.User.Username, orgLabel)
	fmt.Printf("  Config saved to: %s\n", config.Path())
}
