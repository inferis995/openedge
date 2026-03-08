-- Migration: 021_db_retention_setting.sql
-- Add db_retention_days to global_settings table

ALTER TABLE global_settings ADD COLUMN IF NOT EXISTS db_retention_days INTEGER DEFAULT 30;

-- Set default value if it's null
UPDATE global_settings SET db_retention_days = 30 WHERE db_retention_days IS NULL;

-- Add description
COMMENT ON COLUMN global_settings.db_retention_days IS 'Number of days to keep historical tag data before automatic deletion (0 = infinite)';
