//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
)

// The whole point of a box is that it works with no path to the central
// database. It gets its configuration over HTTP instead, and writes it into a
// Postgres of its own.
//
// Neither half of that had ever run. GET /api/edge/config was served and called
// by nothing; the box's agent read gateways straight out of a local database
// that nothing ever filled in. A freshly installed box would have started,
// found zero gateways, polled nothing, and reported itself perfectly healthy.
func TestABoxCanFetchItsConfigurationAndApplyIt(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "edgesync-"+suffix)
	gatewayID := seedInventoryGateway(t, db, org.ID, "edge-plc-"+suffix)

	// A tag and an alarm rule, because the drivers read both and a
	// configuration carrying only gateways would start containers that fail
	// their first query.
	var tagID int
	if err := db.QueryRow(
		`INSERT INTO tags (gateway_id, code, alias, data_type, historize)
		 VALUES ($1, '40001', $2, 'REAL', true) RETURNING id`,
		gatewayID, "temp-"+suffix).Scan(&tagID); err != nil {
		t.Fatalf("creating the tag: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO alarm_definitions (tag_id, alarm_type, threshold, severity, enabled)
		 VALUES ($1, 'high', 80, 'warning', true)`, tagID); err != nil {
		t.Fatalf("creating the alarm rule: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM alarm_definitions WHERE tag_id = $1`, tagID)
	})

	apiKey := mintAPIKey(t, admin, org.ID, "edgesync-"+suffix)

	// ── The box's half: fetch over HTTP, exactly as the agent does ──────────
	cfg, err := edgesync.Fetch(context.Background(), apiBase(), apiKey)
	if err != nil {
		t.Fatalf("a box could not fetch its configuration: %v", err)
	}

	if cfg.OrgID != org.ID {
		t.Fatalf("the configuration is for org %d, the key belongs to %d", cfg.OrgID, org.ID)
	}
	sites, areas, gateways, tags, alarms := cfg.Counts()
	t.Logf("configuration: %d sites, %d areas, %d gateways, %d tags, %d alarm rules",
		sites, areas, gateways, tags, alarms)

	// Every level of the tree has to be there. The endpoint used to return
	// gateways only, and a gateway whose area is missing cannot be written at
	// all — the foreign key refuses it.
	if sites == 0 || areas == 0 || gateways == 0 || tags == 0 || alarms == 0 {
		t.Fatalf("the configuration is incomplete: sites=%d areas=%d gateways=%d tags=%d alarms=%d",
			sites, areas, gateways, tags, alarms)
	}

	var foundGateway, foundTag bool
	for i := range cfg.Gateways {
		if cfg.Gateways[i].ID == gatewayID {
			foundGateway = true
			// The identifier has to survive the trip: the MQTT topics carry it
			// and the central platform resolves it against its own database.
			if cfg.Gateways[i].Name != "edge-plc-"+suffix {
				t.Errorf("gateway %d came back named %q", gatewayID, cfg.Gateways[i].Name)
			}
		}
	}
	for i := range cfg.Tags {
		if cfg.Tags[i].ID == tagID {
			foundTag = true
		}
	}
	if !foundGateway {
		t.Errorf("gateway %d is missing from the configuration", gatewayID)
	}
	if !foundTag {
		t.Errorf("tag %d is missing from the configuration", tagID)
	}

	// ── Applying it ────────────────────────────────────────────────────────
	// Against this same database, which already holds exactly this data: the
	// upsert should be a no-op. That is the point — it exercises every
	// statement, every column name and every foreign key, and proves the
	// operation is idempotent, without needing a second Postgres.
	if err := edgesync.Apply(context.Background(), db, cfg); err != nil {
		t.Fatalf("applying the configuration failed: %v", err)
	}
	if err := edgesync.Apply(context.Background(), db, cfg); err != nil {
		t.Fatalf("applying the configuration a second time failed — it is not idempotent, "+
			"and a box syncs every minute: %v", err)
	}

	// Nothing was destroyed on the way through.
	var stillThere int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = $1`, tagID).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere != 1 {
		t.Fatalf("the tag is gone after applying a configuration that contains it")
	}
}

// The key is what says which plant the box is. A box must not be able to pull
// somebody else's configuration, and the failure would not look like an
// authorization error — it would look like a longer list of gateways.
func TestABoxCanOnlyFetchItsOwnPlant(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	mine := createOrg(t, admin, "edgesync-mine-"+suffix)
	theirs := createOrg(t, admin, "edgesync-theirs-"+suffix)

	myGateway := seedInventoryGateway(t, db, mine.ID, "mine-"+suffix)
	theirGateway := seedInventoryGateway(t, db, theirs.ID, "theirs-"+suffix)

	apiKey := mintAPIKey(t, admin, mine.ID, "edgesync-scope-"+suffix)

	cfg, err := edgesync.Fetch(context.Background(), apiBase(), apiKey)
	if err != nil {
		t.Fatalf("fetching: %v", err)
	}

	var sawMine bool
	for i := range cfg.Gateways {
		switch cfg.Gateways[i].ID {
		case theirGateway:
			t.Errorf("the box for org %d was handed org %d's gateway %q",
				mine.ID, theirs.ID, cfg.Gateways[i].Name)
		case myGateway:
			sawMine = true
		}
	}
	if !sawMine {
		t.Errorf("the box was not given its own gateway %d", myGateway)
	}
}

// A configuration that names no organization would, applied, delete every site
// on the box — the deletes are scoped by it.
func TestAnEmptyConfigurationIsRefusedRatherThanApplied(t *testing.T) {
	db := openDB(t)

	if err := edgesync.Apply(context.Background(), db, &edgesync.Config{}); err == nil {
		t.Fatal("a configuration naming no organization was applied; scoped deletes with no " +
			"scope empty the box")
	}
	if err := edgesync.Apply(context.Background(), db, nil); err == nil {
		t.Fatal("a nil configuration was accepted")
	}
}

// mintAPIKey creates an org API key through the API and returns the plaintext.
func mintAPIKey(t *testing.T, admin *apiClient, orgID int, name string) string {
	t.Helper()

	status, body := admin.do("POST", fmt.Sprintf("/api/organizations/%d/api-keys", orgID),
		map[string]string{"name": name})
	if status != 201 {
		t.Fatalf("creating an API key returned %d: %s", status, truncate(body))
	}
	// The plaintext is returned once, on creation, under "key".
	var created struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("the API key response is not JSON: %v — %s", err, truncate(body))
	}
	if created.Key == "" {
		t.Fatalf("no plaintext key in the response: %s", truncate(body))
	}
	return created.Key
}
