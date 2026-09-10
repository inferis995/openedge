// Package gatewayhealth turns what the drivers say about their link to a PLC
// into something an operator is told about.
//
// Every driver already publishes online/offline/error on sys/health/{id}, with
// a retained last-will so an ungraceful death leaves "offline" behind. Nothing
// read it except a Redis key the web UI paints a dot from. A plant that lost a
// gateway at three in the morning found out when the first shift noticed the
// numbers had stopped moving.
package gatewayhealth

import (
	"fmt"
	"time"
)

// What a driver can report about itself.
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusError   = "error"
)

// What the operator has last been told. Stored, not remembered: a restart of
// core-api must not re-announce an outage that is hours old and already known.
const (
	NotifiedNothing = ""
	NotifiedDown    = "down"
)

// Action is what the watcher should do about one gateway right now.
type Action int

const (
	ActionNone Action = iota
	ActionNotifyDown
	ActionNotifyUp
)

// Config is how patient the watcher is.
type Config struct {
	// Silence is how long a gateway may say nothing before it counts as lost.
	//
	// This is not redundant with the last-will. A last-will only fires when the
	// BROKER decides a client is gone; if the broker itself is restarted or
	// replaced, every retained "online" survives and no will is ever published.
	// Silence is the check that does not depend on the broker being healthy.
	Silence time.Duration
}

// State is everything known about one gateway at the moment it is judged.
type State struct {
	GatewayID int
	Name      string

	// Status is the last thing the driver said, or empty when it has never
	// said anything.
	Status string

	// ReportedAt is when that arrived. Zero when nothing ever has.
	ReportedAt time.Time

	// SeenSince is when this watcher first laid eyes on the gateway — its
	// process start, or when the gateway was created. It is what a gateway
	// that has never reported is measured against, so a freshly started
	// core-api does not declare an entire plant lost the moment it comes up.
	SeenSince time.Time

	// NotifiedAs is what the operator was last told about this gateway.
	NotifiedAs string
}

// Decide reports what should happen about one gateway, and why.
//
// The reason is not decoration: it is the difference between "the driver told
// us the PLC refused the connection" and "we have not heard from the driver at
// all", which lead an electrician to two different ends of the building.
func Decide(s *State, cfg Config, now time.Time) (Action, string) {
	lastContact := s.ReportedAt
	if lastContact.IsZero() {
		lastContact = s.SeenSince
	}
	silentFor := now.Sub(lastContact)

	var reason string
	switch {
	case s.Status == StatusOffline:
		reason = "il driver segnala il gateway offline"
	case s.Status == StatusError:
		reason = "il driver segnala un errore di comunicazione"
	case cfg.Silence > 0 && silentFor >= cfg.Silence:
		// Reported as minutes: an operator reading this on a phone does not
		// need the seconds, and "1h12m0s" is not a sentence.
		reason = fmt.Sprintf("nessun segnale di vita da %s", roughly(silentFor))
	}

	down := reason != ""

	switch {
	case down && s.NotifiedAs != NotifiedDown:
		return ActionNotifyDown, reason
	case !down && s.NotifiedAs == NotifiedDown:
		return ActionNotifyUp, ""
	}
	return ActionNone, ""
}

// roughly renders a duration the way somebody says it out loud.
func roughly(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d secondi", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minuti", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d ore", int(d.Hours()))
	}
	return fmt.Sprintf("%d giorni", int(d.Hours()/24))
}
