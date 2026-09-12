package main

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// myAgentID is which box this is, as the platform knows it.
//
// A box cannot work this out on its own: its identity lives in the key it
// authenticates with, and only the platform can map one to the other. It
// arrives with the configuration and is nought until the first successful pull.
var myAgentID atomic.Int64

// setAgentID records the identity the platform assigned to this box.
func setAgentID(id int) { myAgentID.Store(int64(id)) }

// commandIsForThisBox reports whether an OTA command on the given topic is
// addressed to this box.
//
// Two shapes, and both have to keep working:
//
//	sys/restart/{org}            every box of the organization
//	sys/restart/{org}/{agent}    one box
//
// Only the first existed, which was fine while an organization had one box.
// With three, restarting the one that is misbehaving bounced the other two as
// well — including, in the worst case, a plant that was running perfectly at
// the other end of the country.
//
// A command addressed to a box that does not know its own identity yet is
// refused rather than obeyed. That is the deliberate choice: the alternative is
// a box that has not finished starting bouncing its containers because somebody
// addressed its neighbor.
func commandIsForThisBox(topic string, myOrgID int) bool {
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	// sys / {verb} / {org} [ / {agent} ]
	if len(parts) < 3 || parts[0] != "sys" {
		return false
	}

	org, err := strconv.Atoi(parts[2])
	if err != nil || org != myOrgID || myOrgID == 0 {
		return false
	}

	if len(parts) == 3 {
		return true // addressed to the whole organization
	}
	if len(parts) != 4 {
		return false
	}

	agent, err := strconv.Atoi(parts[3])
	if err != nil {
		return false
	}
	mine := int(myAgentID.Load())
	return mine != 0 && agent == mine
}
