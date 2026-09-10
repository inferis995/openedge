package main

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/gatewayhealth"
)

// sys/health/{id} carries two shapes, because two different things publish on
// it: the drivers write a bare word, driver-manager writes a JSON object when it
// starts or stops a container. Only the bare word was accepted, and only two of
// the three words at that — so a driver reporting "error", and every message
// driver-manager ever sent, were logged as invalid and thrown away.
func TestTheHealthTopicIsReadInBothShapesItCarries(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
		ok      bool
	}{
		{"driver, online", "online", gatewayhealth.StatusOnline, true},
		{"driver, offline", "offline", gatewayhealth.StatusOffline, true},
		{"driver, error", "error", gatewayhealth.StatusError, true},
		{"last will", "offline", gatewayhealth.StatusOffline, true},

		{"whitespace and case", "  OFFLINE\n", gatewayhealth.StatusOffline, true},

		{
			"driver-manager, container stopped",
			`{"gateway_id":3,"status":"offline","timestamp":1757462400000,"driver_type":"MODBUS_TCP"}`,
			gatewayhealth.StatusOffline, true,
		},
		{
			"driver-manager, container failed",
			`{"gateway_id":3,"status":"error","error":"image pull failed","timestamp":1757462400000}`,
			gatewayhealth.StatusError, true,
		},

		{"nonsense", "banana", "", false},
		{"empty", "", "", false},
		{"json with no status", `{"gateway_id":3}`, "", false},
		{"json with a status nobody publishes", `{"status":"degraded"}`, "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseHealthPayload([]byte(c.payload))
			if ok != c.ok {
				t.Fatalf("parseHealthPayload(%q) accepted = %v, want %v", c.payload, ok, c.ok)
			}
			if got != c.want {
				t.Errorf("parseHealthPayload(%q) = %q, want %q", c.payload, got, c.want)
			}
		})
	}
}
