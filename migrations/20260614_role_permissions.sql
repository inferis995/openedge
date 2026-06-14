-- Permessi granulari per utente — estende il semplice admin/user.
-- Una riga per utente; tutti i flag sono false per default (role=user).
-- role=admin ha tutti i flag true — impostati automaticamente da core-api al create.
-- I flag vengono inclusi nel JWT per evitare query per ogni request.
CREATE TABLE IF NOT EXISTS role_permissions (
    id                    SERIAL PRIMARY KEY,
    user_id               INT NOT NULL REFERENCES users(id) ON DELETE CASCADE UNIQUE,
    can_write_tags        BOOLEAN NOT NULL DEFAULT false,
    can_ack_alarms        BOOLEAN NOT NULL DEFAULT false,
    can_export_data       BOOLEAN NOT NULL DEFAULT false,
    can_manage_recipes    BOOLEAN NOT NULL DEFAULT false,
    can_manage_shifts     BOOLEAN NOT NULL DEFAULT false,
    can_view_audit        BOOLEAN NOT NULL DEFAULT false,
    can_download_installer BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_user ON role_permissions(user_id);
