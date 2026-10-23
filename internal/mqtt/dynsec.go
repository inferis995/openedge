package mqtt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"log"

	"github.com/ralph/industrial-edge-middleware/internal/logging"
	"github.com/ralph/industrial-edge-middleware/internal/naming"
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

// dynsecSubscriber is the reply path: the control response topic the broker
// answers on. Kept separate from dynsecPublisher so unit tests can keep
// faking the publish side with a stub.
type dynsecSubscriber interface {
	Subscribe(topic string, handler MessageHandler) error
}

type DynsecClient struct {
	client dynsecPublisher
	sub    dynsecSubscriber

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

type dynsecEntry struct {
	Command         string `json:"command"`
	CorrelationData string `json:"correlationData,omitempty"`
	Error           string `json:"error,omitempty"`
	Data            struct {
		Clients []struct {
			Username string `json:"username"`
			Disabled bool   `json:"disabled"`
			Roles    []struct {
				Rolename string `json:"rolename"`
			} `json:"roles"`
		} `json:"clients"`
	} `json:"data,omitempty"`
}

type dynsecResponse struct {
	Responses []dynsecEntry `json:"responses"`
}

// NewDynsecClient wraps an existing connected MQTT client with dynamic-security
// command support.  It subscribes to the response topic immediately.
func NewDynsecClient(c *Client) *DynsecClient {
	d := &DynsecClient{client: c, sub: c}

	if err := d.ensureSubscribed(); err != nil {
		log.Printf("[MQTT] dynsec response subscription failed at startup: %v (retried before every exchange)", err)
	}
	return d
}

// onResponse delivers a broker control reply to the waiting exchange, if any.
func (d *DynsecClient) onResponse(_ string, payload []byte) {
		var resp dynsecResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			logging.Debugf("[MQTT] dynsec reply not decoded (%v): %s", err, string(payload))
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
}

// ensureSubscribed (re)subscribes the control response topic. The subscription
// has been observed missing on long-lived clients — a failed startup subscribe
// or a reconnect that dropped it — which turns every exchange into a timeout
// while the broker itself answers in milliseconds. Re-subscribing is idempotent,
// so every exchange starts by guaranteeing the reply path is live.
func (d *DynsecClient) ensureSubscribed() error {
	if d.sub == nil {
		return nil // unit-test doubles publish without a subscription side
	}
	return d.sub.Subscribe("$CONTROL/dynamic-security/v1/response", d.onResponse)
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

	if err := d.ensureSubscribed(); err != nil {
		return fmt.Errorf("dynsec response subscription: %w", err)
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

// slugifyTopic reduces a name to the canonical topic form. The reduction itself
// lives in internal/naming so the drivers, the ACL builder and the
// historian's SQL cannot drift apart; see the package comment there.
func slugifyTopic(s string) string {
	return naming.Slug(s)
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
		"acltype": aclType, "topic": topic, "priority": grantPriority, "allow": true,
	})
}

// Priorities. Mosquitto checks a role's ACLs from the highest priority down and
// the first one that matches decides; only when none matches does the broker's
// default apply.
const (
	grantPriority = 10
	denyPriority  = 0
)

// denyEverythingElse closes the role: anything not granted above is refused
// by the role itself, whatever the broker's default says.
//
// Confinement used to rest on that default. The roles subscribe wide (the
// services use literal filters like "data/#") and grant receive only on the
// tenant's own topics, which isolates tenants only if receiving is denied by
// default — and the dynamic-security plugin ships with it ALLOWED. The bundled
// broker's entrypoint flips it, but an external broker, a restored
// dynamic-security.json or a hand-made one does not, and with the stock default
// every tenant's login receives every other tenant's data. This makes the role
// correct on any broker.
func (a *aclSet) denyEverythingElse() {
	for _, t := range []string{"publishClientSend", "publishClientReceive", "subscribePattern"} {
		a.entries = append(a.entries, map[string]interface{}{
			"acltype": t, "topic": "#", "priority": denyPriority, "allow": false,
		})
	}
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

// TenantScope is everything an organization's broker roles are built from.
//
// Every grant is derived from it, and nothing is granted by wildcard across
// tenants: a level that names the organization is pinned to this one, and a
// topic keyed by gateway id is granted only for this organization's gateways.
type TenantScope struct {
	OrgID      int
	OrgName    string
	SiteNames  []string
	GatewayIDs []int
}

// TopicSafe reports whether a name can stand as one topic level: not empty,
// no MQTT wildcard, no level separator, no NUL, not a $-topic. A name that is
// not would, put into a grant verbatim, cover other tenants' topics ("+" is
// every organization, "a/b" is organization a's site b).
func TopicSafe(name string) bool {
	return name != "" && !strings.ContainsAny(name, "+#/\x00") && !strings.HasPrefix(name, "$")
}

// nameVariants returns the distinct spellings of the org name that appear in
// topics at runtime, those safe to grant. Both occur: the drivers interpolate
// the configured org name verbatim (services/driver-*/main.go,
// `data/%s/%s/...` with cfg.OrgName), while internal/sparkplug.BuildLegacyTopic
// slugifies every level. internal/handlers (tags.go, gateways.go) subscribes
// to *both* spellings for exactly this reason, so the ACL must grant both or
// half the legacy tag traffic is silently dropped.
func (s TenantScope) nameVariants() []string {
	var out []string
	for _, n := range []string{s.OrgName, slugifyTopic(s.OrgName)} {
		if TopicSafe(n) && (len(out) == 0 || out[0] != n) {
			out = append(out, n)
		}
	}
	return out
}

// SparkplugGroups are the organization's Sparkplug group ids, one per site:
// "{org-slug}-{site-slug}" (internal/sparkplug BuildGroupID).
//
// Org and site share ONE topic level, and MQTT wildcards only match whole
// levels ("+" cannot prefix-match "acme-*"), so the org's Sparkplug namespace
// can only be granted by naming each of its sites. With no sites there is no
// group, and nothing Sparkplug is granted.
func (s TenantScope) SparkplugGroups() []string {
	org := slugifyTopic(s.OrgName)
	out := make([]string, 0, len(s.SiteNames))
	seen := map[string]bool{}
	for _, site := range s.SiteNames {
		site = slugifyTopic(site)
		g := org + "-" + site
		if site == "" || !TopicSafe(org) || !TopicSafe(g) || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
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
// subscribe with (Mosquitto matches a subscription request against
// subscribePattern by *filter coverage*, so narrowing it below the literal
// filter in the code makes Subscribe() fail outright). What a client is
// DELIVERED is decided by publishClientReceive, granted only on this tenant's
// topics, and the role ends with an explicit deny of everything else — see
// denyEverythingElse. A wide subscription therefore delivers nothing of another
// tenant's.
func orgRoleACLs(s TenantScope) []map[string]interface{} {
	a := newACLSet()
	org := fmt.Sprintf("%d", s.OrgID)

	// --- Legacy tag data: data/{org}/{site}/{area}/{gateway}/{alias} ------------
	for _, n := range s.nameVariants() {
		a.sendReceive("data/" + n + "/#")
	}
	// core-api subscribes with the literal filter "data/#".
	a.subscribe("data/#")

	// --- Sparkplug B -----------------------------------------------------------
	// spBv1.0/{group}/{msgtype}/{edge_node}[/{device}]; a "{ns}/{group}/{msgtype}/#"
	// filter also covers the node-level topics ("#" matches zero levels).
	for _, g := range s.SparkplugGroups() {
		for _, mt := range sparkplugEdgeOriginated {
			a.sendReceive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, g, mt))
			// engine-historian's cloud forwarder re-emits with msgtype and group
			// SWAPPED (services/engine-historian/main.go: "%sspBv1.0/%s/%s/%s/%s"
			// with MessageType before GroupID). Send only, still no command types.
			a.send(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, mt, g))
		}
		for _, mt := range sparkplugCommands {
			// NCMD/DCMD are setpoint writes going core-api -> edge.
			// Receive only: an edge must never SEND a command.
			a.receive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, g, mt))
		}
	}
	// core-api subscribes with the literal filter "spBv1.0/#".
	a.subscribe(sparkplugNamespace + "/#")

	// --- sys/ topics keyed by org id ---------------------------------------------
	// sys/write/{org_id}/...  (external -> core-api write: pinning this level is
	//                          what stops a cross-tenant tag write)
	// sys/edge/{org_id}/ping  (edge manager heartbeat)
	// sys/alarms/{org_id}/... (alarm events)
	a.sendReceive("sys/write/"+org+"/#", "sys/edge/"+org+"/#", "sys/alarms/"+org+"/#")
	a.subscribe("sys/write/#", "sys/edge/#", "sys/alarms/#")

	// OTA commands core-api sends to this organization, or to one of its boxes.
	a.receive("sys/update/"+org, "sys/restart/"+org,
		"sys/update/"+org+"/+", "sys/restart/"+org+"/+")
	a.subscribe("sys/update/#", "sys/restart/#")

	// --- Gateway health ------------------------------------------------------------
	// sys/health/{org_id}/{gateway_id} carries the organization; the old shape
	// sys/health/{gateway_id} does not, so it is granted per gateway of this
	// organization, never as "sys/health/+".
	a.sendReceive("sys/health/" + org + "/+")
	for _, id := range s.GatewayIDs {
		a.sendReceive(fmt.Sprintf("sys/health/%d", id))
	}
	a.subscribe("sys/health/#")

	// --- Commands to this organization's gateways (receive only) -------------------
	// Published BY core-api towards a gateway. Withholding send is what stops a
	// tenant from issuing a write or a reload; granting receive only for this
	// organization's gateway ids is what stops it from reading another tenant's.
	for _, id := range s.GatewayIDs {
		a.receive(
			fmt.Sprintf("sys/command/reload/%d", id),
			fmt.Sprintf("sys/command/write/%d", id),
			fmt.Sprintf("sys/command/settings-reload/%d", id),
			fmt.Sprintf("sys/lorawan/down/%d", id),
			fmt.Sprintf("cmd/write/%d", id),
		)
		a.sendReceive(fmt.Sprintf("cmd/write/result/%d", id))
	}
	// Broadcasts with no tenant data in them (a reload or restore notice).
	a.receive("sys/command/reload", "sys/command/settings-reload", "sys/command/restore-complete")
	a.subscribe("sys/command/#", "sys/lorawan/#")

	// --- cmd/ legacy write-command topics, keyed by org name --------------------
	// cmd/{org}/{site}/{area}/{gateway}/{alias}         core-api -> driver: receive
	// cmd/result/{org}/{site}/{area}/{gateway}/{alias}  driver -> core-api (ack)
	for _, n := range s.nameVariants() {
		a.receive("cmd/" + n + "/#")
		a.sendReceive("cmd/result/" + n + "/#")
	}
	// engine-historian's cloud bridge subscribes to the literal filter "cmd/#".
	a.subscribe("cmd/#")

	// --- NOT granted, on purpose ------------------------------------------------
	// engine-historian's cloud forwarder can prepend an operator-chosen prefix
	// (global_settings.cloud_mqtt_topic) and re-publishes tag data as
	// "{prefix}legacy/{org}/..." and alarms as "{prefix}alarms/{site}/..." — note
	// the alarms form has NO org level at all. Neither topic has a subscriber
	// anywhere in OpenEdge (they exist for third-party cloud brokers), and granting
	// them would mean an unscopeable cross-tenant injection channel, so they are
	// left denied.
	a.denyEverythingElse()
	return a.entries
}

// CreateOrgUser provisions an MQTT user for an organization, confined to its
// own tenant namespace — see orgRoleACLs.
func (d *DynsecClient) CreateOrgUser(scope TenantScope, username, password string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", scope.OrgID)

	return d.send([]map[string]interface{}{
		{
			"command":  "createRole",
			"rolename": roleName,
			"acls":     orgRoleACLs(scope),
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

// UpdateOrgRole rebuilds the ACLs for an org's role: after a rename, or when
// its sites or gateways change.
func (d *DynsecClient) UpdateOrgRole(scope TenantScope) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", scope.OrgID)

	return d.send([]map[string]interface{}{
		{
			"command":  "modifyRole",
			"rolename": roleName,
			"acls":     orgRoleACLs(scope),
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
func uiViewerACLs(s TenantScope) []map[string]interface{} {
	a := newACLSet()
	org := fmt.Sprintf("%d", s.OrgID)

	// Legacy tag data, this org only. Both spellings — see nameVariants.
	for _, n := range s.nameVariants() {
		a.receive("data/" + n + "/#")
	}
	a.subscribe("data/#")

	// Sparkplug: edge-originated message types of this organization's groups
	// only. A viewer has no business seeing NCMD/DCMD — those carry setpoint
	// writes, and reading them discloses what an operator is about to do to a
	// machine. No sites, no groups, nothing granted.
	for _, g := range s.SparkplugGroups() {
		for _, mt := range sparkplugEdgeOriginated {
			a.receive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, g, mt))
			// engine-historian re-emits with group and msgtype swapped.
			a.receive(fmt.Sprintf("%s/%s/%s/#", sparkplugNamespace, mt, g))
		}
	}
	a.subscribe(sparkplugNamespace + "/#")

	// Alarms are keyed by org id, so this one is exact.
	a.receive("sys/alarms/" + org + "/#")
	a.subscribe("sys/alarms/#")

	a.denyEverythingElse()
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
func (d *DynsecClient) EnsureOrgViewer(scope TenantScope, username, password string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	roleName := fmt.Sprintf("org-%d-ui-role", scope.OrgID)
	acls := uiViewerACLs(scope)

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
func (d *DynsecClient) UpdateOrgViewerRole(scope TenantScope) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	return d.send([]map[string]interface{}{
		{
			"command":  "modifyRole",
			"rolename": fmt.Sprintf("org-%d-ui-role", scope.OrgID),
			"acls":     uiViewerACLs(scope),
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

// ── Identities for external systems ──────────────────────────────────────────

// CreateExternalReader gives a SCADA, a Node-RED flow or a MES its own MQTT
// login to read an organization's data.
//
// It carries the same read-only role as the web UI's identity (uiViewerACLs):
// tag data, edge-originated Sparkplug and alarms of this organization, and not
// one publish. Sharing the role means a site added later widens both at once,
// through UpdateOrgViewerRole. Each system gets its own client, so one can be
// revoked without touching the others or the browsers.
func (d *DynsecClient) CreateExternalReader(scope TenantScope, username, password string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	roleName := fmt.Sprintf("org-%d-ui-role", scope.OrgID)
	acls := uiViewerACLs(scope)
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

// DeleteExternalReader revokes one external system's login. The role stays:
// the web UI and the other systems still use it.
func (d *DynsecClient) DeleteExternalReader(username string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()
	return d.send([]map[string]interface{}{
		{"command": "deleteClient", "username": username},
	}, newCorrID())
}

// SetExternalPassword rotates one external system's password without
// touching its roles: rotation never widens or narrows access.
func (d *DynsecClient) SetExternalPassword(username, password string) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()
	return d.send([]map[string]interface{}{
		{"command": "setClientPassword", "username": username, "password": password},
	}, newCorrID())
}

// SetClientEnabled flips a client without deleting it: disabling keeps the
// identity (and an audit trail of it) while cutting access immediately.
func (d *DynsecClient) SetClientEnabled(username string, enabled bool) error {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()
	cmd := "disableClient"
	if enabled {
		cmd = "enableClient"
	}
	return d.send([]map[string]interface{}{
		{"command": cmd, "username": username},
	}, newCorrID())
}

// BrokerClient is one identity the broker knows about, for the maintenance
// panel: what exists, not what we think exists.
type BrokerClient struct {
	Username string `json:"username"`
	Disabled bool   `json:"disabled"`
	Roles    []string `json:"roles"`
}

// ListBrokerClients returns every client known to the broker (drivers, UI
// viewers, external systems). Read-only: the panel shows it so the admin can
// reconcile the broker against the registry, never edits it from here.
func (d *DynsecClient) ListBrokerClients() ([]BrokerClient, error) {
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	ch := make(chan dynsecResponse, 1)
	corr := newCorrID()
	d.mu.Lock()
	d.responseCh, d.correlationID = ch, corr
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.responseCh, d.correlationID = nil, ""
		d.mu.Unlock()
	}()

	data, err := json.Marshal(dynsecCmd{Commands: []map[string]interface{}{
		{"command": "listClients", "verbose": true, "correlationData": corr},
	}})
	if err != nil {
		return nil, err
	}
	if err := d.ensureSubscribed(); err != nil {
		return nil, fmt.Errorf("dynsec response subscription: %w", err)
	}
	if err := d.client.Publish("$CONTROL/dynamic-security/v1", data); err != nil {
		return nil, fmt.Errorf("dynsec publish: %w", err)
	}
	select {
	case resp := <-ch:
		for _, r := range resp.Responses {
			if r.Error != "" {
				return nil, fmt.Errorf("dynsec %s: %s", r.Command, r.Error)
			}
		}
		var out []BrokerClient
		for _, r := range resp.Responses {
			if len(r.Data.Clients) == 0 {
				continue
			}
			for _, c := range r.Data.Clients {
				bc := BrokerClient{Username: c.Username, Disabled: c.Disabled}
				for _, rl := range c.Roles {
					bc.Roles = append(bc.Roles, rl.Rolename)
				}
				out = append(out, bc)
			}
		}
		return out, nil
	case <-time.After(dynsecTimeout):
		return nil, fmt.Errorf("dynsec listClients timed out")
	}
}
