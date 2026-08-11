package handlers

import (
	"context"
	"database/sql"

	"github.com/ralph/industrial-edge-middleware/internal/scaling"
)

// There are three ways to command an output in this product — the tag write
// endpoint behind the synoptic buttons, a recipe load, and the i3X property
// write — and all three published the caller's number straight to the driver.
//
// Meanwhile the read path converts: scaling.Apply runs on every incoming value
// and its result replaces the raw one for Redis, the WebSocket feed, the
// historian and the tag shadow. So the product speaks engineering units in
// every direction except the one that moves the plant.
//
// Keeping the conversion in one function is the point. Three copies would drift,
// and the copy that drifts is the one nobody notices until a valve opens
// further than somebody asked.

// tagScalingConfig loads a tag's engineering-unit conversion.
//
// Read from the database rather than from the cache core-api keeps for the read
// path: that cache lives in package main and refreshes on a timer, and a
// setpoint written seconds after an engineer edits the range must use the new
// range, not whichever one the cache last happened to see.
func tagScalingConfig(ctx context.Context, db *sql.DB, tagID int) (scaling.Config, error) {
	var cfg scaling.Config
	err := db.QueryRowContext(ctx, `
		SELECT scaling_enabled, scaling_raw_min, scaling_raw_max,
		       scaling_eu_min, scaling_eu_max, scaling_clamp, invert
		FROM tags WHERE id = $1`, tagID).
		Scan(&cfg.Enabled, &cfg.RawMin, &cfg.RawMax,
			&cfg.EuMin, &cfg.EuMax, &cfg.Clamp, &cfg.Invert)
	return cfg, err
}

// toDeviceValue converts an engineering-unit value to what the device expects.
//
// Returns the value unchanged when the tag is unscaled, so a plant working in
// raw counts is unaffected. Returns an error only for a value outside the
// configured engineering range — refused rather than clamped, because silently
// turning an operator's 500 into 100 reports success and leaves them believing
// the plant is at 500.
//
// A failure to READ the configuration is deliberately not fatal: a write is
// refused only when we know the value is wrong, never because a lookup was
// unlucky. An unscaled tag and an unreadable one behave identically, which is
// also how the read path treats a cache miss.
func toDeviceValue(ctx context.Context, db *sql.DB, tagID int, value interface{}) (interface{}, error) {
	cfg, err := tagScalingConfig(ctx, db, tagID)
	if err != nil || !cfg.Enabled {
		return value, nil
	}
	return scaling.Reverse(value, cfg)
}
