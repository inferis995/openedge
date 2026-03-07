-- 023_alarm_system.sql

-- Alarm Definitions table (configuration per tag)
CREATE TABLE IF NOT EXISTS alarm_definitions (
    id SERIAL PRIMARY KEY,
    tag_id INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    alarm_type VARCHAR(20) NOT NULL,    -- 'bool_true', 'bool_false', 'high_high', 'high', 'low', 'low_low', 'rate_of_change'
    threshold DOUBLE PRECISION,         -- Use for numeric alarms
    deadband DOUBLE PRECISION DEFAULT 0,-- To prevent chattering
    delay_seconds INT DEFAULT 0,        -- Wait N seconds before triggering
    severity VARCHAR(20) DEFAULT 'warning', -- 'info', 'warning', 'critical'
    message TEXT,                       -- Custom message to show when triggered
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Index for fast lookup by tag
CREATE INDEX IF NOT EXISTS idx_alarm_def_tag ON alarm_definitions(tag_id);

-- Alarm Events table (active and historical events)
DROP TABLE IF EXISTS alarm_events CASCADE;

CREATE TABLE alarm_events (
    id SERIAL,
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    definition_id INTEGER REFERENCES alarm_definitions(id) ON DELETE SET NULL,
    status VARCHAR(50) NOT NULL, -- e.g. ACTIVE, CLEARED, ACKNOWLEDGED
    alarm_type VARCHAR(50) NOT NULL,
    severity VARCHAR(50) NOT NULL,
    message TEXT,
    value_at_trigger DOUBLE PRECISION,
    trigger_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    clear_time TIMESTAMPTZ,
    bg_ack_user VARCHAR(100),
    ack_time TIMESTAMPTZ,
    PRIMARY KEY (id, trigger_time)
);

-- Index for querying active and history
CREATE INDEX IF NOT EXISTS idx_alarm_events_status ON alarm_events(status);
CREATE INDEX IF NOT EXISTS idx_alarm_events_tag ON alarm_events(tag_id);
CREATE INDEX IF NOT EXISTS idx_alarm_events_time ON alarm_events(trigger_time DESC);

-- Convert alarm_events to a timescale hypertable for efficient history (partition by time)
SELECT create_hypertable('alarm_events', 'trigger_time', if_not_exists => TRUE);
