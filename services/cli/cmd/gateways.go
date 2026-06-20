package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var gatewaysCmd = &cobra.Command{
	Use:   "gateways",
	Short: "Manage gateways",
}

var gatewaysListCmd = &cobra.Command{
	Use:   "list",
	Short: "List gateways",
	Run:   runGatewaysList,
}

var gatewaysGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get gateway details",
	Args:  cobra.ExactArgs(1),
	Run:   runGatewaysGet,
}

var gatewaysTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Test gateway connection",
	Args:  cobra.ExactArgs(1),
	Run:   runGatewaysTest,
}

var gatewaysLoRaWANCmd = &cobra.Command{
	Use:   "lorawan <gateway-id>",
	Short: "List LoRaWAN devices for a gateway",
	Args:  cobra.ExactArgs(1),
	Run:   runGatewaysLoRaWAN,
}

var gatewayListOrg int

func init() {
	gatewaysListCmd.Flags().IntVar(&gatewayListOrg, "org", 0, "Filter by organization ID")

	gatewaysCmd.AddCommand(gatewaysListCmd)
	gatewaysCmd.AddCommand(gatewaysGetCmd)
	gatewaysCmd.AddCommand(gatewaysTestCmd)
	gatewaysCmd.AddCommand(gatewaysLoRaWANCmd)
}

type Gateway struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type LoRaWANDevice struct {
	DeviceID     string      `json:"device_id"`
	DevEUI       string      `json:"dev_eui"`
	LastSeen     *time.Time  `json:"last_seen"`
	RSSI         interface{} `json:"rssi"`
	UplinkCount  int         `json:"uplink_count"`
	FieldsCount  int         `json:"fields_count"`
	TagsImported int         `json:"tags_imported"`
}

func runGatewaysList(cmd *cobra.Command, args []string) {
	client := GetClient()

	path := "/api/gateways"
	if gatewayListOrg > 0 {
		path += "?org_id=" + strconv.Itoa(gatewayListOrg)
	}

	var gateways []Gateway
	if err := client.Get(path, &gateways); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(gateways, "", "  ")
		fmt.Println(string(data))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"ID\tNAME\tTYPE\tSTATUS\tENABLED"+ColorReset)
	for _, g := range gateways {
		statusColor := ColorGreen
		if g.Status == "offline" || g.Status == "error" {
			statusColor = ColorRed
		} else if g.Status == "warning" {
			statusColor = ColorYellow
		}
		enabled := ColorGreen + "yes" + ColorReset
		if !g.Enabled {
			enabled = ColorYellow + "no" + ColorReset
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s%s%s\t%s\n",
			g.ID, g.Name, g.Type,
			statusColor, g.Status, ColorReset,
			enabled)
	}
	w.Flush()
}

func runGatewaysGet(cmd *cobra.Command, args []string) {
	client := GetClient()

	var gw map[string]interface{}
	if err := client.Get("/api/gateways/"+args[0], &gw); err != nil {
		PrintError("%v", err)
	}

	data, _ := json.MarshalIndent(gw, "", "  ")
	fmt.Println(string(data))
}

func runGatewaysTest(cmd *cobra.Command, args []string) {
	client := GetClient()

	var result map[string]interface{}
	if err := client.Post("/api/gateways/"+args[0]+"/test", nil, &result); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	if msg, ok := result["message"].(string); ok {
		PrintSuccess("%s", msg)
	} else {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	}
}

func runGatewaysLoRaWAN(cmd *cobra.Command, args []string) {
	client := GetClient()

	var devices []LoRaWANDevice
	if err := client.Get("/api/gateways/"+args[0]+"/lorawan/devices", &devices); err != nil {
		PrintError("%v", err)
	}

	if flagJSON {
		data, _ := json.MarshalIndent(devices, "", "  ")
		fmt.Println(string(data))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, ColorBold+"DEVICE ID\tDEV EUI\tLAST SEEN\tRSSI\tUPLINKS\tFIELDS"+ColorReset)
	for _, d := range devices {
		lastSeen := "never"
		if d.LastSeen != nil {
			lastSeen = d.LastSeen.Format("2006-01-02 15:04:05")
		}
		rssi := "-"
		if d.RSSI != nil {
			rssi = fmt.Sprintf("%v", d.RSSI)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\n",
			d.DeviceID, d.DevEUI, lastSeen, rssi, d.UplinkCount, d.FieldsCount)
	}
	w.Flush()
}
