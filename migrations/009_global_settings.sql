-- Migration: 009_global_settings.sql
-- Create global_settings table for system-wide configuration

CREATE TABLE IF NOT EXISTS global_settings (
    key VARCHAR(50) PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    updated_by INTEGER REFERENCES users(id)
);

-- Insert default settings
INSERT INTO global_settings (key, value, description) VALUES
    ('publish_mode', 'dual', 'Modalita pubblicazione MQTT: dual, sparkplug_only, legacy_only'),
    ('rbe_heartbeat_seconds', '60', 'Intervallo heartbeat in secondi per Sparkplug B RBE'),
    ('rbe_deadband_percent', '0.5', 'Soglia deadband percentuale per valori analogici')
ON CONFLICT (key) DO NOTHING;
