//go:build e2e

package e2e

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/ralph/industrial-edge-middleware/internal/topics"
)

// sys/health used to be keyed by gateway alone, with no organization in it.
// Every other system topic is scoped by organization and the broker's
// permissions follow that scoping; this one could not be, so the grant had to
// be sys/health/+ for everybody — any tenant could read another tenant's
// gateway states or publish a false one.
//
// It carries the organization now, and both shapes are understood while the
// drivers in the field are updated one container at a time. This asserts the
// whole chain: a message published on the new topic reaches the platform and
// changes what it believes about that gateway.
func TestGatewayHealthArrivesOnTheOrganizationScopedTopic(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "health-"+suffix)
	gatewayID := seedInventoryGateway(t, db, org.ID, "health-plc-"+suffix)

	if _, err := db.Exec(
		`UPDATE gateways SET health_status = NULL, health_reported_at = NULL WHERE id = $1`,
		gatewayID); err != nil {
		t.Fatalf("clearing the gateway's health: %v", err)
	}

	publishHealth(t, topics.Health(org.ID, gatewayID), "offline")

	status := waitForHealth(t, db, gatewayID, "offline")
	if status != "offline" {
		t.Fatalf("the platform recorded %q after a message on %s — the chain from the "+
			"driver to the database is broken somewhere",
			status, topics.Health(org.ID, gatewayID))
	}

	// And back, so this is not passing on a column that merely happened to hold
	// the right word.
	publishHealth(t, topics.Health(org.ID, gatewayID), "online")
	if status := waitForHealth(t, db, gatewayID, "online"); status != "online" {
		t.Errorf("after coming back the platform recorded %q", status)
	}
}

// A driver that has not been updated yet still publishes the old shape. It has
// to keep being heard, or upgrading the platform would show every plant as dark
// until the last container in the field had been replaced.
func TestTheOldHealthTopicIsStillUnderstood(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "health-old-"+suffix)
	gatewayID := seedInventoryGateway(t, db, org.ID, "health-old-plc-"+suffix)

	if _, err := db.Exec(
		`UPDATE gateways SET health_status = NULL, health_reported_at = NULL WHERE id = $1`,
		gatewayID); err != nil {
		t.Fatal(err)
	}

	publishHealth(t, fmt.Sprintf("sys/health/%d", gatewayID), "error")

	if status := waitForHealth(t, db, gatewayID, "error"); status != "error" {
		t.Fatalf("a driver publishing the old topic was not heard (recorded %q); every "+
			"gateway whose container has not been updated would look dark", status)
	}
}

func publishHealth(t *testing.T, topic, payload string) {
	t.Helper()

	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", mqttHost(), mqttPort())).
		SetClientID("e2e-health-" + uniqueSuffix()).
		SetUsername(env("E2E_MQTT_USER", "core-api")).
		SetPassword(env("E2E_MQTT_PASSWORD", "")).
		SetConnectTimeout(10 * time.Second).
		SetCleanSession(true)
	if insecureTLS() {
		opts.SetTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
			// #nosec G402 -- opt-in via E2E_TLS_INSECURE; see insecureTLS.
			InsecureSkipVerify: true,
		})
	}

	c := paho.NewClient(opts)
	if tok := c.Connect(); !tok.WaitTimeout(15*time.Second) || tok.Error() != nil {
		t.Fatalf("connecting to the broker: %v", tok.Error())
	}
	defer c.Disconnect(250)

	// Retained, exactly as a driver publishes it: the platform has to see the
	// state of a gateway that reported before it started.
	if tok := c.Publish(topic, 1, true, payload); !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		t.Fatalf("publishing on %s: %v", topic, tok.Error())
	}
}

// waitForHealth polls the column the platform writes, because the path from the
// broker to the database is asynchronous and a single read would be a race.
func waitForHealth(t *testing.T, db *sql.DB, gatewayID int, want string) string {
	t.Helper()

	var status string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.QueryRow(
			`SELECT COALESCE(health_status, '') FROM gateways WHERE id = $1`,
			gatewayID).Scan(&status); err != nil {
			t.Fatalf("reading the gateway's health: %v", err)
		}
		if status == want {
			return status
		}
		time.Sleep(250 * time.Millisecond)
	}
	return status
}
