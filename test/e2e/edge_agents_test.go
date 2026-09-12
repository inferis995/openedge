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

	assignGateway(t, admin, napoli, agents[0].ID)
	assignGateway(t, admin, milano, agents[1].ID)

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

// Nearly every installation has one box, and nobody should have to assign
// anything for it to keep working.
func TestASingleBoxStillGetsEverything(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "agent-solo-"+suffix)
	a := seedInventoryGateway(t, db, org.ID, "a-"+suffix)
	b := seedInventoryGateway(t, db, org.ID, "b-"+suffix)

	key := downloadInstallerKey(t, admin, db, org.ID)

	cfg, err := edgesync.Fetch(context.Background(), apiBase(), key)
	if err != nil {
		t.Fatalf("fetching: %v", err)
	}

	got := map[int]bool{}
	for i := range cfg.Gateways {
		got[cfg.Gateways[i].ID] = true
	}
	if !got[a] || !got[b] {
		t.Fatalf("the only box was given %d gateways and is missing some of its own; "+
			"a single-box plant must work with nothing assigned", len(cfg.Gateways))
	}
	if cfg.Unassigned != 0 {
		t.Errorf("a single-box organization reported %d unassigned gateways; the "+
			"assignment does not apply there and a warning would only be noise", cfg.Unassigned)
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

	status, body := admin.do("PUT", fmt.Sprintf("/api/gateways/%d", myGateway),
		map[string]interface{}{"edge_agent_id": theirAgents[0].ID})
	if status < 400 {
		t.Fatalf("a gateway of org %d was assigned to a box of org %d (status %d): %s",
			mine.ID, theirs.ID, status, truncate(body))
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
