package handlers

import (
	"reflect"
	"testing"
)

func TestParseOrgIDList(t *testing.T) {
	cases := map[string][]int{
		"":            nil,
		"3, 1,3":      {1, 3},
		" 2 ":         {2},
		"0,-4,x,7":    {7},
		",,5,,":       {5},
		"1;2":         nil, // not a separator: refused, not guessed
		"12,2,100,12": {2, 12, 100},
	}
	for in, want := range cases {
		if got := ParseOrgIDList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseOrgIDList(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFormatOrgIDList(t *testing.T) {
	if got := FormatOrgIDList([]int{3, 1, 3, 0, -2}); got != "1,3" {
		t.Errorf("FormatOrgIDList = %q, want \"1,3\"", got)
	}
	if got := FormatOrgIDList(nil); got != "" {
		t.Errorf("FormatOrgIDList(nil) = %q, want empty", got)
	}
}

func TestWriteTopicOrg(t *testing.T) {
	cases := map[string]int{
		"sys/write/4/site/area/gw/valve": 4,
		"sys/write/4":                    4,
		"sys/write/0/s/a/g/t":            0,
		"sys/write/abc/s/a/g/t":          0,
		"sys/write/do_valvola_1":         0, // the old cloud form, no org
		"sys/alarms/4/s/a/g/t":           0,
		"cloud/sys/write/4/s/a/g/t":      0, // prefix must be stripped first
	}
	for in, want := range cases {
		if got := WriteTopicOrg(in); got != want {
			t.Errorf("WriteTopicOrg(%q) = %d, want %d", in, got, want)
		}
	}
}
