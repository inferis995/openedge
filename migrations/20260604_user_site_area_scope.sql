-- ============================================================================
-- User Site/Area Scope Tables
--
-- Allows restricting a user's access to specific sites and/or areas within
-- their organization. If a user has no entries in user_sites, they can access
-- all sites in their org. Same logic applies for areas.
-- ============================================================================

CREATE TABLE IF NOT EXISTS user_sites (
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    site_id INT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, site_id)
);

CREATE TABLE IF NOT EXISTS user_areas (
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    area_id INT NOT NULL REFERENCES areas(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, area_id)
);

CREATE INDEX IF NOT EXISTS idx_user_sites_user_id ON user_sites(user_id);
CREATE INDEX IF NOT EXISTS idx_user_areas_user_id ON user_areas(user_id);
