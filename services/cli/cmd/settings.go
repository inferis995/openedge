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
	rootCmd.AddCommand(settingsCmd)
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
