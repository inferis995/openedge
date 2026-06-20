package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ralph/industrial-edge-middleware/services/cli/internal/config"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the current authenticated user",
	Long:  `Display the current user profile and active configuration.`,
	Run:   runWhoami,
}

type meResponse struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	OrgID    *int   `json:"org_id"`
}

func runWhoami(cmd *cobra.Command, args []string) {
	client := GetClient()
	cfg, _ := config.Load()

	var me meResponse
	if err := client.Get("/api/auth/me", &me); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		out := map[string]interface{}{
			"user":   me,
			"config": cfg,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	orgLabel := ColorYellow + "global admin" + ColorReset
	if me.OrgID != nil {
		orgLabel = fmt.Sprintf("org %d", *me.OrgID)
	}

	fmt.Printf("%sUser%s\n", ColorBold, ColorReset)
	fmt.Printf("  ID:       %d\n", me.ID)
	fmt.Printf("  Username: %s\n", me.Username)
	if me.FullName != "" {
		fmt.Printf("  Name:     %s\n", me.FullName)
	}
	if me.Email != "" {
		fmt.Printf("  Email:    %s\n", me.Email)
	}
	fmt.Printf("  Role:     %s\n", me.Role)
	fmt.Printf("  Scope:    %s\n", orgLabel)

	fmt.Printf("\n%sConnection%s\n", ColorBold, ColorReset)
	fmt.Printf("  URL:      %s\n", cfg.URL)
	fmt.Printf("  Config:   %s\n", config.Path())
	if cfg.Insecure {
		fmt.Printf("  TLS:      %sskip verification%s\n", ColorYellow, ColorReset)
	} else {
		fmt.Printf("  TLS:      %sverified%s\n", ColorGreen, ColorReset)
	}
}
