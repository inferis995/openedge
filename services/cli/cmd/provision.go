package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// Commands that build a plant rather than read it: organization, site, area,
// gateway, tags from a sheet, users. Each create prints the created object
// with --json, so a script can carry its id to the next step.

// ── openedge api ────────────────────────────────────────────────────────────

var apiCmd = &cobra.Command{
	Use:   "api <METHOD> <path> [json-body]",
	Short: "Call any API endpoint with the configured credentials",
	Long: `Calls the OpenEdge API with the URL, token and organization of the CLI
configuration and prints the JSON response. For what no command covers yet.

  openedge api GET /api/sites
  openedge api PUT /api/organizations/3/mfa-required '{"mfa_required":true}'`,
	Args: cobra.RangeArgs(2, 3),
	Run:  runAPI,
}

func runAPI(cmd *cobra.Command, args []string) {
	method := strings.ToUpper(args[0])
	path := args[1]
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var body []byte
	if len(args) == 3 {
		if !json.Valid([]byte(args[2])) {
			PrintError("the body is not valid JSON")
		}
		body = []byte(args[2])
	}
	ctype := ""
	if body != nil {
		ctype = "application/json"
	}
	data, err := GetClient().Do(method, path, body, ctype)
	if len(data) > 0 {
		printIndented(data)
	}
	if err != nil {
		PrintError("%v", err)
	}
}

func printIndented(data []byte) {
	var out bytes.Buffer
	if json.Indent(&out, data, "", "  ") == nil {
		fmt.Println(out.String())
		return
	}
	fmt.Println(string(data))
}

// printCreated prints a created object: as JSON with --json, else a line.
func printCreated(what string, obj map[string]interface{}) {
	if flagJSON {
		data, _ := json.MarshalIndent(obj, "", "  ")
		fmt.Println(string(data))
		return
	}
	PrintSuccess("%s %q created (id %v)", what, obj["name"], idOf(obj))
}

func idOf(obj map[string]interface{}) interface{} {
	if f, ok := obj["id"].(float64); ok {
		return int(f)
	}
	return obj["id"]
}

// printList prints id+name rows, or the JSON array with --json.
func printList(items []map[string]interface{}, extra ...string) {
	if flagJSON {
		data, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(data))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	head := "ID\tNAME"
	for _, e := range extra {
		head += "\t" + strings.ToUpper(e)
	}
	_, _ = fmt.Fprintln(w, ColorBold+head+ColorReset)
	for _, it := range items {
		row := fmt.Sprintf("%v\t%v", idOf(it), it["name"])
		for _, e := range extra {
			row += fmt.Sprintf("\t%v", it[e])
		}
		_, _ = fmt.Fprintln(w, row)
	}
	_ = w.Flush()
}

// ── openedge orgs create ────────────────────────────────────────────────────

var orgsCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an organization (global admin)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var org map[string]interface{}
		if err := GetClient().Post("/api/organizations", map[string]interface{}{"name": args[0]}, &org); err != nil {
			PrintError("%v", err)
		}
		printCreated("Organization", org)
	},
}

// ── openedge sites ──────────────────────────────────────────────────────────

var sitesCmd = &cobra.Command{Use: "sites", Short: "Manage sites (the plants of an organization)"}

var sitesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the sites of the organization",
	Run: func(cmd *cobra.Command, args []string) {
		var items []map[string]interface{}
		if err := GetClient().Get("/api/sites", &items); err != nil {
			PrintError("%v", err)
		}
		printList(items, "org_id")
	},
}

var sitesCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a site in the organization given by --org",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := GetClient()
		if client.OrgID() == 0 {
			PrintError("say which organization: --org <id> (or openedge config set org <id>)")
		}
		var site map[string]interface{}
		if err := client.Post("/api/sites", map[string]interface{}{"org_id": client.OrgID(), "name": args[0]}, &site); err != nil {
			PrintError("%v", err)
		}
		printCreated("Site", site)
	},
}

// ── openedge areas ──────────────────────────────────────────────────────────

var areasCmd = &cobra.Command{Use: "areas", Short: "Manage areas (the parts of a site)"}

var areasListSite int

var areasListCmd = &cobra.Command{
	Use:   "list",
	Short: "List areas, optionally of one site (--site)",
	Run: func(cmd *cobra.Command, args []string) {
		path := "/api/areas"
		if areasListSite > 0 {
			path += "?site_id=" + strconv.Itoa(areasListSite)
		}
		var items []map[string]interface{}
		if err := GetClient().Get(path, &items); err != nil {
			PrintError("%v", err)
		}
		printList(items, "site_id")
	},
}

var areasCreateCmd = &cobra.Command{
	Use:   "create <site-id> <name>",
	Short: "Create an area in a site",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		siteID, err := strconv.Atoi(args[0])
		if err != nil {
			PrintError("site-id must be a number")
		}
		var area map[string]interface{}
		if err := GetClient().Post("/api/areas", map[string]interface{}{"site_id": siteID, "name": args[1]}, &area); err != nil {
			PrintError("%v", err)
		}
		printCreated("Area", area)
	},
}

// ── openedge gateways create ────────────────────────────────────────────────

var (
	gwCreateArea   int
	gwCreateName   string
	gwCreateDriver string
	gwCreateConfig string
	gwCreateScan   int
)

var gatewaysCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a gateway in an area",
	Long: `Creates a gateway. --config is the connection_config object as JSON.

MQTT, external broker, MiDo data loggers:
  openedge gateways create --area 3 --name "MiDo Cariati" --driver MQTT --config '{
    "broker_host":"broker.example.com","broker_port":1883,
    "broker_username":"user","broker_password":"secret",
    "decoder_preset":"mido","stale_after_minutes":420,"alarms_on_history":true}'

Modbus TCP:
  openedge gateways create --area 3 --name PLC1 --driver MODBUS_TCP --config '{"ip":"192.168.1.10","port":502}'`,
	Run: func(cmd *cobra.Command, args []string) {
		var cfg map[string]interface{}
		if err := json.Unmarshal([]byte(gwCreateConfig), &cfg); err != nil {
			PrintError("--config is not a JSON object: %v", err)
		}
		body := map[string]interface{}{
			"area_id": gwCreateArea, "name": gwCreateName,
			"driver_type": strings.ToUpper(gwCreateDriver), "connection_config": cfg,
		}
		if gwCreateScan > 0 {
			body["scan_rate_ms"] = gwCreateScan
		}
		var gw map[string]interface{}
		if err := GetClient().Post("/api/gateways", body, &gw); err != nil {
			PrintError("%v", err)
		}
		printCreated("Gateway", gw)
	},
}

// ── openedge tags import ────────────────────────────────────────────────────

var tagsImportApply bool

var tagsImportCmd = &cobra.Command{
	Use:   "import <gateway-id> <file.csv|file.xlsx>",
	Short: "Import a tag sheet into a gateway (preview, then --apply)",
	Long: `Reads a CSV or Excel sheet (the format of the gateway's tag export) and
shows, row by row, what an import would do. Nothing is written without
--apply, and with --apply the whole file is written or none of it.

Columns: Name;Address;Type plus any of Historize, Deadband, Unit, Decimals,
Scaling, Raw min, Raw max, EU min, EU max, Clamp, Invert, JSON path
(Italian headers are accepted too). Several tags can read one MQTT topic:
they are told apart by their JSON path.`,
	Args: cobra.ExactArgs(2),
	Run:  runTagsImport,
}

type sheetResult struct {
	Rows []struct {
		Line     int    `json:"line"`
		Alias    string `json:"alias"`
		Action   string `json:"action"`
		Problems []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
			Value string `json:"value"`
		} `json:"problems"`
	} `json:"rows"`
	Problems  []map[string]interface{} `json:"problems"`
	Created   int                      `json:"created"`
	Updated   int                      `json:"updated"`
	Unchanged int                      `json:"unchanged"`
	Invalid   int                      `json:"invalid"`
	Applied   bool                     `json:"applied"`
}

func runTagsImport(cmd *cobra.Command, args []string) {
	if _, err := strconv.Atoi(args[0]); err != nil {
		PrintError("gateway-id must be a number")
	}
	file, err := os.ReadFile(args[1])
	if err != nil {
		PrintError("%v", err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("gateway_id", args[0])
	_ = mw.WriteField("apply", strconv.FormatBool(tagsImportApply))
	fw, err := mw.CreateFormFile("file", filepath.Base(args[1]))
	if err != nil {
		PrintError("%v", err)
	}
	_, _ = fw.Write(file)
	_ = mw.Close()

	data, err := GetClient().Do("POST", "/api/tags/import/file", buf.Bytes(), mw.FormDataContentType())
	if err != nil {
		PrintError("%v", err)
	}
	if flagJSON {
		printIndented(data)
		return
	}
	var res sheetResult
	if err := json.Unmarshal(data, &res); err != nil {
		PrintError("unexpected answer: %v", err)
	}
	for _, p := range res.Problems {
		PrintWarning("sheet: %v %v", p["code"], p["value"])
	}
	for _, r := range res.Rows {
		for _, p := range r.Problems {
			PrintWarning("line %d (%s): %s %s %s", r.Line, r.Alias, p.Field, p.Code, p.Value)
		}
	}
	fmt.Printf("create %d · update %d · unchanged %d · invalid %d\n", res.Created, res.Updated, res.Unchanged, res.Invalid)
	switch {
	case res.Applied:
		PrintSuccess("Imported")
	case res.Invalid > 0 || len(res.Problems) > 0:
		PrintError("nothing imported: fix the rows above")
	case tagsImportApply:
		PrintSuccess("Nothing to change")
	default:
		fmt.Println("Preview only: run again with --apply to write it.")
	}
}

// ── openedge decoders ───────────────────────────────────────────────────────

var decodersCmd = &cobra.Command{Use: "decoders", Short: "MQTT payload decoders (presets, dry run)"}

var decodersPresetsCmd = &cobra.Command{
	Use:   "presets",
	Short: "List the shipped decoder presets",
	Run: func(cmd *cobra.Command, args []string) {
		var items []map[string]interface{}
		if err := GetClient().Get("/api/decoders/presets", &items); err != nil {
			PrintError("%v", err)
		}
		if !flagJSON {
			for _, it := range items {
				delete(it, "script")
			}
		}
		printList(items)
	},
}

var (
	decPreset   string
	decScript   string
	decPayload  string
	decEncoding string
	decTopic    string
)

var decodersTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Decode one message with a preset or script; nothing is stored",
	Long: `Runs a decoder on one message and prints every reading it holds, with its
time. Nothing is stored or published.

  mosquitto_sub -h broker -t 'site/123/dataup/up' -C 1 > msg.txt
  openedge decoders test --preset mido --payload-file msg.txt --topic site/123/dataup/up`,
	Run: runDecodersTest,
}

func runDecodersTest(cmd *cobra.Command, args []string) {
	payload, err := os.ReadFile(decPayload)
	if err != nil {
		PrintError("--payload-file: %v", err)
	}
	body := map[string]interface{}{
		"payload":          strings.TrimRight(string(payload), "\r\n"),
		"payload_encoding": decEncoding,
		"topic":            decTopic,
	}
	if decScript != "" {
		script, err := os.ReadFile(decScript)
		if err != nil {
			PrintError("--script-file: %v", err)
		}
		body["script"] = string(script)
	} else {
		body["preset"] = decPreset
	}
	var res map[string]interface{}
	if err := GetClient().Post("/api/decoders/test", body, &res); err != nil {
		PrintError("%v", err)
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(data))
	if e, _ := res["error"].(string); e != "" {
		os.Exit(1)
	}
}

// ── openedge users ──────────────────────────────────────────────────────────

var usersCmd = &cobra.Command{Use: "users", Short: "Manage users"}

var usersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users",
	Run: func(cmd *cobra.Command, args []string) {
		var items []map[string]interface{}
		if err := GetClient().Get("/api/users", &items); err != nil {
			PrintError("%v", err)
		}
		if flagJSON {
			data, _ := json.MarshalIndent(items, "", "  ")
			fmt.Println(string(data))
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, ColorBold+"ID\tUSERNAME\tROLE\tORG"+ColorReset)
		for _, u := range items {
			org := "global"
			if u["org_id"] != nil {
				org = fmt.Sprint(idOf(map[string]interface{}{"id": u["org_id"]}))
			}
			_, _ = fmt.Fprintf(w, "%v\t%v\t%v\t%s\n", idOf(u), u["username"], u["role"], org)
		}
		_ = w.Flush()
	},
}

var (
	userName     string
	userPassword string
	userRole     string
	userFullName string
)

var usersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a user in the organization given by --org",
	Long: `Creates a user. role "user" is read-only (dashboards, trends, alarms,
acknowledging alarms); "admin" manages that organization only. The password
must be at least 12 characters; the user changes it from the profile page.

  openedge users create --org 2 --username mario.rossi --password '<12+ chars>' --role user`,
	Run: func(cmd *cobra.Command, args []string) {
		client := GetClient()
		if client.OrgID() == 0 {
			PrintError("say which organization: --org <id>. A user without one is a global admin")
		}
		body := map[string]interface{}{
			"username": userName, "password": userPassword, "role": userRole,
			"full_name": userFullName, "org_id": client.OrgID(),
		}
		var u map[string]interface{}
		if err := client.Post("/api/users", body, &u); err != nil {
			PrintError("%v", err)
		}
		if flagJSON {
			data, _ := json.MarshalIndent(u, "", "  ")
			fmt.Println(string(data))
			return
		}
		PrintSuccess("User %s created (role %s, organization %d)", userName, userRole, client.OrgID())
	},
}

func init() {
	orgsCmd.AddCommand(orgsCreateCmd)

	sitesCmd.AddCommand(sitesListCmd, sitesCreateCmd)

	areasListCmd.Flags().IntVar(&areasListSite, "site", 0, "Only the areas of this site")
	areasCmd.AddCommand(areasListCmd, areasCreateCmd)

	gatewaysCreateCmd.Flags().IntVar(&gwCreateArea, "area", 0, "Area ID (required)")
	gatewaysCreateCmd.Flags().StringVar(&gwCreateName, "name", "", "Gateway name (required)")
	gatewaysCreateCmd.Flags().StringVar(&gwCreateDriver, "driver", "", "MQTT, MODBUS_TCP, S7, OPC_UA or LORAWAN (required)")
	gatewaysCreateCmd.Flags().StringVar(&gwCreateConfig, "config", "{}", "connection_config as a JSON object")
	gatewaysCreateCmd.Flags().IntVar(&gwCreateScan, "scan-rate", 0, "Poll interval in ms (default from the server)")
	for _, f := range []string{"area", "name", "driver"} {
		_ = gatewaysCreateCmd.MarkFlagRequired(f)
	}
	gatewaysCmd.AddCommand(gatewaysCreateCmd)

	tagsImportCmd.Flags().BoolVar(&tagsImportApply, "apply", false, "Write the sheet (default: preview only)")
	tagsCmd.AddCommand(tagsImportCmd)

	decodersTestCmd.Flags().StringVar(&decPreset, "preset", "", "Preset id (see: openedge decoders presets)")
	decodersTestCmd.Flags().StringVar(&decScript, "script-file", "", "A decode(msg) script instead of a preset")
	decodersTestCmd.Flags().StringVar(&decPayload, "payload-file", "", "File holding the message (required)")
	decodersTestCmd.Flags().StringVar(&decEncoding, "encoding", "text", "How the file holds it: text, hex or base64")
	decodersTestCmd.Flags().StringVar(&decTopic, "topic", "", "Topic the message came on (some presets read it)")
	_ = decodersTestCmd.MarkFlagRequired("payload-file")
	decodersCmd.AddCommand(decodersPresetsCmd, decodersTestCmd)

	usersCreateCmd.Flags().StringVar(&userName, "username", "", "Username (required)")
	usersCreateCmd.Flags().StringVar(&userPassword, "password", "", "Initial password, 12+ characters (required)")
	usersCreateCmd.Flags().StringVar(&userRole, "role", "user", "user (read-only) or admin (of the organization)")
	usersCreateCmd.Flags().StringVar(&userFullName, "full-name", "", "Full name")
	for _, f := range []string{"username", "password"} {
		_ = usersCreateCmd.MarkFlagRequired(f)
	}
	usersCmd.AddCommand(usersListCmd, usersCreateCmd)

	rootCmd.AddCommand(apiCmd, sitesCmd, areasCmd, decodersCmd, usersCmd)
}
