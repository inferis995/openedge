package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var aiopsCmd = &cobra.Command{
	Use:   "aiops",
	Short: "AI-powered analytics and operations",
}

var aiopsSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Get AI operations summary",
	Run:   runAiopsSummary,
}

var aiopsAnomaliesCmd = &cobra.Command{
	Use:   "anomalies",
	Short: "Detect anomalies for a tag",
	Run:   runAiopsAnomalies,
}

var aiopsDigestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Get alarm digest grouped by type",
	Run:   runAiopsDigest,
}

var (
	aiopsHours      int
	aiopsTagID      int
	aiopsWindowHrs  int
)

func init() {
	aiopsSummaryCmd.Flags().IntVar(&aiopsHours, "hours", 24, "Number of hours to include in summary")

	aiopsAnomaliesCmd.Flags().IntVar(&aiopsTagID, "tag", 0, "Tag ID to analyze (required)")
	aiopsAnomaliesCmd.Flags().IntVar(&aiopsWindowHrs, "window", 24, "Analysis window in hours")
	_ = aiopsAnomaliesCmd.MarkFlagRequired("tag")

	aiopsDigestCmd.Flags().IntVar(&aiopsHours, "hours", 24, "Number of hours to include in digest")

	aiopsCmd.AddCommand(aiopsSummaryCmd)
	aiopsCmd.AddCommand(aiopsAnomaliesCmd)
	aiopsCmd.AddCommand(aiopsDigestCmd)
}

type AnomalyPoint struct {
	Timestamp string      `json:"timestamp"`
	Value     interface{} `json:"value"`
	Score     float64     `json:"score"`
	Type      string      `json:"type"`
}

type DigestEntry struct {
	Severity string `json:"severity"`
	TagCode  string `json:"tag_code"`
	Count    int    `json:"count"`
	LastSeen string `json:"last_seen"`
}

func runAiopsSummary(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := "/api/aiops/summary?hours=" + strconv.Itoa(aiopsHours)

	var summary map[string]interface{}
	if err := client.Get(path, &summary); err != nil {
		PrintError("%v", err)
	}

	data, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(data))
}

func runAiopsAnomalies(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := fmt.Sprintf("/api/aiops/anomalies?tag_id=%d&window_hours=%d", aiopsTagID, aiopsWindowHrs)

	var anomalies []AnomalyPoint
	if err := client.Get(path, &anomalies); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(anomalies, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(anomalies) == 0 {
		PrintSuccess("No anomalies detected for tag %d in the last %d hours", aiopsTagID, aiopsWindowHrs)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"TIMESTAMP\tVALUE\tSCORE\tTYPE"+ColorReset)
	for _, a := range anomalies {
		scoreColor := ColorYellow
		if a.Score > 0.8 {
			scoreColor = ColorRed
		}
		fmt.Fprintf(w, "%s\t%v\t%s%.3f%s\t%s\n",
			a.Timestamp, a.Value,
			scoreColor, a.Score, ColorReset,
			a.Type)
	}
	w.Flush()
}

func runAiopsDigest(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := "/api/aiops/alarms/digest?hours=" + strconv.Itoa(aiopsHours)

	var digest []DigestEntry
	if err := client.Get(path, &digest); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(digest, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(digest) == 0 {
		PrintSuccess("No alarms in the last %d hours", aiopsHours)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"SEVERITY\tTAG\tCOUNT\tLAST SEEN"+ColorReset)
	for _, d := range digest {
		severityColor := ColorYellow
		switch d.Severity {
		case "critical":
			severityColor = ColorRed
		case "info":
			severityColor = ColorCyan
		}
		fmt.Fprintf(w, "%s%s%s\t%s\t%d\t%s\n",
			severityColor, d.Severity, ColorReset,
			d.TagCode, d.Count, d.LastSeen)
	}
	w.Flush()
}
