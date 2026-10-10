package models

import (
	"encoding/json"
	"time"
)

// Organization represents the top level of the organizational hierarchy
type Organization struct {
	ID         int       `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	ThemeColor string    `json:"theme_color,omitempty" db:"theme_color"`
}

// Site represents a location belonging to an organization
type Site struct {
	ID        int       `json:"id" db:"id"`
	OrgID     int       `json:"org_id" db:"org_id"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Area represents a sub-section of a site
type Area struct {
	ID        int             `json:"id" db:"id"`
	SiteID    int             `json:"site_id" db:"site_id"`
	Name      string          `json:"name" db:"name"`
	// Perimeter as a GeoJSON geometry (or Feature), drawn on the map page.
	// NULL until someone pastes one in: an area without a boundary simply has
	// no polygon.
	Boundary  json.RawMessage `json:"boundary,omitempty" db:"boundary"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}
