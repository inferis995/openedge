package inventory

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

// Device is one gateway as it appears in the inventory.
type Device struct {
	GatewayID int    `json:"gateway_id"`
	Name      string `json:"name"`
	Site      string `json:"site"`
	Area      string `json:"area"`
	OrgName   string `json:"organization"`

	Protocol string `json:"protocol"`
	Endpoint string `json:"endpoint"`

	ScanRateMs int  `json:"scan_rate_ms"`
	Enabled    bool `json:"enabled"`

	// Health is what the driver last reported: online, offline, error, or empty
	// when it has never said anything.
	Health       string     `json:"health"`
	HealthSeenAt *time.Time `json:"health_seen_at,omitempty"`
	AgentVersion string     `json:"agent_version,omitempty"`

	Tags           int `json:"tags"`
	HistorizedTags int `json:"historized_tags"`
	AlarmedTags    int `json:"alarmed_tags"`

	FirstSeen time.Time `json:"first_seen"`
}

// Collect assembles the inventory.
//
// orgID scopes it to one tenant; zero means every organization, which only a
// global admin ever asks for. Gateways carry no org_id of their own — they hang
// off areas, sites and organizations — so the scope is applied through that
// join and not through anything the caller can set.
func Collect(ctx context.Context, db *sql.DB, orgID int) ([]Device, error) {
	// The tag counts are subqueries rather than joins: three LEFT JOINs onto
	// tags and alarm_definitions multiply the rows against each other, and the
	// counts come out wrong in a way that looks plausible.
	const query = `
		SELECT g.id, g.name, s.name, a.name, o.name,
		       g.driver_type, g.connection_config, g.scan_rate_ms, g.enabled,
		       COALESCE(g.health_status, ''), g.health_reported_at,
		       COALESCE(g.agent_version, ''), g.created_at,
		       (SELECT COUNT(*) FROM tags t WHERE t.gateway_id = g.id),
		       (SELECT COUNT(*) FROM tags t WHERE t.gateway_id = g.id AND t.historize),
		       (SELECT COUNT(DISTINCT t.id) FROM tags t
		          JOIN alarm_definitions ad ON ad.tag_id = t.id AND ad.enabled
		         WHERE t.gateway_id = g.id)
		FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		JOIN organizations o ON o.id = s.org_id
		WHERE ($1 = 0 OR o.id = $1)
		ORDER BY o.name, s.name, a.name, g.name`

	rows, err := db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	devices := []Device{}
	for rows.Next() {
		var d Device
		var rawConfig []byte
		var healthSeen sql.NullTime

		if scanErr := rows.Scan(
			&d.GatewayID, &d.Name, &d.Site, &d.Area, &d.OrgName,
			&d.Protocol, &rawConfig, &d.ScanRateMs, &d.Enabled,
			&d.Health, &healthSeen, &d.AgentVersion, &d.FirstSeen,
			&d.Tags, &d.HistorizedTags, &d.AlarmedTags,
		); scanErr != nil {
			// A device that cannot be read is a device missing from the
			// inventory, and an inventory with a hole in it is worse than none:
			// it is trusted.
			return nil, fmt.Errorf("reading a gateway row: %w", scanErr)
		}

		if healthSeen.Valid {
			t := healthSeen.Time
			d.HealthSeenAt = &t
		}

		var config map[string]interface{}
		if len(rawConfig) > 0 {
			if jsonErr := json.Unmarshal(rawConfig, &config); jsonErr != nil {
				config = nil
			}
		}
		d.Endpoint = Endpoint(d.Protocol, config)

		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// csvHeader is the column order of the exported file. Kept next to the row
// builder so the two cannot drift apart.
var csvHeader = []string{
	"Organizzazione", "Sito", "Area", "Gateway",
	"Protocollo", "Indirizzo",
	"Stato", "Ultimo contatto", "Abilitato",
	"Scan (ms)", "Tag", "Storicizzati", "Con allarmi",
	"Versione agente", "Censito dal",
}

// WriteCSV renders the inventory as the file that goes into a report.
//
// Semicolon-separated, with a UTF-8 byte order mark: this is opened in Excel on
// an Italian machine, where a comma is a decimal separator and a file without a
// BOM turns every accented letter into mojibake.
func WriteCSV(w io.Writer, devices []Device) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	cw.Comma = ';'

	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for i := range devices {
		if err := cw.Write(csvRow(&devices[i])); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func csvRow(d *Device) []string {
	lastSeen := "mai"
	if d.HealthSeenAt != nil {
		lastSeen = d.HealthSeenAt.Format("2006-01-02 15:04")
	}
	health := d.Health
	if health == "" {
		health = "mai contattato"
	}
	enabled := "no"
	if d.Enabled {
		enabled = "sì"
	}
	return []string{
		d.OrgName, d.Site, d.Area, d.Name,
		d.Protocol, d.Endpoint,
		health, lastSeen, enabled,
		strconv.Itoa(d.ScanRateMs),
		strconv.Itoa(d.Tags), strconv.Itoa(d.HistorizedTags), strconv.Itoa(d.AlarmedTags),
		d.AgentVersion,
		d.FirstSeen.Format("2006-01-02"),
	}
}

// Summary is the count of what is installed, by protocol and by state.
type Summary struct {
	Devices    int            `json:"devices"`
	Online     int            `json:"online"`
	Offline    int            `json:"offline"`
	Unknown    int            `json:"unknown"`
	Disabled   int            `json:"disabled"`
	Tags       int            `json:"tags"`
	ByProtocol map[string]int `json:"by_protocol"`
}

// Summarize counts the inventory.
func Summarize(devices []Device) Summary {
	s := Summary{ByProtocol: map[string]int{}}
	for i := range devices {
		d := &devices[i]
		s.Devices++
		s.Tags += d.Tags
		s.ByProtocol[d.Protocol]++

		if !d.Enabled {
			// A disabled gateway is not offline: nobody is asking it anything.
			// Counting it as a fault is how a summary stops being read.
			s.Disabled++
			continue
		}
		switch d.Health {
		case "online":
			s.Online++
		case "offline", "error":
			s.Offline++
		default:
			s.Unknown++
		}
	}
	return s
}
