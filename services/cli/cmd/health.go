package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check API health status",
	Run:   runHealth,
}

var diagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Get system diagnostics",
	Run:   runDiagnostics,
}

func init() {
	// no extra flags needed
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func runHealth(cmd *cobra.Command, args []string) {
	client := GetClient()

	var liveStatus map[string]interface{}
	liveErr := client.Get("/health", &liveStatus)

	var readyStatus map[string]interface{}
	readyErr := client.Get("/ready", &readyStatus)

	if flagJSON {
		combined := map[string]interface{}{
			"health": liveStatus,
			"ready":  readyStatus,
		}
		if liveErr != nil {
			combined["health_error"] = liveErr.Error()
		}
		if readyErr != nil {
			combined["ready_error"] = readyErr.Error()
		}
		data, _ := json.MarshalIndent(combined, "", "  ")
		fmt.Println(string(data))
		return
	}

	// Live check
	fmt.Printf("%-12s ", "Live:")
	if liveErr != nil {
		fmt.Printf(ColorRed+"FAIL"+ColorReset+" — %v\n", liveErr)
	} else {
		status := "ok"
		if s, ok := liveStatus["status"].(string); ok {
			status = s
		}
		color := ColorGreen
		if status != "ok" && status != "healthy" {
			color = ColorRed
		}
		fmt.Printf(color+"%s"+ColorReset+"\n", status)
	}

	// Ready check
	fmt.Printf("%-12s ", "Ready:")
	if readyErr != nil {
		fmt.Printf(ColorRed+"FAIL"+ColorReset+" — %v\n", readyErr)
	} else {
		status := "ok"
		if s, ok := readyStatus["status"].(string); ok {
			status = s
		}
		color := ColorGreen
		if status != "ok" && status != "healthy" && status != "ready" {
			color = ColorYellow
		}
		fmt.Printf(color+"%s"+ColorReset+"\n", status)
	}
}

func runDiagnostics(cmd *cobra.Command, args []string) {
	client := GetClient()

	var diag map[string]interface{}
	if err := client.Get("/api/system/diagnostics", &diag); err != nil {
		PrintError("%v", err)
	}

	data, _ := json.MarshalIndent(diag, "", "  ")
	fmt.Println(string(data))
}
