package cmd

import (
	"encoding/json"
	"testing"
)

const tagsJSON = `[
 {"id":1,"gateway_id":3,"code":"P1","alias":"Pressione Ingresso","data_type":"REAL","eu_unit":"bar","scaling_enabled":true,"description":"x"},
 {"id":2,"gateway_id":3,"code":"Q1","alias":"Portata","data_type":"REAL","eu_unit":"l/s"},
 {"id":3,"gateway_id":4,"code":"PRESS_OUT","alias":"","data_type":"INT","eu_unit":""}
]`

func TestListTagsPath(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"/api/tags":              {},
		"/api/tags?gateway_id=3": {"gateway_id": float64(3), "area_id": float64(9)},
		"/api/tags?area_id=9":    {"area_id": float64(9)},
	}
	for want, args := range cases {
		if got := listTagsPath(args); got != want {
			t.Errorf("%v: got %s, want %s", args, got, want)
		}
	}
}

func TestShapeTagsCompactByDefault(t *testing.T) {
	out, err := shapeTags([]byte(tagsJSON), map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	var tags []map[string]interface{}
	_ = json.Unmarshal([]byte(out), &tags)
	if len(tags) != 3 {
		t.Fatalf("got %d tags", len(tags))
	}
	for _, tg := range tags {
		if _, ok := tg["scaling_enabled"]; ok {
			t.Errorf("compact tag carries extra fields: %v", tg)
		}
		if len(tg) != len(compactTagFields) {
			t.Errorf("compact tag fields %v", tg)
		}
	}
	full, _ := shapeTags([]byte(tagsJSON), map[string]interface{}{"fields": "full"})
	_ = json.Unmarshal([]byte(full), &tags)
	if _, ok := tags[0]["scaling_enabled"]; !ok {
		t.Error("fields=full dropped fields")
	}
}

func TestShapeTagsSearch(t *testing.T) {
	out, _ := shapeTags([]byte(tagsJSON), map[string]interface{}{"search": "press"})
	var tags []map[string]interface{}
	_ = json.Unmarshal([]byte(out), &tags)
	// "Pressione Ingresso" by alias, PRESS_OUT by code (it has no alias).
	if len(tags) != 2 || tags[0]["id"] != float64(1) || tags[1]["id"] != float64(3) {
		t.Errorf("search: %v", tags)
	}
}
