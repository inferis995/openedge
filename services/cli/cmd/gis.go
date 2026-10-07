package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Gateway position and area boundary, from the command line: everything the
// map page can do must also be scriptable (mass provisioning from a CSV of
// GPS coordinates, boundary bulk import).
var (
	gwPositionLat   float64
	gwPositionLon   float64
	gwPositionClear bool

	areaBoundaryFile  string
	areaBoundaryClear bool
)

var gatewayPositionCmd = &cobra.Command{
	Use:   "position <gateway-id>",
	Short: "Set a gateway's geographic position",
	Long: `Set or clear the latitude/longitude of a gateway.

Both coordinates must be given together; --clear removes the position.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := parseID(args[0], "id")
		client := GetClient()
		if gwPositionClear {
			return client.Put(fmt.Sprintf("/api/gateways/%d", id),
				map[string]interface{}{"latitude": nil, "longitude": nil}, nil)
		}
		if gwPositionLat == 0 && gwPositionLon == 0 {
			return fmt.Errorf("give --lat and --lon, or --clear")
		}
		if gwPositionLat < -90 || gwPositionLat > 90 || gwPositionLon < -180 || gwPositionLon > 180 {
			return fmt.Errorf("coordinates out of range: lat [-90,90], lon [-180,180]")
		}
		if err := client.Put(fmt.Sprintf("/api/gateways/%d", id),
			map[string]interface{}{"latitude": gwPositionLat, "longitude": gwPositionLon}, nil); err != nil {
			return err
		}
		fmt.Printf("gateway %d position updated\n", id)
		return nil
	},
}

var areaBoundaryCmd = &cobra.Command{
	Use:   "boundary <area-id>",
	Short: "Set an area's boundary GeoJSON",
	Long: `Set the boundary polygon of an area from a GeoJSON file
(geometry or Feature; a FeatureCollection is rejected), or clear it
with --clear.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := parseID(args[0], "id")
		client := GetClient()
		if areaBoundaryClear {
			if err := client.Put(fmt.Sprintf("/api/areas/%d", id),
				map[string]interface{}{"boundary": nil}, nil); err != nil {
				return err
			}
			fmt.Printf("area %d boundary cleared\n", id)
			return nil
		}
		if areaBoundaryFile == "" {
			return fmt.Errorf("give --file <geojson> or --clear")
		}
		raw, err := os.ReadFile(areaBoundaryFile)
		if err != nil {
			return err
		}
		var probe map[string]interface{}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", areaBoundaryFile, err)
		}
		if t, _ := probe["type"].(string); t == "FeatureCollection" {
			return fmt.Errorf("FeatureCollection not supported: pass a single geometry or Feature")
		}
		var boundary interface{}
		if err := json.Unmarshal(raw, &boundary); err != nil {
			return err
		}
		if err := client.Put(fmt.Sprintf("/api/areas/%d", id),
			map[string]interface{}{"boundary": boundary}, nil); err != nil {
			return err
		}
		fmt.Printf("area %d boundary updated\n", id)
		return nil
	},
}

func init() {
	gatewayPositionCmd.Flags().Float64Var(&gwPositionLat, "lat", 0, "Latitude (decimal degrees)")
	gatewayPositionCmd.Flags().Float64Var(&gwPositionLon, "lon", 0, "Longitude (decimal degrees)")
	gatewayPositionCmd.Flags().BoolVar(&gwPositionClear, "clear", false, "Remove the position")
	areaBoundaryCmd.Flags().StringVar(&areaBoundaryFile, "file", "", "GeoJSON file (geometry or Feature)")
	areaBoundaryCmd.Flags().BoolVar(&areaBoundaryClear, "clear", false, "Remove the boundary")

	gatewaysCmd.AddCommand(gatewayPositionCmd)
	areasCmd.AddCommand(areaBoundaryCmd)
}
