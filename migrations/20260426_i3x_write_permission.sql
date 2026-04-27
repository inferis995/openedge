-- Add i3x_write permission column to users table.
-- false (default) = read-only access to i3X API
-- true            = read + write access to i3X API
-- Admin users bypass this check entirely.
ALTER TABLE users ADD COLUMN IF NOT EXISTS i3x_write BOOLEAN NOT NULL DEFAULT false;
