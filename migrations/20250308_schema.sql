-- ============================================================================
-- OpenEdge Industrial Database - Complete Schema (Version 1)
--
-- This is a CLEAN schema that fixes all known issues:
-- 1. NO tag_data zombie table (only tag_history is used)
-- 2. global_settings is pure KEY-VALUE store (no extra columns)
-- 3. system_events has proper FK to gateways
--
-- Use this for NEW installations only.
-- ============================================================================

-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ============================================================================
-- ORGANIZATIONAL HIERARCHY
-- ============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sites (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS areas (
    id SERIAL PRIMARY KEY,
    site_id INT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS gateways (
    id SERIAL PRIMARY KEY,
    area_id INT NOT NULL REFERENCES areas(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    driver_type VARCHAR(20) NOT NULL CHECK (driver_type IN ('S7', 'MODBUS_TCP', 'MQTT', 'OPC_UA', 'LORAWAN')),
    connection_config JSONB NOT NULL,
    scan_rate_ms INT DEFAULT 1000,
    enabled BOOLEAN DEFAULT TRUE,
    zero_based BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tags (
    id SERIAL PRIMARY KEY,
    gateway_id INT NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
    code VARCHAR(500) NOT NULL,
    alias VARCHAR(100) NOT NULL,
    data_type VARCHAR(20) NOT NULL CHECK (data_type IN ('INT', 'REAL', 'BOOL', 'DINT', 'STRING')),
    historize BOOLEAN DEFAULT FALSE,
    historize_deadband FLOAT DEFAULT 0.0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    sort_order FLOAT DEFAULT 0.0
);

-- ============================================================================
-- AUTHENTICATION & AUDIT
-- ============================================================================

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(20) NOT NULL CHECK (role IN ('admin', 'user')),
    full_name TEXT,
    org_id INT REFERENCES organizations(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
COMMENT ON COLUMN users.org_id IS 'Organization ID. NULL for global admin (access all organizations). INT for regular users (scoped to one org)';

CREATE TABLE IF NOT EXISTS access_logs (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE SET NULL,
    username TEXT NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE SET NULL,
    username VARCHAR(255),
    action VARCHAR(50) NOT NULL,
    ip_address VARCHAR(45),
    user_agent TEXT,
    details JSONB,
    success BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================================
-- SYSTEM CONFIGURATION
-- ============================================================================

CREATE TABLE IF NOT EXISTS global_settings (
    key VARCHAR(50) PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    updated_by INT REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS backup_settings (
    id INTEGER PRIMARY KEY DEFAULT 1,
    enabled BOOLEAN DEFAULT FALSE,
    interval_hours VARCHAR(10) DEFAULT '24h',
    backup_type VARCHAR(10) DEFAULT 'config',
    retention_days INTEGER DEFAULT 7,
    last_run TIMESTAMPTZ,
    last_status VARCHAR(20)
);

-- ============================================================================
-- TIME-SERIES HISTORIAN (TimescaleDB)
-- ============================================================================

-- Main historian table (hypertable)
CREATE TABLE IF NOT EXISTS tag_history (
    id BIGSERIAL,
    time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tag_id INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    value FLOAT8,
    source VARCHAR(20) DEFAULT 'mqtt',
    PRIMARY KEY (time, id)
);

-- Convert to hypertable
SELECT create_hypertable('tag_history', 'time',
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

-- Indexes for tag_history
CREATE INDEX IF NOT EXISTS idx_tag_history_tag_time ON tag_history (tag_id, time DESC);
CREATE INDEX IF NOT EXISTS idx_tag_history_time ON tag_history (time DESC);

-- Enable compression
ALTER TABLE tag_history SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'tag_id',
    timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('tag_history', INTERVAL '7 days', if_not_exists => TRUE);

-- Retention is NOT set here.
--
-- It used to be, at 90 days, and that number won: this file runs at initdb,
-- before core-api has ever started, and if_not_exists meant the application
-- could not correct it afterwards. The application seeds
-- historian_retention_days = 365 and shows that figure in the UI, so the
-- database quietly discarded nine months of history the operator believed was
-- retained — invisibly, because the fallback DELETE at 365 days then found
-- nothing to remove, which is also what a correctly-aged table returns.
--
-- The retention window is an operator setting, so it belongs to the process
-- that can read that setting and re-apply it when it changes:
-- db.EnsureRetentionPolicies, called on every core-api start. Do not add a
-- policy here.

COMMENT ON TABLE tag_history IS 'TimescaleDB hypertable: Time-series historian with automatic compression';
COMMENT ON COLUMN tag_history.value IS 'Actual tag value. NULL = offline/bad-quality marker (source=''offline'') that the query layer turns into a chart gap.';

-- ============================================================================
-- CONTINUOUS AGGREGATES (Trend Performance)
-- ============================================================================

-- 1-minute aggregate
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
    0 AS quality
FROM tag_history
GROUP BY bucket, tag_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('tag_history_1m',
    start_offset => INTERVAL '2 hours',
    end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '1 minute',
    if_not_exists => TRUE
);

ALTER MATERIALIZED VIEW tag_history_1m SET (timescaledb.materialized_only = false);
CREATE INDEX IF NOT EXISTS idx_tag_history_1m_tag_bucket ON tag_history_1m (tag_id, bucket DESC);

-- 1-hour aggregate
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

SELECT add_continuous_aggregate_policy('tag_history_1h',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '5 minutes',
    if_not_exists => TRUE
);

ALTER MATERIALIZED VIEW tag_history_1h SET (timescaledb.materialized_only = false);
CREATE INDEX IF NOT EXISTS idx_tag_history_1h_tag_bucket ON tag_history_1h (tag_id, bucket DESC);

-- 1-day aggregate
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

SELECT add_continuous_aggregate_policy('tag_history_1d',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => TRUE
);

ALTER MATERIALIZED VIEW tag_history_1d SET (timescaledb.materialized_only = false);
CREATE INDEX IF NOT EXISTS idx_tag_history_1d_tag_bucket ON tag_history_1d (tag_id, bucket DESC);

-- ============================================================================
-- SYSTEM EVENTS (Gateway online/offline)
-- ============================================================================

CREATE TABLE IF NOT EXISTS system_events (
    id BIGSERIAL,
    time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    gateway_id INT NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL,
    message TEXT
);

SELECT create_hypertable('system_events', 'time',
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

CREATE INDEX IF NOT EXISTS idx_system_events_gw ON system_events (gateway_id, time DESC);
CREATE INDEX IF NOT EXISTS system_events_time_idx ON system_events (time DESC);

-- ============================================================================
-- ALARM SYSTEM
-- ============================================================================

CREATE TABLE IF NOT EXISTS alarm_definitions (
    id SERIAL PRIMARY KEY,
    tag_id INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    alarm_type VARCHAR(20) NOT NULL,
    threshold DOUBLE PRECISION,
    deadband DOUBLE PRECISION DEFAULT 0,
    delay_seconds INT DEFAULT 0,
    severity VARCHAR(20) DEFAULT 'warning',
    message TEXT,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_alarm_def_tag ON alarm_definitions(tag_id);

CREATE TABLE IF NOT EXISTS alarm_events (
    id SERIAL,
    tag_id INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    definition_id INT REFERENCES alarm_definitions(id) ON DELETE SET NULL,
    status VARCHAR(50) NOT NULL,
    alarm_type VARCHAR(50) NOT NULL,
    severity VARCHAR(50) NOT NULL,
    message TEXT,
    value_at_trigger DOUBLE PRECISION,
    trigger_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    clear_time TIMESTAMPTZ,
    bg_ack_user VARCHAR(100),
    ack_time TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_alarm_events_status ON alarm_events(status);
CREATE INDEX IF NOT EXISTS idx_alarm_events_tag ON alarm_events(tag_id);
CREATE INDEX IF NOT EXISTS idx_alarm_events_time ON alarm_events(trigger_time DESC);
CREATE INDEX IF NOT EXISTS idx_alarm_events_id ON alarm_events(id);

SELECT create_hypertable('alarm_events', 'trigger_time', if_not_exists => TRUE);

-- ============================================================================
-- HELPER FUNCTION: Get tag statistics
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
-- INDEXES
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_sites_org_id ON sites(org_id);
CREATE INDEX IF NOT EXISTS idx_areas_site_id ON areas(site_id);
CREATE INDEX IF NOT EXISTS idx_gateways_area_id ON gateways(area_id);
CREATE INDEX IF NOT EXISTS idx_gateways_enabled ON gateways(enabled) WHERE enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_tags_gateway_id ON tags(gateway_id);
CREATE INDEX IF NOT EXISTS idx_tags_sort_order ON tags(sort_order);
CREATE INDEX IF NOT EXISTS idx_users_org_id ON users(org_id);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_access_logs_user_id ON access_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_access_logs_created_at ON access_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_details_org ON audit_logs((details->>'org_id'));

-- ============================================================================
-- SEED DATA
-- ============================================================================

-- Default organization
INSERT INTO organizations (name)
VALUES ('Default')
ON CONFLICT (name) DO NOTHING;

-- Admin user (password: admin123)
-- The initial admin is NOT seeded here on purpose.
--
-- This file runs from docker-entrypoint-initdb.d on a fresh volume, so a
-- hardcoded hash here would win the race against the application and every
-- installation would ship the same well-known password — including those where
-- the operator set OPENEDGE_INITIAL_ADMIN_PASSWORD.
--
-- The admin user is created at startup by bootstrapAdminIfMissing()
-- (internal/db/db.go), which honours OPENEDGE_INITIAL_ADMIN_PASSWORD and warns
-- loudly when it falls back to the default.

-- Global settings (pure KEY-VALUE store)
INSERT INTO global_settings (key, value, description) VALUES
    ('publish_mode', 'dual', 'Modalita pubblicazione MQTT: dual, sparkplug_only, legacy_only'),
    ('rbe_heartbeat_seconds', '60', 'Intervallo heartbeat in secondi per Sparkplug B RBE'),
    ('rbe_deadband_percent', '0.5', 'Soglia deadband percentuale per valori analogici'),
    ('mqtt_broker_mode', 'internal', 'Internal Mosquitto or external broker'),
    ('mqtt_external_host', '', 'External MQTT broker hostname'),
    ('mqtt_external_port', '1883', 'External MQTT broker port'),
    ('mqtt_username', '', 'MQTT broker username (empty = no auth)'),
    ('mqtt_password', '', 'MQTT broker password'),
    ('mqtt_client_id', 'industrial-edge', 'Client ID for MQTT connection'),
    ('db_retention_days', '90', 'Days to keep historical tag data (0 = infinite)'),
    ('cloud_sync_enabled', 'false', 'Enable Cloud MQTT forwarding'),
    ('cloud_mqtt_host', '', 'Cloud MQTT broker hostname'),
    ('cloud_mqtt_port', '1883', 'Cloud MQTT broker port'),
    ('cloud_mqtt_username', '', 'Cloud MQTT username'),
    ('cloud_mqtt_password', '', 'Cloud MQTT password'),
    ('cloud_mqtt_topic', '', 'Cloud MQTT base topic prefix')
ON CONFLICT (key) DO NOTHING;

-- Backup settings
INSERT INTO backup_settings (id, enabled, interval_hours, backup_type, retention_days)
VALUES (1, FALSE, '24h', 'config', 7)
ON CONFLICT (id) DO NOTHING;
