-- Migration: Add Cloud Sync (MQTT Forwarding) Global Settings
-- Description: Adds configuration key-value pairs to the global_settings table for the Cloud MQTT Forwarder

INSERT INTO global_settings (key, value, description) VALUES
    ('cloud_sync_enabled', 'false', 'Enable or disable forwarding of Sparkplug B data to an external Cloud MQTT broker'),
    ('cloud_mqtt_host', '', 'The hostname or IP address of the external Cloud MQTT broker (e.g., test.mosquitto.org)'),
    ('cloud_mqtt_port', '1883', 'The port of the external Cloud MQTT broker (usually 1883 or 8883)'),
    ('cloud_mqtt_username', '', 'The username for the external Cloud MQTT broker (optional)'),
    ('cloud_mqtt_password', '', 'The password for the external Cloud MQTT broker (optional)'),
    ('cloud_mqtt_topic', '', 'The base destination topic prefix to publish the forwarded Sparkplug B payloads')
ON CONFLICT (key) DO NOTHING;
