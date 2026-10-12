package cmd

import "testing"

func TestParsePositionRows(t *testing.T) {
	rows := [][]string{
		{"Nome", "Posizione"},
		{"MiDo CARIATI_CA_P_1", "39.4931, 16.9584"},
		{"CARIATI_CA_P_2", "39,4940; 16,9601"},
		{"12", "not a place"},
	}
	got, problems := parsePositionRows(rows)
	if len(got) != 2 || len(problems) != 1 {
		t.Fatalf("got %+v, problems %v", got, problems)
	}
	if got[1].lat != 39.494 || got[1].lon != 16.9601 {
		t.Fatalf("decimal comma: %+v", got[1])
	}

	plain, _ := parsePositionRows([][]string{{"7", "39.1", "16.2"}})
	if len(plain) != 1 || plain[0].ref != "7" || plain[0].lon != 16.2 {
		t.Fatalf("no header: %+v", plain)
	}
}

func TestMatchGateway(t *testing.T) {
	gws := []gatewayPositionRow{{ID: 1, Name: "MiDo CARIATI_CA_P_1"}, {ID: 2, Name: "MiDo CARIATI_CA_P_10"}}
	if g, _ := matchGateway(gws, "2"); g == nil || g.ID != 2 {
		t.Fatal("by id")
	}
	if g, _ := matchGateway(gws, "mido cariati_ca_p_1"); g == nil || g.ID != 1 {
		t.Fatal("by exact name, any case")
	}
	if g, why := matchGateway(gws, "CARIATI_CA_P_1"); g != nil || why == "" {
		t.Fatal("two names contain it: must ask for the full name")
	}
	if g, _ := matchGateway(gws, "P_10"); g == nil || g.ID != 2 {
		t.Fatal("one name contains it")
	}
}
