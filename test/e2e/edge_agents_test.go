//go:build e2e

package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
)

// The failure this prevents: two boxes in one organization each pulled every
// gateway of that organization and each tried to poll the other site's PLCs.
// They cannot reach them, so both fail — and since comm_loss exists, both raise
// alarms about equipment that is perfectly fine.
func TestTwoBoxesInOneOrgEachGetOnlyItsOwnGateways(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "agents-"+suffix)

	napoli := seedInventoryGateway(t, db, org.ID, "napoli-"+suffix)
	milano := seedInventoryGateway(t, db, org.ID, "milano-"+suffix)
	orphan := seedInventoryGateway(t, db, org.ID, "orfano-"+suffix)

	// Two installers downloaded: two boxes, two keys.
	firstKey := downloadInstallerKey(t, admin, db, org.ID)
	secondKey := downloadInstallerKey(t, admin, db, org.ID)

	agents := listAgents(t, admin, org.ID)
	if len(agents) != 2 {
		t.Fatalf("two downloads registered %d boxes, want 2", len(agents))
	}
	t.Logf("boxes: %d and %d", agents[0].ID, agents[1].ID)

	// Assigned by an admin OF THAT ORGANIZATION, not by the global admin: the
	// gateway update endpoint takes its scope from the caller's organization,
	// and a global admin who names none has never been able to use it. That is
	// existing behaviour of that endpoint, and it is also the realistic path —
	// a customer's own admin decides which box polls what.
	orgAdmin := createOrgAdmin(t, admin, org.ID, "agents-"+suffix, "e2e-Password-"+suffix)
	assignGateway(t, orgAdmin, napoli, agents[0].ID)
	assignGateway(t, orgAdmin, milano, agents[1].ID)

	first, err := edgesync.Fetch(context.Background(), apiBase(), firstKey)
	if err != nil {
		t.Fatalf("the first box could not fetch: %v", err)
	}
	second, err := edgesync.Fetch(context.Background(), apiBase(), secondKey)
	if err != nil {
		t.Fatalf("the second box could not fetch: %v", err)
	}

	assertHasOnly(t, "the first box", first, napoli, []int{milano, orphan})
	assertHasOnly(t, "the second box", second, milano, []int{napoli, orphan})

	// Neither takes the one nobody claimed — both taking it is the failure
	// being fixed — and the platform says so rather than leaving it silent.
	if first.Unassigned < 1 {
		t.Errorf("the configuration does not report the unassigned gateway; nobody is "+
			"polling it and nothing says so (unassigned=%d)", first.Unassigned)
	}
}

// Installing a box takes nothing away from the server. This is the case the
// old rule got wrong: "the organization's only box takes everything" meant a box
// installed for one PLC the server cannot reach would take every PLC the server
// already polls, fail to reach them, and raise comm_loss about machines that
// were fine. A new box polls what it is given, and nothing else.
func TestANewBoxTakesNothingUntilItIsGivenSomething(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "agent-new-"+suffix)
	onServer := seedInventoryGateway(t, db, org.ID, "server-lan-"+suffix)
	remote := seedInventoryGateway(t, db, org.ID, "reparto-b-"+suffix)

	key := downloadInstallerKey(t, admin, db, org.ID)
	agents := listAgents(t, admin, org.ID)
	if len(agents) != 1 {
		t.Fatalf("one download registered %d boxes, want 1", len(agents))
	}

	cfg, err := edgesync.Fetch(context.Background(), apiBase(), key)
	if err != nil {
		t.Fatalf("fetching: %v", err)
	}
	if len(cfg.Gateways) != 0 {
		t.Fatalf("a box nobody has given anything to was handed %d gateways; it would "+
			"poll PLCs the server already polls", len(cfg.Gateways))
	}

	// Now give it the one PLC the server cannot reach.
	orgAdmin := createOrgAdmin(t, admin, org.ID, "agent-new-"+suffix, "e2e-Password-"+suffix)
	assignGateway(t, orgAdmin, remote, agents[0].ID)

	// The web UI reads the assignment back from the gateway. It was never
	// returned, so every gateway showed as unassigned and an edit could not
	// show what it was.
	status, body := orgAdmin.do("GET", fmt.Sprintf("/api/gateways/%d", remote), nil)
	if status != 200 {
		t.Fatalf("reading the gateway returned %d: %s", status, truncate(body))
	}
	var read struct {
		EdgeAgentID *int `json:"edge_agent_id"`
	}
	if err := json.Unmarshal(body, &read); err != nil {
		t.Fatalf("the gateway is not JSON: %v", err)
	}
	if read.EdgeAgentID == nil || *read.EdgeAgentID != agents[0].ID {
		t.Errorf("the gateway reads back edge_agent_id=%v, want %d", read.EdgeAgentID, agents[0].ID)
	}

	cfg, err = edgesync.Fetch(context.Background(), apiBase(), key)
	if err != nil {
		t.Fatalf("fetching after the assignment: %v", err)
	}
	assertHasOnly(t, "the box", cfg, remote, []int{onServer})
	if cfg.Unassigned != 1 {
		t.Errorf("the configuration reports %d gateways left to the server, want 1 "+
			"(the one on the server's LAN)", cfg.Unassigned)
	}
}

// With the server in the cloud nothing polls a gateway no box was given. A box
// set to scope "all" takes those, which is what a single box used to do on its
// own — now as an explicit choice.
func TestABoxWithScopeAllTakesEveryUnassignedGateway(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "agent-all-"+suffix)
	a := seedInventoryGateway(t, db, org.ID, "a-"+suffix)
	b := seedInventoryGateway(t, db, org.ID, "b-"+suffix)

	key := downloadInstallerKey(t, admin, db, org.ID)
	box := listAgents(t, admin, org.ID)[0]
	setScope(t, admin, org.ID, box.ID, "all", 200)

	cfg, err := edgesync.Fetch(context.Background(), apiBase(), key)
	if err != nil {
		t.Fatalf("fetching: %v", err)
	}
	got := map[int]bool{}
	for i := range cfg.Gateways {
		got[cfg.Gateways[i].ID] = true
	}
	if !got[a] || !got[b] {
		t.Fatalf("a box with scope all was given %d gateways and is missing some", len(cfg.Gateways))
	}
	if cfg.Unassigned != 0 {
		t.Errorf("with a scope-all box nothing is left to the server, but %d was reported", cfg.Unassigned)
	}

	// A second box may not take them too: the same PLC would be polled twice.
	_ = downloadInstallerKey(t, admin, db, org.ID)
	boxes := listAgents(t, admin, org.ID)
	var second int
	for _, x := range boxes {
		if x.ID != box.ID {
			second = x.ID
		}
	}
	setScope(t, admin, org.ID, second, "all", 409)
}

// Removing a box revokes its key in the same step. Deleting the row alone would
// turn the key into one with no box behind it — which is treated as a key from
// before boxes had identities, and polls every gateway of the organization.
func TestRemovingABoxRevokesItsKeyAndReturnsItsGatewaysToTheServer(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "agent-del-"+suffix)
	gw := seedInventoryGateway(t, db, org.ID, "gw-"+suffix)

	key := downloadInstallerKey(t, admin, db, org.ID)
	box := listAgents(t, admin, org.ID)[0]
	orgAdmin := createOrgAdmin(t, admin, org.ID, "agent-del-"+suffix, "e2e-Password-"+suffix)
	assignGateway(t, orgAdmin, gw, box.ID)

	status, body := admin.do("DELETE", fmt.Sprintf("/api/organizations/%d/edge-agents/%d", org.ID, box.ID), nil)
	if status != 200 {
		t.Fatalf("removing the box returned %d: %s", status, truncate(body))
	}

	if cfg, err := edgesync.Fetch(context.Background(), apiBase(), key); err == nil {
		t.Fatalf("the removed box's key still works and was handed %d gateways; a box "+
			"still plugged in would go on polling", len(cfg.Gateways))
	}

	var agent sql.NullInt64
	if err := db.QueryRow(`SELECT edge_agent_id FROM gateways WHERE id = $1`, gw).Scan(&agent); err != nil {
		t.Fatalf("reading the gateway: %v", err)
	}
	if agent.Valid {
		t.Errorf("the gateway is still assigned to box %d, which no longer exists", agent.Int64)
	}
}

// A gateway may only be handed to a box of its own organization. Otherwise an
// admin could hand their gateway to another tenant's box, which would then be
// given its address, its tags and its credentials on the next pull.
func TestAGatewayCannotBeGivenToAnotherTenantsBox(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	mine := createOrg(t, admin, "agent-mine-"+suffix)
	theirs := createOrg(t, admin, "agent-theirs-"+suffix)

	myGateway := seedInventoryGateway(t, db, mine.ID, "mine-"+suffix)
	_ = downloadInstallerKey(t, admin, db, theirs.ID)

	theirAgents := listAgents(t, admin, theirs.ID)
	if len(theirAgents) == 0 {
		t.Fatal("no box was registered for the other organization")
	}

	mineAdmin := createOrgAdmin(t, admin, mine.ID, "agent-mine-"+suffix, "e2e-Password-"+suffix)
	status, body := mineAdmin.do("PUT", fmt.Sprintf("/api/gateways/%d", myGateway),
		map[string]interface{}{"edge_agent_id": theirAgents[0].ID})
	if status < 400 {
		t.Fatalf("a gateway of org %d was assigned to a box of org %d (status %d): %s",
			mine.ID, theirs.ID, status, truncate(body))
	}
}

// The command validity is configurable, within limits that keep it useful: below
// five seconds fresh commands are refused, above an hour it protects nothing.
func TestTheCommandValidityIsConfigurableWithinLimits(t *testing.T) {
	admin, _ := adminSession(t)
	t.Cleanup(func() {
		_, _ = admin.do("PUT", "/api/system/settings", map[string]int{"write_command_max_age_seconds": 30})
	})

	for _, bad := range []int{0, 2, 3601} {
		if status, _ := admin.do("PUT", "/api/system/settings",
			map[string]int{"write_command_max_age_seconds": bad}); status != 400 {
			t.Errorf("a validity of %d seconds was accepted (status %d)", bad, status)
		}
	}
	if status, body := admin.do("PUT", "/api/system/settings",
		map[string]int{"write_command_max_age_seconds": 60}); status != 200 {
		t.Fatalf("a validity of 60 seconds was refused (%d): %s", status, truncate(body))
	}
	status, body := admin.do("GET", "/api/system/settings", nil)
	if status != 200 {
		t.Fatalf("reading the settings returned %d", status)
	}
	var got map[string]interface{}
	_ = json.Unmarshal(body, &got)
	if fmt.Sprint(got["write_command_max_age_seconds"]) != "60" {
		t.Errorf("the validity reads back as %v, want 60", got["write_command_max_age_seconds"])
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

type agentRow struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Gateways int    `json:"gateways"`
}

func listAgents(t *testing.T, admin *apiClient, orgID int) []agentRow {
	t.Helper()
	status, body := admin.do("GET", fmt.Sprintf("/api/organizations/%d/edge-agents", orgID), nil)
	if status != 200 {
		t.Fatalf("listing the boxes returned %d: %s", status, truncate(body))
	}
	var out struct {
		Agents []agentRow `json:"agents"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the box list is not JSON: %v — %s", err, truncate(body))
	}
	return out.Agents
}

func setScope(t *testing.T, admin *apiClient, orgID, agentID int, scope string, want int) {
	t.Helper()
	status, body := admin.do("PUT", fmt.Sprintf("/api/organizations/%d/edge-agents/%d", orgID, agentID),
		map[string]interface{}{"scope": scope})
	if status != want {
		t.Fatalf("setting box %d to scope %q returned %d, want %d: %s",
			agentID, scope, status, want, truncate(body))
	}
}

func assignGateway(t *testing.T, admin *apiClient, gatewayID, agentID int) {
	t.Helper()
	status, body := admin.do("PUT", fmt.Sprintf("/api/gateways/%d", gatewayID),
		map[string]interface{}{"edge_agent_id": agentID})
	if status != 200 {
		t.Fatalf("assigning gateway %d to box %d returned %d: %s",
			gatewayID, agentID, status, truncate(body))
	}
}

// downloadInstallerKey downloads an installer package and returns the API key
// it embedded, which is how a real box authenticates.
func downloadInstallerKey(t *testing.T, admin *apiClient, db *sql.DB, orgID int) string {
	t.Helper()

	status, body := admin.do("GET", fmt.Sprintf("/api/organizations/%d/edge-installer", orgID), nil)
	if status != 200 {
		t.Fatalf("downloading the installer returned %d: %s", status, truncate(body))
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the installer is not a ZIP: %v", err)
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "/.env") {
			continue
		}
		rc, openErr := f.Open()
		if openErr != nil {
			t.Fatalf("opening .env: %v", openErr)
		}
		content, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			t.Fatalf("reading .env: %v", readErr)
		}
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, "CORE_API_TOKEN=") {
				return strings.TrimSpace(strings.TrimPrefix(line, "CORE_API_TOKEN="))
			}
		}
	}
	t.Fatal("no CORE_API_TOKEN found in the installer package")
	return ""
}

func assertHasOnly(t *testing.T, who string, cfg *edgesync.Config, want int, mustNotHave []int) {
	t.Helper()

	got := map[int]bool{}
	for i := range cfg.Gateways {
		got[cfg.Gateways[i].ID] = true
	}
	if !got[want] {
		t.Errorf("%s was not given its own gateway %d", who, want)
	}
	for _, id := range mustNotHave {
		if got[id] {
			t.Errorf("%s was given gateway %d, which is not its own — it will try to poll "+
				"a PLC it cannot reach and raise a comm_loss alarm about it", who, id)
		}
	}
}
