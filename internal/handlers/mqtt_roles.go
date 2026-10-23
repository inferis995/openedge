package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"time"

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
// exact; MQTTRoleReconciler does the same for the gateways, and for every
// organization at start-up.
func refreshOrgMQTTRoles(ctx context.Context, db *sql.DB, dynsec *mqtt.DynsecClient, orgID int) {
	if dynsec == nil || db == nil {
		return
	}
	scope, err := orgMQTTScope(ctx, db, orgID)
	if err != nil {
		slog.Warn("mqtt roles: could not read the organization", "org_id", orgID, "error", err)
		return
	}
	// Never fatal to the caller. A site rename must not fail because the broker
	// is briefly unreachable — the reconciler retries.
	if err := applyOrgMQTTRoles(dynsec, scope); err != nil {
		slog.Warn("mqtt roles: could not refresh", "org_id", orgID, "error", err)
	}
}

func applyOrgMQTTRoles(dynsec *mqtt.DynsecClient, scope mqtt.TenantScope) error {
	if err := dynsec.UpdateOrgRole(scope); err != nil {
		return fmt.Errorf("edge role: %w", err)
	}
	if err := dynsec.UpdateOrgViewerRole(scope); err != nil {
		return fmt.Errorf("web UI role: %w", err)
	}
	return nil
}

// orgMQTTScope reads what an organization's broker roles are built from: its
// name, its sites (they pin the Sparkplug group level) and its gateways (they
// pin the topics keyed by gateway id).
func orgMQTTScope(ctx context.Context, db *sql.DB, orgID int) (mqtt.TenantScope, error) {
	s := mqtt.TenantScope{OrgID: orgID}
	if err := db.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&s.OrgName); err != nil {
		return s, err
	}
	sites, err := orgSiteNames(ctx, db, orgID)
	if err != nil {
		return s, err
	}
	s.SiteNames = sites
	rows, err := db.QueryContext(ctx, `
		SELECT g.id FROM gateways g
		JOIN areas a ON a.id = g.area_id
		JOIN sites si ON si.id = a.site_id
		WHERE si.org_id = $1 ORDER BY g.id`, orgID)
	if err != nil {
		return s, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return s, err
		}
		s.GatewayIDs = append(s.GatewayIDs, id)
	}
	return s, rows.Err()
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

// MQTTRoleReconciler keeps every organization's broker roles equal to what
// its scope calls for.
//
// The site handlers rebuild the roles on the spot, but gateways come and go
// through more paths than one handler (the API, the setup wizard, LoRaWAN
// discovery, a cascade from a deleted area), and a role that is not rebuilt
// keeps the gateway topics of a gateway that moved, or lacks those of a new
// one. So this compares each organization's scope with the one last applied,
// every minute, and rebuilds what changed. At start-up nothing has been
// applied yet, so every role is rebuilt once: an installation whose roles
// were made by an older, wider builder is corrected on its first restart.
type MQTTRoleReconciler struct {
	db      *sql.DB
	dynsec  *mqtt.DynsecClient
	applied map[int]string
	lastErr map[int]string
}

// NewMQTTRoleReconciler returns a reconciler; Run starts it.
func NewMQTTRoleReconciler(db *sql.DB, dynsec *mqtt.DynsecClient) *MQTTRoleReconciler {
	return &MQTTRoleReconciler{db: db, dynsec: dynsec, applied: map[int]string{}, lastErr: map[int]string{}}
}

// Run reconciles now and then every interval, until ctx is done.
func (r *MQTTRoleReconciler) Run(ctx context.Context, every time.Duration) {
	if r == nil || r.dynsec == nil || r.db == nil {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		r.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once reconciles every organization that has broker credentials.
func (r *MQTTRoleReconciler) Once(ctx context.Context) {
	rows, err := r.db.QueryContext(ctx, `SELECT org_id FROM org_mqtt_credentials ORDER BY org_id`)
	if err != nil {
		slog.Warn("mqtt roles: could not list organizations", "error", err)
		return
	}
	var orgs []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			orgs = append(orgs, id)
		}
	}
	_ = rows.Close()
	for _, id := range orgs {
		scope, err := orgMQTTScope(ctx, r.db, id)
		if err != nil {
			continue
		}
		fp := scopeFingerprint(scope)
		if r.applied[id] == fp {
			continue
		}
		if err := applyOrgMQTTRoles(r.dynsec, scope); err != nil {
			// Logged once per distinct error, retried every pass.
			if r.lastErr[id] != err.Error() {
				r.lastErr[id] = err.Error()
				slog.Warn("mqtt roles: could not rebuild", "org_id", id, "error", err)
			}
			continue
		}
		delete(r.lastErr, id)
		r.applied[id] = fp
		slog.Info("mqtt roles: rebuilt", "org_id", id, "sites", len(scope.SiteNames), "gateways", len(scope.GatewayIDs))
	}
}

func scopeFingerprint(s mqtt.TenantScope) string {
	sites := append([]string(nil), s.SiteNames...)
	sort.Strings(sites)
	return fmt.Sprintf("%s\x00%q\x00%v", s.OrgName, sites, s.GatewayIDs)
}
