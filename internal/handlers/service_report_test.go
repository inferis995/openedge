package handlers

import (
	"testing"
	"time"
)

// The report covers a whole calendar month aligned to midnight, not "the last
// thirty days". The document is filed, compared with the one before it, and
// attached to an invoice: two reports that overlap by a few hours cannot be
// compared, and nobody reading them would know why the numbers moved.
func TestThePreviousMonthIsAWholeCalendarMonth(t *testing.T) {
	cases := []struct {
		name     string
		now      time.Time
		wantFrom time.Time
		wantTo   time.Time
	}{
		{
			"mid-month",
			time.Date(2026, 9, 11, 14, 32, 7, 0, time.UTC),
			time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Run on the first of the month, which is when a monthly report is
			// actually generated.
			"first of the month, one minute past midnight",
			time.Date(2026, 9, 1, 0, 1, 0, 0, time.UTC),
			time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// January reports on December of the year before. Subtracting a
			// month from a date built by hand is where that goes wrong.
			"january reports on last december",
			time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC),
			time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// The 31st of March: "one month ago" from a day that does not exist
			// in February is the classic way to land on the 3rd of March.
			"the 31st of a month",
			time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC),
			time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			"march reports on february in a leap year",
			time.Date(2028, 3, 5, 0, 0, 0, 0, time.UTC),
			time.Date(2028, 2, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2028, 3, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to := previousMonth(tc.now)
			if !from.Equal(tc.wantFrom) || !to.Equal(tc.wantTo) {
				t.Errorf("previousMonth(%s) = [%s, %s), want [%s, %s)",
					tc.now.Format(time.RFC3339),
					from.Format(time.RFC3339), to.Format(time.RFC3339),
					tc.wantFrom.Format(time.RFC3339), tc.wantTo.Format(time.RFC3339))
			}
		})
	}
}

// February is 28 or 29 days, not 30 or 31, and the range has to end exactly
// where the next month begins or a day goes missing between two reports.
func TestConsecutiveMonthsMeetExactly(t *testing.T) {
	_, augustEnd, err := parseMonth("2026-08")
	if err != nil {
		t.Fatal(err)
	}
	septemberStart, _, err := parseMonth("2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if !augustEnd.Equal(septemberStart) {
		t.Errorf("August ends at %s and September starts at %s — a gap or an overlap "+
			"between two consecutive reports", augustEnd, septemberStart)
	}
}

func TestParseMonthCoversTheWholeMonth(t *testing.T) {
	from, to, err := parseMonth("2028-02") // leap year
	if err != nil {
		t.Fatal(err)
	}
	if days := to.Sub(from).Hours() / 24; days != 29 {
		t.Errorf("February 2028 is %v days long, want 29", days)
	}
}

func TestAMalformedMonthIsRejected(t *testing.T) {
	for _, bad := range []string{"agosto", "2026", "2026-13", "2026-08-01", ""} {
		if _, _, err := parseMonth(bad); err == nil {
			t.Errorf("parseMonth(%q) was accepted", bad)
		}
	}
}
