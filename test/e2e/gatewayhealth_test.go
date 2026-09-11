//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/gatewayhealth"
)

// The watcher's decision logic is unit-tested against a fake store. Its SQL was
// not tested against anything at all — and a query that does not run fails
// inside a goroutine, logs one line, and leaves a watcher that reports every
// gateway as healthy forever.
//
// That is not a hypothetical. The service report shipped with a FILTER clause
// hung on EXTRACT instead of on the aggregate; it compiled, every unit test
// passed, and Postgres refused the statement the first time anybody asked for a
// report. This is the same class of defect, in code that nobody would even get
// an error page from.
func TestTheGatewayHealthQueriesRunAgainstTheRealDatabase(t *testing.T) {
	db := openDB(t)
	admin, _ := adminSession(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "gwh-"+suffix)
	gatewayID := seedInventoryGateway(t, db, org.ID, "gwh-plc-"+suffix)

	// seedInventoryGateway leaves the gateway disabled so driver-manager does
	// not chase a container for it; the watcher only looks at enabled ones.
	if _, err := db.Exec(
		`UPDATE gateways SET enabled = true, health_status = 'offline',
		                     health_reported_at = NOW(), health_notified_state = ''
		 WHERE id = $1`, gatewayID); err != nil {
		t.Fatalf("preparing the gateway: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`UPDATE gateways SET enabled = false WHERE id = $1`, gatewayID)
	})

	// A nil announcer: what is under test is the SQL, not the delivery. The
	// watcher must record the state either way.
	w := gatewayhealth.NewWatcher(db, nil, gatewayhealth.Config{Silence: time.Hour})

	if err := w.Scan(time.Now()); err != nil {
		t.Fatalf("the watcher's scan failed against the real database: %v", err)
	}

	// The driver reported the gateway offline, so the watcher should have
	// announced it and written down that it did. If the column names in
	// RecordNotified were wrong this is where it shows.
	var notified string
	if err := db.QueryRow(
		`SELECT COALESCE(health_notified_state, '') FROM gateways WHERE id = $1`,
		gatewayID).Scan(&notified); err != nil {
		t.Fatalf("reading back the recorded state: %v", err)
	}
	if notified != "down" {
		t.Fatalf("after a scan over a gateway reported offline, health_notified_state is %q, "+
			"want \"down\" — either the read query does not see the gateway or the write "+
			"does not reach it", notified)
	}

	// And once it answers again, the watcher clears what it recorded.
	if _, err := db.Exec(
		`UPDATE gateways SET health_status = 'online', health_reported_at = NOW() WHERE id = $1`,
		gatewayID); err != nil {
		t.Fatalf("bringing the gateway back: %v", err)
	}
	if err := w.Scan(time.Now()); err != nil {
		t.Fatalf("the second scan failed: %v", err)
	}
	if err := db.QueryRow(
		`SELECT COALESCE(health_notified_state, '') FROM gateways WHERE id = $1`,
		gatewayID).Scan(&notified); err != nil {
		t.Fatalf("reading back the recorded state: %v", err)
	}
	if notified != "" {
		t.Errorf("after the gateway came back, health_notified_state is still %q", notified)
	}
}
