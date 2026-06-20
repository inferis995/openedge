package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Manage tags and their values",
}

var tagsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tags",
	Run:   runTagsList,
}

var tagsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get current tag value",
	Args:  cobra.ExactArgs(1),
	Run:   runTagsGet,
}

var tagsWriteCmd = &cobra.Command{
	Use:   "write <id> <value>",
	Short: "Write a value to a tag",
	Args:  cobra.ExactArgs(2),
	Run:   runTagsWrite,
}

var tagsHistoryCmd = &cobra.Command{
	Use:   "history <id>",
	Short: "Get tag history",
	Args:  cobra.ExactArgs(1),
	Run:   runTagsHistory,
}

var tagsShadowsCmd = &cobra.Command{
	Use:   "shadows",
	Short: "Get tag shadows (live/historic values)",
	Run:   runTagsShadows,
}

var (
	tagsGatewayID  int
	tagsFromTime   string
	tagsToTime     string
	tagsLimit      int
	tagsShadowsGW  int
)

func init() {
	tagsListCmd.Flags().IntVar(&tagsGatewayID, "gateway", 0, "Filter by gateway ID")

	tagsHistoryCmd.Flags().StringVar(&tagsFromTime, "from", "", "Start time (RFC3339)")
	tagsHistoryCmd.Flags().StringVar(&tagsToTime, "to", "", "End time (RFC3339)")
	tagsHistoryCmd.Flags().IntVar(&tagsLimit, "limit", 100, "Max number of results")

	tagsShadowsCmd.Flags().IntVar(&tagsShadowsGW, "gateway", 0, "Filter by gateway ID")

	tagsCmd.AddCommand(tagsListCmd)
	tagsCmd.AddCommand(tagsGetCmd)
	tagsCmd.AddCommand(tagsWriteCmd)
	tagsCmd.AddCommand(tagsHistoryCmd)
	tagsCmd.AddCommand(tagsShadowsCmd)
}

type Tag struct {
	ID           int         `json:"id"`
	Code         string      `json:"code"`
	Alias        string      `json:"alias"`
	DataType     string      `json:"data_type"`
	CurrentValue interface{} `json:"current_value"`
	Unit         string      `json:"unit"`
	GatewayID    int         `json:"gateway_id"`
}

type TagCurrent struct {
	Value     interface{} `json:"value"`
	Quality   string      `json:"quality"`
	Timestamp string      `json:"timestamp"`
	TagID     int         `json:"tag_id"`
	Code      string      `json:"code"`
}

type TagHistoryPoint struct {
	Timestamp string      `json:"timestamp"`
	Value     interface{} `json:"value"`
	Quality   string      `json:"quality"`
}

type TagShadow struct {
	TagID   int         `json:"tag_id"`
	Code    string      `json:"code"`
	Alias   string      `json:"alias"`
	Value   interface{} `json:"value"`
	Source  string      `json:"source"`
	TS      string      `json:"ts"`
}

func runTagsList(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := "/api/tags"
	if tagsGatewayID > 0 {
		path += "?gateway_id=" + strconv.Itoa(tagsGatewayID)
	}

	var tags []Tag
	if err := client.Get(path, &tags); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(tags, "", "  ")
		fmt.Println(string(data))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tCODE\tALIAS\tTYPE\tCURRENT VALUE\tUNIT"+ColorReset)
	for _, t := range tags {
		val := "-"
		if t.CurrentValue != nil {
			val = fmt.Sprintf("%v", t.CurrentValue)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Code, t.Alias, t.DataType, val, t.Unit)
	}
	w.Flush()
}

func runTagsGet(cmd *cobra.Command, args []string) {
	client := GetClient()

	var current TagCurrent
	if err := client.Get("/api/tags/"+args[0]+"/current", &current); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(current, "", "  ")
		fmt.Println(string(data))
		return
	}

	quality := current.Quality
	qualityColor := ColorGreen
	if quality == "bad" || quality == "uncertain" {
		qualityColor = ColorRed
	}

	fmt.Printf("Tag:       %s (ID: %d)\n", current.Code, current.TagID)
	fmt.Printf("Value:     %v\n", current.Value)
	fmt.Printf("Quality:   %s%s%s\n", qualityColor, quality, ColorReset)
	fmt.Printf("Timestamp: %s\n", current.Timestamp)
}

func runTagsWrite(cmd *cobra.Command, args []string) {
	client := GetClient()

	tagID := args[0]
	rawValue := args[1]

	// Try to parse as number first, otherwise use string
	var value interface{} = rawValue
	if f, err := strconv.ParseFloat(rawValue, 64); err == nil {
		value = f
	} else if b, err := strconv.ParseBool(rawValue); err == nil {
		value = b
	}

	body := map[string]interface{}{
		"value": value,
	}

	var result map[string]interface{}
	if err := client.Put("/api/i3x/v1/properties/tag-"+tagID+"/value", body, &result); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	PrintSuccess("Tag %s written: %v", tagID, value)
	if msg, ok := result["message"].(string); ok {
		fmt.Printf("  %s\n", msg)
	}
}

func runTagsHistory(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := "/api/history/stats?tag_id=" + args[0]
	if tagsFromTime != "" {
		path += "&from=" + tagsFromTime
	}
	if tagsToTime != "" {
		path += "&to=" + tagsToTime
	}
	if tagsLimit > 0 {
		path += "&limit=" + strconv.Itoa(tagsLimit)
	}

	var points []TagHistoryPoint
	if err := client.Get(path, &points); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(points, "", "  ")
		fmt.Println(string(data))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"TIMESTAMP\tVALUE\tQUALITY"+ColorReset)
	for _, p := range points {
		qualityColor := ColorGreen
		if p.Quality == "bad" || p.Quality == "uncertain" {
			qualityColor = ColorRed
		}
		fmt.Fprintf(w, "%s\t%v\t%s%s%s\n",
			p.Timestamp, p.Value,
			qualityColor, p.Quality, ColorReset)
	}
	w.Flush()
}

func runTagsShadows(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := "/api/tags/shadows"
	if tagsShadowsGW > 0 {
		path += "?gateway_id=" + strconv.Itoa(tagsShadowsGW)
	}

	var shadows []TagShadow
	if err := client.Get(path, &shadows); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(shadows, "", "  ")
		fmt.Println(string(data))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"TAG ID\tCODE\tALIAS\tVALUE\tSOURCE\tTIMESTAMP"+ColorReset)
	for _, s := range shadows {
		sourceColor := ColorGreen
		if s.Source == "historic" {
			sourceColor = ColorYellow
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%v\t%s%s%s\t%s\n",
			s.TagID, s.Code, s.Alias, s.Value,
			sourceColor, s.Source, ColorReset,
			s.TS)
	}
	w.Flush()
}
