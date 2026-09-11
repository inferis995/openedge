package edgesync

import (
	"context"
	"database/sql"
	"fmt"
)

// Apply mirrors a configuration into the box's own database.
//
// One transaction: a box that applied half a configuration would start drivers
// for gateways whose tags had not arrived yet, and poll nothing while
// reporting itself healthy.
//
// Everything is written with the central identifiers, and anything no longer in
// the payload is removed. That second half matters as much as the first: a
// gateway deleted centrally has to stop being polled, and a tag removed has to
// stop being published, or the box goes on sending data about equipment that
// officially does not exist.
func Apply(ctx context.Context, db *sql.DB, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("no configuration to apply")
	}
	if cfg.OrgID <= 0 {
		// A configuration with no organization would delete every site on the
		// box, because the delete below is scoped by it.
		return fmt.Errorf("the configuration names no organization")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("opening the transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	if err := applyOrg(ctx, tx, cfg); err != nil {
		return err
	}
	if err := applyHierarchy(ctx, tx, cfg); err != nil {
		return err
	}
	if err := applyGateways(ctx, tx, cfg); err != nil {
		return err
	}
	if err := applyTags(ctx, tx, cfg); err != nil {
		return err
	}
	if err := applyAlarms(ctx, tx, cfg); err != nil {
		return err
	}
	if err := pruneSequences(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the configuration: %w", err)
	}
	return nil
}

func applyOrg(ctx context.Context, tx *sql.Tx, cfg *Config) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO organizations (id, name) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`,
		cfg.OrgID, cfg.OrgName)
	if err != nil {
		return fmt.Errorf("writing the organization: %w", err)
	}
	return nil
}

func applyHierarchy(ctx context.Context, tx *sql.Tx, cfg *Config) error {
	for i := range cfg.Sites {
		s := &cfg.Sites[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sites (id, org_id, name) VALUES ($1, $2, $3)
			ON CONFLICT (id) DO UPDATE SET org_id = EXCLUDED.org_id, name = EXCLUDED.name`,
			s.ID, s.OrgID, s.Name); err != nil {
			return fmt.Errorf("writing site %d: %w", s.ID, err)
		}
	}
	for i := range cfg.Areas {
		a := &cfg.Areas[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO areas (id, site_id, name) VALUES ($1, $2, $3)
			ON CONFLICT (id) DO UPDATE SET site_id = EXCLUDED.site_id, name = EXCLUDED.name`,
			a.ID, a.SiteID, a.Name); err != nil {
			return fmt.Errorf("writing area %d: %w", a.ID, err)
		}
	}
	return nil
}

func applyGateways(ctx context.Context, tx *sql.Tx, cfg *Config) error {
	for i := range cfg.Gateways {
		g := &cfg.Gateways[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO gateways (id, area_id, name, driver_type, connection_config, scan_rate_ms, enabled, zero_based)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET
				area_id = EXCLUDED.area_id, name = EXCLUDED.name,
				driver_type = EXCLUDED.driver_type,
				connection_config = EXCLUDED.connection_config,
				scan_rate_ms = EXCLUDED.scan_rate_ms,
				enabled = EXCLUDED.enabled, zero_based = EXCLUDED.zero_based`,
			g.ID, g.AreaID, g.Name, g.DriverType, g.ConnectionConfig,
			g.ScanRateMs, g.Enabled, g.ZeroBased); err != nil {
			return fmt.Errorf("writing gateway %d: %w", g.ID, err)
		}
	}
	return deleteMissing(ctx, tx, cfg.OrgID,
		`DELETE FROM gateways g
		  USING areas a, sites s
		  WHERE a.id = g.area_id AND s.id = a.site_id AND s.org_id = $1
		    AND NOT (g.id = ANY($2))`,
		ids(len(cfg.Gateways), func(i int) int { return cfg.Gateways[i].ID }),
		"gateways")
}

func applyTags(ctx context.Context, tx *sql.Tx, cfg *Config) error {
	for i := range cfg.Tags {
		t := &cfg.Tags[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tags (id, gateway_id, code, alias, data_type, historize,
			                  historize_deadband, sort_order, json_path,
			                  scaling_enabled, scaling_raw_min, scaling_raw_max,
			                  scaling_eu_min, scaling_eu_max, scaling_clamp,
			                  eu_unit, eu_decimals, invert)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
			ON CONFLICT (id) DO UPDATE SET
				gateway_id = EXCLUDED.gateway_id, code = EXCLUDED.code,
				alias = EXCLUDED.alias, data_type = EXCLUDED.data_type,
				historize = EXCLUDED.historize,
				historize_deadband = EXCLUDED.historize_deadband,
				sort_order = EXCLUDED.sort_order, json_path = EXCLUDED.json_path,
				scaling_enabled = EXCLUDED.scaling_enabled,
				scaling_raw_min = EXCLUDED.scaling_raw_min,
				scaling_raw_max = EXCLUDED.scaling_raw_max,
				scaling_eu_min = EXCLUDED.scaling_eu_min,
				scaling_eu_max = EXCLUDED.scaling_eu_max,
				scaling_clamp = EXCLUDED.scaling_clamp,
				eu_unit = EXCLUDED.eu_unit, eu_decimals = EXCLUDED.eu_decimals,
				invert = EXCLUDED.invert`,
			t.ID, t.GatewayID, t.Code, t.Alias, t.DataType, t.Historize,
			t.HistorizeDeadband, t.SortOrder, t.JsonPath,
			t.ScalingEnabled, t.ScalingRawMin, t.ScalingRawMax,
			t.ScalingEuMin, t.ScalingEuMax, t.ScalingClamp,
			t.EuUnit, t.EuDecimals, t.Invert); err != nil {
			return fmt.Errorf("writing tag %d: %w", t.ID, err)
		}
	}
	return deleteMissing(ctx, tx, cfg.OrgID,
		`DELETE FROM tags t
		  USING gateways g, areas a, sites s
		  WHERE g.id = t.gateway_id AND a.id = g.area_id AND s.id = a.site_id
		    AND s.org_id = $1 AND NOT (t.id = ANY($2))`,
		ids(len(cfg.Tags), func(i int) int { return cfg.Tags[i].ID }),
		"tags")
}

func applyAlarms(ctx context.Context, tx *sql.Tx, cfg *Config) error {
	for i := range cfg.Alarms {
		a := &cfg.Alarms[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alarm_definitions (id, tag_id, alarm_type, threshold, deadband,
			                               delay_seconds, severity, message, enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (id) DO UPDATE SET
				tag_id = EXCLUDED.tag_id, alarm_type = EXCLUDED.alarm_type,
				threshold = EXCLUDED.threshold, deadband = EXCLUDED.deadband,
				delay_seconds = EXCLUDED.delay_seconds,
				severity = EXCLUDED.severity, message = EXCLUDED.message,
				enabled = EXCLUDED.enabled`,
			a.ID, a.TagID, a.AlarmType, a.Threshold, a.Deadband,
			a.DelaySeconds, a.Severity, a.Message, a.Enabled); err != nil {
			return fmt.Errorf("writing alarm %d: %w", a.ID, err)
		}
	}
	return deleteMissing(ctx, tx, cfg.OrgID,
		`DELETE FROM alarm_definitions ad
		  USING tags t, gateways g, areas a, sites s
		  WHERE t.id = ad.tag_id AND g.id = t.gateway_id AND a.id = g.area_id
		    AND s.id = a.site_id AND s.org_id = $1 AND NOT (ad.id = ANY($2))`,
		ids(len(cfg.Alarms), func(i int) int { return cfg.Alarms[i].ID }),
		"alarm definitions")
}

// deleteMissing removes the rows of this organization that the payload no
// longer mentions.
//
// The empty list is passed as an empty array rather than skipped: an
// organization whose last gateway was deleted centrally must end up with no
// gateways on the box, and skipping the delete when the list is empty is
// exactly the case where the box keeps polling forever.
func deleteMissing(ctx context.Context, tx *sql.Tx, orgID int, query string, keep []int64, what string) error {
	if _, err := tx.ExecContext(ctx, query, orgID, pqInt64Array(keep)); err != nil {
		return fmt.Errorf("removing the %s that no longer exist: %w", what, err)
	}
	return nil
}

// pruneSequences moves every sequence past the identifiers just written.
//
// The rows arrive with the central platform's identifiers, which the local
// sequences know nothing about. Anything the box inserts by itself afterwards —
// an alarm event, a history row referencing a tag — would otherwise collide
// with an identifier the central platform has already used.
func pruneSequences(ctx context.Context, tx *sql.Tx) error {
	for _, t := range []string{"organizations", "sites", "areas", "gateways", "tags", "alarm_definitions"} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			SELECT setval(pg_get_serial_sequence('%s', 'id'),
			              GREATEST((SELECT COALESCE(MAX(id), 0) FROM %s), 1))`, t, t)); err != nil {
			return fmt.Errorf("moving the %s sequence past the central identifiers: %w", t, err)
		}
	}
	return nil
}

func ids(n int, at func(int) int) []int64 {
	out := make([]int64, n)
	for i := 0; i < n; i++ {
		out[i] = int64(at(i))
	}
	return out
}
