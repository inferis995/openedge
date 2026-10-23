package forecast

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"
)

// Profiles are the forecast profiles of some organizations, by level.
type Profiles map[Scope]map[int]Settings

// LoadProfiles reads every profile of the given organizations.
func LoadProfiles(ctx context.Context, db *sql.DB, orgs []int) (Profiles, error) {
	p := Profiles{}
	if len(orgs) == 0 {
		return p, nil
	}
	rows, err := db.QueryContext(ctx,
		`SELECT scope, scope_id, settings FROM forecast_profiles WHERE org_id = ANY($1)`, pq.Array(orgs))
	if err != nil {
		return nil, fmt.Errorf("forecast profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scope string
		var id int
		var raw []byte
		if err := rows.Scan(&scope, &id, &raw); err != nil {
			return nil, err
		}
		var s Settings
		if err := json.Unmarshal(raw, &s); err != nil {
			continue // a profile that does not parse is ignored, not fatal
		}
		p.Set(Scope(scope), id, s)
	}
	return p, rows.Err()
}

// Set records one profile.
func (p Profiles) Set(scope Scope, id int, s Settings) {
	if p[scope] == nil {
		p[scope] = map[int]Settings{}
	}
	p[scope][id] = s
}

// Layers are a tag's profiles from the organization down, those set.
func (p Profiles) Layers(org, site, area, gateway, tag int) []Settings {
	ids := map[Scope]int{ScopeOrg: org, ScopeSite: site, ScopeArea: area, ScopeGateway: gateway, ScopeTag: tag}
	var out []Settings
	for _, sc := range Scopes {
		if s, ok := p[sc][ids[sc]]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Effective is a tag's settings once every level is merged.
func (p Profiles) Effective(org, site, area, gateway, tag int) Effective {
	return Merge(p.Layers(org, site, area, gateway, tag)...)
}
