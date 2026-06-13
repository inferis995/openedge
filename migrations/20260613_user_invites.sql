-- One-time invite tokens for self-service user onboarding.
-- An org admin creates an invite (POST /api/organizations/:id/invites);
-- the recipient follows the link to POST /api/auth/accept-invite which
-- creates their account and marks the token as used.
CREATE TABLE IF NOT EXISTS user_invites (
    id          SERIAL PRIMARY KEY,
    org_id      INT          NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       TEXT         NOT NULL,
    role        TEXT         NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    token       TEXT         NOT NULL UNIQUE,
    created_by  INT          NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW() + INTERVAL '7 days',
    accepted_at TIMESTAMPTZ,
    accepted_by INT REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_user_invites_token ON user_invites(token) WHERE accepted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_invites_org   ON user_invites(org_id);
