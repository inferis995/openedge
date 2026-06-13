-- Add email to users table for password reset flow.
-- Nullable: existing users and admin-created users may not have one yet.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (email) WHERE email IS NOT NULL;
