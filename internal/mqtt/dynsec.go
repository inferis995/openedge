package mqtt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DynsecClient manages MQTT broker Dynamic Security plugin users via the control API.
// Commands are published to $CONTROL/dynamic-security/v1 and responses arrive on
// $CONTROL/dynamic-security/v1/response.
//
// There are TWO locks here, and the difference is the whole reason this worked
// for nobody. cmdMu serializes commands and IS held across the wait for a
// reply. mu guards responseCh alone and is NEVER held while waiting — the
// subscription callback needs it to hand the reply over, and one lock doing
// both jobs meant the reply could not be delivered until the wait it was
// supposed to end had already timed out.
// dynsecPublisher is the one thing send() needs from the MQTT client.
//
// It is an interface so the locking can be tested: the defect this file exists
// to remember is a deadlock between send() and the subscription callback, and
// a test that cannot run send() can only imitate it — which is exactly how the
// first attempt at a regression test passed while the bug was back in place.
type dynsecPublisher interface {
	Publish(topic string, payload interface{}) error
}

type DynsecClient struct {
	client dynsecPublisher

	// Held for a whole command/response exchange: one in flight at a time.
	cmdMu sync.Mutex

	// Guards responseCh only. Taken for the length of a pointer read.
	mu         sync.Mutex
	responseCh chan dynsecResponse
	// The exchange this channel belongs to, so a reply that arrives after its
	// own command gave up cannot be handed to the next one.
	correlationID string
}

type dynsecCmd struct {
	Commands []map[string]interface{} `json:"commands"`
}

type dynsecResponse struct {
	Responses []struct {
		Command         string `json:"command"`
		CorrelationData string `json:"correlationData,omitempty"`
		Error           string `json:"error,omitempty"`
	} `json:"responses"`
}

// NewDynsecClient wraps an existing connected MQTT client with dynamic-security
// command support.  It subscribes to the response topic immediately.
func NewDynsecClient(c *Client) *DynsecClient {
	d := &DynsecClient{client: c}

	_ = c.Subscribe("$CONTROL/dynamic-security/v1/response", func(_ string, payload []byte) {
		var resp dynsecResponse
		if json.Unmarshal(payload, &resp) != nil {
			return
		}
		d.mu.Lock()
		ch, want := d.responseCh, d.correlationID
		d.mu.Unlock()
		if ch == nil {
			return
		}
		// Every response in a batch carries the correlationData of the command
		// it answers. A reply for an exchange that already gave up must be
		// dropped, not delivered to whoever is waiting now.
		for _, r := range resp.Responses {
			if r.CorrelationData != "" && r.CorrelationData != want {
				return
			}
		}
		select {
		case ch <- resp:
		default:
		}
	})
	return d
}

// dynsecTimeout bounds one command/response exchange with the broker.
const dynsecTimeout = 5 * time.Second

// send publishes a batch of commands and waits for the response.
//
// The caller holds cmdMu. This function must NOT hold mu while waiting: the
// subscription callback takes mu to deliver the very response being waited for.
func (d *DynsecClient) send(commands []map[string]interface{}, correlationID string) error {
	// Attach correlationData to every command
	for _, cmd := range commands {
		cmd["correlationData"] = correlationID
	}

	ch := make(chan dynsecResponse, 1)
	d.mu.Lock()
	d.responseCh, d.correlationID = ch, correlationID
	d.mu.Unlock()

	// Cleared under the same lock the callback reads it under, and always —
	// every path out of this function goes through here.
	defer func() {
		d.mu.Lock()
		d.responseCh, d.correlationID = nil, ""
		d.mu.Unlock()
	}()

	data, err := json.Marshal(dynsecCmd{Commands: commands})
	if err != nil {
		return err
	}

	if err := d.client.Publish("$CONTROL/dynamic-security/v1", data); err != nil {
		return fmt.Errorf("dynsec publish: %w", err)
	}

	select {
	case resp := <-ch:
		for _, r := range resp.Responses {
			if r.Error == "" {
				continue
			}
			// "already exists" is the state we were asking for.
			//
			// This matters most on the installations that ran the broken
			// version: the broker executed every command it was sent, so their
			// roles and clients are already there, while the API recorded the
			// exchange as failed. Treating that as an error would turn a silent
			// failure into a loud one at the moment it started working.
			if strings.Contains(strings.ToLower(r.Error), "already exists") {
				continue
			}
			return fmt.Errorf("dynsec %s: %s", r.Command, r.Error)
		}
		return nil
	case <-time.After(dynsecTimeout):
		return fmt.Errorf("dynsec command timed out (correlationData=%s)", correlationID)
	}
}

// sparkplugNamespace mirrors sparkplug.Namespace.  It is duplicated here instead
// of imported because internal/sparkplug already imports internal/mqtt, so an
// import in the other direction would be a cycle.
const sparkplugNamespace = "spBv1.0"

// sparkplugEdgeOriginated are the Sparkplug B message types an edge legitimately
// *publishes* (birth/death/data).  Everything else — notably NCMD and DCMD — is a
// command issued by core-api towards the edge, so an org role must never be able
// to send them.  DCMD carries setpoint writes to PLCs: granting send on it is a
// control action, not just a data leak.
//
// The list is the full set of MessageType* constants in internal/sparkplug/types.go
// minus the two command types.  There is deliberately no STATE entry: this codebase
// never publishes a Sparkplug STATE message, and STATE does not even use the
// {group}/{msgtype} layout (it is spBv1.0/STATE/{host_id}), so a "spBv1.0/+/STATE/#"
// grant would be dead weight that matches nothing.
var sparkplugEdgeOriginated = []string{"NBIRTH", "NDEATH", "NDATA", "DBIRTH", "DDEATH", "DDATA"}

// sparkplugCommands are the Sparkplug B message types an edge may only *receive*.
var sparkplugCommands = []string{"NCMD", "DCMD"}

// slugifyTopic mirrors the unexported slugify() in internal/sparkplug/topic.go —
// keep the two in sync, otherwise the ACL will not match the topics the drivers
// actually publish on.  Verified identical as of this change: TrimSpace, ToLower,
// " " -> "-", "_" -> "-".
func slugifyTopic(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

// aclSet accumulates Mosquitto dynamic-security ACL entries, de-duplicating
// identical (acltype, topic) pairs.  Duplicates are common because an org name
// that is already slug-shaped ("acme") yields the same topic as its slug.
type aclSet struct {
	entries []map[string]interface{}
	seen    map[string]bool
}

func newACLSet() *aclSet {
	return &aclSet{entries: make([]map[string]interface{}, 0, 64), seen: make(map[string]bool)}
}

func (a *aclSet) add(aclType, topic string) {
	key := aclType + "\x00" + topic
	if a.seen[key] {
		return
	}
	a.seen[key] = true
	a.entries = append(a.entries, map[string]interface{}{
		"acltype": aclType, "topic": topic, "priority": 1, "allow": true,
	})
}

// send lets the client PUBLISH on the filter.  This is the only ACL type that can
// authorise a control action, so it is the one to be stingy with.
func (a *aclSet) send(topics ...string) {
	for _, t := range topics {
		a.add("publishClientSend", t)
	}
}

// receive lets a message on the filter be DELIVERED to the client.  Mosquitto
// evaluates publishClientReceive per delivered message, independently of the
// subscription filter, so this — not subscribePattern — is the read boundary.
func (a *aclSet) receive(topics ...string) {
	for _, t := range topics {
		a.add("publishClientReceive", t)
	}
}

// subscribe lets the client register a subscription whose filter is covered by
// the pattern.  Mosquitto checks the *subscription string* here (a request for
// "sys/health/#" is refused by a "sys/health/+" pattern — it is not a topic
// match), so these must mirror the literal filters the services subscribe with.
// Granting a wide subscribePattern is safe: delivery is still gated by receive().
func (a *aclSet) subscribe(topics ...string) {
	for _, t := range topics {
		a.add("subscribePattern", t)
	}
}

// sendReceive is the read/write grant for topics the edge both produces and may
// legitimately consume within its own tenant.
func (a *aclSet) sendReceive(topics ...string) {
	a.send(topics...)
	a.receive(topics...)
}

// orgNameVariants returns the distinct spellings of an org name that appear in
// topics at runtime.  Both occur: the drivers interpolate the configured org name
// verbatim (services/driver-*/main.go, `data/%s/%s/...` with cfg.OrgName), while
// internal/sparkplug.BuildLegacyTopic slugifies every level.  internal/handlers
// (tags.go, gateways.go) subscribes to *both* spellings for exactly this reason,
// so the ACL must grant both or half the legacy tag traffic is silently dropped.
func orgNameVariants(orgName string) []string {
	out := []string{orgName}
	if s := slugifyTopic(orgName); s != orgName {
		out = append(out, s)
	}
	return out
}

// orgRoleACLs builds the complete ACL set for an organization's Mosquitto role.
//
// Who this applies to: the per-org credentials are handed to *remote edges*
// (internal/handlers/edge_config.go and edge_installer.go put them in the edge's
// CLOUD_MQTT_USER/PASS), which connect to the public broker.  The platform's own
// services (core-api, engine-historian, driver-manager, drivers) authenticate as
// the dynsec admin user (MQTT_USERNAME=${MQTT_ADMIN_USER}) and are unaffected by
// this role — so tightening it cannot break the single-node stack.
//
// Design: subscribePattern is granted at the same width the services actually
// subscribe with, and confinement is enforced by publishClientSend /
// publishClientReceive.  Mosquitto matches a subscription request against
// subscribePattern by *filter coverage*, not topic matching, so narrowing
// subscribePattern below the literal filter in the code would make Subscribe()
// fail outright — a silent, total data-flow outage.  Narrowing receive instead
// drops exactly the out-of-tenant messages and nothing else.
//
// Nothing here grants an unqualified `sys/#` or `spBv1.0/#` publish, because those
// let any tenant publish a DCMD / sys/command write into any other tenant's PLCs.
//
// siteNames is the list of the org's site names.  It is optional (variadic at the
// call sites) so existing callers keep compiling; pass it whenever it is known,
// because it is the only way to scope the Sparkplug namespace exactly — see below.
func orgRoleACLs(orgID int, orgName string, siteNames []string) []map[string]interface{} {
	a := newACLSet()
	org := fmt.Sprintf("%d", orgID)
	names := orgNameVariants(orgName)

	// --- Legacy tag data: data/{org}/{site}/{area}/{gateway}/{alias} ------------
	// Org-scoped by the first level.  Both spellings — see orgNameVariants.
	for _, n := range names {
		a.sendReceive("data/" + n + "/#")
	}
	// core-api subscribes with the literal filter "data/#"
	// (services/core-api/main.go).  Delivery is still confined to data/{org}/# by
	// the receive grants above.
	a.subscribe("data/#")

	// --- Sparkplug B -----------------------------------------------------------
	// Publish layout (internal/sparkplug/topic.go BuildTopic / BuildNBIRTHTopic):
	//   spBv1.0/{group}/{msgtype}/{edge_node}[/{device}]
	// with {group} = BuildGroupID(org, site) = "{org-slug}-{site-slug}" and
	// {edge_node} = BuildEdgeNodeID(area, gateway).  Org and site share ONE topic
	// level, and MQTT wildcards only match whole levels ("+" cannot prefix-match
	// "acme-*"), so the org's Sparkplug namespace can only be granted exactly by
	// naming each of its sites.
	//
	// A "{ns}/{group}/{msgtype}/#" filter also covers the 4-level node-level topics
	// (NBIRTH/NDEATH have no device level): "#" matches zero or more levels.
	groups := make([]string, 0, len(siteNames))
	for _, site := range siteNames {
		if strings.TrimSpace(site) == "" {
			continue
		}
		groups = append(groups, fmt.Sprintf("%s-%s", slugifyTopic(orgName), slugifyTopic(site)))
	}

	if len(groups) > 0 {
		for _, g := range groups {
			for _, mt := range sparkplugEdgeOriginated {
				a.sendReceive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, g, mt))
				// engine-historian's cloud forwarder re-emits with msgtype and group
				// SWAPPED (services/engine-historian/main.go: "%sspBv1.0/%s/%s/%s/%s"
				// with MessageType before GroupID).  Granting only the canonical order
				// would silently kill cloud sync, so the mirrored order is granted too
				// — send only, still no command types.
				a.send(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, mt, g))
			}
			for _, mt := range sparkplugCommands {
				// NCMD/DCMD are setpoint writes travelling core-api -> edge.
				// Receive only: an edge must never SEND a command.
				a.receive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, g, mt))
			}
		}
	} else {
		// RESIDUAL RISK (documented, deliberate): no caller currently has the org's
		// site list to hand (internal/handlers/organizations.go calls CreateOrgUser /
		// UpdateOrgRole without it), so the {group} level cannot be pinned and this
		// branch is what actually runs today.  Instead of the old `spBv1.0/#`
		// wildcard we split by message type:
		//   * send only on edge-originated types (birth/death/data), so a tenant can
		//     no longer publish DCMD/NCMD — the cross-tenant setpoint write, i.e. the
		//     safety event, is closed.
		//   * receive only on the command types the edge must consume, so a tenant can
		//     no longer read every other tenant's live DDATA.
		// What remains shared: a tenant can still publish *data* into another tenant's
		// group (spoofing / integrity, not control) and observe commands addressed to
		// another tenant's group (setpoint disclosure).  Both disappear as soon as
		// siteNames is supplied.
		for _, mt := range sparkplugEdgeOriginated {
			a.send(fmt.Sprintf("%s/+/%s/#", sparkplugNamespace, mt))
			a.send(fmt.Sprintf("%s/%s/#", sparkplugNamespace, mt)) // swapped cloud-forward order
		}
		for _, mt := range sparkplugCommands {
			a.receive(fmt.Sprintf("%s/+/%s/#", sparkplugNamespace, mt))
		}
	}
	// core-api subscribes with the literal filter "spBv1.0/#".
	a.subscribe(sparkplugNamespace + "/#")

	// --- sys/ topics that ARE keyed by org id — scope them exactly -------------
	// sys/write/{org_id}/{site}/{area}/{gateway}/{tag}  (external -> core-api write,
	//                                                    core-api resolves the org
	//                                                    from the topic, so pinning
	//                                                    this level is what stops a
	//                                                    cross-tenant tag write)
	// sys/edge/{org_id}/ping                            (edge manager heartbeat)
	// sys/alarms/{org_id}/{site}/{area}/{gateway}/{tag} (alarm events)
	a.sendReceive("sys/write/"+org+"/#", "sys/edge/"+org+"/#", "sys/alarms/"+org+"/#")
	// Literal filters used by core-api and engine-historian.
	a.subscribe("sys/write/#", "sys/edge/#", "sys/alarms/#")

	// sys/update/{org_id} and sys/restart/{org_id} are OTA commands published by
	// core-api towards the edge: receive-only, and only this org's own.
	a.receive("sys/update/"+org, "sys/restart/"+org)
	// services/driver-manager/main.go subscribes to the literal filters
	// "sys/update/#" and "sys/restart/#"; the subscription is allowed but only this
	// org's own messages are ever delivered.
	a.subscribe("sys/update/#", "sys/restart/#")

	// --- sys/ topics that are keyed by GATEWAY id, not org id ------------------
	// Gateway ids are globally unique integers and the topics carry no org
	// component, so they CANNOT be scoped per-org without changing the topic layout
	// (it would have to become sys/health/{org_id}/{gateway_id}).  Granted as
	// narrowly as the layout permits:
	//
	//   sys/health/{gateway_id} — read/write (LWT + retained status, published by
	//   every driver and by internal/handlers/gateways.go).
	//   RESIDUAL, SHARED ACROSS TENANTS: a tenant can publish or read health for a
	//   gateway id it does not own.  That is an integrity / liveness-disclosure
	//   issue, not a control action.
	a.sendReceive("sys/health/+")
	a.subscribe("sys/health/#") // core-api; drivers use "sys/health/+", also covered

	//   sys/command/reload/{gateway_id}, sys/command/write/{gateway_id},
	//   sys/command/settings-reload, sys/command/restore-complete and
	//   sys/lorawan/down/{gateway_id} are all published BY core-api towards a
	//   gateway.  Receive-only: withholding publishClientSend is what stops a tenant
	//   from issuing a write/reload to another tenant's gateway.
	//   RESIDUAL, SHARED ACROSS TENANTS: a tenant can still observe commands
	//   addressed to other tenants' gateways (gateway ids are not org-scoped).
	a.receive("sys/command/#", "sys/lorawan/down/+")
	a.subscribe("sys/command/#", "sys/lorawan/#")

	// --- cmd/ write-command topics ---------------------------------------------
	// cmd/{org}/{site}/{area}/{gateway}/{alias}  core-api/cloud -> driver (legacy)
	// cmd/write/{gateway_id}                     core-api -> driver (direct)
	// cmd/write/result/{gateway_id}              driver -> core-api (ack)
	// cmd/result/{org}/{site}/{area}/{gateway}/{alias}  driver -> core-api (legacy ack)
	// The two -> driver directions are receive-only for the same reason as DCMD:
	// they are setpoint writes.
	for _, n := range names {
		a.receive("cmd/" + n + "/#")
		a.sendReceive("cmd/result/" + n + "/#")
	}
	// Gateway-keyed, so org-scoping is impossible with the current layout — same
	// RESIDUAL as sys/command above.  "cmd/write/+" is exactly three levels and so
	// does not overlap the four-level "cmd/write/result/+".
	a.receive("cmd/write/+")
	a.sendReceive("cmd/write/result/+")
	// engine-historian's cloud bridge subscribes to the literal filter "cmd/#"
	// (with an optional operator-configured prefix, see below).
	a.subscribe("cmd/#")

	// --- NOT granted, on purpose ------------------------------------------------
	// engine-historian's cloud forwarder can prepend an operator-chosen prefix
	// (global_settings.cloud_mqtt_topic) and re-publishes tag data as
	// "{prefix}legacy/{org}/..." and alarms as "{prefix}alarms/{site}/..." — note
	// the alarms form has NO org level at all.  Neither topic has a subscriber
	// anywhere in OpenEdge (they exist for third-party cloud brokers), and granting
	// them would mean an unscopeable cross-tenant injection channel, so they are
	// left denied.  An operator bridging to a *third-party* broker with a custom
	// prefix must add those grants there, not to this role.
	return a.entries
}

// CreateOrgUser provisions an MQTT user for an organization.
// The user is confined to its own tenant namespace — see orgRoleACLs for the
// exact grant and for the residual risks that the current topic layout forces.
// siteNames is optional; supplying the org's sites pins the Sparkplug group level
// exactly instead of falling back to the narrowed per-message-type grant.
func (d *DynsecClient) CreateOrgUser(orgID int, orgName, username, password string, siteNames ...string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", orgID)

	return d.send([]map[string]interface{}{
		{
			"command":  "createRole",
			"rolename": roleName,
			"acls":     orgRoleACLs(orgID, orgName, siteNames),
		},
		{
			"command":  "createClient",
			"username": username,
			"password": password,
			"roles":    []map[string]interface{}{{"rolename": roleName}},
		},
	}, corrID)
}

// DeleteOrgUser removes an MQTT user and their dedicated role.
func (d *DynsecClient) DeleteOrgUser(orgID int, username string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", orgID)

	return d.send([]map[string]interface{}{
		{"command": "deleteClient", "username": username},
		{"command": "deleteRole", "rolename": roleName},
	}, corrID)
}

// UpdateOrgRole rebuilds the ACLs for an org's role (e.g. after a rename, or when
// the org's site list changes).  Passing the org's site names pins the Sparkplug
// group level exactly — see orgRoleACLs.
func (d *DynsecClient) UpdateOrgRole(orgID int, newOrgName string, siteNames ...string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", orgID)

	return d.send([]map[string]interface{}{
		{
			"command":  "modifyRole",
			"rolename": roleName,
			"acls":     orgRoleACLs(orgID, newOrgName, siteNames),
		},
	}, corrID)
}

// GeneratePassword returns a random 32-character hex password suitable for MQTT credentials.
func GeneratePassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newCorrID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── The identity a browser gets ──────────────────────────────────────────────

// uiViewerACLs is the grant for the web UI's own MQTT connection.
//
// Read-only by construction: subscribePattern and publishClientReceive, and not
// one publishClientSend anywhere in it. That is the whole point of it existing.
//
// The org role beside it is an EDGE identity — it may publish tag data, because
// a gateway has to. Handing that role to a browser would let anyone signed in,
// including a read-only user, publish values indistinguishable from a PLC's. So
// the UI gets its own role, and the only thing it can do is watch.
//
// Delivery is the boundary, not the subscription. The UI subscribes with the
// literal filters "spBv1.0/#" and "data/#" (useSparkplugListener,
// MqttMonitorPage), and Mosquitto still evaluates publishClientReceive for every
// message it is about to deliver — so the wide subscribePattern below grants
// nothing on its own, exactly as in orgRoleACLs.
func uiViewerACLs(orgID int, orgName string, siteNames []string) []map[string]interface{} {
	a := newACLSet()
	org := fmt.Sprintf("%d", orgID)

	// Legacy tag data, this org only. Both spellings — see orgNameVariants.
	for _, n := range orgNameVariants(orgName) {
		a.receive("data/" + n + "/#")
	}
	a.subscribe("data/#")

	// Sparkplug: edge-originated message types only. A viewer has no business
	// seeing NCMD/DCMD — those carry setpoint writes, and reading them discloses
	// what an operator is about to do to a machine.
	groups := make([]string, 0, len(siteNames))
	for _, site := range siteNames {
		if strings.TrimSpace(site) == "" {
			continue
		}
		groups = append(groups, fmt.Sprintf("%s-%s", slugifyTopic(orgName), slugifyTopic(site)))
	}
	if len(groups) > 0 {
		for _, g := range groups {
			for _, mt := range sparkplugEdgeOriginated {
				a.receive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, g, mt))
				// engine-historian re-emits with group and msgtype swapped.
				a.receive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, mt, g))
			}
		}
	} else {
		// Same residual as orgRoleACLs, and for the same reason: no caller has
		// the org's site list, so the {group} level cannot be pinned and a
		// viewer can observe other tenants' DDATA. Narrowed to edge-originated
		// types so at least the command traffic stays private. It closes the
		// moment siteNames is supplied.
		for _, mt := range sparkplugEdgeOriginated {
			a.receive(fmt.Sprintf("%s/+/%s/#", sparkplugNamespace, mt))
			a.receive(fmt.Sprintf("%s/%s/#", sparkplugNamespace, mt))
		}
	}
	a.subscribe(sparkplugNamespace + "/#")

	// Alarms are keyed by org id, so this one is exact.
	a.receive("sys/alarms/" + org + "/#")
	a.subscribe("sys/alarms/#")

	return a.entries
}

// EnsureOrgViewer provisions — or repairs — the read-only MQTT identity the web
// UI connects with, and is safe to call repeatedly.
//
// Idempotent on purpose, because it has two callers with different histories:
// organization creation, where nothing exists yet, and the credentials endpoint,
// which has to serve organizations created before this identity did. createRole
// and createClient are no-ops when the object is already there ("already
// exists" is treated as success in send), modifyRole brings stale ACLs up to
// date, and setClientPassword makes the stored password the true one rather
// than hoping they never diverged.
func (d *DynsecClient) EnsureOrgViewer(orgID int, orgName, username, password string, siteNames ...string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	roleName := fmt.Sprintf("org-%d-ui-role", orgID)
	acls := uiViewerACLs(orgID, orgName, siteNames)

	return d.send([]map[string]interface{}{
		{"command": "createRole", "rolename": roleName, "acls": acls},
		{"command": "modifyRole", "rolename": roleName, "acls": acls},
		{
			"command":  "createClient",
			"username": username,
			"password": password,
			"roles":    []map[string]interface{}{{"rolename": roleName}},
		},
		{"command": "setClientPassword", "username": username, "password": password},
	}, newCorrID())
}

// UpdateOrgViewerRole rebuilds the UI role's ACLs — after a rename, or when the
// org's site list changes.
//
// Deliberately touches the ROLE and not the client: rotating the password here
// would sign out every browser currently connected, and the reason to call this
// is that the grant should get NARROWER, not that the secret went stale.
func (d *DynsecClient) UpdateOrgViewerRole(orgID int, orgName string, siteNames ...string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	return d.send([]map[string]interface{}{
		{
			"command":  "modifyRole",
			"rolename": fmt.Sprintf("org-%d-ui-role", orgID),
			"acls":     uiViewerACLs(orgID, orgName, siteNames),
		},
	}, newCorrID())
}

// DeleteOrgViewer removes the UI identity and its role.
func (d *DynsecClient) DeleteOrgViewer(orgID int, username string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	return d.send([]map[string]interface{}{
		{"command": "deleteClient", "username": username},
		{"command": "deleteRole", "rolename": fmt.Sprintf("org-%d-ui-role", orgID)},
	}, newCorrID())
}

// OrgViewerUsername is the MQTT username the web UI signs in with.
func OrgViewerUsername(orgID int) string { return fmt.Sprintf("org-%d-ui", orgID) }
