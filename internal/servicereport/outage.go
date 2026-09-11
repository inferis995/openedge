// Package servicereport builds the document handed to a customer at the end of
// a month: what is installed, what stopped, for how long, and what was done
// about it.
//
// It is the artifact that justifies a recurring invoice, which means the
// numbers in it are read by somebody deciding whether to keep paying. They have
// to be defensible, and the definitions behind them have to be written down.
package servicereport

import (
	"sort"
	"time"
)

// Outage is one stretch during which a gateway was not answering.
//
// End is the zero time when the outage was still open at the moment the report
// was built — the alarm never cleared. That is not the same as an outage of
// length zero, and the two must not be confused by anything that consumes this.
type Outage struct {
	GatewayID   int
	GatewayName string
	Start       time.Time
	End         time.Time
}

// Open reports whether the outage had not ended.
func (o Outage) Open() bool { return o.End.IsZero() }

// MergeOutages clamps a gateway's outages to the reporting period and merges
// the ones that overlap or touch.
//
// The merge is the point. A gateway with four tags carrying a comm_loss rule
// produces four alarms for one unplugged cable, all covering the same stretch
// of the afternoon. Summing their durations reports four hours of downtime for
// one hour of it — in a document somebody is using to decide whether the
// service is worth paying for.
//
// Outages are clamped rather than dropped: one that started last month and
// ended inside this one is real downtime for this month, for the part that
// falls inside it.
func MergeOutages(outages []Outage, from, to time.Time) []Outage {
	clamped := make([]Outage, 0, len(outages))
	for _, o := range outages {
		end := o.End
		if o.Open() || end.After(to) {
			// Still open, or ended after the period: for this report it lasted
			// until the end of the period. Reporting an open outage as running
			// to "now" would make a report of last month grow every time it was
			// regenerated.
			end = to
		}
		start := o.Start
		if start.Before(from) {
			start = from
		}
		if !end.After(start) {
			continue // entirely outside the period, or zero-length inside it
		}
		clamped = append(clamped, Outage{
			GatewayID: o.GatewayID, GatewayName: o.GatewayName,
			Start: start, End: end,
		})
	}

	if len(clamped) == 0 {
		return nil
	}

	sort.Slice(clamped, func(i, j int) bool {
		if clamped[i].GatewayID != clamped[j].GatewayID {
			return clamped[i].GatewayID < clamped[j].GatewayID
		}
		return clamped[i].Start.Before(clamped[j].Start)
	})

	merged := []Outage{clamped[0]}
	for _, o := range clamped[1:] {
		last := &merged[len(merged)-1]
		// Touching counts as overlapping: two alarms a second apart on the same
		// gateway are one interruption as far as anybody in the plant is
		// concerned.
		if o.GatewayID == last.GatewayID && !o.Start.After(last.End) {
			if o.End.After(last.End) {
				last.End = o.End
			}
			continue
		}
		merged = append(merged, o)
	}
	return merged
}

// TotalDowntime sums merged outages. It must be given the output of
// MergeOutages: given raw ones it double-counts every overlap.
func TotalDowntime(merged []Outage) time.Duration {
	var total time.Duration
	for _, o := range merged {
		total += o.End.Sub(o.Start)
	}
	return total
}

// GatewayUptime is one gateway's availability over the period.
type GatewayUptime struct {
	GatewayID     int           `json:"gateway_id"`
	GatewayName   string        `json:"gateway_name"`
	Interruptions int           `json:"interruptions"`
	Downtime      time.Duration `json:"downtime_ns"`
	// Availability is the fraction of the period the gateway was answering,
	// between 0 and 1.
	Availability float64 `json:"availability"`
}

// Uptime turns merged outages into one row per gateway.
//
// gateways is every gateway that should appear, including the ones that never
// failed: a report that lists only what broke reads as a list of complaints
// rather than as a statement about a plant.
func Uptime(merged []Outage, gateways map[int]string, from, to time.Time) []GatewayUptime {
	period := to.Sub(from)

	byGateway := make(map[int]*GatewayUptime, len(gateways))
	for id, name := range gateways {
		byGateway[id] = &GatewayUptime{GatewayID: id, GatewayName: name, Availability: 1}
	}

	for _, o := range merged {
		row, ok := byGateway[o.GatewayID]
		if !ok {
			// An outage for a gateway that has since been deleted still
			// happened, and leaving it out would quietly improve the numbers.
			row = &GatewayUptime{GatewayID: o.GatewayID, GatewayName: o.GatewayName, Availability: 1}
			byGateway[o.GatewayID] = row
		}
		row.Interruptions++
		row.Downtime += o.End.Sub(o.Start)
	}

	rows := make([]GatewayUptime, 0, len(byGateway))
	for _, row := range byGateway {
		if period > 0 {
			row.Availability = 1 - float64(row.Downtime)/float64(period)
			if row.Availability < 0 {
				row.Availability = 0
			}
		}
		rows = append(rows, *row)
	}

	// Worst first: the reader wants the problems, then the rest by name.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Downtime != rows[j].Downtime {
			return rows[i].Downtime > rows[j].Downtime
		}
		return rows[i].GatewayName < rows[j].GatewayName
	})
	return rows
}

// FleetAvailability is the availability of the plant as a whole: the share of
// all the gateway-time in the period during which gateways were answering.
//
// While every gateway is measured over the same period this is arithmetically
// the same number as the average of the per-gateway percentages — the average
// expands to exactly this ratio. It is written as a ratio of total downtime to
// total gateway-time anyway, because that is the sentence the report puts under
// it, and because the day a gateway installed mid-month is measured over the
// part of the period it existed for, the two stop agreeing and this is the
// definition that stays correct.
func FleetAvailability(rows []GatewayUptime, from, to time.Time) float64 {
	period := to.Sub(from)
	if period <= 0 || len(rows) == 0 {
		return 1
	}
	var down time.Duration
	for _, r := range rows {
		down += r.Downtime
	}
	total := time.Duration(len(rows)) * period
	available := 1 - float64(down)/float64(total)
	if available < 0 {
		return 0
	}
	return available
}
