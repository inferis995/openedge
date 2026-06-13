-- Outbound webhooks — call external URLs when platform events occur.
-- Events: alarm.active | alarm.cleared | tag.write | edge.online | edge.offline
CREATE TABLE IF NOT EXISTS webhooks (
    id                SERIAL       PRIMARY KEY,
    org_id            INT          NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    url               TEXT         NOT NULL,
    secret            TEXT         NOT NULL,          -- used for HMAC-SHA256 signature
    events            TEXT[]       NOT NULL DEFAULT '{}',
    enabled           BOOLEAN      NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_triggered_at TIMESTAMPTZ,
    last_status_code  INT,
    last_error        TEXT
);

CREATE INDEX IF NOT EXISTS idx_webhooks_org_id ON webhooks (org_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_enabled  ON webhooks (enabled) WHERE enabled = true;
