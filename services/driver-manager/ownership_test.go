package main

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

func ptr(i int) *int { return &i }

// The server's driver-manager polls gateways of every organization it hosts.
// Each must be judged against its own organization's boxes only.
func TestAScopeAllBoxOfOneTenantDoesNotTakeAnothersPLCs(t *testing.T) {
	gws := []models.Gateway{{ID: 1}, {ID: 2}}
	orgs := map[int]int{1: 10, 2: 20}
	boxes := map[int][]edgesync.Box{10: {{ID: 5, Scope: edgesync.ScopeAll}}}

	got := ownedByServer(gws, orgs, boxes)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("the server polls %+v, want only gateway 2: org 10's scope-all box takes "+
			"gateway 1, and must not take org 20's gateway 2", got)
	}
}

func TestTheServerSkipsAGatewayGivenToABox(t *testing.T) {
	gws := []models.Gateway{{ID: 1}, {ID: 2, EdgeAgentID: ptr(5)}}
	orgs := map[int]int{1: 10, 2: 10}
	boxes := map[int][]edgesync.Box{10: {{ID: 5, Scope: edgesync.ScopeAssigned}}}

	got := ownedByServer(gws, orgs, boxes)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("the server polls %+v, want only gateway 1: gateway 2 is the box's, and "+
			"polling it too puts every value in history twice", got)
	}
}

func TestWithNoBoxesTheServerPollsEverything(t *testing.T) {
	gws := []models.Gateway{{ID: 1}, {ID: 2}}
	if got := ownedByServer(gws, map[int]int{1: 10, 2: 20}, nil); len(got) != 2 {
		t.Fatalf("with no boxes the server polls %d of 2", len(got))
	}
}

// On the server no registry is set and drivers are the images `make start`
// built locally. On a box the registry is set and they are the published ones.
func TestDriverImagesAreLocalOnTheServerAndPublishedOnABox(t *testing.T) {
	if got := driverImage("driver-s7", "", ""); got != "industrial-driver-s7:latest" {
		t.Errorf("on the server: %q, want the locally built industrial-driver-s7:latest", got)
	}
	got := driverImage("driver-s7", "ghcr.io/inferis995/openedge-", "3.2.0")
	if got != "ghcr.io/inferis995/openedge-driver-s7:3.2.0" {
		t.Errorf("on a box: %q, want the published ghcr.io/inferis995/openedge-driver-s7:3.2.0 — "+
			"a name without a registry resolves to Docker Hub, where it does not exist", got)
	}
	if got := driverImage("driver-s7", "ghcr.io/inferis995/openedge-", ""); got != "ghcr.io/inferis995/openedge-driver-s7:latest" {
		t.Errorf("on a box with no tag: %q", got)
	}
}
