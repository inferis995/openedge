package models

import "database/sql"

// DriverTagColumns is what a driver needs to know about a tag in order to read
// it and publish it, as a SELECT list.
//
// It is here rather than in each driver because it was in each driver: six
// copies of the same list, and when the engineering-unit columns were added to
// the table none of the six were updated. The drivers went on selecting seven
// columns and publishing raw counts, while the rest of the product assumed —
// as the comment on Tag.ScalingEnabled still says — that conversion happened
// at ingestion.
//
// COALESCE is defensive: the columns are NOT NULL today, but a tag row
// restored from an older dump through a path that skips the migrations would
// otherwise fail the scan and take the whole gateway's tag list with it.
const DriverTagColumns = `id, gateway_id, code, alias, data_type, historize,
	COALESCE(historize_deadband, 0),
	COALESCE(scaling_enabled, false),
	COALESCE(scaling_raw_min, 0), COALESCE(scaling_raw_max, 0),
	COALESCE(scaling_eu_min, 0), COALESCE(scaling_eu_max, 0),
	COALESCE(scaling_clamp, false), COALESCE(invert, false)`

// ScanDriverTag reads one row selected with DriverTagColumns.
//
// The destinations are in the same order as the column list and must stay that
// way. Crossing two of them over is not a compile error and not a scan error
// when the types line up — scaling_raw_min and scaling_raw_max are both
// DOUBLE PRECISION, and swapping them inverts every reading of that tag while
// producing numbers in the right range.
// extra receives any columns a particular driver appends to the list, in the
// order it appended them — driver-mqtt selects json_path as well.
func ScanDriverTag(rows *sql.Rows, t *Tag, extra ...interface{}) error {
	dest := []interface{}{
		&t.ID, &t.GatewayID, &t.Code, &t.Alias, &t.DataType, &t.Historize,
		&t.HistorizeDeadband,
		&t.ScalingEnabled,
		&t.ScalingRawMin, &t.ScalingRawMax,
		&t.ScalingEuMin, &t.ScalingEuMax,
		&t.ScalingClamp, &t.Invert,
	}
	return rows.Scan(append(dest, extra...)...)
}
