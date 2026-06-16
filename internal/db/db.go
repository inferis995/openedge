package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// Connect establishes a connection to PostgreSQL using the provided configuration
func Connect(cfg Config) (*sql.DB, error) {
	// statement_timeout=30s: any query running longer than 30 seconds is
	// automatically cancelled by PostgreSQL. This protects the connection pool
	// from slow/hung queries without requiring context plumbing in every handler.
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable options='-c statement_timeout=30000'",
		cfg.Host,
		cfg.Port,
		cfg.User,
		cfg.Password,
		cfg.Database,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Production-ready connection pool settings
	// MaxOpenConns: maximum number of open connections to the database
	db.SetMaxOpenConns(25)
	// MaxIdleConns: maximum number of connections in the idle connection pool
	db.SetMaxIdleConns(10)
	// ConnMaxLifetime: maximum amount of time a connection may be reused
	db.SetConnMaxLifetime(5 * time.Minute)
	// ConnMaxIdleTime: maximum amount of time a connection may be idle
	db.SetConnMaxIdleTime(2 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("[DB] Connection pool configured: MaxOpen=%d, MaxIdle=%d, MaxLifetime=%v",
		25, 10, 5*time.Minute)

	// Run auto-migrations
	if err := runAutoMigrations(db); err != nil {
		log.Printf("Warning: Auto-migration failed: %v", err)
	}

	return db, nil
}

// Close closes the database connection
func Close(db *sql.DB) error {
	if db == nil {
		return nil
	}
	return db.Close()
}

// runAutoMigrations executes pending migrations
func runAutoMigrations(db *sql.DB) error {
	// Migration 008: audit_logs table
	auditLogsTable := `
	CREATE TABLE IF NOT EXISTS audit_logs (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
		username VARCHAR(255),
		action VARCHAR(50) NOT NULL,
		ip_address VARCHAR(45),
		user_agent TEXT,
		details JSONB,
		success BOOLEAN DEFAULT true,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := db.Exec(auditLogsTable); err != nil {
		return fmt.Errorf("failed to create audit_logs table: %w", err)
	}

	// Create indexes if they don't exist
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id)",
		"CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action)",
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			log.Printf("Warning: Failed to create index: %v", err)
		}
	}

	// Migration: i3x_write permission column on users
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS i3x_write BOOLEAN NOT NULL DEFAULT false`); err != nil {
		return fmt.Errorf("failed to add i3x_write column: %w", err)
	}

	// Migration: json_path on tags. Used by the MQTT driver to pull a single
	// field from a JSON payload — e.g. tag.code="factory/sensor/temp" with
	// json_path="temp" extracts 22.5 from {"temp":22.5,"humidity":55}. Empty
	// (NULL) keeps the legacy behaviour (whole payload is the value).
	if _, err := db.Exec(`ALTER TABLE tags ADD COLUMN IF NOT EXISTS json_path TEXT`); err != nil {
		return fmt.Errorf("failed to add tags.json_path column: %w", err)
	}

	// Migration: notification channel config. Operator wires email / Telegram
	// from System → Notifications in the UI; the dispatcher loads these
	// settings on first use and re-reads them once a minute so admin edits
	// take effect without restarting core-api.
	notifSeed := `
	INSERT INTO global_settings (key, value, description) VALUES
		('notif_email_enabled', 'false', 'When true, alarm events are dispatched as plain-text email.'),
		('notif_email_smtp_host', '', 'SMTP relay host (e.g. smtp.gmail.com, smtp.office365.com).'),
		('notif_email_smtp_port', '587', 'SMTP relay port. 587 = STARTTLS submission, 465 = implicit TLS.'),
		('notif_email_use_tls', 'false', 'true -> implicit TLS (port 465). false -> STARTTLS (port 587).'),
		('notif_email_username', '', 'SMTP auth username (often the same as the From address).'),
		('notif_email_password', '', 'SMTP auth password (or Gmail-style app password).'),
		('notif_email_from', '', 'From: address of outgoing alerts.'),
		('notif_email_to', '', 'Comma-separated recipient list.'),
		('notif_telegram_enabled', 'false', 'When true, alarm events are sent to a Telegram chat.'),
		('notif_telegram_bot_token', '', 'Telegram bot token from @BotFather.'),
		('notif_telegram_chat_id', '', 'Numeric chat_id (DM or group). Get it from getUpdates after messaging the bot once.'),
		('notif_min_severity', 'medium', 'Drop alarm events below this severity (low|medium|high|critical).'),
		('notif_on_cleared', 'false', 'true -> also notify when an alarm clears, not only when it fires.'),
		('notif_rate_limit_per_min', '60', 'Global cap (events per minute across all channels) to survive alarm storms.'),
		('backup_enabled', 'true', 'When true, the nightly pg_dump runs on the configured schedule.'),
		('backup_schedule', '0 3 * * *', 'Cron expression (UTC) for the nightly backup. Default: 03:00 every day.'),
		('backup_retention_days', '30', 'Older dump files are auto-pruned after this many days.'),
		('backup_age_recipient', '', 'age public key. When set, every dump is encrypted; safe to copy off-host. Leave empty for plaintext (acceptable on encrypted disks).'),
		('kpi_target_alarms_per_day',  '5',   'Soglia massima allarmi/giorno (good_when=down). Sopra = rosso.'),
		('kpi_target_open_critical',   '0',   'Critical attivi massimi tollerati. 0 = mai. Sopra = rosso.'),
		('kpi_target_bad_quality_1h',  '0',   'Tag in errore massimi tollerati nell''ultima ora.'),
		('kpi_target_writes_24h_min',  '0',   'Soglia minima write PLC nelle 24h (good_when=up). Sotto = warning.'),
		('kpi_target_recipe_loads_24h_min', '0', 'Soglia minima caricamenti ricette nelle 24h.'),
		('kpi_target_logins_24h_min',  '0',   'Soglia minima login nelle 24h.'),
		('oee_window_minutes',         '480', 'Finestra di calcolo OEE in minuti (default 480 = 8h, un turno).'),
		('oee_target',                 '85',  'Target OEE in %% (ISO 22400: 85%% = world-class). Sotto = rosso.'),
		('oee_run_time_tag',           '0',   'tag_id del segnale "macchina in marcia" (BOOL/0-1). 0 = fallback su uptime allarmi.'),
		('oee_produced_tag',           '0',   'tag_id del contatore pezzi prodotti (monotono crescente). 0 = fallback su quality samples.'),
		('oee_good_tag',               '0',   'tag_id del contatore pezzi buoni (monotono crescente). 0 = fallback.'),
		('oee_target_pieces_per_hour', '0',   'Rate target di produzione (pezzi/ora) — usato per il calcolo Performance.')
	ON CONFLICT (key) DO NOTHING;`
	if _, err := db.Exec(notifSeed); err != nil {
		log.Printf("Warning: failed to seed notification settings: %v", err)
	}

	// Migration: recipe management. A recipe is a named set of
	// (tag, value) pairs the operator can "load" with one click — the
	// classic SCADA feature that turns a data viewer into a control
	// system. Three tables:
	//   recipes        — header (name, description, owner org)
	//   recipe_values  — the (tag_id, value) entries
	//   recipe_runs    — audit log of every load, append-only
	recipes := []string{
		`CREATE TABLE IF NOT EXISTS recipes (
			id SERIAL PRIMARY KEY,
			org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			created_by INT REFERENCES users(id) ON DELETE SET NULL,
			UNIQUE (org_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS recipe_values (
			id SERIAL PRIMARY KEY,
			recipe_id INT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
			tag_id INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			value TEXT NOT NULL,
			UNIQUE (recipe_id, tag_id)
		)`,
		`CREATE TABLE IF NOT EXISTS recipe_runs (
			id BIGSERIAL PRIMARY KEY,
			recipe_id INT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
			org_id INT NOT NULL,
			triggered_by INT REFERENCES users(id) ON DELETE SET NULL,
			triggered_username TEXT,
			triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			results JSONB NOT NULL DEFAULT '[]'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recipes_org ON recipes(org_id)`,
		`CREATE INDEX IF NOT EXISTS idx_recipe_runs_recipe ON recipe_runs(recipe_id, triggered_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_recipe_runs_org ON recipe_runs(org_id, triggered_at DESC)`,
	}
	for _, stmt := range recipes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("recipes migration: %w", err)
		}
	}

	// Migration: shifts (turni). Definizione orari di lavoro + assegnamento
	// operatori. weekdays è un array di int (0=Domenica, 1=Lunedì, ..., 6=Sabato)
	// per gestire turni che vanno solo lun-ven o solo weekend. start_time può
	// essere maggiore di end_time (turno notte che incrocia mezzanotte: 22-06).
	shifts := []string{
		`CREATE TABLE IF NOT EXISTS shifts (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			start_time TIME NOT NULL,
			end_time TIME NOT NULL,
			weekdays INT[] NOT NULL DEFAULT '{1,2,3,4,5}',
			active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS shift_assignments (
			id SERIAL PRIMARY KEY,
			shift_id INT NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
			user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			valid_from DATE NOT NULL DEFAULT CURRENT_DATE,
			valid_to DATE,
			UNIQUE (shift_id, user_id, valid_from)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_assignments_shift ON shift_assignments(shift_id)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_assignments_user ON shift_assignments(user_id)`,
		// Seed dei 3 turni standard se non ce ne sono — così l'utente vede
		// subito qualcosa nella UI e può modificarli/disattivarli.
		`INSERT INTO shifts (name, start_time, end_time, weekdays, active)
		SELECT 'Mattina',   '06:00', '14:00', '{1,2,3,4,5}', true
		WHERE NOT EXISTS (SELECT 1 FROM shifts LIMIT 1)`,
		`INSERT INTO shifts (name, start_time, end_time, weekdays, active)
		SELECT 'Pomeriggio','14:00', '22:00', '{1,2,3,4,5}', true
		WHERE NOT EXISTS (SELECT 1 FROM shifts WHERE name = 'Pomeriggio')`,
		`INSERT INTO shifts (name, start_time, end_time, weekdays, active)
		SELECT 'Notte',     '22:00', '06:00', '{1,2,3,4,5}', true
		WHERE NOT EXISTS (SELECT 1 FROM shifts WHERE name = 'Notte')`,
	}
	for _, stmt := range shifts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("shifts migration: %w", err)
		}
	}

	// Migration: maintenance windows. Periodi di manutenzione programmata
	// durante i quali le notifiche email/Telegram sono silenziate (gli
	// allarmi restano comunque in DB per audit). Reduce drasticamente il
	// numero di "alarm spam" durante interventi e fermate pianificate —
	// fattore #1 di customer complaints nei deploy industriali.
	// Migration: custom KPI definitions. Permette all'operatore di definire
	// metriche di produzione (pezzi/h, OEE component, kWh per turno, ...)
	// senza scrivere codice — bastano: nome + tag + aggregazione + finestra.
	// Una formula completa sarebbe più potente ma rende l'UX impossibile
	// per il caporeparto; questo MVP copre l'80% dei casi reali.
	customKPIs := []string{
		`CREATE TABLE IF NOT EXISTS custom_kpis (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			tag_id INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			aggregation TEXT NOT NULL CHECK (aggregation IN ('avg','sum','min','max','last','delta','count')),
			window_minutes INT NOT NULL DEFAULT 1440 CHECK (window_minutes > 0),
			unit TEXT DEFAULT '',
			multiplier DOUBLE PRECISION DEFAULT 1.0,
			good_when TEXT DEFAULT 'up' CHECK (good_when IN ('up','down')),
			target_value DOUBLE PRECISION,
			active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_kpis_active ON custom_kpis(active)`,
	}
	for _, stmt := range customKPIs {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("custom KPIs migration: %w", err)
		}
	}

	maintenance := []string{
		`CREATE TABLE IF NOT EXISTS maintenance_windows (
			id SERIAL PRIMARY KEY,
			title TEXT NOT NULL,
			start_at TIMESTAMPTZ NOT NULL,
			end_at TIMESTAMPTZ NOT NULL,
			reason TEXT,
			created_by INT REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_maintenance_windows_range
			ON maintenance_windows(start_at, end_at)`,
	}
	for _, stmt := range maintenance {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("maintenance windows migration: %w", err)
		}
	}

	// Migration: OEE profiles. Multi-linea/multi-reparto — ogni profilo è
	// un'unità di misura OEE indipendente (una linea, una macchina, un
	// reparto). Il fallback ai settings globali oee_* resta attivo finché
	// non c'è almeno un profilo: così le installazioni esistenti non si
	// rompono. area_id e gateway_id sono solo metadati di scoping per la
	// UI (raggruppamento/filtro); il calcolo legge sempre i 3 tag id qui.
	oeeProfiles := []string{
		`CREATE TABLE IF NOT EXISTS oee_profiles (
			id SERIAL PRIMARY KEY,
			org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT,
			area_id INT REFERENCES areas(id) ON DELETE SET NULL,
			gateway_id INT REFERENCES gateways(id) ON DELETE SET NULL,
			run_time_tag_id INT REFERENCES tags(id) ON DELETE SET NULL,
			produced_tag_id INT REFERENCES tags(id) ON DELETE SET NULL,
			good_tag_id INT REFERENCES tags(id) ON DELETE SET NULL,
			target_pieces_per_hour DOUBLE PRECISION DEFAULT 0,
			window_minutes INT NOT NULL DEFAULT 480 CHECK (window_minutes > 0),
			target_oee DOUBLE PRECISION DEFAULT 85 CHECK (target_oee >= 0 AND target_oee <= 100),
			display_order INT DEFAULT 0,
			enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (org_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oee_profiles_org_enabled
			ON oee_profiles(org_id, enabled, display_order)`,
	}
	for _, stmt := range oeeProfiles {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("oee profiles migration: %w", err)
		}
	}

	// Migration: OEE history (snapshot persistiti) + loss categories.
	// `oee_history` è la tabella di rollup orario/giornaliero: il cron
	// worker scrive una riga per profilo per ora con A/P/Q calcolati,
	// così le query "OEE ultime 4 settimane" non devono scannerare
	// tag_history ogni volta. profile_id = NULL = riga di rollup
	// fabbrica (media tra profili).
	//
	// `oee_loss_categories` sono le Six Big Losses (ISO 22400-2): vengono
	// seedate alla migration con i 6 codici standard. Saranno usate da
	// commit successivi (loss tree + Pareto cause).
	oeeHistoryTables := []string{
		`CREATE TABLE IF NOT EXISTS oee_history (
			id BIGSERIAL PRIMARY KEY,
			profile_id INT REFERENCES oee_profiles(id) ON DELETE CASCADE,
			bucket_start TIMESTAMPTZ NOT NULL,
			bucket_size TEXT NOT NULL CHECK (bucket_size IN ('hour','day','shift')),
			oee DOUBLE PRECISION NOT NULL,
			availability DOUBLE PRECISION NOT NULL,
			performance DOUBLE PRECISION NOT NULL,
			quality DOUBLE PRECISION NOT NULL,
			planned_min DOUBLE PRECISION DEFAULT 0,
			downtime_min DOUBLE PRECISION DEFAULT 0,
			pieces_produced DOUBLE PRECISION DEFAULT 0,
			pieces_good DOUBLE PRECISION DEFAULT 0,
			shift_id INT REFERENCES shifts(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (profile_id, bucket_start, bucket_size)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oee_history_lookup
			ON oee_history(profile_id, bucket_size, bucket_start DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_oee_history_shift
			ON oee_history(shift_id, bucket_start) WHERE shift_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS oee_loss_categories (
			id SERIAL PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			loss_pillar TEXT NOT NULL CHECK (loss_pillar IN ('availability','performance','quality')),
			display_label TEXT NOT NULL,
			color TEXT DEFAULT '#888888'
		)`,
	}
	for _, stmt := range oeeHistoryTables {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("oee history migration: %w", err)
		}
	}

	// Migration aggiuntiva: respect_shifts + respect_maintenance per
	// profilo OEE. Quando true (default), il calcolo Availability divide
	// per Planned Production Time (PPT = window - pause turni -
	// finestre di manutenzione) invece di wall clock. Risultato: una
	// linea che non lavora di domenica non viene penalizzata.
	oeeProfilesAlter := []string{
		`ALTER TABLE oee_profiles ADD COLUMN IF NOT EXISTS respect_shifts BOOLEAN DEFAULT TRUE`,
		`ALTER TABLE oee_profiles ADD COLUMN IF NOT EXISTS respect_maintenance BOOLEAN DEFAULT TRUE`,
	}
	for _, stmt := range oeeProfilesAlter {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("oee profiles alter: %w", err)
		}
	}

	// Seed delle 6 Big Losses standard ISO 22400-2. Servono al loss tree
	// del commit 3 ma le creiamo già ora — niente downside e schema completo.
	lossSeed := `
	INSERT INTO oee_loss_categories (code, loss_pillar, display_label, color) VALUES
		('breakdown',         'availability', 'Guasti / fermi non pianificati', '#dc2626'),
		('setup',             'availability', 'Cambio formato / setup',         '#f59e0b'),
		('minor_stop',        'performance',  'Micro-fermi (< 5 min)',          '#fbbf24'),
		('reduced_speed',     'performance',  'Velocità ridotta / rallentamenti', '#f97316'),
		('startup_defect',    'quality',      'Scarti di avvio',                '#a855f7'),
		('production_defect', 'quality',      'Scarti di produzione',           '#ec4899')
	ON CONFLICT (code) DO NOTHING;`
	if _, err := db.Exec(lossSeed); err != nil {
		log.Printf("Warning: failed to seed loss categories: %v", err)
	}

	// Migration: oee_loss_events. Singolo evento di perdita registrato dal
	// cron worker, agganciato a una causa (allarme critical, maintenance,
	// run_tag=0). Popolato per profilo: ogni profilo "vede" gli eventi che
	// gli appartengono via gateway_id (o tutti, se il profilo non ha scope).
	// UNIQUE (profile_id, source, source_ref) garantisce idempotenza.
	lossEvents := []string{
		`CREATE TABLE IF NOT EXISTS oee_loss_events (
			id BIGSERIAL PRIMARY KEY,
			profile_id INT NOT NULL REFERENCES oee_profiles(id) ON DELETE CASCADE,
			category_id INT NOT NULL REFERENCES oee_loss_categories(id),
			start_at TIMESTAMPTZ NOT NULL,
			end_at TIMESTAMPTZ NOT NULL,
			duration_min DOUBLE PRECISION NOT NULL,
			source TEXT NOT NULL CHECK (source IN ('alarm','maintenance','no_run_tag')),
			source_ref BIGINT,
			notes TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (profile_id, source, source_ref)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oee_loss_events_lookup
			ON oee_loss_events(profile_id, start_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_oee_loss_events_cat
			ON oee_loss_events(profile_id, category_id, start_at)`,
	}
	for _, stmt := range lossEvents {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("oee loss events migration: %w", err)
		}
	}

	// Migration: oee_alert_rules. Regole admin-configurabili per ricevere
	// email/Telegram quando OEE (o componente) di un profilo (o rollup)
	// scende sotto soglia per N minuti consecutivi. Valutate dal cron
	// worker ogni 5 minuti contro oee_history.
	alertRules := []string{
		`CREATE TABLE IF NOT EXISTS oee_alert_rules (
			id SERIAL PRIMARY KEY,
			profile_id INT REFERENCES oee_profiles(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			metric TEXT NOT NULL CHECK (metric IN ('oee','availability','performance','quality')),
			op TEXT NOT NULL CHECK (op IN ('<','>')),
			threshold DOUBLE PRECISION NOT NULL,
			sustained_minutes INT NOT NULL DEFAULT 60 CHECK (sustained_minutes >= 60),
			severity TEXT NOT NULL CHECK (severity IN ('info','warning','critical')),
			enabled BOOLEAN NOT NULL DEFAULT true,
			last_notified_at TIMESTAMPTZ,
			last_state TEXT DEFAULT 'normal' CHECK (last_state IN ('normal','violating')),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oee_alert_rules_enabled
			ON oee_alert_rules(enabled) WHERE enabled = true`,
	}
	for _, stmt := range alertRules {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("oee alert rules migration: %w", err)
		}
	}

	// Migration: per-org MQTT credentials provisioned into Mosquitto DynSec.
	// One row per org; username is stable (org-{id}), password is random.
	orgMqttCredentials := []string{
		`CREATE TABLE IF NOT EXISTS org_mqtt_credentials (
			id         SERIAL PRIMARY KEY,
			org_id     INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			username   TEXT NOT NULL,
			password   TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (org_id),
			UNIQUE (username)
		)`,
	}
	for _, stmt := range orgMqttCredentials {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("org_mqtt_credentials migration: %w", err)
		}
	}

	// Migration: org API keys for edge-to-cloud auth (X-API-Key header).
	orgApiKeys := []string{
		`CREATE TABLE IF NOT EXISTS org_api_keys (
			id           SERIAL PRIMARY KEY,
			org_id       INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			name         TEXT NOT NULL DEFAULT 'default',
			key_prefix   TEXT NOT NULL,
			key_hash     TEXT NOT NULL,
			created_at   TIMESTAMPTZ DEFAULT NOW(),
			last_used_at TIMESTAMPTZ,
			revoked_at   TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_org_api_keys_org_active
			ON org_api_keys(org_id) WHERE revoked_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_org_api_keys_prefix_active
			ON org_api_keys(key_prefix) WHERE revoked_at IS NULL`,
	}
	for _, stmt := range orgApiKeys {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("org_api_keys migration: %w", err)
		}
	}

	// Migration: email column on users (for password-reset flow).
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT`); err != nil {
		return fmt.Errorf("failed to add users.email column: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (email) WHERE email IS NOT NULL`); err != nil {
		log.Printf("Warning: failed to create idx_users_email: %v", err)
	}

	// Migration: password_reset_tokens — one-time tokens, expire 1 hour.
	pwdResetTokens := []string{
		`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id         SERIAL PRIMARY KEY,
			user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token      TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '1 hour',
			used_at    TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_token ON password_reset_tokens (token)`,
	}
	for _, stmt := range pwdResetTokens {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("password_reset_tokens migration: %w", err)
		}
	}

	// Migration: user_invites — org admin invites users via one-time link (7d TTL).
	userInvites := []string{
		`CREATE TABLE IF NOT EXISTS user_invites (
			id          SERIAL PRIMARY KEY,
			org_id      INT         NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			email       TEXT        NOT NULL,
			role        TEXT        NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
			token       TEXT        NOT NULL UNIQUE,
			created_by  INT         NOT NULL REFERENCES users(id),
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at  TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days',
			accepted_at TIMESTAMPTZ,
			accepted_by INT REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_invites_token ON user_invites(token) WHERE accepted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_user_invites_org   ON user_invites(org_id)`,
	}
	for _, stmt := range userInvites {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("user_invites migration: %w", err)
		}
	}

	// Migration: webhooks — outbound HTTP callbacks on platform events.
	webhooks := []string{
		`CREATE TABLE IF NOT EXISTS webhooks (
			id                SERIAL       PRIMARY KEY,
			org_id            INT          NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			url               TEXT         NOT NULL,
			secret            TEXT         NOT NULL,
			events            TEXT[]       NOT NULL DEFAULT '{}',
			enabled           BOOLEAN      NOT NULL DEFAULT true,
			created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			last_triggered_at TIMESTAMPTZ,
			last_status_code  INT,
			last_error        TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_org_id  ON webhooks (org_id)`,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_enabled ON webhooks (enabled) WHERE enabled = true`,
	}
	for _, stmt := range webhooks {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("webhooks migration: %w", err)
		}
	}

	// Migration: SSO/OIDC providers (Google, Azure AD) per org.
	ssoProviders := []string{
		`CREATE TABLE IF NOT EXISTS sso_providers (
			id            SERIAL PRIMARY KEY,
			org_id        INT  NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			provider      TEXT NOT NULL CHECK (provider IN ('google', 'azure')),
			client_id     TEXT NOT NULL,
			client_secret TEXT NOT NULL,
			tenant_id     TEXT,
			domain_hint   TEXT,
			enabled       BOOLEAN NOT NULL DEFAULT true,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (org_id, provider)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sso_providers_org ON sso_providers(org_id) WHERE enabled = true`,
		`CREATE INDEX IF NOT EXISTS idx_sso_providers_domain ON sso_providers(domain_hint) WHERE domain_hint IS NOT NULL`,
	}
	for _, stmt := range ssoProviders {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("sso_providers migration: %w", err)
		}
	}

	// Migration: granular per-user permissions (extends admin/user role).
	rolePermissions := []string{
		`CREATE TABLE IF NOT EXISTS role_permissions (
			id                     SERIAL PRIMARY KEY,
			user_id                INT NOT NULL REFERENCES users(id) ON DELETE CASCADE UNIQUE,
			can_write_tags         BOOLEAN NOT NULL DEFAULT false,
			can_ack_alarms         BOOLEAN NOT NULL DEFAULT false,
			can_export_data        BOOLEAN NOT NULL DEFAULT false,
			can_manage_recipes     BOOLEAN NOT NULL DEFAULT false,
			can_manage_shifts      BOOLEAN NOT NULL DEFAULT false,
			can_view_audit         BOOLEAN NOT NULL DEFAULT false,
			can_download_installer BOOLEAN NOT NULL DEFAULT false,
			created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_role_permissions_user ON role_permissions(user_id)`,
	}
	for _, stmt := range rolePermissions {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("role_permissions migration: %w", err)
		}
	}

	// Seed enterprise notification + connector settings.
	enterpriseSeed := `
	INSERT INTO global_settings (key, value, description) VALUES
		('notif_slack_enabled',    'false', 'When true, alarm events are posted to a Slack channel.'),
		('notif_slack_webhook_url','',      'Slack Incoming Webhook URL from app.slack.com.'),
		('notif_teams_enabled',    'false', 'When true, alarm events are posted to a Microsoft Teams channel.'),
		('notif_teams_webhook_url','',      'Teams Incoming Webhook URL.'),
		('notif_pagerduty_enabled','false', 'When true, alarm events trigger PagerDuty incidents.'),
		('notif_pagerduty_routing_key','',  'PagerDuty Events API v2 routing key (32 hex chars).'),
		('influx_enabled',         'false', 'When true, tag history is pushed to InfluxDB v2 in real time.'),
		('influx_url',             '',      'InfluxDB v2 base URL, e.g. https://us-east-1-1.aws.cloud2.influxdata.com'),
		('influx_token',           '',      'InfluxDB v2 API token with write access to the bucket.'),
		('influx_org',             '',      'InfluxDB organization name or ID.'),
		('influx_bucket',          '',      'InfluxDB bucket to write into.'),
		('influx_batch_size',      '500',   'Number of points to batch before flushing to InfluxDB.'),
		('influx_flush_interval',  '10',    'Seconds between forced flushes even if batch_size not reached.')
	ON CONFLICT (key) DO NOTHING;`
	if _, err := db.ExecContext(context.Background(), enterpriseSeed); err != nil {
		log.Printf("Warning: failed to seed enterprise settings: %v", err)
	}

	// Bootstrap "safety net" admin: il file migrations/20250308_schema.sql
	// seedando admin/admin123 gira solo se Postgres trova il volume vuoto
	// (docker-entrypoint-initdb.d). Su un volume preesistente la seed
	// non viene applicata e l'utente non riesce a loggarsi.
	// Qui controlliamo a runtime: se la tabella users esiste ed è vuota,
	// inseriamo l'admin di default. Idempotente via ON CONFLICT.
	if err := bootstrapAdminIfMissing(db); err != nil {
		log.Printf("Warning: bootstrap admin check failed: %v", err)
	}

	// Migration: add LORAWAN as a valid driver_type.
	// The CHECK constraint must be dropped and re-created because PostgreSQL
	// does not support ALTER CONSTRAINT … for CHECK constraints.
	_, _ = db.ExecContext(context.Background(),
		`ALTER TABLE gateways DROP CONSTRAINT IF EXISTS gateways_driver_type_check`)
	if _, err := db.ExecContext(context.Background(),
		`ALTER TABLE gateways ADD CONSTRAINT gateways_driver_type_check
		 CHECK (driver_type IN ('S7','MODBUS_TCP','MQTT','OPC_UA','LORAWAN'))`); err != nil {
		log.Printf("Warning: failed to update gateways_driver_type_check: %v", err)
	}

	// Migration: backup catalog and audit tables.
	if _, err := db.ExecContext(context.Background(), `
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
		)`); err != nil {
		log.Printf("Warning: failed to create backup_catalog: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS backup_audit (
			id         BIGSERIAL PRIMARY KEY,
			action     TEXT NOT NULL,
			filename   TEXT,
			user_email TEXT,
			ip_addr    TEXT,
			details    TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		log.Printf("Warning: failed to create backup_audit: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_backup_audit_created ON backup_audit (created_at DESC)`); err != nil {
		log.Printf("Warning: failed to create backup_audit index: %v", err)
	}

	// Migration: synoptics — SCADA mimic pages. Org-scoped canvas with a
	// background and a JSONB array of freely positioned widgets that bind to
	// tags. The whole layout is saved atomically (no per-widget sub-CRUD).
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS synoptics (
			id               SERIAL PRIMARY KEY,
			org_id           INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
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
		)`); err != nil {
		log.Printf("Warning: failed to create synoptics: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_synoptics_org ON synoptics(org_id, name)`); err != nil {
		log.Printf("Warning: failed to create synoptics index: %v", err)
	}
	// Synoptics can be filed under the plant hierarchy (site → area → line)
	// to power a navigation work tree. Both nullable: an org-level synoptic
	// with no site/area is the "General" node.
	if _, err := db.ExecContext(context.Background(),
		`ALTER TABLE synoptics ADD COLUMN IF NOT EXISTS site_id INT REFERENCES sites(id) ON DELETE SET NULL`); err != nil {
		log.Printf("Warning: failed to add synoptics.site_id: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`ALTER TABLE synoptics ADD COLUMN IF NOT EXISTS area_id INT REFERENCES areas(id) ON DELETE SET NULL`); err != nil {
		log.Printf("Warning: failed to add synoptics.area_id: %v", err)
	}

	// Migration: EU scaling columns on tags.
	// Adds 9 columns for raw-to-engineering-unit conversion (4-20mA, etc.).
	// All idempotent via ADD COLUMN IF NOT EXISTS.
	scalingCols := []string{
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS scaling_enabled BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS scaling_raw_min DOUBLE PRECISION NOT NULL DEFAULT 0`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS scaling_raw_max DOUBLE PRECISION NOT NULL DEFAULT 100`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS scaling_eu_min  DOUBLE PRECISION NOT NULL DEFAULT 0`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS scaling_eu_max  DOUBLE PRECISION NOT NULL DEFAULT 100`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS scaling_clamp   BOOLEAN NOT NULL DEFAULT true`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS eu_unit         TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS eu_decimals     INT  NOT NULL DEFAULT 2`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS invert          BOOLEAN NOT NULL DEFAULT false`,
	}
	for _, stmt := range scalingCols {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("scaling columns migration: %w", err)
		}
	}

	// Migration: security center — account lockout, MFA, last login tracking
	securityUserCols := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS failed_login_count INT NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret TEXT`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_ip INET`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ DEFAULT NOW()`,
	}
	for _, stmt := range securityUserCols {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: security user cols migration: %v", err)
		}
	}

	// Migration: gateway heartbeat columns
	gatewayHeartbeatCols := []string{
		`ALTER TABLE gateways ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ`,
		`ALTER TABLE gateways ADD COLUMN IF NOT EXISTS last_seen_ip INET`,
		`ALTER TABLE gateways ADD COLUMN IF NOT EXISTS agent_version VARCHAR(30)`,
	}
	for _, stmt := range gatewayHeartbeatCols {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: gateway heartbeat cols migration: %v", err)
		}
	}

	// Migration: security_events table
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS security_events (
			id         BIGSERIAL PRIMARY KEY,
			org_id     INT REFERENCES organizations(id) ON DELETE CASCADE,
			event_type VARCHAR(50) NOT NULL,
			severity   VARCHAR(10) NOT NULL DEFAULT 'medium',
			actor      TEXT,
			resource   TEXT,
			detail     JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		log.Printf("Warning: security_events table migration: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_sec_events_org_time ON security_events(org_id, created_at DESC)`); err != nil {
		log.Printf("Warning: security_events index: %v", err)
	}

	// Migration: OTA edge update system.
	// edge_releases: super admin publishes new edge versions.
	// org_update_approvals: per-org approval + status tracking.
	otaUpdates := []string{
		`CREATE TABLE IF NOT EXISTS edge_releases (
			id SERIAL PRIMARY KEY,
			version VARCHAR(30) NOT NULL UNIQUE,
			release_notes TEXT NOT NULL DEFAULT '',
			artifact_url TEXT NOT NULL,
			sha256_checksum CHAR(64) NOT NULL,
			published_by INT REFERENCES users(id),
			published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			is_stable BOOLEAN NOT NULL DEFAULT TRUE
		)`,
		`CREATE TABLE IF NOT EXISTS org_update_approvals (
			id SERIAL PRIMARY KEY,
			org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			release_id INT NOT NULL REFERENCES edge_releases(id),
			approved_by INT REFERENCES users(id),
			approved_at TIMESTAMPTZ,
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			error_msg TEXT,
			UNIQUE(org_id, release_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_org_approvals_org ON org_update_approvals(org_id)`,
	}
	for _, stmt := range otaUpdates {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("ota updates migration: %w", err)
		}
	}

	log.Println("[DB] Auto-migrations completed successfully")
	return nil
}

// bootstrapAdminIfMissing assicura che esista almeno un utente admin
// quando lo schema base è già stato creato ma il seed iniziale non è
// stato applicato (es. volume Postgres riusato da un'installazione
// precedente). Inserisce admin/admin123 con hash bcrypt pre-calcolato
// matching quello in migrations/20250308_schema.sql.
//
// Skip silente se:
//   - la tabella users non esiste (schema base non ancora creato)
//   - esiste già almeno un utente (admin o qualunque)
func bootstrapAdminIfMissing(db *sql.DB) error {
	// Esiste la tabella users?
	var exists bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'users'
		)`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil // schema base non ancora applicato
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // c'è già almeno un utente
	}

	// bcrypt hash di "admin123" (stesso del seed SQL) — costo 10
	const adminHash = "$2a$10$Ot0N4fXJ903diSev0X27KOCcTqI01lTp4gREcAJP/UOOxaRmChBfm"
	_, err := db.Exec(`
		INSERT INTO users (username, password_hash, role, full_name, org_id)
		VALUES ('admin', $1, 'admin', 'System Administrator', NULL)
		ON CONFLICT (username) DO NOTHING`, adminHash)
	if err != nil {
		return err
	}
	log.Println("[DB] Bootstrap: created default admin/admin123 (users table was empty)")
	return nil
}

