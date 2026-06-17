-- Synoptics — SCADA mimic pages. A synoptic is an org-scoped canvas with a
-- background and a JSON array of widgets. Each widget is positioned freely
-- (absolute x/y/w/h) and can bind to a tag; the live value drives its visual
-- state at runtime. Storing the whole layout as JSONB keeps the designer
-- atomic (save the entire page in one write) and avoids per-widget sub-CRUD.
CREATE TABLE IF NOT EXISTS synoptics (
    id               SERIAL PRIMARY KEY,
    org_id           INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    site_id          INT REFERENCES sites(id) ON DELETE SET NULL,
    area_id          INT REFERENCES areas(id) ON DELETE SET NULL,
    name             TEXT NOT NULL,
    description      TEXT,
    background_color VARCHAR(20) NOT NULL DEFAULT '#0f172a',
    canvas_w         INT NOT NULL DEFAULT 1280 CHECK (canvas_w > 0),
    canvas_h         INT NOT NULL DEFAULT 720 CHECK (canvas_h > 0),
    layout           JSONB NOT NULL DEFAULT '[]',
    created_by       INT REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, name)
);

CREATE INDEX IF NOT EXISTS idx_synoptics_org ON synoptics(org_id, name);
