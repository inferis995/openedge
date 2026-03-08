-- Migration 010: TimescaleDB historian
-- Replaces InfluxDB with TimescaleDB hypertable for time-series storage.
--
-- Schema design:
--   GOOD quality: value = actual float64, quality = 0
--   BAD quality:  value = NULL,           quality = 1
-- SQL aggregates (MAX, AVG, MIN…) naturally ignore NULL values, so BAD
-- readings are excluded from aggregations and all-BAD buckets return NULL,
-- which the chart renders as a visible gap.

-- Enable TimescaleDB (requires timescale/timescaledb image)
CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

-- ─── Main historian table ────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS tag_data (
    time    TIMESTAMPTZ NOT NULL,
    tag_id  INTEGER     NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    value   FLOAT8,                    -- NULL when quality is BAD
    quality SMALLINT    NOT NULL DEFAULT 0  -- 0 = GOOD, 1 = BAD
);

-- Convert to hypertable partitioned by day
SELECT create_hypertable(
    'tag_data', 'time',
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists       => TRUE
);

-- Primary access pattern: tag + time range descending
CREATE INDEX IF NOT EXISTS idx_tag_data_tag_time
    ON tag_data (tag_id, time DESC);

-- ─── System events table ─────────────────────────────────────────────────────
-- Replaces InfluxDB system_events measurement (gateway connect/disconnect).
CREATE TABLE IF NOT EXISTS system_events (
    time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    gateway_id INTEGER     NOT NULL,
    status     VARCHAR(20) NOT NULL,   -- 'online' | 'offline'
    message    TEXT
);

SELECT create_hypertable(
    'system_events', 'time',
    chunk_time_interval => INTERVAL '7 days',
    if_not_exists       => TRUE
);

CREATE INDEX IF NOT EXISTS idx_system_events_gw
    ON system_events (gateway_id, time DESC);
