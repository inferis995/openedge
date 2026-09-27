package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/shifts"
)

// dashScope is whose data a dashboard shows.
//
// The dashboard overview read every table whole: any signed-in user of any
// organization saw every other organization's alarm messages and tag names,
// who logged in and wrote to a PLC, recipe loads, OEE profiles and custom
// KPIs. Each block now narrows by the caller's organization; a global admin
// with none selected still sees the whole installation.
//
// The predicates inline the organization id. It is an int from the JWT
// (middleware.GetOrganizationID), never text from the request, and inlining
// keeps the many small helpers here free of positional-argument bookkeeping.
type dashScope struct {
	all bool
	org int
}

func dashScopeOf(c *gin.Context) dashScope {
	if org, ok := middleware.GetOrganizationID(c); ok {
		return dashScope{org: org}
	}
	if middleware.IsGlobalAdmin(c) {
		return dashScope{all: true}
	}
	return dashScope{org: -1} // sees nothing
}

func (s dashScope) shifts() shifts.Scope {
	return shifts.Scope{OrgID: s.org, All: s.all}
}

// tags restricts a tag id column to this organization's tags.
func (s dashScope) tags(col string) string {
	if s.all {
		return "TRUE"
	}
	return fmt.Sprintf(`%s IN (SELECT t.id FROM tags t JOIN gateways g ON g.id = t.gateway_id
		JOIN areas a ON a.id = g.area_id JOIN sites si ON si.id = a.site_id WHERE si.org_id = %d)`, col, s.org)
}

// gateways restricts a gateway id column to this organization's gateways.
func (s dashScope) gateways(col string) string {
	if s.all {
		return "TRUE"
	}
	return fmt.Sprintf(`%s IN (SELECT g.id FROM gateways g JOIN areas a ON a.id = g.area_id
		JOIN sites si ON si.id = a.site_id WHERE si.org_id = %d)`, col, s.org)
}

// users restricts a user id column to this organization's people.
func (s dashScope) users(col string) string {
	if s.all {
		return "TRUE"
	}
	return fmt.Sprintf(`%s IN (SELECT id FROM users WHERE org_id = %d)`, col, s.org)
}

// orgCol restricts an org_id column.
func (s dashScope) orgCol(col string) string {
	if s.all {
		return "TRUE"
	}
	return fmt.Sprintf(`%s = %d`, col, s.org)
}

// orgPtr is the organization filter the OEE and KPI helpers take: nil = all.
func (s dashScope) orgPtr() *int {
	if s.all {
		return nil
	}
	o := s.org
	return &o
}
