package gatewayhealth

import (
	"encoding/json"
	"strings"
)

// ParsePayload reads a status off a sys/health topic, in either of the two
// shapes that topic actually carries.
//
// The drivers publish a bare word — "online", "offline", "error". driver-manager
// publishes a JSON GatewayStatus object on the same topic when it starts or
// stops a container. Anything else, including the empty payload that clears a
// retained status when a gateway is deleted, is not a status.
//
// It lives here because two services read that topic. core-api had learned
// both shapes; engine-historian had not, stored driver-manager's whole JSON
// object as the status, overflowed the column, and lost every driver failure
// from the event log.
func ParsePayload(payload []byte) (string, bool) {
	valid := func(s string) (string, bool) {
		switch s {
		case StatusOnline, StatusOffline, StatusError:
			return s, true
		}
		return "", false
	}

	if s, ok := valid(strings.ToLower(strings.TrimSpace(string(payload)))); ok {
		return s, true
	}

	var obj struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(payload, &obj); err == nil {
		return valid(strings.ToLower(strings.TrimSpace(obj.Status)))
	}
	return "", false
}
