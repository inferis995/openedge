package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Synoptic pages from the command line: everything the dashboard import
// does must also be scriptable (mass rollout of one HTML monitor to 88
// gateways, re-bind after retagging).
var (
	synHTMLName        string
	synHTMLFile        string
	synHTMLBinds       []string
	synHTMLTsProp      string
	synHTMLWidth       int
	synHTMLHeight      int
	synHTMLSite        int
	synHTMLArea        int
	synHTMLDescription string
)

var synopticsCmd = &cobra.Command{
	Use:   "synoptics",
	Short: "Manage synoptic (SCADA mimic) pages",
}

var synopticsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List synoptic pages",
	RunE: func(cmd *cobra.Command, args []string) error {
		var result interface{}
		if err := GetClient().Get("/api/synoptics", &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var avevaScriptRe = regexp.MustCompile(`(?i)<script[^>]+src=["'][^"']*resources/apis/[^"']*["'][^>]*>\s*</script>\s*`)

func synopticsImportHTMLRun(cmd *cobra.Command, args []string) error {
	raw, err := os.ReadFile(synHTMLFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", synHTMLFile, err)
	}
	source := avevaScriptRe.ReplaceAllString(string(raw), "")

	type binding struct {
		Prop  string `json:"prop"`
		TagID *int   `json:"tagId"`
	}
	var bindings []binding
	for _, b := range synHTMLBinds {
		prop, idStr, ok := strings.Cut(b, "=")
		if !ok || prop == "" {
			return fmt.Errorf("bad --bind %q, want prop=tagId", b)
		}
		id, err := strconv.Atoi(strings.TrimSpace(idStr))
		if err != nil {
			return fmt.Errorf("bad --bind %q: tag id must be a number", b)
		}
		idCopy := id
		bindings = append(bindings, binding{Prop: strings.TrimSpace(prop), TagID: &idCopy})
	}
	if bindings == nil {
		bindings = []binding{}
	}

	widgetID := "html-" + strings.ToLower(strings.ReplaceAll(synHTMLName, " ", "-"))
	layout := []map[string]interface{}{
		{
			"id":    widgetID,
			"type":  "html",
			"x":     0,
			"y":     0,
			"w":     synHTMLWidth,
			"h":     synHTMLHeight,
			"label": synHTMLName,
			"config": map[string]interface{}{
				"htmlSource": source,
				"bindings":   bindings,
				"tsProp":     synHTMLTsProp,
			},
		},
	}
	body := map[string]interface{}{
		"name":             synHTMLName,
		"description":      synHTMLDescription,
		"background_color": "#0f172a",
		"canvas_w":         synHTMLWidth,
		"canvas_h":         synHTMLHeight,
		"layout":           layout,
	}
	if synHTMLSite > 0 {
		body["site_id"] = synHTMLSite
	}
	if synHTMLArea > 0 {
		body["area_id"] = synHTMLArea
	}

	var result map[string]interface{}
	if err := GetClient().Post("/api/synoptics", body, &result); err != nil {
		return err
	}
	if id, ok := result["id"]; ok {
		fmt.Printf("synoptic %v created (%s)\n", id, synHTMLName)
		return nil
	}
	return printJSON(result)
}

var synopticsImportHTMLCmd = &cobra.Command{
	Use:   "import-html",
	Short: "Create a synoptic page from an HTML widget file",
	Long: `Create a synoptic page showing one imported HTML widget full-canvas.

The page declares its custom properties with
<meta name="oe-props" content="pressione1, batteria, ts"> (or they are
read from cwidget.on('...') calls); bind each to a tag with --bind,
repeatable. The ts prop needs no tag: the platform feeds the newest
sample time automatically.`,
	Args: cobra.NoArgs,
	RunE: synopticsImportHTMLRun,
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func init() {
	synopticsImportHTMLCmd.Flags().StringVar(&synHTMLName, "name", "", "Page name (required)")
	synopticsImportHTMLCmd.Flags().StringVar(&synHTMLFile, "file", "", "HTML file to import (required)")
	synopticsImportHTMLCmd.Flags().StringArrayVar(&synHTMLBinds, "bind", nil, "prop=tagId binding (repeatable)")
	synopticsImportHTMLCmd.Flags().StringVar(&synHTMLTsProp, "ts-prop", "", "Prop fed with the newest sample time")
	synopticsImportHTMLCmd.Flags().IntVar(&synHTMLWidth, "width", 1280, "Canvas width")
	synopticsImportHTMLCmd.Flags().IntVar(&synHTMLHeight, "height", 720, "Canvas height")
	synopticsImportHTMLCmd.Flags().IntVar(&synHTMLSite, "site", 0, "Site ID (optional)")
	synopticsImportHTMLCmd.Flags().IntVar(&synHTMLArea, "area", 0, "Area ID (optional)")
	synopticsImportHTMLCmd.Flags().StringVar(&synHTMLDescription, "description", "", "Page description")
	_ = synopticsImportHTMLCmd.MarkFlagRequired("name")
	_ = synopticsImportHTMLCmd.MarkFlagRequired("file")
	synopticsCmd.AddCommand(synopticsListCmd)
	synopticsCmd.AddCommand(synopticsImportHTMLCmd)
}
