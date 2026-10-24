package cmd

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseIDList(t *testing.T) {
	got, err := parseIDList(" 3, 4,,5 ")
	if err != nil || !reflect.DeepEqual(got, []int{3, 4, 5}) {
		t.Fatalf("got %v %v", got, err)
	}
	for _, bad := range []string{"a", "0", "-2"} {
		if _, err := parseIDList(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSaveMachineKeepsWhatIsNotChanged(t *testing.T) {
	state := 9
	cur, _ := json.Marshal(map[string]interface{}{
		"id": 4, "name": "Pompa", "description": "vecchia", "state_tag_id": state,
		"members": []map[string]interface{}{{"tag_id": 1}, {"tag_id": 2}},
	})
	var sent machineInput
	put := func(path string, body interface{}) ([]byte, error) {
		if path != "/api/forecast/assets/4" {
			t.Errorf("PUT %s", path)
		}
		sent = body.(machineInput)
		return []byte("{}"), nil
	}
	get := func(string) ([]byte, error) { return cur, nil }
	desc := "nuova"
	if _, err := saveMachine(get, nil, put, 4, &machineChanges{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if sent.Name != "Pompa" || sent.Description != "nuova" || sent.StateTagID == nil || *sent.StateTagID != 9 ||
		!reflect.DeepEqual(sent.TagIDs, []int{1, 2}) {
		t.Errorf("sent %+v", sent)
	}

	none := 0
	if _, err := saveMachine(get, nil, put, 4, &machineChanges{StateTag: &none, Tags: []int{7}}); err != nil {
		t.Fatal(err)
	}
	if sent.StateTagID != nil || !reflect.DeepEqual(sent.TagIDs, []int{7}) {
		t.Errorf("state tag 0 removes it, tags replace: %+v", sent)
	}
}

func TestSaveMachineNewNeedsNameAndTags(t *testing.T) {
	post := func(path string, body interface{}) ([]byte, error) { return []byte("{}"), nil }
	name := "X"
	if _, err := saveMachine(nil, post, nil, 0, &machineChanges{Name: &name}); err == nil {
		t.Error("a machine without tags was accepted")
	}
	var sent machineInput
	post = func(path string, body interface{}) ([]byte, error) { sent = body.(machineInput); return []byte("{}"), nil }
	if _, err := saveMachine(nil, post, nil, 0, &machineChanges{Name: &name, Tags: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if sent.Name != "X" || sent.StateTagID != nil {
		t.Errorf("sent %+v", sent)
	}
}

func TestMachineChangesFromArgs(t *testing.T) {
	ch, err := machineChangesFromArgs(map[string]interface{}{"tag_ids": []interface{}{float64(3), float64(4)}, "state_tag_id": float64(0)})
	if err != nil || !reflect.DeepEqual(ch.Tags, []int{3, 4}) || ch.StateTag == nil || *ch.StateTag != 0 || ch.Name != nil {
		t.Fatalf("%+v %v", ch, err)
	}
	ch, err = machineChangesFromArgs(map[string]interface{}{"tag_ids": "5,6", "name": "M"})
	if err != nil || !reflect.DeepEqual(ch.Tags, []int{5, 6}) || *ch.Name != "M" || ch.StateTag != nil {
		t.Fatalf("%+v %v", ch, err)
	}
	if _, err := machineChangesFromArgs(map[string]interface{}{"tag_ids": []interface{}{"x"}}); err == nil {
		t.Error("a non-number tag accepted")
	}
}
