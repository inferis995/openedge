package handlers

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
)

// Keeping the broker's per-tenant ACLs as narrow as the data allows.
//
// Sparkplug topics put the organization and the site in ONE level —
// spBv1.0/{org-slug}-{site-slug}/... — and MQTT wildcards match whole levels, so
// "+" cannot prefix-match "acme-*". An organization's Sparkplug namespace can
// therefore only be granted by naming each of its sites.
//
// Nothing did. Both roles were built at organization creation, when no site
// exists yet, and never rebuilt afterwards, so every deployment ran on the
// fallback grant: narrowed by message type, but with the group level left as a
// wildcard. One tenant could observe another tenant's live DDATA. The code said
// so — "closes as soon as siteNames is supplied" — and nothing ever supplied it.
//
// This is what supplies it. The moment a site appears, disappears or is renamed,
// both roles are rebuilt from the org's actual sites and the group level becomes
// exact.
func refreshOrgMQTTRoles(ctx context.Context, db *sql.DB, dynsec *mqtt.DynsecClient, orgID int) {
	if dynsec == nil || db == nil {
		return
	}

	var orgName string
	if err := db.QueryRowContext(ctx,
		`SELECT name FROM organizations WHERE id = $1`, orgID,
	).Scan(&orgName); err != nil {
		slog.Warn("mqtt roles: could not read the organization", "org_id", orgID, "error", err)
		return
	}

	sites, err := orgSiteNames(ctx, db, orgID)
	if err != nil {
		slog.Warn("mqtt roles: could not read the organization's sites",
			"org_id", orgID, "error", err)
		return
	}

	// Never fatal to the caller. A site rename must not fail because the broker
	// is briefly unreachable — the grant stays as it was, which is wider than we
	// want but not broken, and the next change retries.
	if err := dynsec.UpdateOrgRole(orgID, orgName, sites...); err != nil {
		slog.Warn("mqtt roles: could not refresh the edge role",
			"org_id", orgID, "sites", len(sites), "error", err)
	}
	if err := dynsec.UpdateOrgViewerRole(orgID, orgName, sites...); err != nil {
		slog.Warn("mqtt roles: could not refresh the web UI role",
			"org_id", orgID, "sites", len(sites), "error", err)
	}
}

// orgSiteNames returns the organization's site names, which are what pin the
// Sparkplug group level.
func orgSiteNames(ctx context.Context, db *sql.DB, orgID int) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sites WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
