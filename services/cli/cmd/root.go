package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/api"
	"github.com/ralph/industrial-edge-middleware/services/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	flagURL      string
	flagToken    string
	flagOrg      int
	flagJSON     bool
	flagInsecure bool
)

var rootCmd = &cobra.Command{
	Use:   "openedge",
	Short: "OpenEdge CLI — Industrial IoT Platform",
	Long: `OpenEdge CLI provides command-line access to the OpenEdge Industrial IoT Platform.

Manage gateways, tags, alarms, fleet operations, and AI-powered analytics.
Run 'openedge mcp' to start an MCP server for AI agent integration (Claude Code, Cursor).

Quick start:
  openedge login --url https://app.example.com
  openedge gateways list
  openedge tags list --gateway 2
  openedge alarms list --active`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagURL, "url", "", "OpenEdge API URL (overrides config)")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "API token (overrides config)")
	rootCmd.PersistentFlags().IntVar(&flagOrg, "org", 0, "Organization ID (overrides config)")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output raw JSON")
	rootCmd.PersistentFlags().BoolVar(&flagInsecure, "insecure", false, "Skip TLS certificate verification (on-prem self-signed certs)")

	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(whoamiCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(orgsCmd)
	rootCmd.AddCommand(gatewaysCmd)
	rootCmd.AddCommand(tagsCmd)
	rootCmd.AddCommand(alarmsCmd)
	rootCmd.AddCommand(fleetCmd)
	rootCmd.AddCommand(aiopsCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(diagnosticsCmd)
	rootCmd.AddCommand(mcpCmd)
}

// GetClient returns an API client built from config file → env vars → CLI flags.
// Priority (highest wins): CLI flags > env vars > ~/.openedge/config.json
func GetClient() *api.Client {
	cfg, _ := config.Load()

	// Layer 2: environment variable overrides
	if v := os.Getenv("OPENEDGE_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("OPENEDGE_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("OPENEDGE_ORG_ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.OrgID = n
		}
	}
	if os.Getenv("OPENEDGE_INSECURE") == "true" {
		cfg.Insecure = true
	}

	// Layer 3: CLI flag overrides (highest priority)
	if flagURL != "" {
		cfg.URL = flagURL
	}
	if flagToken != "" {
		cfg.Token = flagToken
	}
	if flagOrg != 0 {
		cfg.OrgID = flagOrg
	}
	if flagInsecure {
		cfg.Insecure = true
	}

	if cfg.URL == "" {
		fmt.Fprintln(os.Stderr, ColorRed+"Error: no API URL configured."+ColorReset)
		fmt.Fprintln(os.Stderr, "  Run:  openedge login --url <url>")
		fmt.Fprintln(os.Stderr, "  Or:   export OPENEDGE_URL=<url>")
		os.Exit(1)
	}

	return api.NewWithOptions(cfg.URL, cfg.Token, cfg.OrgID, cfg.Insecure)
}

// ANSI color codes.
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
)

// PrintError prints an error to stderr in red and exits with code 1.
func PrintError(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "%sError: %s%s\n", ColorRed, fmt.Sprintf(msg, args...), ColorReset)
	os.Exit(1)
}

// PrintSuccess prints a success message in green.
func PrintSuccess(msg string, args ...interface{}) {
	fmt.Printf("%s✓ %s%s\n", ColorGreen, fmt.Sprintf(msg, args...), ColorReset)
}

// PrintWarning prints a warning message in yellow.
func PrintWarning(msg string, args ...interface{}) {
	fmt.Printf("%s⚠ %s%s\n", ColorYellow, fmt.Sprintf(msg, args...), ColorReset)
}
