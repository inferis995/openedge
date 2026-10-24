package cmd

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseResValue(t *testing.T) {
	days := &shiftsRes.fields[3]
	if v, err := parseResValue(days, "1, 2,3"); err != nil || !reflect.DeepEqual(v, []int{1, 2, 3}) {
		t.Errorf("days %v %v", v, err)
	}
	start := &maintenanceRes.fields[1]
	v, err := parseResValue(start, "2026-11-03 08:00")
	want := time.Date(2026, 11, 3, 8, 0, 0, 0, time.Local).Format(time.RFC3339)
	if err != nil || v != want {
		t.Errorf("local time %v %v, want %s", v, err, want)
	}
	if _, err := parseResValue(start, "domani"); err == nil {
		t.Error("not a time, accepted")
	}
	pph := &oeeProfilesRes.fields[5]
	if v, _ := parseResValue(pph, "120,5"); v != 120.5 {
		t.Errorf("decimal comma: %v", v)
	}
	run := &oeeProfilesRes.fields[2]
	if v, _ := parseResValue(run, "0"); v != nil {
		t.Errorf("0 clears a tag: %v", v)
	}
	if v, _ := resValue(&shiftsRes.fields[4], false); v != false {
		t.Errorf("json bool: %v", v)
	}
}

func TestSaveResourceUpdatesOnlyGivenFields(t *testing.T) {
	list := `[{"id":1,"name":"Mattino","start_time":"06:00","end_time":"14:00","weekdays":[1,2,3,4,5],"active":true,"platform":false},
	          {"id":2,"name":"Notte","start_time":"22:00","end_time":"06:00","weekdays":[1],"active":true}]`
	raw := func(string) ([]byte, error) { return []byte(list), nil }
	var path string
	var body map[string]interface{}
	put := func(p string, b interface{}) ([]byte, error) {
		path, body = p, b.(map[string]interface{})
		return []byte(`{}`), nil
	}
	if _, err := saveResource(&shiftsRes, raw, nil, put, 1, map[string]interface{}{"end_time": "13:30"}); err != nil {
		t.Fatal(err)
	}
	if path != "/api/shifts/1" || body["end_time"] != "13:30" || body["name"] != "Mattino" || body["active"] != true {
		t.Errorf("%s %v", path, body)
	}
	if _, ok := body["platform"]; ok {
		t.Error("only the resource's own fields are sent back")
	}
	if _, err := saveResource(&shiftsRes, raw, nil, put, 9, map[string]interface{}{}); err == nil {
		t.Error("an unknown id")
	}
	post := func(p string, b interface{}) ([]byte, error) { return json.Marshal(b) }
	if _, err := saveResource(&shiftsRes, nil, post, nil, 0, map[string]interface{}{"name": "X"}); err == nil ||
		!strings.Contains(err.Error(), "start_time") {
		t.Errorf("a new shift without hours: %v", err)
	}
}

func TestResourceToolsAreListed(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range resourceTools() {
		names[tl.Name] = true
	}
	for _, n := range []string{"list_shifts", "save_shifts", "delete_shifts", "list_maintenance", "save_maintenance",
		"delete_maintenance", "list_oee_profiles", "save_oee_profiles", "delete_oee_profiles"} {
		if !names[n] {
			t.Errorf("missing %s", n)
		}
	}
}

func TestForecastSetup(t *testing.T) {
	raw := func(path string) ([]byte, error) {
		switch path {
		case "/api/forecast/overview":
			return []byte(`{"engines":["statistical"],"tags":[{"tag_id":1,"status":"ok"},{"tag_id":2,"status":"learning"}]}`), nil
		case "/api/forecast/assets":
			return []byte(`{"assets":[]}`), nil
		case "/api/shifts":
			return []byte(`[{"name":"Mattino","start_time":"06:00","end_time":"14:00","active":true}]`), nil
		case "/api/forecast/oee":
			return []byte(`{"profiles":[{"profile_id":1,"name":"L","missing":["produced"]}]}`), nil
		}
		return nil, errors.New("404")
	}
	steps, err := forecastSetup(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range steps {
		got[s.Key] = s.State
	}
	want := map[string]string{"tags": "done", "model": "optional", "machines": "todo", "shifts": "done", "oee": "todo", "levels": "done"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v (maintenance unreadable: left out)", got, want)
	}
}
