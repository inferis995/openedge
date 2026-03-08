-- Migration 019: Add org_id to users table
-- Allows global admin (org_id=NULL) to access all organizations

-- Add org_id column to users table (nullable for global admin)
ALTER TABLE users ADD COLUMN IF NOT EXISTS org_id INT REFERENCES organizations(id) ON DELETE SET NULL;

-- Add index for efficient queries
CREATE INDEX IF NOT EXISTS idx_users_org_id ON users(org_id);

-- Update existing admin users to have org_id=NULL (global admin)
UPDATE users SET org_id = NULL WHERE role = 'admin';

-- Comment explaining the design
COMMENT ON COLUMN users.org_id IS 'Organization ID. NULL for global admin (access all organizations). INT for regular users (scoped to one org)';
