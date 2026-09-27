//go:build e2e

package e2e

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// An organization gives a SCADA or a Node-RED flow its own MQTT login: it
// reads that organization's data, not another's, and stops working when it is
// revoked.
func TestAnExternalSystemGetsARevocableReadOnlyMQTTLogin(t *testing.T) {
	admin, _ := adminSession(t)
	suffix := uniqueSuffix()
	mine := createOrg(t, admin, "mqttext-"+suffix)
	other := createOrg(t, admin, "mqttother-"+suffix)
	orgAdmin := createOrgAdmin(t, admin, mine.ID, "mqttext-"+suffix, "e2e-Password-"+suffix)

	status, body := orgAdmin.do(http.MethodPost, fmt.Sprintf("/api/organizations/%d/mqtt-clients", mine.ID),
		map[string]string{"description": "Node-RED linea 1"})
	if status != http.StatusCreated {
		t.Fatalf("creating the login returned %d: %s", status, truncate(body))
	}
	var created struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Password == "" {
		t.Fatalf("no password in the response: %s", truncate(body))
	}

	// The list never carries the password.
	_, list := orgAdmin.do(http.MethodGet, fmt.Sprintf("/api/organizations/%d/mqtt-clients", mine.ID), nil)
	if strings.Contains(string(list), created.Password) {
		t.Error("the list of logins exposes the password")
	}

	// It cannot manage another organization's logins.
	if status, _ := orgAdmin.do(http.MethodGet, fmt.Sprintf("/api/organizations/%d/mqtt-clients", other.ID), nil); status != http.StatusForbidden {
		t.Errorf("an organization's admin listed another's MQTT logins: %d", status)
	}

	ext, err := connectAs(created.Username, created.Password)
	if err != nil {
		t.Fatalf("the new login cannot connect: %v", err)
	}
	got := make(chan string, 10)
	if tok := ext.Subscribe("data/#", 1, func(_ paho.Client, m paho.Message) { got <- m.Topic() }); !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		t.Fatalf("subscribing: %v", tok.Error())
	}

	platform := mqttConnect(t, "e2e-ext-pub-"+suffix)
	mineTopic := fmt.Sprintf("data/%s/site/area/gw/tag", strings.ToLower(mine.Name))
	otherTopic := fmt.Sprintf("data/%s/site/area/gw/tag", strings.ToLower(other.Name))
	publish(t, platform, otherTopic, map[string]interface{}{"v": 1})
	publish(t, platform, mineTopic, map[string]interface{}{"v": 2})

	deadline := time.After(10 * time.Second)
	sawMine := false
	for !sawMine {
		select {
		case topic := <-got:
			if topic == otherTopic {
				t.Fatalf("an external login of org %d received org %d's data", mine.ID, other.ID)
			}
			sawMine = topic == mineTopic
		case <-deadline:
			t.Fatal("the external login did not receive its own organization's data")
		}
	}
	// A little longer, in case the other organization's message is behind it.
	select {
	case topic := <-got:
		if topic == otherTopic {
			t.Fatalf("an external login of org %d received org %d's data", mine.ID, other.ID)
		}
	case <-time.After(time.Second):
	}
	ext.Disconnect(100)

	// Revoked: it can no longer sign in.
	if status, body := orgAdmin.do(http.MethodDelete,
		fmt.Sprintf("/api/organizations/%d/mqtt-clients/%d", mine.ID, created.ID), nil); status != http.StatusNoContent {
		t.Fatalf("revoking returned %d: %s", status, truncate(body))
	}
	if c, err := connectAs(created.Username, created.Password); err == nil {
		c.Disconnect(100)
		t.Fatal("a revoked login still connects")
	}
}

func connectAs(username, password string) (paho.Client, error) {
	scheme := "tcp"
	if env("E2E_MQTT_TLS", "") == "1" {
		scheme = "ssl"
	}
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("%s://%s:%s", scheme, mqttHost(), mqttPort())).
		SetClientID("e2e-ext-" + uniqueSuffix()).
		SetUsername(username).SetPassword(password).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(false).
		SetCleanSession(true)
	if scheme == "ssl" {
		opts.SetTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
			// #nosec G402 -- opt-in via E2E_TLS_INSECURE; see insecureTLS.
			InsecureSkipVerify: insecureTLS(),
		})
	}
	c := paho.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(15 * time.Second) {
		return nil, fmt.Errorf("timeout")
	}
	return c, tok.Error()
}
