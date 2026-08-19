//go:build e2e

package e2e

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// The broker, as a browser reaches it.
//
// The web UI watches live values over a WebSocket straight to the broker,
// proxied by nginx at /mqtt. For a while that connection was ANONYMOUS: the
// listener was configured allow_anonymous true and, because per_listener_settings
// bound the dynsec plugin to the other listener, it carried no ACLs at all.
//
// On the on-prem stack that is a broker on the LAN. On the cloud stack nginx is
// published by Traefik on 443, so it was a broker on the internet — subscribe to
// # and read every tenant's live plant data, publish cmd/write/{gateway} and the
// drivers write that setpoint to a PLC.
//
// These tests exercise the WebSocket path, which the rest of the suite never
// touches: everything else speaks plain MQTT on 1883/8883.

// wsBrokerURL is the address a browser would use.
//
// In the cloud job that is the public one, through Traefik and nginx — the exact
// path an outsider would take. Directly, it is mosquitto's own WebSocket port.
func wsBrokerURL(t *testing.T) string {
	t.Helper()
	if proxiedDeployment() {
		u, err := url.Parse(apiBase())
		if err != nil {
			t.Fatalf("E2E_API_URL is not a URL: %v", err)
		}
		scheme := "ws"
		if u.Scheme == "https" {
			scheme = "wss"
		}
		return fmt.Sprintf("%s://%s/mqtt", scheme, u.Host)
	}
	return fmt.Sprintf("ws://%s:%s/mqtt", mqttHost(), env("E2E_MQTT_WS_PORT", "9001"))
}

func wsClientOptions(t *testing.T, clientID string) *paho.ClientOptions {
	t.Helper()
	opts := paho.NewClientOptions().
		AddBroker(wsBrokerURL(t)).
		SetClientID(clientID).
		SetConnectTimeout(10 * time.Second).
		SetCleanSession(true)
	if insecureTLS() {
		opts.SetTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
			// #nosec G402 -- opt-in via E2E_TLS_INSECURE; see insecureTLS.
			InsecureSkipVerify: true,
		})
	}
	return opts
}

// The one that matters. Nothing else in this suite would notice the broker being
// open, because everything else authenticates.
func TestBrokerRefusesAnonymousBrowserConnections(t *testing.T) {
	c := paho.NewClient(wsClientOptions(t, "e2e-anon-"+uniqueSuffix()))

	tok := c.Connect()
	if !tok.WaitTimeout(20 * time.Second) {
		// A timeout is not a refusal, and treating it as one would let this test
		// pass against a broker that is simply slow — or absent.
		t.Fatal("the anonymous connection neither succeeded nor was refused within 20s; " +
			"this test proves nothing in that state")
	}
	if tok.Error() == nil {
		c.Disconnect(100)
		t.Fatalf("an anonymous client connected to the broker at %s. Through this path a "+
			"stranger can subscribe to every tenant's live data and publish cmd/write/{gateway}, "+
			"which the drivers execute as a setpoint write", wsBrokerURL(t))
	}
}

// And the other half: a signed-in session can still get an identity and use it,
// or the fix above would just be an outage.
func TestTheWebUICanSignInToTheBroker(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	raw := org.mustDo(http.MethodGet, "/api/mqtt/ui-credentials", nil, http.StatusOK)
	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		t.Fatalf("decode credentials: %v — %s", err, truncate(raw))
	}
	if creds.Username == "" || creds.Password == "" {
		t.Fatalf("the API issued empty credentials: %s", truncate(raw))
	}
	if creds.Path != "/mqtt" {
		t.Errorf("path is %q; the frontend and nginx both assume /mqtt", creds.Path)
	}

	opts := wsClientOptions(t, "e2e-ui-"+uniqueSuffix()).
		SetUsername(creds.Username).SetPassword(creds.Password)
	c := paho.NewClient(opts)

	tok := c.Connect()
	if !tok.WaitTimeout(20 * time.Second) {
		t.Fatal("the web UI's own credentials timed out connecting to the broker")
	}
	if err := tok.Error(); err != nil {
		t.Fatalf("the web UI's own credentials were refused: %v", err)
	}
	defer c.Disconnect(100)

	// It must be able to watch — this is the filter useSparkplugListener uses.
	sub := c.Subscribe("spBv1.0/#", 0, func(paho.Client, paho.Message) {})
	if !sub.WaitTimeout(10*time.Second) || sub.Error() != nil {
		t.Fatalf("the UI identity cannot subscribe to spBv1.0/#: %v", sub.Error())
	}
}

// Read-only is a property of the running broker, not just of the ACL we build.
// The unit test in internal/mqtt asserts the role contains no publishClientSend;
// this asserts Mosquitto agrees, against the deployed dynamic-security config.
func TestTheWebUIIdentityCannotWriteASetpoint(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	raw := org.mustDo(http.MethodGet, "/api/mqtt/ui-credentials", nil, http.StatusOK)
	var creds struct{ Username, Password string }
	if err := json.Unmarshal(raw, &creds); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}

	c := paho.NewClient(wsClientOptions(t, "e2e-ro-"+uniqueSuffix()).
		SetUsername(creds.Username).SetPassword(creds.Password))
	tok := c.Connect()
	if !tok.WaitTimeout(20*time.Second) || tok.Error() != nil {
		t.Fatalf("connect with the UI identity: %v", tok.Error())
	}
	defer c.Disconnect(100)

	// cmd/write/{gateway_id} is what the S7 and Modbus drivers act on
	// (services/driver-*/main.go). A browser must not be able to reach it.
	//
	// Mosquitto does not NAK a denied publish at QoS 0, and at QoS 1 it simply
	// never acknowledges, so the assertion is on the acknowledgement: a token
	// that never completes is the broker refusing.
	pub := c.Publish(fmt.Sprintf("cmd/write/%d", fx.gwID), 1, false, `{"value":1}`)
	if pub.WaitTimeout(5*time.Second) && pub.Error() == nil {
		t.Fatalf("the web UI identity published a setpoint write to cmd/write/%d — "+
			"the broker is enforcing no read-only boundary on it", fx.gwID)
	}
}
