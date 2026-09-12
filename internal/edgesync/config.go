// Package edgesync carries the configuration of a plant from the central
// platform down to the box installed inside it.
//
// A box on a plant floor has no path to the central database — that is the
// whole reason it exists. It keeps its own Postgres, its own broker and its own
// history, so it goes on polling the PLCs whether the link is up or not. What
// it cannot do is invent its own configuration: which gateways exist, at which
// addresses, with which tags and which alarms is decided in one place and
// mirrored down.
//
// The types live here rather than in the handler that serves them, and in the
// agent that consumes them, because two definitions of the same payload drift
// the first time somebody adds a field to one of them.
package edgesync

import "github.com/ralph/industrial-edge-middleware/internal/models"

// Config is everything a box needs to do its job.
//
// Identifiers are the CENTRAL ones and are preserved on the way down. They are
// not cosmetic: the MQTT topics carry them — data/{tag_id}, sys/health/{gateway_id} —
// and the central platform resolves them against its own database. A box that
// renumbered locally would publish data nobody could attribute.
type Config struct {
	OrgID   int    `json:"org_id"`
	OrgName string `json:"org_name"`

	MQTT MQTTCreds `json:"mqtt"`

	Sites    []Site                   `json:"sites"`
	Areas    []Area                   `json:"areas"`
	Gateways []models.Gateway         `json:"gateways"`
	Tags     []models.Tag             `json:"tags"`
	Alarms   []models.AlarmDefinition `json:"alarms"`

	// Unassigned is how many gateways of this organization no box has been made
	// responsible for. Always zero where there is a single box, because there
	// the assignment is ignored; a number here means somebody has to say which
	// box polls what, and until they do those gateways are polled by nobody.
	Unassigned int `json:"unassigned_gateways"`
}

// MQTTCreds is where the box publishes and with which credentials.
type MQTTCreds struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Site is one plant.
type Site struct {
	ID    int    `json:"id"`
	OrgID int    `json:"org_id"`
	Name  string `json:"name"`
}

// Area is one department within a plant.
type Area struct {
	ID     int    `json:"id"`
	SiteID int    `json:"site_id"`
	Name   string `json:"name"`
}

// Counts summarizes a configuration in one line, for logs and for the operator.
func (c *Config) Counts() (sites, areas, gateways, tags, alarms int) {
	return len(c.Sites), len(c.Areas), len(c.Gateways), len(c.Tags), len(c.Alarms)
}
