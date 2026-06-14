-- SSO/OIDC providers per org — supporta Google e Microsoft Azure AD.
-- Una riga per provider per org; domain_hint forza l'assegnazione dell'utente
-- all'org quando il suo email domain corrisponde (es. @acme.com → org 5).
CREATE TABLE IF NOT EXISTS sso_providers (
    id           SERIAL PRIMARY KEY,
    org_id       INT  NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL CHECK (provider IN ('google', 'azure')),
    client_id    TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    tenant_id    TEXT,          -- Azure AD tenant (guid o domain, es. acme.onmicrosoft.com)
    domain_hint  TEXT,          -- es. "acme.com" — auto-assign org su login SSO
    enabled      BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_sso_providers_org ON sso_providers(org_id) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_sso_providers_domain ON sso_providers(domain_hint) WHERE domain_hint IS NOT NULL;
