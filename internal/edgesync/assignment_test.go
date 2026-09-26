package edgesync_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

func agent(id int) *int { return &id }

func gw(id int, box *int) models.Gateway {
	return models.Gateway{ID: id, EdgeAgentID: box}
}

func ids(gws []models.Gateway) map[int]bool {
	out := map[int]bool{}
	for i := range gws {
		out[gws[i].ID] = true
	}
	return out
}

func assigned(id int) edgesync.Box { return edgesync.Box{ID: id, Scope: edgesync.ScopeAssigned} }
func all(id int) edgesync.Box      { return edgesync.Box{ID: id, Scope: edgesync.ScopeAll} }

// No boxes: the server polls everything. The installation that never adds a
// box must work with nothing configured.
func TestWithNoBoxesTheServerPollsEverything(t *testing.T) {
	gws := []models.Gateway{gw(1, nil), gw(2, nil)}
	if got := edgesync.ServerGateways(nil, gws); len(got) != 2 {
		t.Fatalf("with no boxes the server polls %d of 2 gateways", len(got))
	}
}

// The scenario this rule exists for: a server that reaches most PLCs, and one
// box installed next to a PLC it cannot reach.
func TestTheServerKeepsItsPLCsAndTheBoxTakesOnlyItsOwn(t *testing.T) {
	boxes := []edgesync.Box{assigned(7)}
	gws := []models.Gateway{gw(1, nil), gw(2, nil), gw(3, agent(7))}

	server := ids(edgesync.ServerGateways(boxes, gws))
	box := ids(edgesync.GatewaysFor(7, boxes, gws))

	if !server[1] || !server[2] || server[3] {
		t.Errorf("the server polls %v, want 1 and 2 only", server)
	}
	if !box[3] || box[1] || box[2] {
		t.Errorf("the box polls %v, want 3 only — the old rule gave a lone box every "+
			"gateway, including the ones on the server's LAN", box)
	}
}

// A box installed and not yet given anything takes nothing.
func TestANewBoxTakesNothing(t *testing.T) {
	boxes := []edgesync.Box{assigned(7)}
	gws := []models.Gateway{gw(1, nil), gw(2, nil)}
	if got := edgesync.GatewaysFor(7, boxes, gws); len(got) != 0 {
		t.Fatalf("a box given nothing polls %d gateways", len(got))
	}
}

// Cloud server, which polls nothing: the box with scope "all" takes what no box
// was given, plus its own.
func TestABoxWithScopeAllTakesTheUnassigned(t *testing.T) {
	boxes := []edgesync.Box{all(7), assigned(8)}
	gws := []models.Gateway{gw(1, nil), gw(2, agent(8)), gw(3, agent(7))}

	got := ids(edgesync.GatewaysFor(7, boxes, gws))
	if !got[1] || !got[3] || got[2] {
		t.Errorf("the scope-all box polls %v, want 1 (unassigned) and 3 (its own), not 2", got)
	}
	if n := len(edgesync.ServerGateways(boxes, gws)); n != 0 {
		t.Errorf("with a scope-all box the server still polls %d gateways", n)
	}
}

// Deleting a box sets its gateways' edge_agent_id to NULL, but an id can also
// be left pointing at a box that is not in the list. Either way the gateway
// must go back to someone, never to nobody.
func TestAGatewayOfAMissingBoxGoesBackToTheServer(t *testing.T) {
	gws := []models.Gateway{gw(1, agent(99))}
	if got := edgesync.ServerGateways([]edgesync.Box{assigned(7)}, gws); len(got) != 1 {
		t.Fatal("a gateway assigned to a box that does not exist is polled by nobody")
	}
}

// A key from before boxes had identities keeps getting everything.
func TestALegacyKeyKeepsGettingEverything(t *testing.T) {
	gws := []models.Gateway{gw(1, nil), gw(2, agent(7))}
	if got := edgesync.GatewaysFor(edgesync.Server, []edgesync.Box{assigned(7)}, gws); len(got) != 2 {
		t.Fatalf("a legacy key got %d of 2 gateways", len(got))
	}
}

// The whole property, over every combination of a small world: each gateway
// has exactly one poller. Two would put every value in history twice; none
// looks like a quiet plant.
func TestEveryGatewayHasExactlyOnePoller(t *testing.T) {
	scopes := []string{edgesync.ScopeAssigned, edgesync.ScopeAll}
	owners := []*int{nil, agent(1), agent(2), agent(3)} // 3 does not exist
	for _, s1 := range scopes {
		for _, s2 := range scopes {
			if s1 == edgesync.ScopeAll && s2 == edgesync.ScopeAll {
				continue // refused by the API and by a unique index
			}
			boxes := []edgesync.Box{{ID: 1, Scope: s1}, {ID: 2, Scope: s2}}
			for i, o := range owners {
				g := []models.Gateway{gw(100+i, o)}
				pollers := len(edgesync.ServerGateways(boxes, g)) +
					len(edgesync.GatewaysFor(1, boxes, g)) +
					len(edgesync.GatewaysFor(2, boxes, g))
				if pollers != 1 {
					t.Errorf("scopes %s/%s, assigned to %v: %d pollers, want exactly 1",
						s1, s2, o, pollers)
				}
			}
		}
	}
}

func TestTagsAndAlarmsFollowTheirGateways(t *testing.T) {
	cfg := &edgesync.Config{
		Gateways: []models.Gateway{gw(1, agent(7)), gw(2, nil)},
		Tags:     []models.Tag{{ID: 10, GatewayID: 1}, {ID: 20, GatewayID: 2}},
		Alarms:   []models.AlarmDefinition{{ID: 100, TagID: 10}, {ID: 200, TagID: 20}},
	}
	cfg.FilterTree(7, []edgesync.Box{assigned(7)})

	if len(cfg.Tags) != 1 || cfg.Tags[0].ID != 10 {
		t.Errorf("tags after filtering: %+v, want only tag 10", cfg.Tags)
	}
	if len(cfg.Alarms) != 1 || cfg.Alarms[0].ID != 100 {
		t.Errorf("alarms after filtering: %+v, want only alarm 100", cfg.Alarms)
	}
	if cfg.Unassigned != 1 {
		t.Errorf("Unassigned = %d, want 1: gateway 2 is left to the server", cfg.Unassigned)
	}
}

func TestOnlyTheTwoScopesAreValid(t *testing.T) {
	for _, s := range []string{"assigned", "all"} {
		if !edgesync.ValidScope(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range []string{"", "ALL", "everything", "none"} {
		if edgesync.ValidScope(s) {
			t.Errorf("%q accepted", s)
		}
	}
}
