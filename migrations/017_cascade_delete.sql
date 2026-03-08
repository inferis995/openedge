-- Migration 017: Enable CASCADE delete for tags when gateway is deleted

-- Drop existing foreign key constraint on tags.gateway_id
ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_gateway_id_fkey;

-- Add new constraint with CASCADE
ALTER TABLE tags ADD CONSTRAINT tags_gateway_id_fkey
    FOREIGN KEY (gateway_id) REFERENCES gateways(id) ON DELETE CASCADE;

-- Also update tag_data and system_events for consistency
ALTER TABLE tag_data DROP CONSTRAINT IF EXISTS tag_data_tag_id_fkey;
ALTER TABLE tag_data ADD CONSTRAINT tag_data_tag_id_fkey
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE;

-- Alarms should also cascade (only if table exists)
DO $$
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'alarms') THEN
        ALTER TABLE alarms DROP CONSTRAINT IF EXISTS alarms_tag_id_fkey;
        ALTER TABLE alarms ADD CONSTRAINT alarms_tag_id_fkey
            FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE;
    END IF;
END $$;

-- Also handle alarm_configs table if it exists
DO $$
BEGIN
    IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'alarm_configs') THEN
        ALTER TABLE alarm_configs DROP CONSTRAINT IF EXISTS alarm_configs_tag_id_fkey;
        ALTER TABLE alarm_configs ADD CONSTRAINT alarm_configs_tag_id_fkey
            FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE;
    END IF;
END $$;
