package handlers

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/tagsheet"
)

func TestASheetPreviewSaysWhatEachRowWouldDo(t *testing.T) {
	existing := map[string]existingTag{
		"DB1.DBD0": {id: 1, sheetTag: tagsheet.Tag{Alias: "Temp", Address: "DB1.DBD0", DataType: "REAL", Unit: "°C", Decimals: 1}},
		"DB1.DBD4": {id: 2, sheetTag: tagsheet.Tag{Alias: "Press", Address: "DB1.DBD4", DataType: "REAL", Unit: "bar"}},
		"DB9.DBD0": {id: 3, sheetTag: tagsheet.Tag{Alias: "Motor.Speed", Address: "DB9.DBD0", DataType: "REAL"}},
	}
	sheet := tagsheet.Parse([]byte(
		"Nome;Indirizzo;Tipo;Unità\n" +
			"Temp;DB1.DBD0;REAL;°C\n" + // same as stored (decimals not in the sheet)
			"Pressione;DB1.DBD4;REAL;bar\n" + // renamed
			"Nuovo;DB1.DBD8;INT;\n" + // new
			"Motor.Speed;DB9.DBD0;REAL;\n" + // same as stored
			"Rotto;DB1.DBD12;WORD;\n")) // invalid type
	res := planSheet(sheet, existing)

	want := []string{"unchanged", "update", "create", "unchanged", "invalid"}
	for i, r := range res.Rows {
		if r.Action != want[i] {
			t.Errorf("line %d (%s): %s, want %s — problems %v", r.Line, r.Alias, r.Action, want[i], r.Problems)
		}
	}
	if got := res.Rows[1].Changes; len(got) != 1 || got[0] != "alias" {
		t.Errorf("the rename reports changes %v, want [alias]", got)
	}
	if res.Created != 1 || res.Updated != 1 || res.Unchanged != 2 || res.Invalid != 1 {
		t.Errorf("counts: %+v", res)
	}
	// An empty unit cell on a new row is "not given": it must not show as a
	// change to "" on an existing one.
	if len(res.Rows[0].Changes) != 0 {
		t.Errorf("an identical row reports changes %v", res.Rows[0].Changes)
	}
}

func TestAMissingColumnLeavesAnExistingValueAlone(t *testing.T) {
	existing := map[string]existingTag{
		"40001": {id: 1, sheetTag: tagsheet.Tag{Alias: "P", Address: "40001", DataType: "INT", Scaling: true, RawMax: 27648, EuMax: 10}},
	}
	sheet := tagsheet.Parse([]byte("Name,Address,Type\nP,40001,INT\n"))
	res := planSheet(sheet, existing)
	if res.Rows[0].Action != "unchanged" {
		t.Errorf("a sheet without scaling columns would change %v", res.Rows[0].Changes)
	}
}

// A decoded MQTT frame feeds several tags from one topic, told apart by their
// JSON path. The sheet must carry them all and update each one in place.
func TestTagsSharingATopicAreToldApartByTheirJSONPath(t *testing.T) {
	const topic = "sorical/863266053566249/dataup/up"
	existing := map[string]existingTag{
		tagsheet.Key(topic, "in7"):      {id: 1, sheetTag: tagsheet.Tag{Alias: "P1_AN1", Address: topic, DataType: "REAL", JSONPath: "in7"}},
		tagsheet.Key(topic, "batteria"): {id: 2, sheetTag: tagsheet.Tag{Alias: "P1_BATT", Address: topic, DataType: "REAL", JSONPath: "batteria"}},
	}
	sheet := tagsheet.Parse([]byte("Name;Address;Type;JSON path\n" +
		"P1_AN1;" + topic + ";REAL;in7\n" + // unchanged
		"P1_BATTERIA;" + topic + ";REAL;batteria\n" + // renamed
		"P1_AN2;" + topic + ";REAL;in8\n")) // new
	res := planSheet(sheet, existing)
	want := []string{"unchanged", "update", "create"}
	for i, r := range res.Rows {
		if r.Action != want[i] {
			t.Errorf("line %d (%s): %s, want %s — %v", r.Line, r.Alias, r.Action, want[i], r.Problems)
		}
	}
	if res.Rows[1].tagID != 2 {
		t.Errorf("the rename would write to tag %d, want 2", res.Rows[1].tagID)
	}

	// The same topic and path twice is a duplicate; without a path a row
	// could be any of the tags on the topic.
	sheet = tagsheet.Parse([]byte("Name;Address;Type;JSON path\n" +
		"A;" + topic + ";REAL;in7\nB;" + topic + ";REAL;in7\n"))
	if sheet.RowProblems(3) == nil {
		t.Error("two rows with the same topic and path were accepted")
	}
	sheet = tagsheet.Parse([]byte("Name;Address;Type\nX;" + topic + ";REAL\n"))
	if r := planSheet(sheet, existing).Rows[0]; r.Action != "invalid" || r.Problems[0].Code != "ambiguous_address" {
		t.Errorf("a row without a path on a shared topic: %s %v", r.Action, r.Problems)
	}
}

// The one tag on an address whose path changes is updated, not doubled.
func TestChangingTheJSONPathOfTheOnlyTagOnATopicUpdatesIt(t *testing.T) {
	existing := map[string]existingTag{
		tagsheet.Key("plant/t1", "temp"): {id: 9, sheetTag: tagsheet.Tag{Alias: "T1", Address: "plant/t1", DataType: "REAL", JSONPath: "temp"}},
	}
	res := planSheet(tagsheet.Parse([]byte("Name;Address;Type;JSON path\nT1;plant/t1;REAL;data.temp\n")), existing)
	if r := res.Rows[0]; r.Action != "update" || r.tagID != 9 {
		t.Errorf("%s on tag %d, want update of 9", r.Action, r.tagID)
	}
}
