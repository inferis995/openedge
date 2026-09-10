package alarms

import (
	"math"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// Health alarm types watch the LINK to the device instead of the value that
// arrives over it. Every other rule can only fire while readings keep coming
// in; these two fire precisely when they stop meaning anything.
const (
	// AlarmCommLoss fires when no trustworthy sample has arrived for the rule's
	// delay. It covers both a driver reporting bad quality and a driver that
	// has stopped reporting the tag at all — from the alarm engine's side those
	// are the same failure, and neither used to produce anything.
	AlarmCommLoss = "comm_loss"

	// AlarmFrozen fires when samples keep arriving but the value has not moved
	// by more than the deadband for the rule's delay: a stuck sensor, a halted
	// PLC program, or a watchdog counter that stopped counting. Unlike a dead
	// link this one looks perfectly healthy from the outside, which is what
	// makes it worth a rule of its own.
	AlarmFrozen = "frozen"
)

// qualityGood is the OPC UA "Good" status code, which every driver in this
// repository uses to mean "this reading can be trusted".
const qualityGood = 192

// defaultHealthTimeoutSeconds applies to a health rule saved without a delay.
// Zero would mean "alarm on the first missed poll", which is never what an
// operator wants from either of these rules.
const defaultHealthTimeoutSeconds = 60

// isHealthAlarm reports whether a rule is driven by the health of the link
// rather than by the value. These rules are judged on the clock in tickDelays
// and never against an incoming sample, because what triggers them is the
// ABSENCE of a sample (or of a change) — something no single sample can show.
func isHealthAlarm(alarmType string) bool {
	return alarmType == AlarmCommLoss || alarmType == AlarmFrozen
}

// hasHealthAlarm reports whether any of a tag's rules needs health state kept.
func hasHealthAlarm(defs []models.AlarmDefinition) bool {
	for i := range defs {
		if isHealthAlarm(defs[i].AlarmType) {
			return true
		}
	}
	return false
}

// healthTimeout is the silence, or stillness, a rule tolerates before firing.
func healthTimeout(def *models.AlarmDefinition) time.Duration {
	secs := def.DelaySeconds
	if secs <= 0 {
		secs = defaultHealthTimeoutSeconds
	}
	return time.Duration(secs) * time.Second
}

// anchor is the last value a frozen rule accepted as a real change, and when it
// happened. It is per-rule because the deadband is per-rule: the same samples
// can be "still" for a rule with deadband 1.0 and "moving" for one with 0.
//
// Comparing against the anchor rather than against the previous sample is what
// makes a slow drift count as movement: a value creeping up by less than the
// deadband every poll still leaves the anchor eventually, and a tag that only
// dithers inside the deadband never does.
type anchor struct {
	value float64
	since time.Time
}

// sampleState is everything the health rules of one tag remember between
// samples. It exists only for tags that actually have a health rule.
type sampleState struct {
	lastGood  time.Time // arrival of the last trustworthy sample
	lastValue float64
	hasValue  bool
	anchors   map[int]*anchor // definition id -> last real change
}

// recordGoodSample takes note of a trustworthy reading: the sign of life the
// comm_loss rules wait for, and the movement the frozen rules measure.
// Caller holds m.mu.
func (m *Manager) recordGoodSample(tagID int, defs []models.AlarmDefinition, val float64, now time.Time) {
	if m.samples == nil {
		m.samples = make(map[int]*sampleState)
	}
	s := m.samples[tagID]
	if s == nil {
		s = &sampleState{anchors: make(map[int]*anchor)}
		m.samples[tagID] = s
	}

	s.lastGood = now
	s.lastValue = val
	s.hasValue = true

	for i := range defs {
		def := &defs[i]
		if def.AlarmType != AlarmFrozen {
			continue
		}
		a := s.anchors[def.ID]
		if a == nil {
			s.anchors[def.ID] = &anchor{value: val, since: now}
			continue
		}
		if math.Abs(val-a.value) > def.Deadband {
			a.value = val
			a.since = now
		}
	}
}

// tickHealth judges every health rule against the clock. Caller holds m.mu;
// the transitions it decides are queued, not executed.
func (m *Manager) tickHealth(now time.Time) {
	for tagID, defs := range m.definitions {
		if !hasHealthAlarm(defs) {
			continue
		}

		s := m.samples[tagID]
		if s == nil {
			// First tick for this tag. Start its clock now instead of treating
			// "never heard from" as a silence that began at the zero time: a
			// gateway that starts up next to a dead PLC still alarms, one
			// timeout later rather than instantly.
			if m.samples == nil {
				m.samples = make(map[int]*sampleState)
			}
			m.samples[tagID] = &sampleState{lastGood: now, anchors: make(map[int]*anchor)}
			continue
		}

		silence := now.Sub(s.lastGood)

		for i := range defs {
			def := &defs[i]
			timeout := healthTimeout(def)

			switch def.AlarmType {
			case AlarmCommLoss:
				m.applyHealth(tagID, def, silence >= timeout, s.lastValue, now)

			case AlarmFrozen:
				// A tag nobody is hearing from is mute, not frozen. comm_loss
				// is the rule that describes that, and raising both for one
				// unplugged cable would only double the operator's work.
				a := s.anchors[def.ID]
				still := s.hasValue && a != nil &&
					silence <= timeout &&
					now.Sub(a.since) >= timeout
				m.applyHealth(tagID, def, still, s.lastValue, now)
			}
		}
	}
}

// applyHealth turns a health verdict into the same ACTIVE/CLEARED transitions
// the value rules produce, so these alarms reach alarm_events, MQTT and the
// notification channels by exactly the same road as any other.
//
// A health rule fires the moment it is violated: its delay has already been
// spent as the silence that made it violated, and letting tickDelays run the
// delay a second time would double every timeout the operator configured.
// Caller holds m.mu.
func (m *Manager) applyHealth(tagID int, def *models.AlarmDefinition, violated bool, val float64, now time.Time) {
	tracks := m.activeTracks[tagID]
	if tracks == nil {
		if !violated {
			return
		}
		tracks = make(map[int]*activeAlarmTrack)
		m.activeTracks[tagID] = tracks
	}

	track, tracking := tracks[def.ID]

	switch {
	case violated && !tracking:
		track = &activeAlarmTrack{
			Definition:      *def,
			ActiveSince:     now,
			Triggered:       true,
			InitialValue:    val,
			FiredAt:         now,
			Event:           &eventRef{},
			NextInsertRetry: now.Add(alarmInsertRetryInterval),
		}
		tracks[def.ID] = track
		m.queueIO(alarmIO{
			tagID:       tagID,
			lookupAlias: true,
			def:         *def,
			value:       val,
			status:      "ACTIVE",
			ref:         track.Event,
			triggeredAt: now,
			notify:      true,
		})

	case !violated && tracking:
		m.queueIO(alarmIO{
			tagID:       tagID,
			lookupAlias: true,
			def:         *def,
			value:       val,
			status:      "CLEARED",
			ref:         track.Event,
			triggeredAt: track.FiredAt,
			notify:      true,
		})
		delete(tracks, def.ID)
	}
}

// pruneHealthState drops the state of tags and rules that no longer have a
// health definition. Without it a gateway whose alarms are edited often leaks
// one anchor per deleted rule for the lifetime of the process.
// Caller holds m.mu.
func (m *Manager) pruneHealthState(newDefs map[int][]models.AlarmDefinition) {
	for tagID, s := range m.samples {
		defs := newDefs[tagID]
		if !hasHealthAlarm(defs) {
			delete(m.samples, tagID)
			continue
		}
		for defID := range s.anchors {
			stillDefined := false
			for i := range defs {
				if defs[i].ID == defID && defs[i].AlarmType == AlarmFrozen {
					stillDefined = true
					break
				}
			}
			if !stillDefined {
				delete(s.anchors, defID)
			}
		}
	}
}
