package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Platform settings",
}

var settingsWriteMaxAgeCmd = &cobra.Command{
	Use:   "write-max-age [seconds]",
	Short: "Show or set how long a write command to a PLC stays valid",
	Long: `A setpoint or command older than this is refused by the driver instead of
executed, and the operator is told why. It is what stops commands given while
the link to an edge box was down from running hours later, when it returns.
Between 5 and 3600 seconds; 30 suits nearly everyone.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runSettingsWriteMaxAge,
}

func init() {
	settingsCmd.AddCommand(settingsWriteMaxAgeCmd)
	settingsCmd.AddCommand(settingsRetainCmd)
	settingsCmd.AddCommand(settingsPublishModeCmd)
	rootCmd.AddCommand(settingsCmd)
}

// Retain last values: drivers publish legacy tag data retained, so a late
// subscriber (external app, monitor "Ultimi valori" tab) immediately reads
// the freshest known value. Every payload carries its timestamp.
var settingsRetainCmd = &cobra.Command{
	Use:   "retain [on|off]",
	Short: "Show or set retained publishing of the last known values",
	Long: `With retain ON (default), drivers publish legacy data topics with the
retained flag: one message per tag is always kept by the broker and handed
to any late subscriber. Same topics, same payloads — only the flag changes.
The broker persists retained messages across restarts (5-min autosave).`,
	Args: cobra.MaximumNArgs(1),
	Run:  runSettingsRetain,
}

func runSettingsRetain(cmd *cobra.Command, args []string) {
	client := GetClient()
	if len(args) == 0 {
		var s map[string]interface{}
		if err := client.Get("/api/system/settings", &s); err != nil {
			PrintError("%v", err)
		}
		v, ok := s["retain_last_values"]
		if !ok || v == "" {
			v = "true (default)"
		}
		fmt.Printf("Retain last values: %v.\n", v)
		return
	}
	var on bool
	switch args[0] {
	case "on", "true", "1":
		on = true
	case "off", "false", "0":
		on = false
	default:
		PrintError("expected on or off, got %q", args[0])
	}
	if err := client.Put("/api/system/settings", map[string]bool{"retain_last_values": on}, nil); err != nil {
		PrintError("%v", err)
	}
	if on {
		PrintSuccess("retain ON: drivers publish legacy data retained")
	} else {
		PrintSuccess("retain OFF: legacy data goes out live-only")
	}
}

var settingsPublishModeCmd = &cobra.Command{
	Use:   "publish-mode [legacy|dual|sparkplug]",
	Short: "Show or set the driver publish format",
	Args:  cobra.MaximumNArgs(1),
	Run:   runSettingsPublishMode,
}

func runSettingsPublishMode(cmd *cobra.Command, args []string) {
	client := GetClient()
	if len(args) == 0 {
		var s map[string]interface{}
		if err := client.Get("/api/system/settings", &s); err != nil {
			PrintError("%v", err)
		}
		v, ok := s["publish_mode"]
		if !ok || v == "" {
			v = "dual (default)"
		}
		fmt.Printf("Publish mode: %v.\n", v)
		return
	}
	switch args[0] {
	case "legacy", "dual", "sparkplug":
	default:
		PrintError("expected legacy, dual or sparkplug, got %q", args[0])
	}
	if err := client.Put("/api/system/settings", map[string]string{"publish_mode": args[0]}, nil); err != nil {
		PrintError("%v", err)
	}
	PrintSuccess("publish mode now %s", args[0])
}

func runSettingsWriteMaxAge(cmd *cobra.Command, args []string) {
	client := GetClient()
	if len(args) == 0 {
		var s map[string]interface{}
		if err := client.Get("/api/system/settings", &s); err != nil {
			PrintError("%v", err)
		}
		v, ok := s["write_command_max_age_seconds"]
		if !ok || v == "" {
			v = "30 (default)"
		}
		fmt.Printf("Write commands stay valid for %v seconds.\n", v)
		return
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 5 || n > 3600 {
		PrintError("seconds must be a number between 5 and 3600, got %q", args[0])
	}
	if err := client.Put("/api/system/settings", map[string]int{"write_command_max_age_seconds": n}, nil); err != nil {
		PrintError("%v", err)
	}
	PrintSuccess("write commands now stay valid for %d seconds", n)
}
