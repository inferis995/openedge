-- Per-org MQTT credentials provisioned by core-api into Mosquitto Dynamic Security.
-- One record per organization; username is stable (org-{id}), password is random.
CREATE TABLE IF NOT EXISTS org_mqtt_credentials (
    id           SERIAL PRIMARY KEY,
    org_id       INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    username     TEXT NOT NULL,
    password     TEXT NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (org_id),
    UNIQUE (username)
);

-- API keys for edge-to-cloud authentication.
-- Edge manager uses X-API-Key header to pull its gateway config without a human JWT.
-- The full key is shown ONCE on creation; only the SHA-256 hash is stored.
CREATE TABLE IF NOT EXISTS org_api_keys (
    id           SERIAL PRIMARY KEY,
    org_id       INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT 'default',
    key_prefix   TEXT NOT NULL,          -- e.g. "oe_a1b2c3d4" shown in UI
    key_hash     TEXT NOT NULL,          -- SHA-256(full_key) hex
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_org_api_keys_org_active
    ON org_api_keys(org_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_org_api_keys_prefix_active
    ON org_api_keys(key_prefix) WHERE revoked_at IS NULL;
