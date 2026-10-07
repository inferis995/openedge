package handlers

import (
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func TestTheWorkbookHasOneRowPerInstantAndLocalTimes(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)
	ms := func(d time.Duration) int64 { return t0.Add(d).UnixMilli() }
	tags := []*exportTag{
		{ID: 1, Alias: "AN1", Unit: "bar", points: []HistoryDataPoint{{Timestamp: ms(0), Value: f64(3)}, {Timestamp: ms(15 * time.Minute), Value: f64(3.1)}}},
		{ID: 2, Alias: "BATT", Unit: "V", points: []HistoryDataPoint{{Timestamp: ms(15 * time.Minute), Value: f64(3.66)}}},
	}
	rome, _ := time.LoadLocation("Europe/Rome")
	f, err := buildExportWorkbook(tags, t0, t0.Add(time.Hour), rome, true, time.Minute, "mean")
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := f.GetRows("Data")
	if len(rows) != 3 {
		t.Fatalf("%d rows, want header + 2 instants: %v", len(rows), rows)
	}
	if rows[0][1] != "AN1 (bar)" || rows[0][2] != "BATT (V)" {
		t.Errorf("header %v", rows[0])
	}
	if rows[1][0] != "2026-10-01 09:00:00" || rows[2][1] != "3.1" || rows[2][2] != "3.66" {
		t.Errorf("rows %v", rows[1:])
	}
	// Bucketed: min and max columns after each tag.
	tags[0].points[0].Min, tags[0].points[0].Max = f64(2.5), f64(3.5)
	f, _ = buildExportWorkbook(tags, t0, t0.Add(time.Hour), time.UTC, false, time.Minute, "mean")
	rows, _ = f.GetRows("Data")
	if len(rows[0]) != 7 || rows[1][2] != "2.5" || rows[1][3] != "3.5" {
		t.Errorf("bucketed header %v, first row %v", rows[0], rows[1])
	}
}
