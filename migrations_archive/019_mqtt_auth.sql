-- Migration 019: MQTT Authentication Settings
-- Adds username, password, and client ID for external broker authentication

ALTER TABLE global_settings ADD COLUMN IF NOT EXISTS mqtt_username VARCHAR(255) DEFAULT '';
ALTER TABLE global_settings ADD COLUMN IF NOT EXISTS mqtt_password VARCHAR(255) DEFAULT '';
ALTER TABLE global_settings ADD COLUMN IF NOT EXISTS mqtt_client_id VARCHAR(127) DEFAULT 'industrial-edge';

-- Add comment for documentation
COMMENT ON COLUMN global_settings.mqtt_username IS 'Username for MQTT broker authentication (empty = no auth)';
COMMENT ON COLUMN global_settings.mqtt_password IS 'Password for MQTT broker authentication';
COMMENT ON COLUMN global_settings.mqtt_client_id IS 'Client ID for MQTT connection';
