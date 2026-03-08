-- Allow NULL value in tag_history for offline gap markers.
-- When a device goes offline (DEATH/disconnect), the historian stores a row
-- with value=NULL, source='offline'. The history API returns this as
-- quality=BAD, and the frontend renders a visible gap in the trend chart.
ALTER TABLE tag_history ALTER COLUMN value DROP NOT NULL;
