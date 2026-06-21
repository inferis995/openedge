package models

import "time"

type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

type User struct {
	ID           int        `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"` // Never expose hash in JSON
	Role         UserRole   `json:"role"`
	FullName     string     `json:"full_name"`
	OrgID        *int       `json:"org_id"` // NULL for global admin
	I3xWrite     bool       `json:"i3x_write"`
	SiteIDs      []int      `json:"site_ids"`  // empty = access all org sites
	AreaIDs      []int      `json:"area_ids"`  // empty = access all accessible areas
	CreatedAt    time.Time  `json:"created_at"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token       string `json:"token,omitempty"`
	User        User   `json:"user,omitempty"`
	MFARequired bool   `json:"mfa_required,omitempty"`
	MFAToken    string `json:"mfa_token,omitempty"`
}
