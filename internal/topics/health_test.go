package topics_test

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/topics"
)

func TestTheTopicCarriesTheOrganization(t *testing.T) {
	if got := topics.Health(7, 42); got != "sys/health/7/42" {
		t.Errorf("got %q, want sys/health/7/42", got)
	}
}

// A driver started by hand, without the environment a supervisor gives it,
// should still land somewhere a listener is looking rather than on
// sys/health/0/42, which nothing subscribes to and nothing would report.
func TestWithNoOrganizationItFallsBackToTheOldShape(t *testing.T) {
	for _, org := range []int{0, -1} {
		if got := topics.Health(org, 42); got != "sys/health/42" {
			t.Errorf("Health(%d, 42) = %q, want the old shape", org, got)
		}
	}
}

// Drivers are updated one container at a time, so during a rollout the same
// broker carries both shapes. A platform that understood only the new one
// would show every not-yet-updated gateway as never having reported — which
// looks exactly like a plant that has gone dark.
func TestBothShapesAreUnderstood(t *testing.T) {
	org, gw, ok := topics.ParseHealth("sys/health/7/42")
	if !ok || org != 7 || gw != 42 {
		t.Errorf("new shape: org=%d gw=%d ok=%v", org, gw, ok)
	}

	org, gw, ok = topics.ParseHealth("sys/health/42")
	if !ok || org != 0 || gw != 42 {
		t.Errorf("old shape: org=%d gw=%d ok=%v — a driver that has not been updated "+
			"yet would stop being heard", org, gw, ok)
	}
}

// What the platform writes, the platform must read back.
func TestWhatIsPublishedIsWhatIsParsed(t *testing.T) {
	for _, c := range []struct{ org, gw int }{{7, 42}, {1, 1}, {0, 9}, {999, 100000}} {
		topic := topics.Health(c.org, c.gw)
		org, gw, ok := topics.ParseHealth(topic)
		if !ok {
			t.Fatalf("Health(%d,%d) produced %q, which ParseHealth refuses", c.org, c.gw, topic)
		}
		if gw != c.gw {
			t.Errorf("%q parsed to gateway %d, want %d", topic, gw, c.gw)
		}
		wantOrg := c.org
		if wantOrg < 0 {
			wantOrg = 0
		}
		if org != wantOrg {
			t.Errorf("%q parsed to org %d, want %d", topic, org, wantOrg)
		}
	}
}

func TestRubbishIsRefused(t *testing.T) {
	for _, topic := range []string{
		"", "sys", "sys/health", "sys/health/abc", "sys/health/7/abc",
		"sys/health/abc/42", "sys/health/7/42/1", "sys/alarms/7/42",
		"data/7/42", "health/42",
		// Zero is not a gateway and not an organization. Accepting it would
		// turn a malformed topic into an update against gateway zero.
		"sys/health/0", "sys/health/0/42", "sys/health/7/0",
	} {
		if _, _, ok := topics.ParseHealth(topic); ok {
			t.Errorf("ParseHealth(%q) was accepted", topic)
		}
	}
}

// The subscription has to cover both shapes, or half the fleet goes quiet the
// moment the platform is updated.
func TestTheFilterCoversBothShapes(t *testing.T) {
	if topics.HealthFilter != "sys/health/#" {
		t.Fatalf("filter = %q; a single-level wildcard would miss one of the two shapes",
			topics.HealthFilter)
	}
}
