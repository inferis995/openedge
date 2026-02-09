-- Drop alarms table
DROP TABLE IF EXISTS alarms;

-- Remove alarm columns from tags table
ALTER TABLE tags DROP COLUMN IF EXISTS alarm_enabled;
ALTER TABLE tags DROP COLUMN IF EXISTS alarm_threshold;
ALTER TABLE tags DROP COLUMN IF EXISTS alarm_operator;
ALTER TABLE tags DROP COLUMN IF EXISTS alarm_priority;
