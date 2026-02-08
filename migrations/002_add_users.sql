-- Add users table for authentication
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(20) NOT NULL CHECK (role IN ('admin', 'user')),
    full_name TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Add access_logs for audit trail
CREATE TABLE IF NOT EXISTS access_logs (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE SET NULL,
    username TEXT NOT NULL, -- Keep username even if user deleted
    event_type VARCHAR(50) NOT NULL, -- 'LOGIN_SUCCESS', 'LOGIN_FAILED', 'LOGOUT'
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_access_logs_user_id ON access_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_access_logs_created_at ON access_logs(created_at);

-- Seed initial admin user (password: 'admin123')
-- Hash generated via bcrypt cost 10
INSERT INTO users (username, password_hash, role, full_name)
VALUES ('admin', '$2a$10$Ot0N4fXJ903diSev0X27KOCcTqI01lTp4gREcAJP/UOOxaRmChBfm', 'admin', 'System Administrator')
ON CONFLICT (username) DO NOTHING;

-- Seed default organization for State 0
INSERT INTO organizations (name)
VALUES ('Default')
ON CONFLICT DO NOTHING;
