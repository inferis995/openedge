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

// Supplying the sites is what closes the cross-tenant hole, in BOTH roles.
//
// Sparkplug puts the organization and the site in one topic level —
// spBv1.0/{org-slug}-{site-slug}/... — and MQTT wildcards match whole levels, so
// "+" cannot prefix-match "acme-*". Without the site list the grant has to use
// that "+", and "+" is every other tenant's group too.
//
// Both roles were built once, at organization creation, when no site exists yet,
// and never rebuilt — so every deployment ran on the wildcard. The comment in
// orgRoleACLs said it would close "as soon as siteNames is supplied"; nothing
// supplied it until refreshOrgMQTTRoles. This is the assertion that the promise
// is real, and that it stays real.
func TestSupplyingSitesRemovesTheCrossTenantWildcard(t *testing.T) {
	for _, role := range []struct {
		name  string
		build func(sites []string) []map[string]interface{}
	}{
		{"web UI viewer", func(s []string) []map[string]interface{} { return uiViewerACLs(7, "Acme Corp", s) }},
		{"edge", func(s []string) []map[string]interface{} { return orgRoleACLs(7, "Acme Corp", s) }},
	} {
		t.Run(role.name, func(t *testing.T) {
			wildcards := func(acls []map[string]interface{}) []string {
				var out []string
				for _, acl := range acls {
					topic, _ := acl["topic"].(string)
					// The group level is the one right after the namespace.
					if strings.HasPrefix(topic, sparkplugNamespace+"/+/") {
						out = append(out, topic)
					}
				}
				return out
			}

			// Without sites the wildcard is there — otherwise this test would be
			// asserting the absence of something that never appears.
			if got := wildcards(role.build(nil)); len(got) == 0 {
				t.Fatal("no group-level wildcard even without sites; this test cannot detect " +
					"the regression it exists for")
			}

			if got := wildcards(role.build([]string{"Plant A", "Plant B"})); len(got) > 0 {
				t.Errorf("with the org's sites supplied the grant still wildcards the Sparkplug "+
					"group level: %v. That level carries {org}-{site}, so a wildcard there is "+
					"every other tenant's namespace", got)
			}
		})
	}
}

// And the pinned form must actually name this org's groups, or "no wildcards"
// could be satisfied by granting nothing.
func TestPinnedSitesNameTheOrganizationsOwnGroups(t *testing.T) {
	acls := uiViewerACLs(7, "Acme Corp", []string{"Plant A"})

	found := false
	for _, acl := range acls {
		if topic, _ := acl["topic"].(string); strings.Contains(topic, "acme-corp-plant-a") {
			found = true
		}
	}
	if !found {
		t.Fatal("the grant names no group for org 'Acme Corp' site 'Plant A' — the viewer would " +
			"see none of its own Sparkplug traffic")
	}
}
