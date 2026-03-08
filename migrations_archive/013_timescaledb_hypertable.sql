-- Migration 013: Enable TimescaleDB and convert tag_history to hypertable
-- This provides optimized time-series storage with automatic partitioning

-- Enable TimescaleDB extension (must be first)
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Drop existing indexes (will be recreated automatically by hypertable)
DROP INDEX IF EXISTS idx_tag_history_tag_time;
DROP INDEX IF EXISTS idx_tag_history_time;

-- Remove primary key (hypertables use time as partition key)
ALTER TABLE tag_history DROP CONSTRAINT IF EXISTS tag_history_pkey;

-- Create a composite primary key that includes time (required for hypertables)
ALTER TABLE tag_history ADD CONSTRAINT tag_history_pkey PRIMARY KEY (time, id);

-- Convert to hypertable (partition by time)
-- chunk_time_interval: 1 day partitions (good balance for query performance)
SELECT create_hypertable(
    'tag_history',
    'time',
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

-- Re-create optimized indexes for hypertable
CREATE INDEX IF NOT EXISTS idx_tag_history_tag_time ON tag_history (tag_id, time DESC);
CREATE INDEX IF NOT EXISTS idx_tag_history_time ON tag_history (time DESC);

-- Enable compression (saves ~90% storage for time-series data)
ALTER TABLE tag_history SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'tag_id',
    timescaledb.compress_orderby = 'time DESC'
);

-- Add compression policy (compress chunks older than 7 days)
SELECT add_compression_policy('tag_history', INTERVAL '7 days', if_not_exists => TRUE);

-- Add retention policy (keep data for 1 year, then drop)
-- Automatic data cleanup to prevent disk exhaustion in production
SELECT add_retention_policy('tag_history', INTERVAL '1 year', if_not_exists => TRUE);

-- Also convert system_events to hypertable if it has data
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'system_events') THEN
        -- Check if it's already a hypertable
        IF NOT EXISTS (SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_name = 'system_events') THEN
            ALTER TABLE system_events DROP CONSTRAINT IF EXISTS system_events_pkey;
            ALTER TABLE system_events ADD CONSTRAINT system_events_pkey PRIMARY KEY (time, id);
            PERFORM create_hypertable('system_events', 'time', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE);
        END IF;
    END IF;
END $$;

-- Comment
COMMENT ON TABLE tag_history IS 'TimescaleDB hypertable: Time-series historian with automatic compression';
