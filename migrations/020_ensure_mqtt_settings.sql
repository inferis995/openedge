-- Migration 020: Ensure MQTT Settings Exist
-- Inserts missing MQTT settings keys if they don't exist
-- This fixes the issue where UPDATE would fail silently if rows don't exist

-- Insert MQTT broker settings if they don't exist
INSERT INTO global_settings (key, value, updated_at)
VALUES 
    ('mqtt_broker_mode', 'internal', NOW()),
    ('mqtt_external_host', '', NOW()),
    ('mqtt_external_port', '1883', NOW()),
    ('mqtt_username', '', NOW()),
    ('mqtt_password', '', NOW()),
    ('mqtt_client_id', 'industrial-edge', NOW())
ON CONFLICT (key) DO NOTHING;
