-- Migration: 011_stale_threshold.sql
-- Add stale threshold setting for data freshness detection

-- Insert stale threshold setting (default: 30 seconds)
INSERT INTO global_settings (key, value, description) VALUES
    ('stale_threshold_seconds', '30', 'Soglia in secondi oltre cui un dato viene considerato "stale" (vecchio). I dati piu vecchi di questa soglia vengono mostrati con indicatore di warning.')
ON CONFLICT (key) DO NOTHING;

-- Add comment explaining the setting
COMMENT ON COLUMN global_settings.value IS 'Per stale_threshold_seconds: valore intero in secondi (es. 30 = 30 secondi)';
