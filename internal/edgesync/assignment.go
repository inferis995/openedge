package edgesync

import "github.com/ralph/industrial-edge-middleware/internal/models"

// Every gateway is polled by exactly one thing: its box, or the server.
//
// That sentence is the whole rule, and it is written once, here, because three
// places have to agree on it: the configuration a box downloads, the heartbeat
// with which a box vouches for its gateways, and the server's own
// driver-manager deciding what to poll itself. Two copies of the rule diverge
// on the first change, and the divergence is a PLC read by two pollers — whose
// values land twice in history — or by none, which looks like a quiet plant.
//
// The old rule had a case that made mixing impossible: "the organization's only
// box takes everything". That was right while the server never polled, and
// wrong the moment it does: a box installed for one unreachable PLC would have
// taken every PLC the server already reaches, failed to reach them, and raised
// communication-loss alarms about equipment that was fine. Taking everything is
// now something a box is told to do, not something it infers from being alone.

// Server is the owner returned for a gateway no box is responsible for.
const Server = 0

// A box's scope says what it polls besides the gateways assigned to it.
const (
	// ScopeAssigned: only the gateways explicitly given to it. The default,
	// and the only safe one when the server polls too.
	ScopeAssigned = "assigned"
	// ScopeAll: also every gateway of the organization given to no box. For
	// an installation where the server is in the cloud and polls nothing. At
	// most one box per organization may have it.
	ScopeAll = "all"
)

// Box is an installed box as the rule needs to see it.
type Box struct {
	ID    int    `json:"id"`
	Scope string `json:"scope"`
}

// ValidScope reports whether s is a scope a box can be given.
func ValidScope(s string) bool { return s == ScopeAssigned || s == ScopeAll }

// Owner returns the box responsible for a gateway, or Server.
//
// A gateway assigned to a box that no longer exists falls through as if it had
// never been assigned: deleting a box must not leave its PLCs polled by nobody.
func Owner(gw *models.Gateway, boxes []Box) int {
	if gw.EdgeAgentID != nil {
		for _, b := range boxes {
			if b.ID == *gw.EdgeAgentID {
				return b.ID
			}
		}
	}
	for _, b := range boxes {
		if b.Scope == ScopeAll {
			return b.ID
		}
	}
	return Server
}

// GatewaysFor returns the gateways box agentID polls.
//
// agentID zero is a key minted before boxes had identities. It keeps getting
// every gateway, as it always has: the plants such a key runs must not stop
// being polled because this rule changed. A box on such a key cannot share an
// organization with the server polling or with other boxes; re-issuing its
// installer gives it an identity.
func GatewaysFor(agentID int, boxes []Box, gateways []models.Gateway) []models.Gateway {
	if agentID == Server {
		return gateways
	}
	return ownedBy(agentID, boxes, gateways)
}

// ServerGateways returns the gateways the server itself polls: those no box is
// responsible for.
func ServerGateways(boxes []Box, gateways []models.Gateway) []models.Gateway {
	return ownedBy(Server, boxes, gateways)
}

func ownedBy(owner int, boxes []Box, gateways []models.Gateway) []models.Gateway {
	out := make([]models.Gateway, 0, len(gateways))
	for i := range gateways {
		if Owner(&gateways[i], boxes) == owner {
			out = append(out, gateways[i])
		}
	}
	return out
}

// ServerOwned counts the gateways left to the server. Whether that is fine
// depends on whether the server polls: on-prem it does, and these are simply
// its PLCs; a cloud server does not, and these are polled by nobody.
func ServerOwned(boxes []Box, gateways []models.Gateway) int {
	return len(ServerGateways(boxes, gateways))
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
func (c *Config) FilterTree(agentID int, boxes []Box) {
	c.Unassigned = ServerOwned(boxes, c.Gateways)
	c.Gateways = GatewaysFor(agentID, boxes, c.Gateways)

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
