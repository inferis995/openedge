-- Industrial Edge Middleware - Database Schema
-- This file creates all tables for the multi-tenant organizational structure

-- Organizations table (top level of hierarchy)
CREATE TABLE IF NOT EXISTS organizations (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Sites table (belongs to an organization)
CREATE TABLE IF NOT EXISTS sites (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Areas table (belongs to a site)
CREATE TABLE IF NOT EXISTS areas (
    id SERIAL PRIMARY KEY,
    site_id INT NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Gateways table (belongs to an area, represents PLC/Device connections)
CREATE TABLE IF NOT EXISTS gateways (
    id SERIAL PRIMARY KEY,
    area_id INT NOT NULL REFERENCES areas(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    driver_type VARCHAR(20) NOT NULL CHECK (driver_type IN ('S7', 'MODBUS_TCP')),
    connection_config JSONB NOT NULL,
    scan_rate_ms INT DEFAULT 1000,
    enabled BOOLEAN DEFAULT TRUE,
    zero_based BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Tags table (data points to read from gateways)
CREATE TABLE IF NOT EXISTS tags (
    id SERIAL PRIMARY KEY,
    gateway_id INT NOT NULL REFERENCES gateways(id) ON DELETE RESTRICT,
    code VARCHAR(50) NOT NULL,
    alias VARCHAR(100) NOT NULL,
    data_type VARCHAR(20) NOT NULL CHECK (data_type IN ('INT', 'REAL', 'BOOL', 'DINT')),
    historize BOOLEAN DEFAULT FALSE,
    historize_deadband FLOAT DEFAULT 0.0,
    alarm_enabled BOOLEAN DEFAULT FALSE,
    alarm_threshold FLOAT,
    alarm_operator VARCHAR(2) CHECK (alarm_operator IN ('>', '<', '=')),
    alarm_priority INT DEFAULT 1 CHECK (alarm_priority BETWEEN 1 AND 5),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Alarms table (tracks alarm state transitions)
CREATE TABLE IF NOT EXISTS alarms (
    id SERIAL PRIMARY KEY,
    tag_id INT NOT NULL REFERENCES tags(id) ON DELETE RESTRICT,
    state VARCHAR(20) NOT NULL CHECK (state IN ('ACTIVE', 'RTN', 'ACKNOWLEDGED', 'CLEAR')),
    message TEXT,
    triggered_at TIMESTAMPTZ NOT NULL,
    acknowledged_at TIMESTAMPTZ,
    cleared_at TIMESTAMPTZ
);

-- Indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_sites_org_id ON sites(org_id);
CREATE INDEX IF NOT EXISTS idx_areas_site_id ON areas(site_id);
CREATE INDEX IF NOT EXISTS idx_gateways_area_id ON gateways(area_id);
CREATE INDEX IF NOT EXISTS idx_gateways_enabled ON gateways(enabled) WHERE enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_tags_gateway_id ON tags(gateway_id);
CREATE INDEX IF NOT EXISTS idx_tags_alarm_enabled ON tags(alarm_enabled) WHERE alarm_enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_alarms_tag_id ON alarms(tag_id);
CREATE INDEX IF NOT EXISTS idx_alarms_state ON alarms(state);
