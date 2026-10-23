package forecast

import (
	"errors"
	"fmt"
)

// Scope is a level of the plant hierarchy a profile can be set on. A tag's
// settings are its profiles merged from the organization down to the tag.
type Scope string

const (
	ScopeOrg     Scope = "org"
	ScopeSite    Scope = "site"
	ScopeArea    Scope = "area"
	ScopeGateway Scope = "gateway"
	ScopeTag     Scope = "tag"
)

// Scopes from the most general to the most specific: the order profiles are
// merged in.
var Scopes = []Scope{ScopeOrg, ScopeSite, ScopeArea, ScopeGateway, ScopeTag}

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool {
	for _, x := range Scopes {
		if x == s {
			return true
		}
	}
	return false
}

// Settings is one profile. A nil field inherits from the level above.
type Settings struct {
	Enabled      *bool `json:"enabled,omitempty"`
	Kind         *Kind `json:"kind,omitempty"`
	HorizonHours *int  `json:"horizon_hours,omitempty"`
	ContextDays  *int  `json:"context_days,omitempty"`
}

// Effective is what a tag ends up with once every level is merged.
type Effective struct {
	Enabled      bool `json:"enabled"`
	Kind         Kind `json:"kind"`
	HorizonHours int  `json:"horizon_hours"`
	ContextDays  int  `json:"context_days"`
}

// Defaults apply where no level says otherwise: on, kind inferred, one day
// ahead from four weeks of history.
var Defaults = Effective{Enabled: true, Kind: KindAuto, HorizonHours: 24, ContextDays: 28}

// Limits on the numbers a profile may set.
const (
	MaxHorizonHours = 24 * 30
	MaxContextDays  = 365
)

// Validate rejects values a profile may not hold.
func (s Settings) Validate() error {
	var errs []error
	if s.Kind != nil && !s.Kind.Valid() {
		errs = append(errs, fmt.Errorf("kind %q is not one of auto, process, cyclic, degradation, counter, state", *s.Kind))
	}
	if s.HorizonHours != nil && (*s.HorizonHours < 1 || *s.HorizonHours > MaxHorizonHours) {
		errs = append(errs, fmt.Errorf("horizon_hours must be between 1 and %d", MaxHorizonHours))
	}
	if s.ContextDays != nil && (*s.ContextDays < 1 || *s.ContextDays > MaxContextDays) {
		errs = append(errs, fmt.Errorf("context_days must be between 1 and %d", MaxContextDays))
	}
	return errors.Join(errs...)
}

// Merge applies profiles, most general first, over the defaults.
func Merge(layers ...Settings) Effective {
	e := Defaults
	for _, l := range layers {
		if l.Enabled != nil {
			e.Enabled = *l.Enabled
		}
		if l.Kind != nil {
			e.Kind = *l.Kind
		}
		if l.HorizonHours != nil {
			e.HorizonHours = *l.HorizonHours
		}
		if l.ContextDays != nil {
			e.ContextDays = *l.ContextDays
		}
	}
	return e
}
