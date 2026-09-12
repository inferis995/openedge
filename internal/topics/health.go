// Package topics owns the shape of the MQTT topics the platform uses.
//
// It exists because one of them was spelled out with fmt.Sprintf in seven
// different files, and changing it meant finding all seven. The one that got
// away stays silent: a driver publishing where nobody listens looks exactly
// like a driver that has nothing to say.
package topics

import (
	"fmt"
	"strconv"
	"strings"
)

// HealthFilter is what a subscriber asks for to receive gateway health.
//
// The multi-level wildcard covers both the current shape and the one it
// replaces, which is what lets a platform be upgraded before the boxes in the
// field are.
const HealthFilter = "sys/health/#"

// Health returns the retained topic a gateway publishes its link state on.
//
// It used to be sys/health/{gateway_id} — keyed by gateway, with no
// organization in it. Every other system topic is scoped by organization and
// the broker's permissions follow that scoping; this one could not be, so the
// grant had to be sys/health/+ for everybody. Any tenant could read another
// tenant's gateway states, or publish a false one. That was a nuisance while
// the topic fed a colored dot in a web page. It stopped being one when it
// started raising alarms and sending notifications.
//
// An organization of zero falls back to the old shape rather than publishing
// to sys/health/0/{id}. A driver started by hand, without the environment a
// supervisor would give it, should still land somewhere a listener is looking —
// and ParseHealth accepts both.
func Health(orgID, gatewayID int) string {
	if orgID <= 0 {
		return fmt.Sprintf("sys/health/%d", gatewayID)
	}
	return fmt.Sprintf("sys/health/%d/%d", orgID, gatewayID)
}

// ParseHealth reads a gateway health topic in either shape.
//
// Both are accepted on purpose and for a while. Drivers are updated one
// container at a time, so during a rollout the same broker carries both — and a
// platform that understood only the new one would show every not-yet-updated
// gateway as never having reported, which is indistinguishable from a plant
// that has actually gone dark.
//
// orgID is zero for the old shape: the topic simply does not carry it.
func ParseHealth(topic string) (orgID, gatewayID int, ok bool) {
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	if len(parts) < 3 || parts[0] != "sys" || parts[1] != "health" {
		return 0, 0, false
	}

	switch len(parts) {
	case 3: // sys/health/{gateway}
		id, err := strconv.Atoi(parts[2])
		if err != nil || id <= 0 {
			return 0, 0, false
		}
		return 0, id, true

	case 4: // sys/health/{org}/{gateway}
		org, err := strconv.Atoi(parts[2])
		if err != nil || org <= 0 {
			return 0, 0, false
		}
		id, err := strconv.Atoi(parts[3])
		if err != nil || id <= 0 {
			return 0, 0, false
		}
		return org, id, true
	}
	return 0, 0, false
}
