package gatewayhealth

import "testing"

func TestParsePayload(t *testing.T) {
	cases := []struct {
		payload, want string
		ok            bool
	}{
		{"online", StatusOnline, true},
		{" OFFLINE\n", StatusOffline, true},
		{"error", StatusError, true},
		// driver-manager's shape.
		{`{"gateway_id":3,"status":"error","error":"No such image"}`, StatusError, true},
		{`{"status":"online"}`, StatusOnline, true},
		// The retained status cleared when a gateway is deleted.
		{"", "", false},
		{`{"status":"starting"}`, "", false},
		{"garbage", "", false},
	}
	for _, c := range cases {
		got, ok := ParsePayload([]byte(c.payload))
		if ok != c.ok || got != c.want {
			t.Errorf("ParsePayload(%q) = %q, %v; want %q, %v", c.payload, got, ok, c.want, c.ok)
		}
	}
}
