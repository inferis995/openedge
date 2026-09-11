package inventory_test

import (
	"strings"
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/inventory"
)

func TestModbusTCP(t *testing.T) {
	got := inventory.Endpoint("MODBUS_TCP", map[string]interface{}{
		"ip": "192.168.1.50", "port": float64(502), "slave_id": float64(3),
	})
	if got != "192.168.1.50:502, slave 3" {
		t.Errorf("got %q", got)
	}
}

// JSON has no integers: every number arrives as a float64, and "slave 3.000000"
// reads as a mistake on a printed page.
func TestNumbersAreNotPrintedAsFloats(t *testing.T) {
	got := inventory.Endpoint("MODBUS_TCP", map[string]interface{}{
		"ip": "10.1.2.3", "slave_id": float64(3), "port": float64(502),
	})
	// Asserted on the numbers themselves rather than on the whole line: the
	// first version of this test looked for ".0" anywhere and matched the IP
	// address, which is a dotted number and entirely correct.
	if !strings.HasSuffix(got, "slave 3") {
		t.Errorf("the slave id was not rendered as a whole number: %q", got)
	}
	if strings.Contains(got, ":502.") {
		t.Errorf("the port was printed with a decimal part: %q", got)
	}
}

func TestModbusRTUNamesTheSerialLine(t *testing.T) {
	got := inventory.Endpoint("MODBUS_TCP", map[string]interface{}{
		"transport": "rtu", "device": "/dev/ttyUSB0",
		"baud_rate": float64(9600), "parity": "even", "slave_id": float64(7),
	})
	for _, want := range []string{"/dev/ttyUSB0", "9600 baud", "parità even", "slave 7"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from %q", want, got)
		}
	}
}

// RTU over TCP and plain TCP look identical in a host:port line, and mistaking
// one for the other produces a connection that opens and then never answers —
// the hardest fault in this system to read. The inventory has to tell them
// apart, because that line is what somebody troubleshoots from.
func TestRTUOverTCPIsNotMistakenForPlainTCP(t *testing.T) {
	plain := inventory.Endpoint("MODBUS_TCP", map[string]interface{}{
		"ip": "10.0.0.5", "port": float64(4001),
	})
	wrapped := inventory.Endpoint("MODBUS_TCP", map[string]interface{}{
		"transport": "rtuovertcp", "ip": "10.0.0.5", "port": float64(4001),
	})
	if plain == wrapped {
		t.Fatalf("both transports rendered as %q", plain)
	}
	if !strings.Contains(wrapped, "RTU over TCP") {
		t.Errorf("got %q", wrapped)
	}
}

func TestS7CarriesRackAndSlot(t *testing.T) {
	got := inventory.Endpoint("S7", map[string]interface{}{
		"ip": "192.168.0.1", "rack": float64(0), "slot": float64(2),
	})
	if got != "192.168.0.1:102, rack 0 slot 2" {
		t.Errorf("got %q", got)
	}
}

// An electrician reading the inventory needs to know a gateway is not
// configured, not to see a blank cell they will read as "fine".
func TestAMissingAddressSaysSo(t *testing.T) {
	got := inventory.Endpoint("S7", map[string]interface{}{})
	if !strings.Contains(got, "non configurato") {
		t.Errorf("got %q", got)
	}
}

func TestOPCUAUsesTheEndpointURL(t *testing.T) {
	got := inventory.Endpoint("OPC_UA", map[string]interface{}{
		"endpoint": "opc.tcp://10.1.1.7:4840", "auth_mode": "Anonymous",
	})
	if !strings.Contains(got, "opc.tcp://10.1.1.7:4840") || !strings.Contains(got, "Anonymous") {
		t.Errorf("got %q", got)
	}
}

func TestMQTTWithNoExternalBrokerSaysWhichBrokerItIs(t *testing.T) {
	got := inventory.Endpoint("MQTT", map[string]interface{}{})
	if !strings.Contains(got, "interno") {
		t.Errorf("got %q", got)
	}
}

func TestMQTTOverTLSIsDistinguishable(t *testing.T) {
	plain := inventory.Endpoint("MQTT", map[string]interface{}{
		"broker_host": "broker.example.com", "broker_port": float64(1883),
	})
	tls := inventory.Endpoint("MQTT", map[string]interface{}{
		"broker_host": "broker.example.com", "broker_port": float64(8883), "broker_tls": true,
	})
	if !strings.HasPrefix(plain, "mqtt://") {
		t.Errorf("plain = %q", plain)
	}
	if !strings.HasPrefix(tls, "mqtts://") {
		t.Errorf("tls = %q; an inventory that cannot show which links are encrypted "+
			"is useless for the audit it exists for", tls)
	}
}

func TestAnUnknownDriverDoesNotInventSomething(t *testing.T) {
	if got := inventory.Endpoint("SOMETHING_NEW", map[string]interface{}{"ip": "1.2.3.4"}); got != "—" {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// The property that matters most: this line gets printed and emailed
// ---------------------------------------------------------------------------

// The inventory is exported, attached to a report, and handed to somebody
// outside the company. connection_config holds broker and server passwords.
// Endpoint is built from a whitelist, so it cannot emit them by construction —
// and this is what says so out loud, for every protocol, for every secret-ish
// key, including ones nobody has added yet.
func TestNoCredentialEverReachesTheInventory(t *testing.T) {
	const canary = "SuperSegreta123!"

	drivers := []string{"MODBUS_TCP", "S7", "OPC_UA", "MQTT", "LORAWAN"}

	for _, driver := range drivers {
		// A configuration that is plausible for the driver AND carries every
		// secret-shaped field, plus a couple nobody has invented yet.
		config := map[string]interface{}{
			"ip":          "10.0.0.1",
			"host":        "10.0.0.1",
			"broker_host": "broker.example.com",
			"endpoint":    "opc.tcp://10.0.0.1:4840",
			"device":      "/dev/ttyUSB0",
			"username":    "operatore",
		}
		for _, k := range inventory.SecretKeys() {
			config[k] = canary
		}
		config["future_credential_nobody_has_added_yet"] = canary
		config["passphrase"] = canary

		got := inventory.Endpoint(driver, config)
		if strings.Contains(got, canary) {
			t.Errorf("%s leaked a credential into the inventory line: %q", driver, got)
		}
	}
}

// A credential can also hide inside a URL. An OPC UA endpoint with userinfo is
// a legitimate thing to have configured and an illegitimate thing to print.
func TestCredentialsInsideAnEndpointURLAreStripped(t *testing.T) {
	got := inventory.Endpoint("OPC_UA", map[string]interface{}{
		"endpoint": "opc.tcp://admin:SuperSegreta123!@10.1.1.7:4840",
	})
	if strings.Contains(got, "SuperSegreta123!") || strings.Contains(got, "admin") {
		t.Fatalf("the userinfo part of the URL was printed: %q", got)
	}
	if !strings.Contains(got, "10.1.1.7:4840") {
		t.Errorf("the host was lost along with the credentials: %q", got)
	}
}
