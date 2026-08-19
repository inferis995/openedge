package mqtt

import (
	"strings"
	"testing"
)

// The property the whole fix rests on: a browser cannot publish.
//
// The web UI used to reach the broker anonymously, which on the cloud
// deployment meant anyone on the internet could subscribe to every tenant's
// live data AND publish cmd/write/{gateway} — a setpoint on a real machine. The
// answer was to give the UI an identity; the answer is only worth anything if
// that identity is read-only.
//
// It would have been easier to hand the UI the org's existing role. That role
// belongs to an EDGE: it may publish tag data, because a gateway must. Any
// signed-in user, including one whose whole purpose is to look at a dashboard,
// would then be able to inject readings indistinguishable from a PLC's.
func TestUIViewerCannotPublishAnything(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sites []string
	}{
		// Both branches: with the org's sites the Sparkplug group is pinned,
		// without them it falls back to the wildcard form. A grant that leaks
		// into only one of them is the kind that ships.
		{"sites known", []string{"Plant A", "Plant B"}},
		{"sites unknown", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acls := uiViewerACLs(7, "Acme Corp", tc.sites)
			if len(acls) == 0 {
				t.Fatal("the viewer role is empty — it would see nothing, and this test would " +
					"pass on a role that grants nothing at all")
			}

			sawReceive, sawSubscribe := false, false
			for _, acl := range acls {
				aclType, _ := acl["acltype"].(string)
				topic, _ := acl["topic"].(string)
				switch aclType {
				case "publishClientSend":
					t.Errorf("the viewer role may PUBLISH on %q. This identity is handed to a "+
						"browser: publishing means injecting tag values, and on cmd/ topics it "+
						"means writing a setpoint to a PLC", topic)
				case "publishClientReceive":
					sawReceive = true
				case "subscribePattern", "subscribeLiteral":
					sawSubscribe = true
				default:
					t.Errorf("unexpected ACL type %q on %q", aclType, topic)
				}
			}
			if !sawReceive || !sawSubscribe {
				t.Errorf("the viewer must be able to subscribe and receive (receive=%v subscribe=%v)",
					sawReceive, sawSubscribe)
			}
		})
	}
}

// A viewer sees its own organization's data, not the neighbours'.
func TestUIViewerIsScopedToItsOrganization(t *testing.T) {
	acls := uiViewerACLs(7, "Acme Corp", []string{"Plant A"})

	var receives []string
	for _, acl := range acls {
		if acl["acltype"] == "publishClientReceive" {
			receives = append(receives, acl["topic"].(string))
		}
	}
	if len(receives) == 0 {
		t.Fatal("no receive grants at all")
	}

	for _, topic := range receives {
		// Legacy data and alarms are keyed by org, so they must name it.
		if strings.HasPrefix(topic, "data/") && !strings.Contains(strings.ToLower(topic), "acme") {
			t.Errorf("receive grant %q is not scoped to the organization", topic)
		}
		if strings.HasPrefix(topic, "sys/alarms/") && !strings.HasPrefix(topic, "sys/alarms/7/") {
			t.Errorf("alarm grant %q is not scoped to org 7", topic)
		}
		// Setpoint traffic is none of a dashboard's business, in either
		// direction: reading a DCMD discloses what is about to be written.
		for _, cmd := range sparkplugCommands {
			if strings.Contains(topic, "/"+cmd+"/") || strings.HasSuffix(topic, "/"+cmd) {
				t.Errorf("the viewer may receive %s traffic via %q — that is setpoint disclosure", cmd, topic)
			}
		}
		if strings.HasPrefix(topic, "cmd/") || strings.HasPrefix(topic, "sys/command/") {
			t.Errorf("the viewer may receive command traffic via %q", topic)
		}
	}
}

// The edge role is the counterpart: it MUST be able to publish, or no gateway
// can report a value. Without this, deleting every send grant everywhere would
// satisfy the test above.
func TestTheEdgeRoleStillPublishes(t *testing.T) {
	sends := 0
	for _, acl := range orgRoleACLs(7, "Acme Corp", []string{"Plant A"}) {
		if acl["acltype"] == "publishClientSend" {
			sends++
		}
	}
	if sends == 0 {
		t.Fatal("the org (edge) role can no longer publish — no gateway could report data")
	}
}
