package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage local CLI configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long:  `Display the active configuration loaded from ~/.openedge/config.json and environment variables.`,
	Run:   runConfigShow,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a single key in ~/.openedge/config.json.

Available keys:
  url       OpenEdge API URL (e.g. https://app.example.com)
  org       Organization ID (integer, 0 = global admin)
  insecure  Skip TLS verification: true or false

Example:
  openedge config set url https://app.example.com
  openedge config set org 3
  openedge config set insecure true`,
	Args: cobra.ExactArgs(2),
	Run:  runConfigSet,
}

var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Delete the local config file",
	Long:  `Remove ~/.openedge/config.json entirely. Same effect as 'openedge logout'.`,
	Run: func(cmd *cobra.Command, args []string) {
		path := config.Path()
		if err := config.Delete(); err != nil {
			PrintError("removing config: %v", err)
		}
		PrintSuccess("Config reset — file removed")
		fmt.Printf("  Deleted: %s\n", path)
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configResetCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		PrintError("loading config: %v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("%sConfig file%s: %s\n\n", ColorBold, ColorReset, config.Path())

	urlDisplay := cfg.URL
	if urlDisplay == "" {
		urlDisplay = ColorRed + "(not set)" + ColorReset
	}

	tokenDisplay := ColorRed + "(not set)" + ColorReset
	if cfg.Token != "" {
		// Show only first 12 + last 4 chars for security
		if len(cfg.Token) > 20 {
			tokenDisplay = cfg.Token[:12] + "..." + cfg.Token[len(cfg.Token)-4:]
		} else {
			tokenDisplay = "***"
		}
		tokenDisplay = ColorGreen + tokenDisplay + ColorReset
	}

	orgDisplay := fmt.Sprintf("%d", cfg.OrgID)
	if cfg.OrgID == 0 {
		orgDisplay = ColorYellow + "0 (global admin)" + ColorReset
	}

	insecureDisplay := ColorGreen + "false (TLS verified)" + ColorReset
	if cfg.Insecure {
		insecureDisplay = ColorYellow + "true (skip TLS)" + ColorReset
	}

	fmt.Printf("  %-12s %s\n", "url", urlDisplay)
	fmt.Printf("  %-12s %s\n", "token", tokenDisplay)
	fmt.Printf("  %-12s %s\n", "org_id", orgDisplay)
	fmt.Printf("  %-12s %s\n", "insecure", insecureDisplay)

	fmt.Printf("\n%sEnv var overrides%s (if set):\n", ColorBold, ColorReset)
	fmt.Printf("  OPENEDGE_URL      OPENEDGE_TOKEN      OPENEDGE_ORG_ID      OPENEDGE_INSECURE\n")
}

func runConfigSet(cmd *cobra.Command, args []string) {
	key := strings.ToLower(args[0])
	val := args[1]

	cfg, err := config.Load()
	if err != nil {
		PrintError("loading config: %v", err)
	}

	switch key {
	case "url":
		cfg.URL = val
	case "org", "org_id":
		n, err := strconv.Atoi(val)
		if err != nil {
			PrintError("org must be an integer, got %q", val)
		}
		cfg.OrgID = n
	case "insecure":
		b, err := strconv.ParseBool(val)
		if err != nil {
			PrintError("insecure must be true or false, got %q", val)
		}
		cfg.Insecure = b
	default:
		PrintError("unknown key %q — valid keys: url, org, insecure", key)
	}

	if err := config.Save(cfg); err != nil {
		PrintError("saving config: %v", err)
	}

	PrintSuccess("Set %s = %s", key, val)
	fmt.Printf("  Config: %s\n", config.Path())
}
