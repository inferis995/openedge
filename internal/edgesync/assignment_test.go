package edgesync_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

func agent(id int) *int { return &id }

// three gateways: one for box 1, one for box 2, one nobody has claimed.
func threeGateways() []models.Gateway {
	return []models.Gateway{
		{ID: 10, Name: "Napoli", EdgeAgentID: agent(1)},
		{ID: 20, Name: "Milano", EdgeAgentID: agent(2)},
		{ID: 30, Name: "non assegnato"},
	}
}

func names(gws []models.Gateway) []string {
	out := make([]string, 0, len(gws))
	for i := range gws {
		out = append(out, gws[i].Name)
	}
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Nearly every installation has one box, and nobody should have to assign
// anything for it to work.
func TestTheOnlyBoxTakesEverything(t *testing.T) {
	got := edgesync.GatewaysFor(1, 1, threeGateways())
	if len(got) != 3 {
		t.Fatalf("the only box was given %v, want all three", names(got))
	}
}

// The defect this whole thing exists for. Two boxes in one organization each
// pulled every gateway and each tried to poll the other site's PLCs — which
// they cannot reach, and which now produce comm_loss alarms about equipment
// that is perfectly fine.
func TestWithTwoBoxesEachTakesOnlyItsOwn(t *testing.T) {
	first := edgesync.GatewaysFor(1, 2, threeGateways())
	second := edgesync.GatewaysFor(2, 2, threeGateways())

	if !same(names(first), []string{"Napoli"}) {
		t.Errorf("box 1 was given %v, want only Napoli", names(first))
	}
	if !same(names(second), []string{"Milano"}) {
		t.Errorf("box 2 was given %v, want only Milano", names(second))
	}

	// And neither of them takes the unclaimed one, because both taking it is
	// the failure being fixed.
	for _, n := range append(names(first), names(second)...) {
		if n == "non assegnato" {
			t.Error("an unassigned gateway was handed to a box in an organization with two")
		}
	}
}

// Every key minted before boxes had identities carries no box. The plants
// running on those keys must not stop polling the day this ships.
func TestALegacyKeyKeepsGettingEverything(t *testing.T) {
	got := edgesync.GatewaysFor(0, 2, threeGateways())
	if len(got) != 3 {
		t.Fatalf("a key with no box behind it was given %v, want all three — those "+
			"installations were working yesterday", names(got))
	}
}

// An organization that has boxes registered but none assigned anything yet is
// the normal state right after the second installer is downloaded.
func TestNothingIsHandedOutTwiceWhenNothingIsAssigned(t *testing.T) {
	gws := []models.Gateway{{ID: 10, Name: "a"}, {ID: 20, Name: "b"}}

	first := edgesync.GatewaysFor(1, 2, gws)
	second := edgesync.GatewaysFor(2, 2, gws)

	if len(first) != 0 || len(second) != 0 {
		t.Fatalf("with nothing assigned, box 1 got %v and box 2 got %v — they would both "+
			"poll the same PLCs", names(first), names(second))
	}
}

// Silence about it would be worse than the ambiguity: somebody has to be told
// that those gateways are polled by nobody.
func TestUnassignedGatewaysAreCounted(t *testing.T) {
	if got := edgesync.UnassignedIn(2, threeGateways()); got != 1 {
		t.Errorf("unassigned = %d, want 1", got)
	}
	// With a single box the assignment is ignored, so there is nothing to warn
	// about and a warning would only train people to skip warnings.
	if got := edgesync.UnassignedIn(1, threeGateways()); got != 0 {
		t.Errorf("unassigned with one box = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// FilterTree
// ---------------------------------------------------------------------------

func aTree() *edgesync.Config {
	return &edgesync.Config{
		OrgID:    1,
		Gateways: threeGateways(),
		Tags: []models.Tag{
			{ID: 100, GatewayID: 10, Alias: "napoli-temp"},
			{ID: 200, GatewayID: 20, Alias: "milano-temp"},
			{ID: 300, GatewayID: 30, Alias: "orfano"},
		},
		Alarms: []models.AlarmDefinition{
			{ID: 1000, TagID: 100},
			{ID: 2000, TagID: 200},
			{ID: 3000, TagID: 300},
		},
	}
}

// A tag whose gateway was filtered out cannot be written at all — the foreign
// key refuses it — and the box would fail to apply the whole configuration,
// including the part that was correct.
func TestTagsAndAlarmsFollowTheirGateways(t *testing.T) {
	cfg := aTree()
	cfg.FilterTree(1, 2)

	if len(cfg.Gateways) != 1 || cfg.Gateways[0].ID != 10 {
		t.Fatalf("gateways = %v", names(cfg.Gateways))
	}
	if len(cfg.Tags) != 1 || cfg.Tags[0].ID != 100 {
		t.Errorf("the box was given tags that do not belong to its gateways: %+v", cfg.Tags)
	}
	if len(cfg.Alarms) != 1 || cfg.Alarms[0].ID != 1000 {
		t.Errorf("the box was given alarm rules for tags it does not have: %+v", cfg.Alarms)
	}
}

func TestTheOnlyBoxStillGetsTheWholeTree(t *testing.T) {
	cfg := aTree()
	cfg.FilterTree(1, 1)

	if len(cfg.Gateways) != 3 || len(cfg.Tags) != 3 || len(cfg.Alarms) != 3 {
		t.Fatalf("the only box lost part of its configuration: %d gateways, %d tags, %d alarms",
			len(cfg.Gateways), len(cfg.Tags), len(cfg.Alarms))
	}
}

// The count has to survive the filtering: it is computed over the whole
// organization, not over what is left afterwards.
func TestTheUnassignedCountIsAboutTheOrganizationNotTheBox(t *testing.T) {
	cfg := aTree()
	cfg.FilterTree(1, 2)

	if cfg.Unassigned != 1 {
		t.Errorf("unassigned = %d, want 1 — counted before the tree was narrowed", cfg.Unassigned)
	}
}
