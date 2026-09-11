package servicereport

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/inventory"
)

// Report is the document.
type Report struct {
	Organization string    `json:"organization"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	GeneratedAt  time.Time `json:"generated_at"`

	Plant   Plant    `json:"plant"`
	Service Service  `json:"service"`
	Alarms  Alarms   `json:"alarms"`
	History History  `json:"history"`
	Backups *Backups `json:"backups,omitempty"`
}

// Plant is what is installed.
type Plant struct {
	Devices    int            `json:"devices"`
	Online     int            `json:"online"`
	Offline    int            `json:"offline"`
	Unknown    int            `json:"unknown"`
	Disabled   int            `json:"disabled"`
	Tags       int            `json:"tags"`
	ByProtocol map[string]int `json:"by_protocol"`
}

// Service is continuity of service over the period.
type Service struct {
	FleetAvailability float64         `json:"fleet_availability"`
	Interruptions     int             `json:"interruptions"`
	Downtime          time.Duration   `json:"downtime_ns"`
	Gateways          []GatewayUptime `json:"gateways"`
}

// Alarms is what the plant raised.
type Alarms struct {
	Total        int            `json:"total"`
	BySeverity   map[string]int `json:"by_severity"`
	StillOpen    int            `json:"still_open"`
	Acknowledged int            `json:"acknowledged"`
	// MeanAck is the mean time between an alarm firing and somebody
	// acknowledging it, over the alarms that were acknowledged. Zero when none
	// were.
	MeanAck time.Duration `json:"mean_ack_ns"`
	TopTags []TagAlarms   `json:"top_tags"`
}

// TagAlarms is one tag's alarm count over the period.
type TagAlarms struct {
	Alias string `json:"alias"`
	Count int    `json:"count"`
}

// History is what was recorded.
type History struct {
	Samples int64 `json:"samples"`
	Tags    int   `json:"tags"`
}

// Backups covers the platform, not one tenant: backup_audit carries no
// organization. It is therefore filled in only for a global admin, and left out
// of a single tenant's report rather than showing them somebody else's.
type Backups struct {
	Taken     int        `json:"taken"`
	Failed    int        `json:"failed"`
	LastTaken *time.Time `json:"last_taken,omitempty"`
}

// Build assembles the report for one organization over [from, to).
//
// orgID zero means every organization, which only a global admin asks for.
func Build(ctx context.Context, db *sql.DB, orgID int, from, to time.Time) (*Report, error) {
	if !to.After(from) {
		return nil, fmt.Errorf("the period ends before it starts")
	}

	r := &Report{From: from, To: to, GeneratedAt: time.Now().UTC()}

	if orgID > 0 {
		if err := db.QueryRowContext(ctx,
			`SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&r.Organization); err != nil {
			return nil, fmt.Errorf("reading the organization: %w", err)
		}
	} else {
		r.Organization = "Tutte le organizzazioni"
	}

	devices, err := inventory.Collect(ctx, db, orgID)
	if err != nil {
		return nil, fmt.Errorf("building the plant section: %w", err)
	}
	summary := inventory.Summarize(devices)
	r.Plant = Plant{
		Devices: summary.Devices, Online: summary.Online, Offline: summary.Offline,
		Unknown: summary.Unknown, Disabled: summary.Disabled, Tags: summary.Tags,
		ByProtocol: summary.ByProtocol,
	}

	gateways := make(map[int]string, len(devices))
	for i := range devices {
		if devices[i].Enabled {
			// A disabled gateway is not expected to answer, so counting its
			// silence against the availability of the plant would be reporting
			// a decision as a fault.
			gateways[devices[i].GatewayID] = devices[i].Name
		}
	}

	if err := r.buildService(ctx, db, orgID, gateways, from, to); err != nil {
		return nil, err
	}
	if err := r.buildAlarms(ctx, db, orgID, from, to); err != nil {
		return nil, err
	}
	if err := r.buildHistory(ctx, db, orgID, from, to); err != nil {
		return nil, err
	}
	if orgID == 0 {
		if err := r.buildBackups(ctx, db, from, to); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// buildService reads the communication outages from the alarms the health rules
// raised, and turns them into availability.
//
// The source is alarm_events rather than a table of its own: comm_loss and
// frozen already write one row per interruption, with the time it started and
// the time it cleared. A second record of the same fact would be a second
// record to keep in step with the first.
func (r *Report) buildService(ctx context.Context, db *sql.DB, orgID int, gateways map[int]string, from, to time.Time) error {
	rows, err := db.QueryContext(ctx, `
		SELECT g.id, g.name, e.trigger_time, e.clear_time
		FROM alarm_events e
		JOIN tags t ON t.id = e.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		JOIN organizations o ON o.id = s.org_id
		WHERE e.alarm_type = 'comm_loss'
		  AND ($1 = 0 OR o.id = $1)
		  AND e.trigger_time < $3
		  AND (e.clear_time IS NULL OR e.clear_time > $2)`,
		orgID, from, to)
	if err != nil {
		return fmt.Errorf("reading the interruptions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var outages []Outage
	for rows.Next() {
		var o Outage
		var clear sql.NullTime
		if scanErr := rows.Scan(&o.GatewayID, &o.GatewayName, &o.Start, &clear); scanErr != nil {
			return fmt.Errorf("reading an interruption: %w", scanErr)
		}
		if clear.Valid {
			o.End = clear.Time
		}
		outages = append(outages, o)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	merged := MergeOutages(outages, from, to)
	r.Service = Service{
		Interruptions: len(merged),
		Downtime:      TotalDowntime(merged),
		Gateways:      Uptime(merged, gateways, from, to),
	}
	r.Service.FleetAvailability = FleetAvailability(r.Service.Gateways, from, to)
	return nil
}

func (r *Report) buildAlarms(ctx context.Context, db *sql.DB, orgID int, from, to time.Time) error {
	r.Alarms.BySeverity = map[string]int{}

	rows, err := db.QueryContext(ctx, `
		SELECT e.severity,
		       COUNT(*),
		       COUNT(*) FILTER (WHERE e.clear_time IS NULL OR e.clear_time >= $3),
		       COUNT(*) FILTER (WHERE e.ack_time IS NOT NULL),
		       -- FILTER attaches to the aggregate, which is AVG, and has to sit
		       -- inside EXTRACT. Hung on the EXTRACT instead it is a syntax
		       -- error: EXTRACT is not an aggregate, and Postgres refuses the
		       -- whole statement.
		       COALESCE(EXTRACT(EPOCH FROM AVG(e.ack_time - e.trigger_time)
		                        FILTER (WHERE e.ack_time IS NOT NULL)), 0)
		FROM alarm_events e
		JOIN tags t ON t.id = e.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		JOIN organizations o ON o.id = s.org_id
		WHERE ($1 = 0 OR o.id = $1)
		  AND e.trigger_time >= $2 AND e.trigger_time < $3
		GROUP BY e.severity`,
		orgID, from, to)
	if err != nil {
		return fmt.Errorf("reading the alarms: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ackTotal float64
	var ackCount int
	for rows.Next() {
		var severity string
		var count, open, acked int
		var meanAckSeconds float64
		if scanErr := rows.Scan(&severity, &count, &open, &acked, &meanAckSeconds); scanErr != nil {
			return fmt.Errorf("reading an alarm group: %w", scanErr)
		}
		r.Alarms.BySeverity[severity] = count
		r.Alarms.Total += count
		r.Alarms.StillOpen += open
		r.Alarms.Acknowledged += acked
		// Weighted back up by the number of alarms in the group, so the mean is
		// over alarms and not over severities: three info alarms acknowledged
		// in a minute and one critical in an hour must not average to half an
		// hour.
		ackTotal += meanAckSeconds * float64(acked)
		ackCount += acked
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if ackCount > 0 {
		r.Alarms.MeanAck = time.Duration(ackTotal/float64(ackCount)) * time.Second
	}

	return r.buildTopTags(ctx, db, orgID, from, to)
}

func (r *Report) buildTopTags(ctx context.Context, db *sql.DB, orgID int, from, to time.Time) error {
	rows, err := db.QueryContext(ctx, `
		SELECT t.alias, COUNT(*) AS n
		FROM alarm_events e
		JOIN tags t ON t.id = e.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		JOIN organizations o ON o.id = s.org_id
		WHERE ($1 = 0 OR o.id = $1)
		  AND e.trigger_time >= $2 AND e.trigger_time < $3
		GROUP BY t.alias
		ORDER BY n DESC, t.alias
		LIMIT 10`,
		orgID, from, to)
	if err != nil {
		return fmt.Errorf("reading the noisiest tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var row TagAlarms
		if scanErr := rows.Scan(&row.Alias, &row.Count); scanErr != nil {
			return fmt.Errorf("reading a tag group: %w", scanErr)
		}
		r.Alarms.TopTags = append(r.Alarms.TopTags, row)
	}
	return rows.Err()
}

func (r *Report) buildHistory(ctx context.Context, db *sql.DB, orgID int, from, to time.Time) error {
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT h.tag_id)
		FROM tag_history h
		JOIN tags t ON t.id = h.tag_id
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id
		JOIN sites s ON s.id = a.site_id
		JOIN organizations o ON o.id = s.org_id
		WHERE ($1 = 0 OR o.id = $1)
		  AND h.time >= $2 AND h.time < $3`,
		orgID, from, to).Scan(&r.History.Samples, &r.History.Tags)
	if err != nil {
		return fmt.Errorf("reading the historian: %w", err)
	}
	return nil
}

func (r *Report) buildBackups(ctx context.Context, db *sql.DB, from, to time.Time) error {
	var b Backups
	var last sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE action = 'backup_created'),
		       COUNT(*) FILTER (WHERE action = 'backup_failed'),
		       MAX(created_at) FILTER (WHERE action = 'backup_created')
		FROM backup_audit
		WHERE created_at >= $1 AND created_at < $2`,
		from, to).Scan(&b.Taken, &b.Failed, &last)
	if err != nil {
		return fmt.Errorf("reading the backup log: %w", err)
	}
	if last.Valid {
		t := last.Time
		b.LastTaken = &t
	}
	r.Backups = &b
	return nil
}
