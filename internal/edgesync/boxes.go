package edgesync

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// querier is the part of *sql.DB and *sql.Tx the loaders need.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

// LoadBoxes returns the boxes of one organization.
func LoadBoxes(ctx context.Context, q querier, orgID int) ([]Box, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, COALESCE(scope, 'assigned') FROM edge_agents WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, fmt.Errorf("loading the boxes of org %d: %w", orgID, err)
	}
	defer func() { _ = rows.Close() }()
	boxes := []Box{}
	for rows.Next() {
		var b Box
		if err := rows.Scan(&b.ID, &b.Scope); err != nil {
			return nil, fmt.Errorf("reading a box: %w", err)
		}
		boxes = append(boxes, b)
	}
	return boxes, rows.Err()
}

// LoadAllBoxes returns every box, grouped by organization. For the server's own
// driver-manager, which polls gateways of every organization it hosts.
func LoadAllBoxes(ctx context.Context, q querier) (map[int][]Box, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT org_id, id, COALESCE(scope, 'assigned') FROM edge_agents ORDER BY org_id, id`)
	if err != nil {
		return nil, fmt.Errorf("loading the boxes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byOrg := map[int][]Box{}
	for rows.Next() {
		var org int
		var b Box
		if err := rows.Scan(&org, &b.ID, &b.Scope); err != nil {
			return nil, fmt.Errorf("reading a box: %w", err)
		}
		byOrg[org] = append(byOrg[org], b)
	}
	return byOrg, rows.Err()
}

// ServerPollerSeenKey is the global_settings row the server's own
// driver-manager stamps, in Unix milliseconds, on every sync. It is how the
// platform tells an on-prem server that polls its PLCs from a cloud server that
// polls nothing — and therefore whether a gateway no box is responsible for is
// the server's, or nobody's.
const ServerPollerSeenKey = "server_poller_seen_at"

// MarkServerPolling records that the server's driver-manager is running.
func MarkServerPolling(ctx context.Context, db *sql.DB, at time.Time) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO global_settings (key, value, description) VALUES ($1, $2,
		  'Set by the server''s driver-manager on every sync. Not a setting: do not edit.')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		ServerPollerSeenKey, strconv.FormatInt(at.UnixMilli(), 10))
	return err
}
