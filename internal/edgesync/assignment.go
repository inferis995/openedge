package edgesync

import "github.com/ralph/industrial-edge-middleware/internal/models"

// GatewaysFor decides which gateways a box is responsible for.
//
// The rule has three cases, and the awkward one is the middle:
//
//	a key with no box behind it   -> everything
//	the organization's only box   -> everything
//	one of several boxes          -> only what it was given
//
// The first case exists because every key minted before boxes had identities
// has no box behind it, and the plants those keys are running must not stop
// polling the day this ships. They were the organization's only box; they
// behave as one.
//
// The second is what keeps a single-box installation — which is nearly all of
// them — working with nobody having to assign anything.
//
// The third is the point. Two boxes that both take every gateway of the
// organization both try to reach PLCs on the other site, fail, and now that
// comm_loss exists they raise alarms about equipment that is perfectly fine.
// Once there are several boxes, each takes only what it was given.
//
// A gateway left unassigned in an organization with several boxes is polled by
// nobody. That is deliberate — the alternative is two boxes fighting over it —
// and it is why UnassignedIn exists, so it can be said out loud rather than
// discovered.
func GatewaysFor(agentID, agentsInOrg int, gateways []models.Gateway) []models.Gateway {
	if agentID == 0 || agentsInOrg <= 1 {
		return gateways
	}

	mine := make([]models.Gateway, 0, len(gateways))
	for i := range gateways {
		if id := gateways[i].EdgeAgentID; id != nil && *id == agentID {
			mine = append(mine, gateways[i])
		}
	}
	return mine
}

// UnassignedIn counts the gateways nobody has been made responsible for.
//
// Zero in an organization with one box, because there the rule ignores the
// assignment entirely. It only becomes a number worth reporting when a second
// box appears, which is exactly when somebody needs to be told.
func UnassignedIn(agentsInOrg int, gateways []models.Gateway) int {
	if agentsInOrg <= 1 {
		return 0
	}
	n := 0
	for i := range gateways {
		if gateways[i].EdgeAgentID == nil {
			n++
		}
	}
	return n
}

// FilterTree narrows a configuration to one box.
//
// Gateways first, then the tags of those gateways, then the alarm rules of
// those tags. The order is not cosmetic: a configuration carrying a tag whose
// gateway was filtered out cannot be written at all — the foreign key refuses
// it — and the box would fail to apply anything, including the part that was
// correct.
//
// Sites and areas are left whole. They are a handful of rows, they cost
// nothing, and a box that knows the shape of the plant it sits in produces
// better logs than one that knows only its own corner.
func (c *Config) FilterTree(agentID, agentsInOrg int) {
	c.Unassigned = UnassignedIn(agentsInOrg, c.Gateways)
	c.Gateways = GatewaysFor(agentID, agentsInOrg, c.Gateways)

	keptGateway := make(map[int]bool, len(c.Gateways))
	for i := range c.Gateways {
		keptGateway[c.Gateways[i].ID] = true
	}

	tags := make([]models.Tag, 0, len(c.Tags))
	keptTag := make(map[int]bool, len(c.Tags))
	for i := range c.Tags {
		if keptGateway[c.Tags[i].GatewayID] {
			tags = append(tags, c.Tags[i])
			keptTag[c.Tags[i].ID] = true
		}
	}
	c.Tags = tags

	alarms := make([]models.AlarmDefinition, 0, len(c.Alarms))
	for i := range c.Alarms {
		if keptTag[c.Alarms[i].TagID] {
			alarms = append(alarms, c.Alarms[i])
		}
	}
	c.Alarms = alarms
}
