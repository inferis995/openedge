-- Migration 018: MQTT Broker Settings
-- Allows choosing between internal Mosquitto or external broker

-- Add MQTT broker settings to global_settings table
ALTER TABLE global_settings ADD COLUMN IF NOT EXISTS mqtt_broker_mode VARCHAR(20) DEFAULT 'internal';
ALTER TABLE global_settings ADD COLUMN IF NOT EXISTS mqtt_external_host VARCHAR(255) DEFAULT '';
ALTER TABLE global_settings ADD COLUMN IF NOT EXISTS mqtt_external_port INTEGER DEFAULT 1883;

-- Comment: mqtt_broker_mode can be 'internal' (use embedded Mosquitto) or 'external' (use custom broker)
