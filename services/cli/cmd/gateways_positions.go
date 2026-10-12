package cmd

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xuri/excelize/v2"
)

// ── openedge gateways positions <file> ──────────────────────────────────────
//
// Positions for many gateways at once, from the sheet a site survey usually
// produces: one row per gateway, its id or name, latitude and longitude (or
// both in one cell, as copied from Google Maps). CSV (comma, semicolon or
// tab) or Excel. Previews unless --apply.

var gwPositionsApply bool

type gatewayPositionRow struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

var gatewaysPositionsCmd = &cobra.Command{
	Use:   "positions <file.csv|file.xlsx>",
	Short: "Set the map position of many gateways from a sheet",
	Long: `Reads one row per gateway: the gateway (id or name), then its latitude and
longitude — or a single "position" column holding both ("39.3592, 16.5142",
as copied from Google Maps). A header row is optional; with one, the columns
are found by name (gateway/id/name, lat/latitude, lon/lng/longitude,
position/coordinates). Decimal comma or point. Previews unless --apply.

  openedge gateways positions sorical.csv
  openedge gateways positions sorical.csv --apply`,
	Args: cobra.ExactArgs(1),
	Run:  runGatewaysPositions,
}

func runGatewaysPositions(cmd *cobra.Command, args []string) {
	rows, err := readSheet(args[0])
	if err != nil {
		PrintError("%v", err)
	}
	entries, problems := parsePositionRows(rows)

	var gateways []gatewayPositionRow
	if err := GetClient().Get("/api/gateways", &gateways); err != nil {
		PrintError("%v", err)
	}

	changed := 0
	for _, e := range entries {
		gw, why := matchGateway(gateways, e.ref)
		if gw == nil {
			problems = append(problems, fmt.Sprintf("row %d: %q — %s", e.line, e.ref, why))
			continue
		}
		cur := "—"
		if gw.Latitude != nil && gw.Longitude != nil {
			cur = fmt.Sprintf("%.6f, %.6f", *gw.Latitude, *gw.Longitude)
		}
		fmt.Printf("  %4d  %-40s  %s → %.6f, %.6f\n", gw.ID, gw.Name, cur, e.lat, e.lon)
		changed++
		if gwPositionsApply {
			if err := GetClient().Put(fmt.Sprintf("/api/gateways/%d", gw.ID),
				map[string]interface{}{"latitude": e.lat, "longitude": e.lon}, nil); err != nil {
				PrintError("gateway %d (%s): %v — the rows above it were saved", gw.ID, gw.Name, err)
			}
		}
	}
	for _, p := range problems {
		fmt.Printf("  ! %s\n", p)
	}
	switch {
	case changed == 0:
		PrintError("no row matched a gateway")
	case gwPositionsApply:
		PrintSuccess("%d gateways positioned", changed)
	default:
		fmt.Printf("\n%d gateways would be positioned. Run again with --apply to save.\n", changed)
	}
}

type positionEntry struct {
	line     int
	ref      string
	lat, lon float64
}

// readSheet returns the cells of a CSV (comma, semicolon or tab) or of the
// first sheet of an Excel workbook.
func readSheet(path string) ([][]string, error) {
	if strings.EqualFold(filepath.Ext(path), ".xlsx") {
		f, err := excelize.OpenFile(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return f.GetRows(f.GetSheetName(0))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	first := strings.SplitN(text, "\n", 2)[0]
	sep := ','
	if strings.Count(first, ";") > strings.Count(first, ",") {
		sep = ';'
	}
	if strings.Count(first, "\t") > strings.Count(first, string(sep)) {
		sep = '\t'
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	return r.ReadAll()
}

// parsePositionRows finds the columns (by header, or gateway/lat/lon in that
// order) and reads each row's coordinates.
func parsePositionRows(rows [][]string) ([]positionEntry, []string) {
	colRef, colLat, colLon, colPos := 0, 1, 2, -1
	start := 0
	if len(rows) > 0 && looksLikeHeader(rows[0]) {
		colRef, colLat, colLon, colPos = -1, -1, -1, -1
		for i, h := range rows[0] {
			switch strings.ToLower(strings.TrimSpace(h)) {
			case "gateway", "id", "name", "nome", "gateway_id", "gateway id":
				if colRef < 0 {
					colRef = i
				}
			case "lat", "latitude", "latitudine":
				colLat = i
			case "lon", "lng", "long", "longitude", "longitudine":
				colLon = i
			case "position", "posizione", "coordinates", "coordinate", "coords":
				colPos = i
			}
		}
		if colRef < 0 {
			colRef = 0
		}
		start = 1
	}
	var out []positionEntry
	var problems []string
	cell := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	for n := start; n < len(rows); n++ {
		row := rows[n]
		ref := cell(row, colRef)
		if ref == "" {
			continue
		}
		var latS, lonS string
		if colPos >= 0 && cell(row, colPos) != "" {
			latS, lonS = splitPair(cell(row, colPos))
		} else {
			latS, lonS = cell(row, colLat), cell(row, colLon)
			// No longitude column, but the latitude cell holds a pair.
			if lonS == "" {
				latS, lonS = splitPair(latS)
			}
		}
		lat, err1 := parseCoordNumber(latS)
		lon, err2 := parseCoordNumber(lonS)
		if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			problems = append(problems, fmt.Sprintf("row %d: %q — coordinates not readable (%q, %q)", n+1, ref, latS, lonS))
			continue
		}
		out = append(out, positionEntry{line: n + 1, ref: ref, lat: lat, lon: lon})
	}
	return out, problems
}

func looksLikeHeader(row []string) bool {
	for _, c := range row {
		if _, err := parseCoordNumber(c); err == nil && strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// splitPair splits "39.35, 16.51" or "39,35; 16,51" or "39.35 16.51".
func splitPair(s string) (string, string) {
	s = strings.TrimSpace(s)
	for _, sep := range []string{";", ", ", "\t", " "} {
		if parts := strings.SplitN(s, sep, 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	// "39.35,16.51": a comma between two dotted decimals.
	if strings.Count(s, ",") == 1 && strings.Count(s, ".") == 2 {
		parts := strings.SplitN(s, ",", 2)
		return parts[0], parts[1]
	}
	return s, ""
}

func parseCoordNumber(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
}

// matchGateway finds the gateway a row names: by id, by exact name (any
// case), or by the one name that contains it.
func matchGateway(gws []gatewayPositionRow, ref string) (*gatewayPositionRow, string) {
	if id, err := strconv.Atoi(ref); err == nil {
		for i := range gws {
			if gws[i].ID == id {
				return &gws[i], ""
			}
		}
	}
	var contains []int
	for i := range gws {
		if strings.EqualFold(gws[i].Name, ref) {
			return &gws[i], ""
		}
		if strings.Contains(strings.ToLower(gws[i].Name), strings.ToLower(ref)) {
			contains = append(contains, i)
		}
	}
	switch len(contains) {
	case 1:
		return &gws[contains[0]], ""
	case 0:
		return nil, "no gateway with this id or name"
	}
	return nil, fmt.Sprintf("%d gateways match: give the id or the full name", len(contains))
}

func init() {
	gatewaysPositionsCmd.Flags().BoolVar(&gwPositionsApply, "apply", false, "Save the positions (default: preview only)")
	gatewaysCmd.AddCommand(gatewaysPositionsCmd)
}
