-- Add OPC_UA to gateways driver_type constraint

DO $$
DECLARE constraint_name text;
BEGIN
    SELECT con.conname INTO constraint_name
    FROM pg_catalog.pg_constraint con
            INNER JOIN pg_catalog.pg_class rel ON rel.oid = con.conrelid
            INNER JOIN pg_catalog.pg_namespace nsp ON nsp.oid = con.connamespace
    WHERE nsp.nspname = 'public'
      AND rel.relname = 'gateways'
      AND con.contype = 'c'
      AND pg_get_constraintdef(con.oid) LIKE '%driver_type%';

    IF constraint_name IS NOT NULL THEN
        EXECUTE 'ALTER TABLE gateways DROP CONSTRAINT ' || constraint_name;
    END IF;
END $$;

ALTER TABLE gateways ADD CONSTRAINT gateways_driver_type_check CHECK (driver_type IN ('S7', 'MODBUS_TCP', 'MQTT', 'OPC_UA'));
