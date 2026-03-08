-- Audit Logs Table - Track user login/logout and system events
CREATE TABLE IF NOT EXISTS audit_logs (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    username VARCHAR(255),
    action VARCHAR(50) NOT NULL,           -- 'login', 'logout', 'login_failed', 'password_change', etc.
    ip_address VARCHAR(45),                 -- IPv6 compatible
    user_agent TEXT,
    details JSONB,                          -- Additional context (organization, role changes, etc.)
    success BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Index for efficient querying by user
CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
-- Index for efficient querying by date range
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at DESC);
-- Index for efficient querying by action type
CREATE INDEX idx_audit_logs_action ON audit_logs(action);
-- Index for filtering by organization (stored in details)
CREATE INDEX idx_audit_logs_details_org ON audit_logs((details->>'org_id'));
