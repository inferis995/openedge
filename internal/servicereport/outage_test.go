package servicereport_test

import (
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/servicereport"
)

// A calendar month, which is what these reports cover.
var (
	from = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to   = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
)

func at(day, hour int) time.Time {
	return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
}

func outage(gateway int, startDay, startHour, endDay, endHour int) servicereport.Outage {
	return servicereport.Outage{
		GatewayID: gateway, GatewayName: "PLC",
		Start: at(startDay, startHour), End: at(endDay, endHour),
	}
}

// The reason this merge exists. A gateway with four tags carrying a comm_loss
// rule raises four alarms for one unplugged cable, all covering the same
// afternoon. Summed, that is four hours of downtime for one hour of it — in a
// document somebody is using to decide whether to keep paying.
func TestFourAlarmsForOneCableAreOneInterruption(t *testing.T) {
	var raw []servicereport.Outage
	for i := 0; i < 4; i++ {
		raw = append(raw, outage(1, 10, 14, 10, 15))
	}

	merged := servicereport.MergeOutages(raw, from, to)
	if len(merged) != 1 {
		t.Fatalf("four overlapping alarms produced %d interruptions, want 1", len(merged))
	}
	if got := servicereport.TotalDowntime(merged); got != time.Hour {
		t.Errorf("downtime = %s, want 1h", got)
	}
}

func TestSeparateOutagesStaySeparate(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{
		outage(1, 3, 10, 3, 11),
		outage(1, 17, 9, 17, 12),
	}, from, to)

	if len(merged) != 2 {
		t.Fatalf("two outages a fortnight apart produced %d, want 2", len(merged))
	}
	if got := servicereport.TotalDowntime(merged); got != 4*time.Hour {
		t.Errorf("downtime = %s, want 4h", got)
	}
}

// Partly overlapping is still one interruption, and its length is the union.
func TestPartlyOverlappingOutagesBecomeOne(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{
		outage(1, 10, 8, 10, 12),
		outage(1, 10, 11, 10, 15),
	}, from, to)

	if len(merged) != 1 {
		t.Fatalf("got %d interruptions, want 1", len(merged))
	}
	if got := servicereport.TotalDowntime(merged); got != 7*time.Hour {
		t.Errorf("downtime = %s, want 7h (08:00 to 15:00)", got)
	}
}

// Two gateways failing at the same time are two interruptions. Merging across
// gateways would report one plant-wide event and hide which machine stopped.
func TestOutagesOnDifferentGatewaysAreNotMerged(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{
		outage(1, 10, 14, 10, 15),
		outage(2, 10, 14, 10, 15),
	}, from, to)

	if len(merged) != 2 {
		t.Fatalf("two gateways down together produced %d interruptions, want 2", len(merged))
	}
}

// An outage that started last month and ended in this one is this month's
// downtime, for the part that falls inside it.
func TestAnOutageStartingBeforeThePeriodIsClamped(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{{
		GatewayID: 1,
		Start:     time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
		End:       at(1, 6),
	}}, from, to)

	if got := servicereport.TotalDowntime(merged); got != 6*time.Hour {
		t.Errorf("downtime = %s, want 6h — only the part inside the month counts", got)
	}
}

// An outage still open when the report is built ran to the end of the period,
// not to "now". Otherwise a report of last month grows every time it is
// regenerated, and two copies of the same document disagree.
func TestAnOpenOutageEndsWithThePeriodNotWithToday(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{{
		GatewayID: 1, Start: at(31, 20), // never cleared
	}}, from, to)

	if got := servicereport.TotalDowntime(merged); got != 4*time.Hour {
		t.Errorf("downtime = %s, want 4h (20:00 to midnight)", got)
	}
}

func TestAnOutageEntirelyOutsideThePeriodIsIgnored(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{{
		GatewayID: 1,
		Start:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
	}}, from, to)

	if len(merged) != 0 {
		t.Fatalf("an outage from two months ago appeared in this report: %v", merged)
	}
}

// Two alarms a second apart on the same gateway are one interruption as far as
// anybody in the plant is concerned.
func TestTouchingOutagesAreOneInterruption(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{
		{GatewayID: 1, Start: at(10, 8), End: at(10, 9)},
		{GatewayID: 1, Start: at(10, 9), End: at(10, 10)},
	}, from, to)

	if len(merged) != 1 {
		t.Fatalf("got %d interruptions, want 1", len(merged))
	}
	if got := servicereport.TotalDowntime(merged); got != 2*time.Hour {
		t.Errorf("downtime = %s, want 2h", got)
	}
}

// ---------------------------------------------------------------------------
// Uptime
// ---------------------------------------------------------------------------

// A report that lists only what broke reads as a list of complaints. The
// gateways that ran all month belong in it too, at 100%.
func TestGatewaysThatNeverFailedAppearAtFullAvailability(t *testing.T) {
	rows := servicereport.Uptime(nil, map[int]string{1: "PLC A", 2: "PLC B"}, from, to)

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Availability != 1 || r.Interruptions != 0 {
			t.Errorf("%s: availability %.4f, %d interruptions", r.GatewayName, r.Availability, r.Interruptions)
		}
	}
}

func TestAvailabilityIsTheShareOfThePeriodTheGatewayAnswered(t *testing.T) {
	// 31 days; one gateway down for 31 hours is 1/24 of the month.
	merged := servicereport.MergeOutages([]servicereport.Outage{
		{GatewayID: 1, Start: at(1, 0), End: at(2, 7)},
	}, from, to)

	rows := servicereport.Uptime(merged, map[int]string{1: "PLC A"}, from, to)
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	want := 1 - 31.0/(31*24)
	if diff := rows[0].Availability - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("availability = %.6f, want %.6f", rows[0].Availability, want)
	}
}

// A gateway deleted after it failed still failed. Dropping its outage would
// quietly improve the figures.
func TestAnOutageOnADeletedGatewayStillCounts(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{
		{GatewayID: 99, GatewayName: "PLC rimosso", Start: at(10, 0), End: at(11, 0)},
	}, from, to)

	rows := servicereport.Uptime(merged, map[int]string{1: "PLC A"}, from, to)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (the live gateway and the deleted one)", len(rows))
	}
}

// The worst gateway goes first: the reader is looking for the problem.
func TestTheWorstGatewayIsListedFirst(t *testing.T) {
	merged := servicereport.MergeOutages([]servicereport.Outage{
		{GatewayID: 1, Start: at(10, 0), End: at(10, 1)},
		{GatewayID: 2, Start: at(10, 0), End: at(11, 0)},
	}, from, to)

	rows := servicereport.Uptime(merged, map[int]string{1: "PLC A", 2: "PLC B"}, from, to)
	if rows[0].GatewayID != 2 {
		t.Errorf("the gateway with 24h of downtime is not first: %v", rows)
	}
}

// ---------------------------------------------------------------------------
// FleetAvailability
// ---------------------------------------------------------------------------

// One dead gateway out of fifty is 2% of the plant's capacity, and the figure
// in the report has to say so.
//
// An earlier version of this test claimed it also distinguished the
// gateway-time ratio from the average of the per-gateway percentages. It does
// not, and could not: while every gateway is measured over the same period the
// two expand to the same number. Replacing the formula with the average left
// this test green, which is how the claim was found to be false.
func TestOneDeadGatewayDoesNotSinkTheWholeSite(t *testing.T) {
	gateways := map[int]string{}
	for i := 1; i <= 50; i++ {
		gateways[i] = "PLC"
	}
	// Gateway 1 down for the entire month.
	merged := servicereport.MergeOutages([]servicereport.Outage{
		{GatewayID: 1, Start: from, End: to},
	}, from, to)

	rows := servicereport.Uptime(merged, gateways, from, to)
	fleet := servicereport.FleetAvailability(rows, from, to)

	if fleet < 0.97 || fleet > 0.99 {
		t.Errorf("fleet availability = %.4f, want about 0.98 (49 of 50 gateways up)", fleet)
	}
}

// A period with nothing in it must not divide by zero, and must not report a
// plant as 0% available because nothing has happened yet.
func TestAnEmptyReportIsFullyAvailable(t *testing.T) {
	if got := servicereport.FleetAvailability(nil, from, to); got != 1 {
		t.Errorf("fleet availability with no gateways = %v, want 1", got)
	}
	rows := servicereport.Uptime(nil, map[int]string{1: "PLC"}, from, from)
	if got := servicereport.FleetAvailability(rows, from, from); got != 1 {
		t.Errorf("fleet availability over a zero-length period = %v, want 1", got)
	}
}
