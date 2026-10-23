package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
	"github.com/ralph/industrial-edge-middleware/internal/naming"
)

// Organization and site names end up inside MQTT topics, and the broker's
// per-tenant grants are made of them (internal/mqtt TenantScope). Two rules
// keep one tenant's grant from covering another's:
//
//   - a name must be usable as one topic level: no "+" or "#" (a wildcard
//     would cover every tenant), no NUL, not starting with "$";
//   - two organizations must not reduce to the same topic form: "Acme Corp"
//     and "acme-corp" both publish under data/acme-corp/, and the Sparkplug
//     group of org "a-b" site "c" is the group of org "a" site "b-c". Whoever
//     came second would read the first one's data with its own credentials.
var errTopicName = errors.New("topic name")

func topicNameError(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errTopicName}, args...)...)
}

func checkTopicSafe(kind, name string) error {
	if strings.ContainsAny(name, "+#\x00") || strings.HasPrefix(strings.TrimSpace(name), "$") {
		return topicNameError("the %s name cannot contain + or #, or start with $", kind)
	}
	if !mqtt.TopicSafe(naming.Slug(name)) {
		return topicNameError("the %s name is not valid", kind)
	}
	return nil
}

// checkTopicNames refuses an organization name and set of site names that
// would make this organization's topics another organization's. orgID is the
// organization being created (0) or changed.
func checkTopicNames(ctx context.Context, db *sql.DB, orgID int, orgName string, siteNames []string) error {
	if err := checkTopicSafe("organization", orgName); err != nil {
		return err
	}
	for _, s := range siteNames {
		if err := checkTopicSafe("site", s); err != nil {
			return err
		}
	}
	var other string
	err := db.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id <> $1 AND `+
		naming.SQL("name")+` = $2 LIMIT 1`, orgID, naming.Slug(orgName)).Scan(&other)
	if err == nil {
		return topicNameError("organization %q already uses the same topics; choose a different name", other)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	scope := mqtt.TenantScope{OrgName: orgName, SiteNames: siteNames}
	groups := scope.SparkplugGroups()
	if len(groups) == 0 {
		return nil
	}
	err = db.QueryRowContext(ctx, `
		SELECT o.name FROM sites s JOIN organizations o ON o.id = s.org_id
		WHERE o.id <> $1 AND `+naming.SQL("o.name")+` || '-' || `+naming.SQL("s.name")+` = ANY($2)
		LIMIT 1`, orgID, pq.Array(groups)).Scan(&other)
	if err == nil {
		return topicNameError("a site of organization %q already uses the same Sparkplug group; choose a different name", other)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// checkSiteTopicName checks the organization's sites as they would be with
// this one created (siteID 0) or renamed.
func (h *SitesHandler) checkSiteTopicName(c *gin.Context, orgID, siteID int, name string) error {
	ctx := c.Request.Context()
	var orgName string
	if err := h.db.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&orgName); err != nil {
		return err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT name FROM sites WHERE org_id = $1 AND id <> $2`, orgID, siteID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	sites := []string{name}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		sites = append(sites, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return checkTopicNames(ctx, h.db, orgID, orgName, sites)
}

// respondTopicNameError answers 409 for a name that is refused, 500 otherwise.
func respondTopicNameError(c *gin.Context, err error) {
	if errors.Is(err, errTopicName) {
		c.JSON(http.StatusConflict, gin.H{"error": strings.TrimPrefix(err.Error(), errTopicName.Error()+": ")})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check the name"})
}
