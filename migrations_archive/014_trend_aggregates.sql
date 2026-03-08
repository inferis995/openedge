-- Migration 014: Continuous Aggregates for Trend Performance
-- Creates hierarchical aggregates: raw -> 1m -> 1h -> 1d
-- All aggregates query the raw hypertable directly for TimescaleDB compatibility

-- ============================================================================
-- 1-MINUTE CONTINUOUS AGGREGATE
-- Purpose: Pre-aggregated data for 1-6 hour time ranges
-- ============================================================================

CREATE MATERIALIZED VIEW IF NOT EXISTS tag_history_1m
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 minute', time) AS bucket,
    tag_id,
    AVG(value) AS avg_value,
    MIN(value) AS min_value,
    MAX(value) AS max_value,
    LAST(value, time) AS last_value,
    COUNT(*) AS sample_count,
    0 AS quality  -- Aggregated data is always GOOD quality
FROM tag_history
GROUP BY bucket, tag_id
WITH NO DATA;

-- Add refresh policy for 1m aggregate
SELECT add_continuous_aggregate_policy('tag_history_1m',
    start_offset => INTERVAL '2 hours',
    end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '1 minute',
    if_not_exists => TRUE
);

-- Enable real-time aggregation
ALTER MATERIALIZED VIEW tag_history_1m SET (
    timescaledb.materialized_only = false
);

-- Indexes for 1m aggregate
CREATE INDEX IF NOT EXISTS idx_tag_history_1m_tag_bucket
    ON tag_history_1m (tag_id, bucket DESC);

-- ============================================================================
-- 1-HOUR CONTINUOUS AGGREGATE
-- Purpose: Pre-aggregated data for 6 hour - 7 day time ranges
-- Note: Queries raw hypertable directly (not 1m aggregate) for compatibility
-- ============================================================================

CREATE MATERIALIZED VIEW IF NOT EXISTS tag_history_1h
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    tag_id,
    AVG(value) AS avg_value,
    MIN(value) AS min_value,
    MAX(value) AS max_value,
    LAST(value, time) AS last_value,
    COUNT(*) AS sample_count,
    0 AS quality
FROM tag_history
GROUP BY bucket, tag_id
WITH NO DATA;

-- Add refresh policy for 1h aggregate
SELECT add_continuous_aggregate_policy('tag_history_1h',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '5 minutes',
    if_not_exists => TRUE
);

-- Enable real-time aggregation
ALTER MATERIALIZED VIEW tag_history_1h SET (
    timescaledb.materialized_only = false
);

-- Indexes for 1h aggregate
CREATE INDEX IF NOT EXISTS idx_tag_history_1h_tag_bucket
    ON tag_history_1h (tag_id, bucket DESC);

-- ============================================================================
-- 1-DAY CONTINUOUS AGGREGATE
-- Purpose: Pre-aggregated data for 7+ day time ranges
-- Note: Queries raw hypertable directly for compatibility
-- ============================================================================

CREATE MATERIALIZED VIEW IF NOT EXISTS tag_history_1d
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    tag_id,
    AVG(value) AS avg_value,
    MIN(value) AS min_value,
    MAX(value) AS max_value,
    LAST(value, time) AS last_value,
    COUNT(*) AS sample_count,
    0 AS quality
FROM tag_history
GROUP BY bucket, tag_id
WITH NO DATA;

-- Add refresh policy for 1d aggregate
SELECT add_continuous_aggregate_policy('tag_history_1d',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => TRUE
);

-- Enable real-time aggregation
ALTER MATERIALIZED VIEW tag_history_1d SET (
    timescaledb.materialized_only = false
);

-- Indexes for 1d aggregate
CREATE INDEX IF NOT EXISTS idx_tag_history_1d_tag_bucket
    ON tag_history_1d (tag_id, bucket DESC);

-- ============================================================================
-- COMPRESSION POLICIES
-- ============================================================================
-- Note: Continuous aggregates are automatically compressed by TimescaleDB
-- We only need to set compression on the raw tag_history hypertable

-- Compress raw data after 7 days
ALTER TABLE tag_history SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'tag_id',
    timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('tag_history', INTERVAL '7 days', if_not_exists => TRUE);

-- Continuous aggregates (tag_history_1m, tag_history_1h, tag_history_1d)
-- are automatically compressed by TimescaleDB - no manual compression needed

-- ============================================================================
-- RETENTION POLICIES
-- ============================================================================

-- Keep raw data for 90 days
SELECT add_retention_policy('tag_history', INTERVAL '90 days', if_not_exists => TRUE);

-- Note: Continuous aggregates automatically inherit retention behavior
-- No explicit retention policies needed on tag_history_1m, tag_history_1h, tag_history_1d

-- ============================================================================
-- HELPER FUNCTION: Get tag statistics for time range
-- ============================================================================

CREATE OR REPLACE FUNCTION get_tag_stats(
    p_tag_id INTEGER,
    p_start TIMESTAMPTZ,
    p_end TIMESTAMPTZ
)
RETURNS TABLE (
    min_value DOUBLE PRECISION,
    max_value DOUBLE PRECISION,
    avg_value DOUBLE PRECISION,
    std_dev DOUBLE PRECISION,
    sample_count BIGINT,
    first_value DOUBLE PRECISION,
    last_value DOUBLE PRECISION,
    first_timestamp BIGINT,
    last_timestamp BIGINT
)
LANGUAGE plpgsql STABLE
AS $$
BEGIN
    RETURN QUERY
    WITH stats AS (
        SELECT
            MIN(value) AS min_val,
            MAX(value) AS max_val,
            AVG(value) AS avg_val,
            COALESCE(STDDEV(value), 0) AS std_val,
            COUNT(*) AS cnt,
            FIRST_VALUE(value ORDER BY time) AS first_val,
            LAST_VALUE(value ORDER BY time) AS last_val,
            EXTRACT(EPOCH FROM FIRST_VALUE(time ORDER BY time))::BIGINT * 1000 AS first_ts,
            EXTRACT(EPOCH FROM LAST_VALUE(time ORDER BY time))::BIGINT * 1000 AS last_ts
        FROM tag_history
        WHERE tag_id = p_tag_id
          AND time >= p_start
          AND time <= p_end
    )
    SELECT
        min_val::DOUBLE PRECISION,
        max_val::DOUBLE PRECISION,
        avg_val::DOUBLE PRECISION,
        std_val::DOUBLE PRECISION,
        cnt,
        first_val::DOUBLE PRECISION,
        last_val::DOUBLE PRECISION,
        first_ts,
        last_ts
    FROM stats;
END;
$$;

-- ============================================================================
-- COMMENTS
-- ============================================================================

-- Note: Comments are omitted to avoid issues with TimescaleDB continuous aggregates
-- which are created as materialized views but accessed as tables
