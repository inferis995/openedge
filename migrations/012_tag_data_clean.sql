-- Migration 012: Tag Data Clean Storage
-- Simple, precise storage: ONLY save when PLC sends valid data
-- NO data = GAP in chart (connectNulls: false shows gaps)

-- Drop old tables if exist (clean slate)
DROP TABLE IF EXISTS tag_history CASCADE;

-- Main historian table (simple, clean)
-- NO quality column - if it's here, it's GOOD. If not here, it's a GAP.
// Modifying Historian table to allow explicit NULLs for gaps
CREATE TABLE tag_history (
    id        BIGSERIAL,
    time      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tag_id    INTEGER     NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    value     FLOAT8,                            -- NULL = explicit gap / offline marker

    source    VARCHAR(20) DEFAULT 'mqtt',    -- 'mqtt', 'modbus', 'opcua', 's7', 'sparkplug_b'
    PRIMARY KEY (id)
);

-- Optimized index for time-range queries per tag
CREATE INDEX idx_tag_history_tag_time ON tag_history (tag_id, time DESC);
CREATE INDEX idx_tag_history_time ON tag_history (time DESC);

-- Table for system events (gateway online/offline)
CREATE TABLE IF NOT EXISTS system_events (
    id        BIGSERIAL PRIMARY KEY,
    time      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    gateway_id INTEGER    NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
    status    VARCHAR(20) NOT NULL,          -- 'online' | 'offline'
    message   TEXT
);

CREATE INDEX IF NOT EXISTS idx_system_events_gw ON system_events (gateway_id, time DESC);

-- Comment explaining the design
COMMENT ON TABLE tag_history IS 'Historian: ONLY valid PLC data. Missing rows = GAP (PLC offline)';
COMMENT ON COLUMN tag_history.value IS 'Actual tag value. NULL never stored - query returns gap if no data';
