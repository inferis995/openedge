//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// These tests follow a value across every process in the stack: driver topic ->
// broker -> historian/core-api -> Redis and Postgres -> API. That crossing is
// where the expensive defects lived — a tag alias containing a hyphen was
// silently never historised, a Sparkplug metric from a spec-compliant device was
// stored as NULL, and one tenant's readings could be written under another
// tenant's tag id. None of it is visible from inside a single package.

// ── MQTT helper ─────────────────────────────────────────────────────────────

func mqttConnect(t *testing.T, clientID string) paho.Client {
	t.Helper()

	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", mqttHost(), mqttPort())).
		SetClientID(clientID).
		SetConnectTimeout(10 * time.Second).
		SetCleanSession(true)

	// The shipped broker sets allow_anonymous false, so credentials are needed.
	if u := env("E2E_MQTT_USER", ""); u != "" {
		opts.SetUsername(u).SetPassword(env("E2E_MQTT_PASSWORD", ""))
	}

	c := paho.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(15 * time.Second) {
		t.Fatalf("MQTT connect to %s:%s timed out", mqttHost(), mqttPort())
	}
	if err := tok.Error(); err != nil {
		t.Fatalf("MQTT connect to %s:%s: %v (set E2E_MQTT_USER/E2E_MQTT_PASSWORD if the broker requires auth)",
			mqttHost(), mqttPort(), err)
	}
	t.Cleanup(func() { c.Disconnect(250) })
	return c
}

func publish(t *testing.T, c paho.Client, topic string, payload interface{}) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal MQTT payload: %v", err)
	}
	tok := c.Publish(topic, 1, false, body)
	if !tok.WaitTimeout(10 * time.Second) {
		t.Fatalf("publish to %s timed out", topic)
	}
	if err := tok.Error(); err != nil {
		t.Fatalf("publish to %s: %v", topic, err)
	}
}

// ── Fixture ─────────────────────────────────────────────────────────────────

type fixture struct {
	org      orgRef
	siteID   int
	areaID   int
	gwID     int
	tagID    int
	tagAlias string
	// dataTopic is the legacy topic a driver publishes this tag on.
	dataTopic string
}

func createEntity(t *testing.T, c *apiClient, path string, body interface{}) int {
	t.Helper()
	raw := c.mustDo(http.MethodPost, path, body, http.StatusCreated)
	var out struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode POST %s response: %v — body: %s", path, err, truncate(raw))
	}
	if out.ID == 0 {
		t.Fatalf("POST %s returned id 0 — body: %s", path, truncate(raw))
	}
	return out.ID
}

// newFixture builds a full hierarchy: org -> site -> area -> gateway -> tag.
//
// The alias deliberately contains a HYPHEN. That is not cosmetic: the historian
// used to rewrite the topic alias's hyphens to underscores while every SQL
// comparison produced the hyphenated form, so exactly these tags were silently
// never historised.
func newFixture(t *testing.T, admin *apiClient) fixture {
	t.Helper()
	suffix := uniqueSuffix()

	org := createOrg(t, admin, "e2e-flow-"+suffix)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", org.ID)}

	siteID := createEntity(t, orgScoped, "/api/sites", map[string]interface{}{
		"name": "site-" + suffix, "org_id": org.ID,
	})
	areaID := createEntity(t, orgScoped, "/api/areas", map[string]interface{}{
		"name": "area-" + suffix, "site_id": siteID,
	})
	gwID := createEntity(t, orgScoped, "/api/gateways", map[string]interface{}{
		"name":              "gw-" + suffix,
		"area_id":           areaID,
		"driver_type":       "MODBUS_TCP",
		"connection_config": map[string]interface{}{"ip": "127.0.0.1", "port": 502},
		"scan_rate_ms":      1000,
	})

	alias := "motor-speed-" + suffix
	tagID := createEntity(t, orgScoped, "/api/tags", map[string]interface{}{
		"gateway_id": gwID,
		"code":       "40001",
		"alias":      alias,
		"data_type":  "REAL",
		"historize":  true,
	})

	return fixture{
		org: org, siteID: siteID, areaID: areaID, gwID: gwID,
		tagID: tagID, tagAlias: alias,
		dataTopic: fmt.Sprintf("data/%s/%s/%s/%s/%s",
			slug("e2e-flow-"+suffix), slug("site-"+suffix),
			slug("area-"+suffix), slug("gw-"+suffix), slug(alias)),
	}
}

// slug mirrors sparkplug.slugify, which the drivers use to build topics.
func slug(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r == ' ' || r == '_':
			out = append(out, '-')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// ── Tests ───────────────────────────────────────────────────────────────────

// TestPublishedValueReachesTheAPI is the ingest contract: a driver publishes,
// and the value must become readable through the API. It uses a hyphenated
// alias on purpose (see newFixture).
func TestPublishedValueReachesTheAPI(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)

	mq := mqttConnect(t, "e2e-publisher-"+uniqueSuffix())
	want := 42.5
	publish(t, mq, fx.dataTopic, map[string]interface{}{
		"tag_id": fx.tagID,
		"org_id": fx.org.ID,
		"v":      want,
		"ts":     time.Now().UnixMilli(),
		"q":      0,
	})

	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	// The value travels through the broker and another process, so poll rather
	// than sleeping a fixed amount.
	deadline := time.Now().Add(20 * time.Second)
	for {
		status, raw := orgScoped.do(http.MethodGet, fmt.Sprintf("/api/tags/%d/current", fx.tagID), nil)
		if status == http.StatusOK {
			var cur struct {
				V interface{} `json:"v"`
				Q int         `json:"q"`
			}
			if err := json.Unmarshal(raw, &cur); err == nil && cur.V != nil {
				if got, ok := cur.V.(float64); ok && got == want {
					return // ingest works
				}
				t.Fatalf("current value = %v (quality %d), want %v", cur.V, cur.Q, want)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("published value never became readable at /api/tags/%d/current (topic %s) — the ingest path is broken",
				fx.tagID, fx.dataTopic)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestPublishedValueIsHistorised covers the OTHER consumer of the same message.
//
// TestPublishedValueReachesTheAPI proves core-api received it and cached it in
// Redis — and that passed while engine-historian, a separate process, was being
// refused by the broker on every single connection attempt. It never read
// MQTT_USERNAME/MQTT_PASSWORD, which the compose file had been passing it all
// along, so on any authenticated deployment nothing was ever written to
// tag_history: the historian's entire job, silently not happening, with a green
// stack and a green test suite.
//
// Asserting the live value is therefore not enough — the persisted series has
// to be asserted separately, because a different process owns it.
func TestPublishedValueIsHistorised(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin) // historize: true
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	mq := mqttConnect(t, "e2e-historian-"+uniqueSuffix())
	want := 73.25
	publish(t, mq, fx.dataTopic, map[string]interface{}{
		"tag_id": fx.tagID,
		"org_id": fx.org.ID,
		"v":      want,
		"ts":     time.Now().UnixMilli(),
		"q":      0,
	})

	// raw=true bypasses the continuous aggregates, which only materialise on
	// their own schedule and would make a fresh point look missing.
	query := fmt.Sprintf("/api/history?tag_id=%d&start=%s&end=%s&raw=true",
		fx.tagID,
		time.Now().Add(-1*time.Hour).UTC().Format(time.RFC3339),
		time.Now().Add(1*time.Hour).UTC().Format(time.RFC3339))

	deadline := time.Now().Add(30 * time.Second)
	for {
		status, raw := orgScoped.do(http.MethodGet, query, nil)
		if status == http.StatusOK {
			var resp struct {
				Data []struct {
					Value *float64 `json:"value"`
				} `json:"data"`
			}
			if err := json.Unmarshal(raw, &resp); err == nil {
				for _, p := range resp.Data {
					if p.Value != nil && *p.Value == want {
						return // the historian is alive and writing
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("value %v published on %s never reached tag_history — "+
				"engine-historian is not consuming (check whether the broker is refusing it)",
				want, fx.dataTopic)
		}
		time.Sleep(1 * time.Second)
	}
}

// TestTagCurrentValueIsOrgScoped: this endpoint only checked that the tag id
// EXISTED, so any authenticated user could read every tenant's live process
// values by iterating ids.
func TestTagCurrentValueIsOrgScoped(t *testing.T) {
	admin, _ := adminSession(t)

	victim := newFixture(t, admin)

	suffix := uniqueSuffix()
	attackerOrg := createOrg(t, admin, "e2e-peek-"+suffix)
	attacker := createOrgAdmin(t, admin, attackerOrg.ID, "e2e-peek-"+suffix, "e2e-Password-"+suffix)

	status, body := attacker.do(http.MethodGet, fmt.Sprintf("/api/tags/%d/current", victim.tagID), nil)
	if status < 400 {
		t.Fatalf("an admin of another org read tag %d's live value (status %d): %s",
			victim.tagID, status, truncate(body))
	}

	status, body = attacker.do(http.MethodGet, fmt.Sprintf("/api/tags/%d/shadow", victim.tagID), nil)
	if status < 400 {
		t.Fatalf("an admin of another org read tag %d's shadow (status %d): %s",
			victim.tagID, status, truncate(body))
	}
}

// TestAlarmEventIsConsumedAndBecomesVisible covers the half of the alarm chain
// that lives between processes. Alarms were evaluated, persisted and had a fully
// implemented notification dispatcher — and no notification was ever sent,
// because NOTHING published to sys/alarms, the only topic that reaches the
// dispatcher. Both halves passed their unit tests.
//
// Evaluation itself runs inside the DRIVER processes (internal/alarms.Manager,
// used by driver-modbus, -s7, -opcua, -redis, …): each driver compares the
// values it polls from the field and publishes the event. Publishing a value on
// a data/ topic therefore does NOT produce an alarm — no driver ever saw it —
// so this test plays the driver's part and asserts the rest of the chain:
// broker -> core-api's sys/alarms subscription -> Postgres -> the API an
// operator's dashboard reads. That is precisely the join that was missing, and
// it is what a "CRITICAL: failed to subscribe to alarms topic" at startup, or a
// silently-denied subscribe, would break.
func TestAlarmEventIsConsumedAndBecomesVisible(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	// A high alarm above 100. The endpoint REPLACES the tag's whole alarm
	// configuration, so the body is an array; alarm_type is lowercase ("high"),
	// matching isConditionViolated.
	orgScoped.mustDo(http.MethodPut, fmt.Sprintf("/api/tags/%d/alarms", fx.tagID),
		[]map[string]interface{}{{
			"tag_id":        fx.tagID,
			"alarm_type":    "high",
			"threshold":     100.0,
			"severity":      "critical",
			"message":       "e2e high alarm",
			"deadband":      1.0,
			"delay_seconds": 0,
			"enabled":       true,
		}}, http.StatusOK)

	// The definition id is what correlates ACTIVE with CLEARED, so read it back
	// rather than guessing.
	raw := orgScoped.mustDo(http.MethodGet, fmt.Sprintf("/api/tags/%d/alarms", fx.tagID), nil, http.StatusOK)
	var defs []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(raw, &defs); err != nil || len(defs) == 0 {
		t.Fatalf("could not read back the alarm definition: %v — body: %s", err, truncate(raw))
	}
	defID := defs[0].ID

	// Publish exactly what a driver publishes. event_id 0 means "nothing has
	// persisted this yet", which is the branch core-api must handle.
	alarmTopic := fmt.Sprintf("sys/alarms/%d/%s/%s/%s/%s",
		fx.org.ID, slug("site"), slug("area"), slug("gw"), slug(fx.tagAlias))

	pub := mqttConnect(t, "e2e-alarm-publisher-"+uniqueSuffix())
	publish(t, pub, alarmTopic, map[string]interface{}{
		"event_id":         0,
		"tag_id":           fx.tagID,
		"definition_id":    defID,
		"status":           "ACTIVE",
		"alarm_type":       "high",
		"severity":         "critical",
		"message":          "e2e high alarm",
		"value_at_trigger": 150.0,
		"threshold":        100.0,
		"tag_alias":        fx.tagAlias,
		"timestamp":        time.Now().UnixMilli(),
	})

	// It crosses the broker and another process, so poll.
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, body := orgScoped.do(http.MethodGet, "/api/alarms/active", nil)
		if status == http.StatusOK {
			var active []struct {
				TagID        int    `json:"tag_id"`
				DefinitionID int    `json:"definition_id"`
				Status       string `json:"status"`
			}
			if err := json.Unmarshal(body, &active); err == nil {
				for _, a := range active {
					if a.TagID == fx.tagID && a.DefinitionID == defID {
						return // the event survived the whole chain
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("an alarm published on %s never appeared at /api/alarms/active — "+
				"the alarm-to-operator path is not wired, so nobody would ever be paged",
				alarmTopic)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
