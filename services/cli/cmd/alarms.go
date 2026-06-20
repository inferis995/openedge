package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var alarmsCmd = &cobra.Command{
	Use:   "alarms",
	Short: "Manage alarms",
}

var alarmsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List alarms",
	Run:   runAlarmsList,
}

var alarmsAckCmd = &cobra.Command{
	Use:   "ack <id>",
	Short: "Acknowledge an alarm",
	Args:  cobra.ExactArgs(1),
	Run:   runAlarmsAck,
}

var (
	alarmsActive   bool
	alarmsSeverity string
)

func init() {
	alarmsListCmd.Flags().BoolVar(&alarmsActive, "active", false, "Show only active alarms")
	alarmsListCmd.Flags().StringVar(&alarmsSeverity, "severity", "", "Filter by severity: critical, warning, info")

	alarmsCmd.AddCommand(alarmsListCmd)
	alarmsCmd.AddCommand(alarmsAckCmd)
}

type Alarm struct {
	ID           int         `json:"id"`
	TagID        int         `json:"tag_id"`
	TagCode      string      `json:"tag_code"`
	Severity     string      `json:"severity"`
	Value        interface{} `json:"value"`
	Message      string      `json:"message"`
	TriggeredAt  string      `json:"triggered_at"`
	AckedAt      *string     `json:"acked_at"`
	AckedBy      *string     `json:"acked_by"`
	Active       bool        `json:"active"`
}

func runAlarmsList(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := "/api/alarms/active"
	sep := "?"
	if alarmsSeverity != "" {
		path += sep + "severity=" + alarmsSeverity
		sep = "&"
	}

	var alarms []Alarm
	if err := client.Get(path, &alarms); err != nil {
		PrintError("%v", err)
	}

	// Client-side filter for active if flag set (API returns active by default)
	if alarmsActive {
		active := alarms[:0]
		for _, a := range alarms {
			if a.Active {
				active = append(active, a)
			}
		}
		alarms = active
	}

	if flagJSON {
		data, _ := json.MarshalIndent(alarms, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(alarms) == 0 {
		PrintSuccess("No alarms found")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tTAG\tSEVERITY\tVALUE\tSINCE\tACKED"+ColorReset)
	for _, a := range alarms {
		severityColor := ColorYellow
		switch a.Severity {
		case "critical":
			severityColor = ColorRed
		case "info":
			severityColor = ColorCyan
		}

		acked := "-"
		if a.AckedAt != nil {
			acked = *a.AckedAt
			if a.AckedBy != nil {
				acked += " by " + *a.AckedBy
			}
		}

		tag := a.TagCode
		if tag == "" {
			tag = fmt.Sprintf("tag-%d", a.TagID)
		}

		fmt.Fprintf(w, "%d\t%s\t%s%s%s\t%v\t%s\t%s\n",
			a.ID, tag,
			severityColor, a.Severity, ColorReset,
			a.Value, a.TriggeredAt, acked)
	}
	w.Flush()
}

func runAlarmsAck(cmd *cobra.Command, args []string) {
	client := GetClient()

	var result map[string]interface{}
	if err := client.Post("/api/alarms/"+args[0]+"/ack", nil, &result); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	PrintSuccess("Alarm %s acknowledged", args[0])
	if msg, ok := result["message"].(string); ok {
		fmt.Printf("  %s\n", msg)
	}
}
