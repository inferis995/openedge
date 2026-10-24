package forecast

import (
	"sort"
	"time"
)

// Member is what an asset's health is computed from, per tag: its forecast
// status, the alarm rules it may trip and how the values that arrived compare
// with the previous forecast.
type Member struct {
	TagID   int      `json:"tag_id"`
	Alias   string   `json:"alias"`
	Status  string   `json:"status"`
	Risks   []Risk   `json:"risks,omitempty"`
	Anomaly *Anomaly `json:"anomaly,omitempty"`
}

// Issue is one thing that lowers an asset's health, worst first.
type Issue struct {
	TagID  int    `json:"tag_id"`
	Alias  string `json:"alias"`
	Kind   string `json:"kind"` // risk, anomaly
	Points int    `json:"points"`
	// For a risk: which, how likely, when.
	Risk *Risk `json:"risk,omitempty"`
	// For an anomaly: how many of how many values were outside.
	Outside int `json:"outside,omitempty"`
	Checked int `json:"checked,omitempty"`
}

// Health of an asset, 0–100.
type Health struct {
	// Score is 100 minus the penalty of its worst tag: an asset is as healthy
	// as its weakest part. Nil when no member has a reliable forecast — then
	// nothing is known, which is not the same as healthy.
	Score *int `json:"score"`
	// Level: good (≥80), watch (50–79), critical (<50), unknown.
	Level  string  `json:"level"`
	Issues []Issue `json:"issues"`
	// Known is how many members have a reliable forecast, of Members.
	Known   int `json:"known"`
	Members int `json:"members"`
}

// Penalties, in points off 100.
var riskPenalty = map[string]int{AlmostCertain: 45, Likely: 30, Possible: 10}

const (
	// A drift reaching a limit is worth less the further away it is.
	trendPenaltyNear = 20 // within a week
	trendPenaltyFar  = 8  // within a month
	// A tag whose every recent value fell outside the forecast loses this.
	anomalyPenaltyMax = 35
)

// AssetHealth scores an asset from its members at time now.
func AssetHealth(members []Member, now time.Time) Health {
	h := Health{Members: len(members), Issues: []Issue{}}
	worst := 0
	for i := range members {
		m := &members[i]
		if m.Status != StatusOK {
			continue
		}
		h.Known++
		penalty := 0
		for j := range m.Risks {
			r := m.Risks[j]
			p := riskPenalty[r.Likelihood]
			if r.Likelihood == Trend {
				switch d := r.At.Sub(now); {
				case d <= 7*24*time.Hour:
					p = trendPenaltyNear
				case d <= 30*24*time.Hour:
					p = trendPenaltyFar
				}
			}
			if p == 0 {
				continue
			}
			h.Issues = append(h.Issues, Issue{TagID: m.TagID, Alias: m.Alias, Kind: "risk", Points: p, Risk: &r})
			if p > penalty {
				penalty = p
			}
		}
		if a := m.Anomaly; a != nil && a.Outside > 0 && a.Checked > 0 {
			p := anomalyPenaltyMax * a.Outside / a.Checked
			if p < 5 {
				p = 5
			}
			h.Issues = append(h.Issues, Issue{TagID: m.TagID, Alias: m.Alias, Kind: "anomaly", Points: p,
				Outside: a.Outside, Checked: a.Checked})
			penalty += p
		}
		if penalty > worst {
			worst = penalty
		}
	}
	sort.SliceStable(h.Issues, func(i, j int) bool { return h.Issues[i].Points > h.Issues[j].Points })
	if h.Known == 0 {
		h.Level = "unknown"
		return h
	}
	score := 100 - worst
	if score < 0 {
		score = 0
	}
	h.Score = &score
	switch {
	case score >= 80:
		h.Level = "good"
	case score >= 50:
		h.Level = "watch"
	default:
		h.Level = "critical"
	}
	return h
}
