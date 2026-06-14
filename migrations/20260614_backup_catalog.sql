-- Backup catalog: one row per backup file (scheduled or manual).
-- sha256 allows integrity verification on download/restore.
CREATE TABLE IF NOT EXISTS backup_catalog (
    id             SERIAL PRIMARY KEY,
    filename       TEXT NOT NULL,
    size_bytes     BIGINT NOT NULL DEFAULT 0,
    sha256         TEXT,
    encrypted      BOOLEAN NOT NULL DEFAULT FALSE,
    schema_version TEXT,
    storage        TEXT NOT NULL DEFAULT 'local',
    s3_key         TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ,
    UNIQUE (filename)
);

-- Audit trail: every backup-related operation with who/when/what.
CREATE TABLE IF NOT EXISTS backup_audit (
    id         BIGSERIAL PRIMARY KEY,
    action     TEXT NOT NULL,
    filename   TEXT,
    user_email TEXT,
    ip_addr    TEXT,
    details    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_audit_created ON backup_audit (created_at DESC);
