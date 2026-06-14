-- Add LoRaWAN as a supported driver type.
-- The CHECK constraint must be dropped and re-created because PostgreSQL
-- does not support ALTER CONSTRAINT … USING for CHECK constraints.
ALTER TABLE gateways
    DROP CONSTRAINT IF EXISTS gateways_driver_type_check;

ALTER TABLE gateways
    ADD CONSTRAINT gateways_driver_type_check
    CHECK (driver_type IN ('S7', 'MODBUS_TCP', 'MQTT', 'OPC_UA', 'LORAWAN'));
