//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The heartbeat had never once arrived, on any installation.
//
// It sat behind RequireAuth, which parses the Bearer token as a JWT; the only
// thing that calls it is a box, which sends an API key. Every heartbeat since
// the feature existed got a 401 that nobody read, and the fleet page showed
// every plant as offline for a reason that had nothing to do with the plant.
//
// It also took the organization from the request body — which the caller
// writes — so any authenticated user of any tenant could have refreshed another
// organization's gateways and made a dead plant look alive.
func TestABoxHeartbeatIsAcceptedAndRecorded(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "hb-"+suffix)
	gatewayID := seedInventoryGateway(t, db, org.ID, "hb-plc-"+suffix)

	apiKey := downloadInstallerKey(t, admin, db, org.ID)

	before := time.Now().UTC()
	status, body := postHeartbeat(t, apiKey, `{"agent_version":"e2e-1.0","ts":1}`)
	if status != http.StatusOK {
		t.Fatalf("the heartbeat returned %d: %s — a box cannot report in at all",
			status, truncate(body))
	}

	// The box's own row.
	var lastSeen *time.Time
	var version string
	if err := db.QueryRow(
		`SELECT a.last_seen_at, COALESCE(a.agent_version, '')
		 FROM edge_agents a WHERE a.org_id = $1
		 ORDER BY a.created_at DESC LIMIT 1`, org.ID).Scan(&lastSeen, &version); err != nil {
		t.Fatalf("reading the box back: %v", err)
	}
	if lastSeen == nil || lastSeen.Before(before.Add(-time.Minute)) {
		t.Errorf("the box's last contact was not recorded: %v", lastSeen)
	}
	if version != "e2e-1.0" {
		t.Errorf("agent version = %q, want e2e-1.0", version)
	}

	// And the gateway it is responsible for.
	var gatewaySeen *time.Time
	if err := db.QueryRow(
		`SELECT last_seen_at FROM gateways WHERE id = $1`, gatewayID).Scan(&gatewaySeen); err != nil {
		t.Fatalf("reading the gateway back: %v", err)
	}
	if gatewaySeen == nil {
		t.Error("the gateway's last contact was not refreshed by its own box's heartbeat")
	}
}

// The organization comes from the key. A box holding one plant's key must not
// be able to vouch for another plant, whatever it puts in the body.
func TestAHeartbeatOnlyVouchesForItsOwnPlant(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	mine := createOrg(t, admin, "hb-mine-"+suffix)
	theirs := createOrg(t, admin, "hb-theirs-"+suffix)

	theirGateway := seedInventoryGateway(t, db, theirs.ID, "theirs-"+suffix)
	if _, err := db.Exec(`UPDATE gateways SET last_seen_at = NULL WHERE id = $1`, theirGateway); err != nil {
		t.Fatalf("clearing the other plant's contact time: %v", err)
	}

	myKey := downloadInstallerKey(t, admin, db, mine.ID)

	// The body names the other organization. It used to be believed.
	status, body := postHeartbeat(t, myKey,
		fmt.Sprintf(`{"org_id":%d,"agent_version":"spoof"}`, theirs.ID))
	if status != http.StatusOK {
		t.Fatalf("the heartbeat returned %d: %s", status, truncate(body))
	}

	var seen *time.Time
	if err := db.QueryRow(
		`SELECT last_seen_at FROM gateways WHERE id = $1`, theirGateway).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != nil {
		t.Fatalf("a box holding org %d's key refreshed org %d's gateway by naming it in the "+
			"request body — a dead plant would look alive", mine.ID, theirs.ID)
	}
}

func TestAHeartbeatWithoutAKeyIsRefused(t *testing.T) {
	status, _ := postHeartbeat(t, "", `{"agent_version":"x"}`)
	if status != http.StatusUnauthorized {
		t.Errorf("an unauthenticated heartbeat returned %d, want 401", status)
	}
	status, _ = postHeartbeat(t, "oe_deadbeef_notarealkey", `{"agent_version":"x"}`)
	if status != http.StatusUnauthorized {
		t.Errorf("a heartbeat with an invalid key returned %d, want 401", status)
	}
}

func postHeartbeat(t *testing.T, apiKey, body string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, apiBase()+"/api/edge/heartbeat",
		strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("posting the heartbeat: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the answer: %v", err)
	}
	return resp.StatusCode, raw
}
