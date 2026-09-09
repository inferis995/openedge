package modbus

import (
	"strings"
	"testing"
	"time"
)

// A configuration written before transports existed must still mean TCP.
//
// Every Modbus gateway already configured on a running plant carries an `ip`
// and a `port` and nothing else. If adding RTU changed what those mean, the
// change would ship as a platform that stops reading the plants it was already
// reading — which is not a regression an operator reports as "the new serial
// feature", it is one they report as "everything is down".
func TestAConfigWithNoTransportIsStillTCP(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"ip": "192.168.1.10", "port": float64(502), "slave_id": float64(3)},
		{"ip_address": "10.0.0.5"},                    // no port: 502
		{"ip": "192.168.1.10", "port": float64(5020)}, // non-standard port
	} {
		c, err := NewClientFromConfig(cfg)
		if err != nil {
			t.Fatalf("%v: %v", cfg, err)
		}
		if got := c.config.transport(); got != TransportTCP {
			t.Errorf("%v: transport %q, want %q", cfg, got, TransportTCP)
		}
		url, err := c.config.url()
		if err != nil {
			t.Fatalf("%v: url: %v", cfg, err)
		}
		if !strings.HasPrefix(url, "tcp://") {
			t.Errorf("%v: url %q does not start with tcp://", cfg, url)
		}
	}

	// And the values still land where they used to.
	c, err := NewClientFromConfig(map[string]interface{}{
		"ip": "192.168.1.10", "port": float64(502), "slave_id": float64(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if url, _ := c.config.url(); url != "tcp://192.168.1.10:502" {
		t.Errorf("url %q, want tcp://192.168.1.10:502", url)
	}
	if c.config.SlaveID != 3 {
		t.Errorf("slave id %d, want 3", c.config.SlaveID)
	}
	if c.config.Timeout != 5*time.Second {
		t.Errorf("tcp timeout %v, want 5s — the old value", c.config.Timeout)
	}
}

// A Config built in Go with no transport is TCP too.
//
// This is a second test rather than a case of the one above, because it covers
// a second default. NewClientFromConfig sets Transport explicitly, so it never
// exercises the fallback inside transport() — and the first version of the test
// above went through NewClientFromConfig and therefore passed with that
// fallback deliberately changed to RTU. It asserted the behavior of one path
// while claiming the behavior of both.
//
// The fallback matters to anyone who builds a Config directly, which the
// drivers may and a future caller certainly will.
func TestAConfigBuiltInGoWithNoTransportIsTCP(t *testing.T) {
	cfg := Config{Host: "192.168.1.10", Port: 502}

	if got := cfg.transport(); got != TransportTCP {
		t.Fatalf("a Config with an empty Transport reports %q, want %q — anything "+
			"constructing a Config directly would silently change wire", got, TransportTCP)
	}
	url, err := cfg.url()
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if url != "tcp://192.168.1.10:502" {
		t.Errorf("url %q, want tcp://192.168.1.10:502", url)
	}
}

// RTU reaches a serial port, and the URL the library wants has three slashes.
func TestRTUBuildsASerialURL(t *testing.T) {
	c, err := NewClientFromConfig(map[string]interface{}{
		"transport": "rtu",
		"device":    "/dev/ttyUSB0",
		"baud_rate": float64(9600),
		"parity":    "E",
		"data_bits": float64(8),
		"stop_bits": float64(1),
		"slave_id":  float64(7),
	})
	if err != nil {
		t.Fatalf("NewClientFromConfig: %v", err)
	}

	url, err := c.config.url()
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if url != "rtu:///dev/ttyUSB0" {
		t.Errorf("url %q, want rtu:///dev/ttyUSB0 — the library strips the scheme "+
			"and takes the rest as the device path, so an absolute path has three slashes", url)
	}
	if c.config.BaudRate != 9600 || c.config.DataBits != 8 || c.config.StopBits != 1 {
		t.Errorf("serial parameters lost: %+v", c.config)
	}
	if c.config.Parity != "E" {
		t.Errorf("parity %q, want E", c.config.Parity)
	}
	if c.config.SlaveID != 7 {
		t.Errorf("slave id %d, want 7", c.config.SlaveID)
	}

	// A serial timeout must not be the TCP one: a slave that is switched off
	// would hold the poll loop for five seconds every scan, and every other
	// device on the bus would go stale waiting for it.
	if c.config.Timeout >= 5*time.Second {
		t.Errorf("rtu timeout %v — as long as the TCP one, so one dead slave "+
			"stalls the whole bus", c.config.Timeout)
	}
}

// A Windows COM port is a device path too.
func TestRTUAcceptsAWindowsPort(t *testing.T) {
	c, err := NewClientFromConfig(map[string]interface{}{
		"transport": "rtu", "device": "COM3",
	})
	if err != nil {
		t.Fatalf("NewClientFromConfig: %v", err)
	}
	if url, _ := c.config.url(); url != "rtu://COM3" {
		t.Errorf("url %q, want rtu://COM3", url)
	}
}

// RTU over TCP is a serial gateway in transparent mode: TCP on the wire, RTU
// framing inside. Confusing it with plain TCP gives a socket that opens and
// then answers nothing, which is the hardest kind of fault to read.
func TestRTUOverTCPIsNotPlainTCP(t *testing.T) {
	c, err := NewClientFromConfig(map[string]interface{}{
		"transport": "rtuovertcp", "ip": "192.168.1.50", "port": float64(4001),
	})
	if err != nil {
		t.Fatalf("NewClientFromConfig: %v", err)
	}
	url, _ := c.config.url()
	if url != "rtuovertcp://192.168.1.50:4001" {
		t.Errorf("url %q, want rtuovertcp://192.168.1.50:4001", url)
	}
}

// A configuration that cannot produce a connection must be refused when it is
// saved, not on the first poll: the operator is looking at the form now and
// will be looking at a red gateway in an hour.
func TestAnImpossibleConfigIsRefusedUpFront(t *testing.T) {
	for name, cfg := range map[string]map[string]interface{}{
		"rtu without a device":     {"transport": "rtu"},
		"tcp without a host":       {"transport": "tcp"},
		"no host and no transport": {},
		"unknown transport":        {"transport": "carrier pigeon", "ip": "10.0.0.1"},
		"bad parity":               {"transport": "rtu", "device": "/dev/ttyUSB0", "parity": "Z"},
	} {
		if _, err := NewClientFromConfig(cfg); err == nil {
			t.Errorf("%s: accepted, and would have failed later on the plant", name)
		}
	}
}

// Parity is written differently on every datasheet and in every form.
func TestParityAcceptsWhatPeopleActuallyType(t *testing.T) {
	none := []string{"", "N", "n", "none", " NONE "}
	even := []string{"E", "e", "even", "Even"}
	odd := []string{"O", "o", "odd"}

	for _, v := range none {
		if p, err := parseParity(v); err != nil || p != 0 {
			t.Errorf("parity %q: got %v, %v — want none", v, p, err)
		}
	}
	for _, v := range even {
		if p, err := parseParity(v); err != nil || p != 1 {
			t.Errorf("parity %q: got %v, %v — want even", v, p, err)
		}
	}
	for _, v := range odd {
		if p, err := parseParity(v); err != nil || p != 2 {
			t.Errorf("parity %q: got %v, %v — want odd", v, p, err)
		}
	}
	if _, err := parseParity("Z"); err == nil {
		t.Error("parity \"Z\" accepted — a typo in a form would reach the bus as a guess")
	}
}

// Numbers arrive as float64 from JSON, as int from Go, and as strings from a
// form that did not coerce them. All three are the same number.
func TestNumbersArriveInThreeShapes(t *testing.T) {
	for name, cfg := range map[string]map[string]interface{}{
		"float64": {"transport": "rtu", "device": "/dev/ttyS0", "baud_rate": float64(115200)},
		"int":     {"transport": "rtu", "device": "/dev/ttyS0", "baud_rate": 115200},
		"string":  {"transport": "rtu", "device": "/dev/ttyS0", "baud_rate": "115200"},
	} {
		c, err := NewClientFromConfig(cfg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.config.BaudRate != 115200 {
			t.Errorf("%s: baud rate %d, want 115200", name, c.config.BaudRate)
		}
	}
}
