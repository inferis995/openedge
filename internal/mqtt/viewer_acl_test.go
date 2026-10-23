package mqtt

import (
	"sort"
	"strings"
	"testing"
)

// ── A model of how Mosquitto's dynamic-security plugin decides ────────────────
//
// For a client with one role: its ACLs of the asked type are tried from the
// highest priority down, and the first whose topic filter matches decides.
// When none matches, the broker's default decides — and the plugin's stock
// default ALLOWS receiving. The tests below run with that default, the
// unfavorable one, so a role is only isolated if it is isolated on its own.

func filterMatches(filter, topic string) bool {
	f, t := strings.Split(filter, "/"), strings.Split(topic, "/")
	for i, part := range f {
		if part == "#" {
			return true
		}
		if i >= len(t) {
			return false
		}
		if part != "+" && part != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}

func decide(acls []map[string]interface{}, aclType, topic string, stockDefault bool) bool {
	matching := make([]map[string]interface{}, 0, len(acls))
	for _, a := range acls {
		if a["acltype"] == aclType {
			matching = append(matching, a)
		}
	}
	sort.SliceStable(matching, func(i, j int) bool {
		return matching[i]["priority"].(int) > matching[j]["priority"].(int)
	})
	for _, a := range matching {
		if filterMatches(a["topic"].(string), topic) {
			return a["allow"].(bool)
		}
	}
	return stockDefault
}

// receives: would a message on topic be delivered to this client?
func receives(acls []map[string]interface{}, topic string) bool {
	return decide(acls, "publishClientReceive", topic, true)
}

// sends: may this client publish on topic?
func sends(acls []map[string]interface{}, topic string) bool {
	return decide(acls, "publishClientSend", topic, false)
}

var (
	acme = TenantScope{OrgID: 7, OrgName: "Acme Corp", SiteNames: []string{"Plant A"}, GatewayIDs: []int{70, 71}}
	// The neighbor: another organization on the same broker.
	beta = TenantScope{OrgID: 8, OrgName: "Beta", SiteNames: []string{"North"}, GatewayIDs: []int{80}}
)

// Topics that belong to Beta, one per kind of traffic the broker carries.
var betaTopics = []string{
	"data/Beta/North/Area/GW/temp",
	"data/beta/north/area/gw/temp",
	"spBv1.0/beta-north/DDATA/area-gw/dev",
	"spBv1.0/beta-north/NBIRTH/area-gw",
	"spBv1.0/beta-north/DCMD/area-gw/dev",
	"spBv1.0/DDATA/beta-north/area-gw/dev",
	"sys/alarms/8/north/area/gw/temp",
	"sys/write/8/north/area/gw/temp",
	"sys/edge/8/ping",
	"sys/update/8",
	"sys/restart/8/box-1",
	"sys/health/80",
	"sys/health/8/80",
	"sys/command/reload/80",
	"sys/command/write/80",
	"sys/command/settings-reload/80",
	"sys/lorawan/down/80",
	"cmd/write/80",
	"cmd/write/result/80",
	"cmd/Beta/North/Area/GW/temp",
	"cmd/result/beta/north/area/gw/temp",
}

func roles(s TenantScope) map[string][]map[string]interface{} {
	return map[string][]map[string]interface{}{"edge": orgRoleACLs(s), "web UI viewer": uiViewerACLs(s)}
}

// The property the whole file is about: with Acme's credentials, nothing of
// Beta's is delivered and nothing can be published into Beta's topics — on a
// broker whose default lets everything through.
func TestATenantNeitherReadsNorWritesAnotherTenantsTopics(t *testing.T) {
	for name, acls := range roles(acme) {
		for _, topic := range betaTopics {
			if receives(acls, topic) {
				t.Errorf("%s role of org 7 RECEIVES %q, which belongs to org 8", name, topic)
			}
			if sends(acls, topic) {
				t.Errorf("%s role of org 7 may PUBLISH on %q, which belongs to org 8", name, topic)
			}
		}
	}
}

// The list above must really be Beta's traffic, or the test proves nothing:
// Beta's own edge role reaches every one of those topics.
func TestTheNeighborsTopicsAreRealTopics(t *testing.T) {
	edge := orgRoleACLs(beta)
	for _, topic := range betaTopics {
		if !receives(edge, topic) && !sends(edge, topic) {
			t.Errorf("%q is not a topic of org 8 at all; the isolation test would pass on it vacuously", topic)
		}
	}
}

// Same with an organization that has no site yet: it has no Sparkplug group
// either, and must not get a group-level wildcard in its place.
func TestAnOrganizationWithoutSitesGetsNoSparkplugWildcard(t *testing.T) {
	bare := TenantScope{OrgID: 7, OrgName: "Acme Corp"}
	for name, acls := range roles(bare) {
		for _, topic := range betaTopics {
			if receives(acls, topic) || sends(acls, topic) {
				t.Errorf("%s role of an organization with no sites reaches %q", name, topic)
			}
		}
		for _, a := range acls {
			topic := a["topic"].(string)
			if a["allow"] == true && strings.HasPrefix(topic, sparkplugNamespace+"/") && a["acltype"] != "subscribePattern" {
				t.Errorf("%s role grants %s on %q with no site to scope it", name, a["acltype"], topic)
			}
		}
	}
}

// No grant that delivers or publishes uses a wildcard where a tenant's
// identity belongs: the level after data/, cmd/, sys/<kind>/, the gateway id,
// or the Sparkplug group.
func TestNoGrantWildcardsTheTenantLevel(t *testing.T) {
	allowedPlus := map[string]bool{
		"sys/health/7/+": true, "sys/update/7/+": true, "sys/restart/7/+": true,
	}
	for name, acls := range roles(acme) {
		for _, a := range acls {
			topic := a["topic"].(string)
			if a["allow"] != true || a["acltype"] == "subscribePattern" {
				continue
			}
			if strings.Contains(topic, "+") && !allowedPlus[topic] {
				t.Errorf("%s role grants %s on %q: a single-level wildcard on a tenant level", name, a["acltype"], topic)
			}
			parts := strings.Split(topic, "/")
			if len(parts) <= 2 && strings.HasSuffix(topic, "#") {
				t.Errorf("%s role grants %s on %q: wider than any one tenant", name, a["acltype"], topic)
			}
		}
	}
}

// And the role still works for its owner: an edge publishes and receives its
// own traffic, a viewer receives its own data.
func TestATenantStillReachesItsOwnTopics(t *testing.T) {
	edge, viewer := orgRoleACLs(acme), uiViewerACLs(acme)
	for _, topic := range []string{
		"data/Acme Corp/Plant A/Area/GW/temp",
		"data/acme-corp/plant-a/area/gw/temp",
		"spBv1.0/acme-corp-plant-a/DDATA/area-gw/dev",
		"sys/alarms/7/plant-a/area/gw/temp",
	} {
		if !receives(edge, topic) || !receives(viewer, topic) {
			t.Errorf("org 7 does not receive its own %q (edge %v, viewer %v)", topic, receives(edge, topic), receives(viewer, topic))
		}
	}
	for _, topic := range []string{
		"data/acme-corp/plant-a/area/gw/temp",
		"spBv1.0/acme-corp-plant-a/DDATA/area-gw/dev",
		"spBv1.0/DDATA/acme-corp-plant-a/area-gw/dev",
		"sys/health/70",
		"sys/health/7/71",
		"cmd/write/result/71",
		"sys/edge/7/ping",
	} {
		if !sends(edge, topic) {
			t.Errorf("the edge of org 7 cannot publish its own %q — no gateway could report", topic)
		}
	}
	for _, topic := range []string{
		"sys/command/reload/70", "cmd/write/71", "sys/lorawan/down/70", "sys/update/7", "sys/restart/7/box-1",
		"spBv1.0/acme-corp-plant-a/DCMD/area-gw/dev", "sys/command/settings-reload",
	} {
		if !receives(edge, topic) {
			t.Errorf("the edge of org 7 does not receive the command %q addressed to it", topic)
		}
	}
}

// Commands travel core-api → edge only. Neither role may send one, to anybody,
// itself included: a DCMD or a cmd/write is a setpoint on a real machine.
func TestNoRoleCanIssueACommand(t *testing.T) {
	for name, acls := range roles(acme) {
		for _, topic := range []string{
			"spBv1.0/acme-corp-plant-a/DCMD/area-gw/dev", "spBv1.0/acme-corp-plant-a/NCMD/area-gw",
			"cmd/write/70", "sys/command/write/70", "sys/command/reload/70", "cmd/acme-corp/plant-a/area/gw/t",
			"sys/update/7", "sys/restart/7",
		} {
			if sends(acls, topic) {
				t.Errorf("%s role of org 7 may publish the command %q", name, topic)
			}
		}
	}
}

// A browser identity cannot publish at all, whatever the scope.
func TestUIViewerCannotPublishAnything(t *testing.T) {
	for _, s := range []TenantScope{acme, {OrgID: 7, OrgName: "Acme Corp"}} {
		acls := uiViewerACLs(s)
		for _, a := range acls {
			if a["acltype"] == "publishClientSend" && a["allow"] == true {
				t.Errorf("the viewer role may PUBLISH on %q", a["topic"])
			}
		}
		if sends(acls, "data/acme-corp/plant-a/area/gw/temp") {
			t.Error("the viewer can publish tag data")
		}
	}
}

// A name that is not one topic level must not become a grant: "+" would be
// every organization, "a/b" organization a's site b.
func TestUnsafeNamesAreNeverGranted(t *testing.T) {
	for _, name := range []string{"+", "#", "Beta/North", "$SYS"} {
		s := TenantScope{OrgID: 9, OrgName: name, SiteNames: []string{"x"}}
		for role, acls := range roles(s) {
			for _, topic := range betaTopics {
				if receives(acls, topic) || sends(acls, topic) {
					t.Errorf("org named %q: %s role reaches %q", name, role, topic)
				}
			}
		}
	}
	if TopicSafe("+") || TopicSafe("a/b") || TopicSafe("") || TopicSafe("$x") || !TopicSafe("Acme Corp") {
		t.Error("TopicSafe disagrees with the cases above")
	}
}

// The pinned groups name this organization's sites, so "no wildcard" cannot be
// satisfied by granting nothing.
func TestPinnedSitesNameTheOrganizationsOwnGroups(t *testing.T) {
	if got := acme.SparkplugGroups(); len(got) != 1 || got[0] != "acme-corp-plant-a" {
		t.Fatalf("groups %v, want [acme-corp-plant-a]", got)
	}
	if !receives(uiViewerACLs(acme), "spBv1.0/acme-corp-plant-a/DDATA/area-gw/dev") {
		t.Fatal("the viewer sees none of its own Sparkplug traffic")
	}
}

// Every role ends with the explicit deny: correct on a broker whose default
// allows everything, not only on ours.
func TestEveryRoleClosesItself(t *testing.T) {
	for name, acls := range roles(acme) {
		for _, typ := range []string{"publishClientSend", "publishClientReceive", "subscribePattern"} {
			found := false
			for _, a := range acls {
				if a["acltype"] == typ && a["topic"] == "#" && a["allow"] == false {
					found = true
				}
			}
			if !found {
				t.Errorf("%s role has no closing deny for %s", name, typ)
			}
		}
	}
}
