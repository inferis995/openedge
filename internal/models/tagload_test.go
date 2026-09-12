package models

import (
	"reflect"
	"strings"
	"testing"
)

// topLevelColumns counts the entries of a SELECT list, ignoring commas that
// sit inside a function call such as COALESCE(x, 0).
func topLevelColumns(list string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range list {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(list[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(list[start:]))
	return out
}

// The column list and the scan destinations are two halves of one statement
// written in two places, and Postgres will not tell you they disagree until a
// driver starts up in front of a plant. A missing destination is a scan error
// that takes the whole gateway's tag list with it; a surplus one is the same.
func TestTheColumnListAndTheScanAgreeOnCount(t *testing.T) {
	cols := topLevelColumns(DriverTagColumns)

	// Count the destinations by their field names, read out of the source of
	// truth: every field ScanDriverTag writes into.
	want := []string{
		"id", "gateway_id", "code", "alias", "data_type", "historize",
		"historize_deadband",
		"scaling_enabled",
		"scaling_raw_min", "scaling_raw_max",
		"scaling_eu_min", "scaling_eu_max",
		"scaling_clamp", "invert",
	}
	if len(cols) != len(want) {
		t.Fatalf("DriverTagColumns selects %d columns, the scan expects %d:\n%v",
			len(cols), len(want), cols)
	}
	for i, col := range cols {
		if !strings.Contains(col, want[i]) {
			t.Errorf("column %d is %q, the scan writes it into %q — the two lists "+
				"have drifted out of order", i, col, want[i])
		}
	}
}

// Every scaling field on Tag must appear in the list. Adding one to the struct
// and forgetting it here leaves it at its zero value in every driver: for
// ScalingEnabled that is silently no conversion at all.
func TestEveryScalingFieldOfTagIsSelected(t *testing.T) {
	rt := reflect.TypeOf(Tag{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !strings.HasPrefix(f.Name, "Scaling") && f.Name != "Invert" {
			continue
		}
		col := f.Tag.Get("db")
		if col == "" {
			t.Errorf("%s has no db tag, so nothing can check it is selected", f.Name)
			continue
		}
		if !strings.Contains(DriverTagColumns, col) {
			t.Errorf("Tag.%s maps to column %q, which DriverTagColumns does not "+
				"select; every driver would read it as its zero value", f.Name, col)
		}
	}
}
