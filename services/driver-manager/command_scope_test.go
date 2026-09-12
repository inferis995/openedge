package main

import "testing"

func TestACommandForTheWholeOrganizationReachesEveryBox(t *testing.T) {
	setAgentID(7)
	if !commandIsForThisBox("sys/restart/42", 42) {
		t.Error("a command addressed to the organization did not reach this box")
	}
	if !commandIsForThisBox("sys/update/42", 42) {
		t.Error("an update addressed to the organization did not reach this box")
	}
}

// The failure this prevents: restarting the box that is misbehaving bounced
// every other box of the organization too, including a plant running perfectly
// at the other end of the country.
func TestACommandForAnotherBoxIsIgnored(t *testing.T) {
	setAgentID(7)
	if commandIsForThisBox("sys/restart/42/9", 42) {
		t.Error("this box obeyed a restart addressed to box 9")
	}
}

func TestACommandForThisBoxIsObeyed(t *testing.T) {
	setAgentID(7)
	if !commandIsForThisBox("sys/restart/42/7", 42) {
		t.Error("this box ignored a restart addressed to it")
	}
}

// The broker's permissions already confine delivery to the organization's own
// topics, but a subscription is a wildcard and code that trusts the broker for
// its authorization has no answer the day the broker is misconfigured.
func TestACommandForAnotherOrganizationIsIgnored(t *testing.T) {
	setAgentID(7)
	for _, topic := range []string{"sys/restart/99", "sys/restart/99/7", "sys/update/99"} {
		if commandIsForThisBox(topic, 42) {
			t.Errorf("this box obeyed %q, which belongs to another organization", topic)
		}
	}
}

// A box that has not yet learnt its identity must not bounce its containers
// because somebody addressed its neighbor.
func TestABoxThatDoesNotKnowItselfRefusesAnAddressedCommand(t *testing.T) {
	setAgentID(0)
	if commandIsForThisBox("sys/restart/42/7", 42) {
		t.Error("a box with no identity obeyed a command addressed to a specific box")
	}
	// The case the guard actually exists for. Without "mine != 0" the
	// comparison is 0 == 0, and a box that has not yet learnt its identity
	// obeys anything addressed to box zero — which is what every box looks
	// like in the seconds after it starts. The first version of this test
	// only tried box 7, and stayed green with the guard removed.
	if commandIsForThisBox("sys/restart/42/0", 42) {
		t.Error("a box with no identity obeyed a command addressed to box 0")
	}
	// But one addressed to the whole organization still applies: that is how
	// every installation worked before boxes had identities, and those are
	// exactly the ones with no identity.
	if !commandIsForThisBox("sys/restart/42", 42) {
		t.Error("a box with no identity ignored a command addressed to its organization; " +
			"every installation from before this existed is in that state")
	}
}

func TestRubbishTopicsAreIgnored(t *testing.T) {
	setAgentID(7)
	for _, topic := range []string{
		"", "sys", "sys/restart", "sys/restart/abc", "sys/restart/42/abc",
		"sys/restart/42/7/8", "data/42", "restart/42",
		// Three levels, this organization's id, and not a command topic at
		// all. Nothing in the first version of this list had that shape, so
		// removing the check on the first level left the test green.
		"data/restart/42", "spBv1.0/x/42",
	} {
		if commandIsForThisBox(topic, 42) {
			t.Errorf("this box obeyed %q", topic)
		}
	}
}

// An organization id of zero is what an unconfigured box has. It must not match
// a topic that happens to carry a zero.
func TestAnUnconfiguredBoxObeysNothing(t *testing.T) {
	setAgentID(7)
	if commandIsForThisBox("sys/restart/0", 0) {
		t.Error("a box with no organization obeyed a command")
	}
}
