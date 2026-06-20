package cmd

import (
	"fmt"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/config"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove saved credentials",
	Long:  `Delete the local config file (~/.openedge/config.json) to log out.`,
	Run: func(cmd *cobra.Command, args []string) {
		path := config.Path()
		if err := config.Delete(); err != nil {
			PrintError("removing config: %v", err)
		}
		PrintSuccess("Logged out — credentials removed")
		fmt.Printf("  Deleted: %s\n", path)
	},
}
