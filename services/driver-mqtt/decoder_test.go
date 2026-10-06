package main

import (
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/decoder"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// A MiDo UP frame received in the field: five quarter-hourly readings taken
// 09:15–10:15 UTC, sent at 13:07.
const midoUPFrame = "DBBivAPxacp1pzaoAAVpyj8UCQAA4AAAAAAAP4AAAOEAAAAAAD+AAADiAAAAAAA/gAAAAwABxAAAAAAAxQAAAAAAxgAAAAAAxwAAAAAAyAAAAAAAacpCmAkAAOAAAAAAAD+AAADhAAAAAAA/gAAA4gAAAAAAP4AAAAMAAcQAAAAAAMUAAAAAAMYAAAAAAMcAAAAAAMgAAAAAAGnKRhwJAADgAAAAAAA/gAAA4QAAAAAAP4AAAOIAAAAAAD+AAAADAAHEAAAAAADFAAAAAADGAAAAAADHAAAAAADIAAAAAABpykmgCQAA4AAAAAAAP4AAAOEAAAAAAD+AAADiAAAAAAA/gAAAAwABxAAAAAAAxQAAAAAAxgAAAAAAxwAAAAAAyAAAAAAAacpNJAkAAOAAAAAAAD+AAADhAAAAAAA/gAAA4gAAAAAAP4AAAAMAAcQAAAAAAMUAAAAAAMYAAAAAAMcAAAAAAMgAAAAAAA=="

type published struct {
	tagID int
	value interface{}
	ts    int64
}

func tagMapping(id int, alias, path, dataType string) TagMapping {
	p := path
	return TagMapping{Tag: models.Tag{ID: id, Alias: alias, DataType: dataType, JsonPath: &p}, SourceTopic: "mido/202400444/up"}
}

// One decoded message feeds every tag on its topic, each reading at the time
// the device took it, oldest first.
func TestDecodedFrameFeedsEveryTagAtItsOwnTime(t *testing.T) {
	src, _ := decoder.PresetScript("mido")
	dec, err := decoder.Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	var out []published
	d := &Driver{
		lastMessageTime: map[int]time.Time{},
		connectionLost:  map[int]bool{},
		publish: func(tagID int, _ string, value interface{}, _ string, _ int, ts int64) {
			out = append(out, published{tagID, value, ts})
		},
	}
	mappings := []TagMapping{
		tagMapping(1, "AN1", "in7", "REAL"),
		tagMapping(2, "Batteria", "batteria", "REAL"),
		tagMapping(3, "Stato", "in3", "INT"),
		tagMapping(4, "BatteriaBassa", "stato_bit.batteria_bassa", "BOOL"),
		tagMapping(5, "Assente", "in30", "REAL"), // not in the frame: nothing published
	}
	d.handleDecodedMessage(dec, mappings, "mido/202400444/up", []byte(midoUPFrame))

	byTag := map[int][]published{}
	for _, p := range out {
		byTag[p.tagID] = append(byTag[p.tagID], p)
	}
	if n := len(byTag[1]); n != 5 {
		t.Fatalf("AN1: %d readings, want 5", n)
	}
	first := time.UnixMilli(byTag[1][0].ts).UTC().Format("15:04")
	last := time.UnixMilli(byTag[1][4].ts).UTC().Format("15:04")
	if first != "09:15" || last != "10:15" {
		t.Errorf("AN1 readings %s … %s, want 09:15 … 10:15", first, last)
	}
	for i := 1; i < len(byTag[1]); i++ {
		if byTag[1][i].ts-byTag[1][i-1].ts != 15*60*1000 {
			t.Errorf("readings %d and %d are not 15 minutes apart", i-1, i)
		}
	}
	if len(byTag[2]) != 1 || byTag[2][0].value != 3.66 || time.UnixMilli(byTag[2][0].ts).UTC().Format("15:04") != "13:07" {
		t.Errorf("battery: %+v, want 3.66 at 13:07", byTag[2])
	}
	if len(byTag[3]) != 5 {
		t.Errorf("Stato: %d readings, want 5", len(byTag[3]))
	}
	if len(byTag[4]) != 1 || byTag[4][0].value != false {
		t.Errorf("battery-low bit: %+v", byTag[4])
	}
	if len(byTag[5]) != 0 {
		t.Errorf("a tag whose key is not in the frame got %d values", len(byTag[5]))
	}
}

// Two tags reading different fields of one JSON message on one topic: the
// second used to be dropped when the mappings were built.
func TestTwoTagsOnOneTopicBothGetTheirField(t *testing.T) {
	var out []published
	d := &Driver{
		lastMessageTime: map[int]time.Time{},
		connectionLost:  map[int]bool{},
		publish: func(tagID int, _ string, value interface{}, _ string, _ int, ts int64) {
			out = append(out, published{tagID, value, ts})
		},
	}
	for _, m := range []TagMapping{tagMapping(1, "Temp", "temp", "REAL"), tagMapping(2, "Hum", "hum", "REAL")} {
		d.handleSourceMessage(m, "sensor/1", []byte(`{"temp":22.5,"hum":55}`))
	}
	if len(out) != 2 || out[0].value != 22.5 || out[1].value != 55.0 {
		t.Fatalf("published %+v", out)
	}
}
