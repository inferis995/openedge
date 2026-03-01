-- Migration 016: Backup settings for automatic scheduled backups

CREATE TABLE IF NOT EXISTS backup_settings (
    id INTEGER PRIMARY KEY DEFAULT 1,
    enabled BOOLEAN DEFAULT FALSE,
    interval_hours VARCHAR(10) DEFAULT '24h',  -- '6h', '12h', '24h', '7d'
    backup_type VARCHAR(10) DEFAULT 'config',   -- 'config' or 'full'
    retention_days INTEGER DEFAULT 7,
    last_run TIMESTAMPTZ,
    last_status VARCHAR(20)
);

-- Insert default settings
INSERT INTO backup_settings (id, enabled, interval_hours, backup_type, retention_days)
VALUES (1, FALSE, '24h', 'config', 7)
ON CONFLICT (id) DO NOTHING;
