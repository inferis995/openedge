// Package inventory builds the list of what is actually installed: every
// gateway, how it is reached, what it speaks, and when it was last heard from.
//
// It is the document a customer asks for at handover and an auditor asks for
// under NIS2, and until now it existed only in somebody's head. Nothing here
// interrogates a device: it is assembled from what the platform already knows,
// which is the difference between an inventory that is always current and one
// that is a survey somebody did once.
package inventory

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Endpoint renders, in one line, where a gateway is and how it is reached.
//
// Built from a WHITELIST of keys per protocol, never by printing the
// configuration. That is not tidiness: connection_config holds broker and
// server passwords, and this line ends up in an exported file, in a report, and
// on a printed page handed to somebody outside the company. A blacklist would
// leak the first credential field anybody adds afterwards; a whitelist stays
// silent about it.
func Endpoint(driverType string, config map[string]interface{}) string {
	switch strings.ToUpper(driverType) {
	case "MODBUS_TCP":
		return modbusEndpoint(config)
	case "S7":
		return s7Endpoint(config)
	case "OPC_UA":
		return opcuaEndpoint(config)
	case "MQTT":
		return mqttEndpoint(config)
	case "LORAWAN":
		return lorawanEndpoint(config)
	}
	return "—"
}

func modbusEndpoint(c map[string]interface{}) string {
	var where string
	switch strings.ToLower(str(c, "transport", "mode")) {
	case "rtu":
		where = str(c, "device", "serial_port", "port_name")
		if where == "" {
			where = "seriale non configurata"
		}
		var bits []string
		if baud := num(c, "baud_rate", "baudrate", "speed"); baud != "" {
			bits = append(bits, baud+" baud")
		}
		if parity := str(c, "parity"); parity != "" {
			bits = append(bits, "parità "+parity)
		}
		if len(bits) > 0 {
			where += " (" + strings.Join(bits, ", ") + ")"
		}
	case "rtuovertcp":
		where = hostPort(c, 502) + " (RTU over TCP)"
	default:
		where = hostPort(c, 502)
	}
	if id := num(c, "slave_id", "unit_id"); id != "" {
		where += ", slave " + id
	}
	return where
}

func s7Endpoint(c map[string]interface{}) string {
	where := hostPort(c, 102)
	rack, slot := num(c, "rack"), num(c, "slot")
	if rack == "" {
		rack = "0"
	}
	if slot == "" {
		slot = "0"
	}
	return fmt.Sprintf("%s, rack %s slot %s", where, rack, slot)
}

func opcuaEndpoint(c map[string]interface{}) string {
	ep := str(c, "endpoint")
	if ep == "" {
		return "endpoint non configurato"
	}
	// An endpoint URL can legitimately carry credentials in its userinfo part.
	// They are stripped rather than printed.
	if at := strings.Index(ep, "@"); at >= 0 {
		if scheme := strings.Index(ep, "://"); scheme >= 0 && scheme < at {
			ep = ep[:scheme+3] + ep[at+1:]
		}
	}
	if mode := str(c, "auth_mode", "authMode"); mode != "" {
		ep += " (" + mode + ")"
	}
	return ep
}

func mqttEndpoint(c map[string]interface{}) string {
	host := str(c, "broker_host")
	if host == "" {
		return "broker interno OpenEdge"
	}
	port := num(c, "broker_port")
	if port == "" {
		port = "1883"
	}
	scheme := "mqtt"
	if b, ok := c["broker_tls"].(bool); ok && b {
		scheme = "mqtts"
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

func lorawanEndpoint(c map[string]interface{}) string {
	if app := str(c, "application_id", "app_id"); app != "" {
		return "applicazione " + app
	}
	return "rete LoRaWAN"
}

// hostPort renders host:port, falling back to the protocol's usual port.
func hostPort(c map[string]interface{}, defaultPort int) string {
	host := str(c, "ip", "ip_address", "host")
	if host == "" {
		return "indirizzo non configurato"
	}
	port := num(c, "port")
	if port == "" {
		port = strconv.Itoa(defaultPort)
	}
	return host + ":" + port
}

// str returns the first key present as a non-empty string.
func str(c map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := c[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// num returns the first key present as a number, rendered without a decimal
// point when it has no fractional part: JSON turns every integer into a float64
// and "slave 3.000000" reads as a mistake.
func num(c map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		switch v := c[k].(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			return strconv.Itoa(v)
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// SecretKeys are the configuration fields that must never reach an exported
// inventory. Endpoint works from a whitelist and so cannot emit them by
// construction; this list exists so a test can assert that, and so the rule has
// somewhere to be written down.
func SecretKeys() []string {
	keys := []string{
		"password", "broker_password", "secret", "token",
		"private_key", "key", "client_secret", "api_key",
	}
	sort.Strings(keys)
	return keys
}
