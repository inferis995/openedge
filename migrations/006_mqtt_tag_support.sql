-- Expand code column to support MQTT topics (which can be longer than 50 chars)
ALTER TABLE tags ALTER COLUMN code TYPE VARCHAR(500);

-- Update data_type CHECK to include STRING
DO $$
DECLARE constraint_name text;
BEGIN
    SELECT con.conname INTO constraint_name
    FROM pg_catalog.pg_constraint con
            INNER JOIN pg_catalog.pg_class rel ON rel.oid = con.conrelid
            INNER JOIN pg_catalog.pg_namespace nsp ON nsp.oid = con.connamespace
    WHERE nsp.nspname = 'public'
      AND rel.relname = 'tags'
      AND con.contype = 'c'
      AND pg_get_constraintdef(con.oid) LIKE '%data_type%';

    IF constraint_name IS NOT NULL THEN
        EXECUTE 'ALTER TABLE tags DROP CONSTRAINT ' || constraint_name;
    END IF;
END $$;

ALTER TABLE tags ADD CONSTRAINT tags_data_type_check CHECK (data_type IN ('INT', 'REAL', 'BOOL', 'DINT', 'STRING'));
