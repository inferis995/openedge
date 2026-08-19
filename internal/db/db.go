package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// defaultInitialAdminPassword is used only when OPENEDGE_INITIAL_ADMIN_PASSWORD
// is unset, and its use is logged as a prominent warning at startup.
const defaultInitialAdminPassword = "admin123"

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

	// The web UI's own MQTT identity, separate from the one above.
	//
	// The row above is an EDGE credential: its role may publish tag data, because
	// a gateway must. The browser gets its own, bound to a read-only role, so a
	// signed-in user cannot publish values that look like they came off a PLC.
	// Nullable because organizations created before this column exists get their
	// viewer provisioned on first use rather than by backfilling secrets here.
	for _, stmt := range []string{
		`ALTER TABLE org_mqtt_credentials ADD COLUMN IF NOT EXISTS ui_username TEXT`,
		`ALTER TABLE org_mqtt_credentials ADD COLUMN IF NOT EXISTS ui_password TEXT`,
	} {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("org_mqtt_credentials ui columns migration: %w", err)
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
	// Per-org uniqueness: same email may exist in different orgs (SSO multi-tenant).
	// Global admins (org_id IS NULL) are excluded — covered by idx_users_email above.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_org_email ON users (org_id, email) WHERE org_id IS NOT NULL AND email IS NOT NULL`); err != nil {
		log.Printf("Warning: failed to create idx_users_org_email: %v", err)
	}

	// Migration: password_reset_tokens — one-time tokens, expire 1 hour.
	// NOTE: `token` holds the hex SHA-256 DIGEST of the emailed token, never the token
	// itself (see auth.hashResetToken) — a stolen dump or read replica must not yield
	// usable account-takeover links. No column change is needed: the digest is the same
	// 64-char hex shape the plaintext token had. Rows written before that change are
	// plaintext and will simply never match; they expire within the hour.
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

	// NOTE: the admin bootstrap deliberately does NOT run here. See BootstrapAdmin.

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

	// Migration: LoRaWAN device auto-discovery table.
	// Stores every device that has sent at least one uplink through a LoRaWAN
	// gateway so the UI can show discovered devices and auto-create tags.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS lorawan_devices (
			id               SERIAL PRIMARY KEY,
			gateway_id       INT NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
			device_id        VARCHAR(128) NOT NULL,
			dev_eui          VARCHAR(32)  NOT NULL DEFAULT '',
			last_seen        TIMESTAMPTZ  NOT NULL DEFAULT now(),
			last_rssi        REAL,
			last_snr         REAL,
			last_f_port      INT,
			available_fields JSONB        NOT NULL DEFAULT '{}',
			raw_payload      JSONB,
			uplink_count     BIGINT       NOT NULL DEFAULT 0,
			UNIQUE(gateway_id, device_id)
		)
	`); err != nil {
		log.Printf("Warning: lorawan_devices table: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_lorawan_devices_gw ON lorawan_devices(gateway_id)`); err != nil {
		log.Printf("Warning: lorawan_devices index: %v", err)
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

	// Migration: UDT — user-defined types.
	//
	// The problem they solve: tags are flat. Fifty identical motors mean fifty
	// times N tags entered by hand, and moving one alarm threshold means fifty
	// edits, of which one will be missed. A type declares the shape once —
	// members, addresses relative to a base, scaling, alarms, historisation —
	// and every instance is generated from it and stays bound to it.
	//
	// Instances are NOT copies. Editing the type reconciles every instance, so
	// the type is the single place the truth lives. See the reconciler in
	// internal/handlers/udt.go for what that costs on a member removal.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS udt_types (
			id          SERIAL PRIMARY KEY,
			org_id      INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_by  INT REFERENCES users(id) ON DELETE SET NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (org_id, name)
		)`); err != nil {
		log.Printf("Warning: failed to create udt_types: %v", err)
	}

	// A member is one tag-to-be. address_suffix is appended to the instance's
	// base address, which is what makes the same type work across a Modbus
	// gateway (base "40001", suffix "+2") and an S7 one (base "DB10", suffix
	// ".DBX0.1") without the type knowing which protocol it will land on.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS udt_members (
			id                 SERIAL PRIMARY KEY,
			type_id            INT NOT NULL REFERENCES udt_types(id) ON DELETE CASCADE,
			name               TEXT NOT NULL,
			address_suffix     TEXT NOT NULL DEFAULT '',
			data_type          VARCHAR(20) NOT NULL,
			historize          BOOLEAN NOT NULL DEFAULT false,
			historize_deadband DOUBLE PRECISION NOT NULL DEFAULT 0,
			scaling_enabled    BOOLEAN NOT NULL DEFAULT false,
			scaling_raw_min    DOUBLE PRECISION NOT NULL DEFAULT 0,
			scaling_raw_max    DOUBLE PRECISION NOT NULL DEFAULT 100,
			scaling_eu_min     DOUBLE PRECISION NOT NULL DEFAULT 0,
			scaling_eu_max     DOUBLE PRECISION NOT NULL DEFAULT 100,
			scaling_clamp      BOOLEAN NOT NULL DEFAULT true,
			eu_unit            TEXT NOT NULL DEFAULT '',
			eu_decimals        INT NOT NULL DEFAULT 2,
			invert             BOOLEAN NOT NULL DEFAULT false,
			sort_order         INT NOT NULL DEFAULT 0,
			UNIQUE (type_id, name)
		)`); err != nil {
		log.Printf("Warning: failed to create udt_members: %v", err)
	}

	// Alarms belong to the member, so "high pressure at 8 bar" is stated once
	// for every motor that will ever exist rather than per instance.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS udt_member_alarms (
			id            SERIAL PRIMARY KEY,
			member_id     INT NOT NULL REFERENCES udt_members(id) ON DELETE CASCADE,
			alarm_type    VARCHAR(20) NOT NULL,
			threshold     DOUBLE PRECISION,
			deadband      DOUBLE PRECISION NOT NULL DEFAULT 0,
			delay_seconds INT NOT NULL DEFAULT 0,
			severity      VARCHAR(20) NOT NULL DEFAULT 'warning',
			message       TEXT NOT NULL DEFAULT '',
			enabled       BOOLEAN NOT NULL DEFAULT true
		)`); err != nil {
		log.Printf("Warning: failed to create udt_member_alarms: %v", err)
	}

	// An instance binds a type to one gateway at one base address. The org is
	// derived through the gateway rather than stored, so an instance cannot
	// drift into a different tenant than the tags it owns.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS udt_instances (
			id           SERIAL PRIMARY KEY,
			type_id      INT NOT NULL REFERENCES udt_types(id) ON DELETE RESTRICT,
			gateway_id   INT NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
			name         TEXT NOT NULL,
			base_address TEXT NOT NULL DEFAULT '',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (gateway_id, name)
		)`); err != nil {
		log.Printf("Warning: failed to create udt_instances: %v", err)
	}

	// The link from a generated tag back to what generated it. ON DELETE
	// CASCADE from the instance is deliberate — deleting an instance is an
	// explicit act on that equipment — while udt_member_id is SET NULL so a
	// member removal cannot silently take tags (and their history) with it;
	// the reconciler decides that, loudly. See udt.go.
	for _, stmt := range []string{
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS udt_instance_id INT REFERENCES udt_instances(id) ON DELETE CASCADE`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS udt_member_id   INT REFERENCES udt_members(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_tags_udt_instance ON tags (udt_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_udt_members_type ON udt_members (type_id, sort_order)`,
		`CREATE INDEX IF NOT EXISTS idx_udt_instances_type ON udt_instances (type_id)`,
		`CREATE INDEX IF NOT EXISTS idx_udt_member_alarms_member ON udt_member_alarms (member_id)`,
	} {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: UDT migration (%s…): %v", stmt[:intMinDB(52, len(stmt))], err)
		}
	}

	// Migration: OAuth 2.1 authorization server.
	//
	// This is what lets a remote client — an MCP client, a connector — obtain a
	// token by sending the user to sign in here, instead of the user pasting a
	// long-lived JWT into somebody else's configuration file.
	//
	// Codes and refresh tokens are stored as SHA-256 hashes, never in the
	// clear: a database dump is a serious event, and it should not also be a
	// bag of live credentials.
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS oauth_clients (
			id                 SERIAL PRIMARY KEY,
			client_id          TEXT UNIQUE NOT NULL,
			client_secret_hash TEXT,
			client_name        TEXT NOT NULL,
			redirect_uris      TEXT NOT NULL,
			scope              TEXT NOT NULL DEFAULT 'openedge:read openedge:write',
			created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// consumed_at is kept rather than deleting the row: a code presented
		// twice means it leaked, and the second attempt has to be detectable.
		`CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
			code_hash             TEXT PRIMARY KEY,
			client_id             TEXT NOT NULL,
			user_id               INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id                INT,
			redirect_uri          TEXT NOT NULL,
			scope                 TEXT NOT NULL,
			code_challenge        TEXT NOT NULL,
			code_challenge_method TEXT NOT NULL,
			resource              TEXT,
			expires_at            TIMESTAMPTZ NOT NULL,
			consumed_at           TIMESTAMPTZ,
			created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
			token_hash TEXT PRIMARY KEY,
			client_id  TEXT NOT NULL,
			user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id     INT,
			scope      TEXT NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			revoked_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_codes_expiry ON oauth_authorization_codes (expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_refresh_user ON oauth_refresh_tokens (client_id, user_id)`,
	} {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: OAuth migration (%s…): %v", stmt[:intMinDB(52, len(stmt))], err)
		}
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
		// last_totp_counter — the TOTP time step most recently accepted for this user.
		// A TOTP code is valid for the whole skew window, so without recording the step
		// an observed code can be replayed. auth.CompleteMFALogin requires each accepted
		// counter to be strictly greater than this value.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS last_totp_counter BIGINT`,
		// token_version — JWT invalidation epoch, bumped by auth.ResetPassword and
		// embedded as a claim by auth.generateToken. TODO(security): middleware.RequireAuth
		// must still compare the claim against this column to actually revoke old sessions.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 0`,
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

	// Migration: historian retention days setting
	historianSeed := `
	INSERT INTO global_settings (key, value, description) VALUES
		('historian_retention_days', '365', 'Number of days to retain historian data. 0 disables automatic cleanup.')
	ON CONFLICT (key) DO NOTHING;`
	if _, err := db.ExecContext(context.Background(), historianSeed); err != nil {
		log.Printf("Warning: failed to seed historian_retention_days: %v", err)
	}

	// Migration: OT Compliance — asset inventory, CVE tracking, compliance frameworks.
	// Part of "From Visibility to Compliance in Four Steps" (Steps 1 & 2).
	otComplianceTables := []string{
		// OT Asset inventory (discovered + manually entered devices)
		`CREATE TABLE IF NOT EXISTS ot_assets (
			id              SERIAL PRIMARY KEY,
			org_id          INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			gateway_id      INT REFERENCES gateways(id) ON DELETE SET NULL,
			ip_address      VARCHAR(45),
			mac_address     VARCHAR(17),
			hostname        VARCHAR(255),
			vendor          VARCHAR(128),
			device_type     VARCHAR(64),
			model           VARCHAR(128),
			firmware_ver    VARCHAR(64),
			protocol        VARCHAR(32),
			os_info         VARCHAR(128),
			is_authorized   BOOLEAN NOT NULL DEFAULT true,
			risk_score      REAL NOT NULL DEFAULT 0,
			last_seen       TIMESTAMPTZ NOT NULL DEFAULT now(),
			discovered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			notes           TEXT,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(org_id, ip_address)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ot_assets_org ON ot_assets(org_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ot_assets_risk ON ot_assets(org_id, risk_score DESC)`,
		// CVE matches per asset
		`CREATE TABLE IF NOT EXISTS ot_asset_cves (
			id          SERIAL PRIMARY KEY,
			asset_id    INT NOT NULL REFERENCES ot_assets(id) ON DELETE CASCADE,
			cve_id      VARCHAR(20) NOT NULL,
			severity    VARCHAR(10) NOT NULL,
			cvss_score  REAL,
			description TEXT,
			published   TIMESTAMPTZ,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(asset_id, cve_id)
		)`,
		// Compliance framework checklists (NIS2 + IEC 62443)
		`CREATE TABLE IF NOT EXISTS compliance_frameworks (
			id          SERIAL PRIMARY KEY,
			code        VARCHAR(32) NOT NULL UNIQUE,
			name        VARCHAR(128) NOT NULL,
			version     VARCHAR(32)
		)`,
		`CREATE TABLE IF NOT EXISTS compliance_requirements (
			id              SERIAL PRIMARY KEY,
			framework_id    INT NOT NULL REFERENCES compliance_frameworks(id),
			req_code        VARCHAR(32) NOT NULL,
			category        VARCHAR(64),
			title           VARCHAR(255) NOT NULL,
			description     TEXT,
			weight          INT NOT NULL DEFAULT 1,
			UNIQUE(framework_id, req_code)
		)`,
		`CREATE TABLE IF NOT EXISTS compliance_assessments (
			id              SERIAL PRIMARY KEY,
			org_id          INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			requirement_id  INT NOT NULL REFERENCES compliance_requirements(id),
			status          VARCHAR(16) NOT NULL DEFAULT 'not_assessed',
			evidence        TEXT,
			notes           TEXT,
			assessed_by     INT REFERENCES users(id),
			assessed_at     TIMESTAMPTZ,
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(org_id, requirement_id)
		)`,
		// Scan jobs (network discovery scans)
		`CREATE TABLE IF NOT EXISTS ot_scan_jobs (
			id          SERIAL PRIMARY KEY,
			org_id      INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			subnet      VARCHAR(64) NOT NULL,
			status      VARCHAR(16) NOT NULL DEFAULT 'pending',
			started_at  TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			found_count INT NOT NULL DEFAULT 0,
			new_count   INT NOT NULL DEFAULT 0,
			error       TEXT,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_by  INT REFERENCES users(id)
		)`,
	}
	for _, stmt := range otComplianceTables {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("ot_compliance migration: %w", err)
		}
	}

	// Seed NIS2 and IEC 62443 frameworks if not present
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO compliance_frameworks (code, name, version) VALUES
		('NIS2',    'NIS2 Directive Art. 21', '2022/2555'),
		('IEC62443','IEC 62443 Industrial Cybersecurity', '2023')
		ON CONFLICT (code) DO NOTHING
	`)

	// NIS2 Art.21 requirements (10 key measures)
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO compliance_requirements (framework_id, req_code, category, title, description, weight)
		SELECT f.id, r.code, r.cat, r.title, r.desc, r.w
		FROM compliance_frameworks f,
		(VALUES
		  ('NIS2-A','Governance','Politiche di sicurezza','Politiche documentate per la sicurezza dei sistemi informativi e di rete',2),
		  ('NIS2-B','Risk Management','Analisi del rischio','Valutazione e gestione dei rischi di sicurezza informatica',3),
		  ('NIS2-C','Incident','Gestione degli incidenti','Procedure per rilevamento, notifica e risposta agli incidenti',3),
		  ('NIS2-D','Continuity','Continuità operativa','Business continuity e disaster recovery',2),
		  ('NIS2-E','Supply Chain','Sicurezza della catena di fornitura','Valutazione sicurezza dei fornitori e terze parti',2),
		  ('NIS2-F','Acquisition','Sicurezza nello sviluppo','Sicurezza nell acquisizione e sviluppo di sistemi',1),
		  ('NIS2-G','Vulnerability','Gestione vulnerabilità','Procedure per gestione e divulgazione delle vulnerabilità',3),
		  ('NIS2-H','Cryptography','Crittografia','Uso di crittografia e cifratura',2),
		  ('NIS2-I','Access','Controllo degli accessi','Gestione identità e accessi privilegiati (MFA)',3),
		  ('NIS2-L','Awareness','Formazione','Formazione in materia di sicurezza informatica',1)
		) AS r(code, cat, title, desc, w)
		WHERE f.code = 'NIS2'
		ON CONFLICT (framework_id, req_code) DO NOTHING
	`)

	// IEC 62443 SL1 requirements (12 key controls)
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO compliance_requirements (framework_id, req_code, category, title, description, weight)
		SELECT f.id, r.code, r.cat, r.title, r.desc, r.w
		FROM compliance_frameworks f,
		(VALUES
		  ('SL1-IAC','Access Control','Controllo Accessi OT','Identificazione e autenticazione per tutti gli utenti OT',3),
		  ('SL1-UC','Use Control','Controllo Utilizzo','Autorizzazione per tutte le operazioni sui sistemi OT',2),
		  ('SL1-SI','System Integrity','Integrità del Sistema','Protezione da modifiche non autorizzate ai sistemi di controllo',3),
		  ('SL1-DC','Data Confidentiality','Confidenzialità Dati','Protezione dei dati in transito e a riposo',2),
		  ('SL1-RDF','Restricted Data Flow','Flusso Dati Controllato','Segmentazione delle reti OT e zone di sicurezza',3),
		  ('SL1-TRE','Timely Response','Risposta Tempestiva','Capacità di rispondere agli incidenti di sicurezza OT',2),
		  ('SL1-RA','Resource Availability','Disponibilità Risorse','Continuità operativa dei sistemi di controllo',2),
		  ('SL1-SWI','Software Integrity','Integrità Software','Autenticità e integrità del software OT',2),
		  ('SL1-SCI','Supply Chain Integrity','Integrità Supply Chain','Verifica componenti e software di terze parti',1),
		  ('SL1-PM','Patch Management','Gestione Patch','Politiche di aggiornamento per sistemi OT',2),
		  ('SL1-CB','Component Backup','Backup Componenti','Backup e ripristino delle configurazioni OT',2),
		  ('SL1-NM','Network Monitoring','Monitoraggio Rete','Rilevamento anomalie e accessi non autorizzati',3)
		) AS r(code, cat, title, desc, w)
		WHERE f.code = 'IEC62443'
		ON CONFLICT (framework_id, req_code) DO NOTHING
	`)

	// Migration: OT threat events (Step 3 — continuous monitoring)
	threatTables := []string{
		`CREATE TABLE IF NOT EXISTS ot_threat_events (
			id          SERIAL PRIMARY KEY,
			org_id      INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			asset_id    INT REFERENCES ot_assets(id) ON DELETE SET NULL,
			event_type  VARCHAR(32) NOT NULL,
			severity    VARCHAR(10) NOT NULL DEFAULT 'info',
			title       VARCHAR(255) NOT NULL,
			description TEXT,
			source      VARCHAR(64),
			ip_address  VARCHAR(45),
			resolved    BOOLEAN NOT NULL DEFAULT false,
			resolved_at TIMESTAMPTZ,
			resolved_by INT REFERENCES users(id),
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ot_threats_org ON ot_threat_events(org_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_ot_threats_unresolved ON ot_threat_events(org_id, resolved) WHERE resolved = false`,
	}
	for _, stmt := range threatTables {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: ot_threat_events migration: %v", err)
		}
	}

	// Migration: compliance reports (Step 4 — audit-ready reports)
	reportTables := []string{
		`CREATE TABLE IF NOT EXISTS compliance_reports (
			id              SERIAL PRIMARY KEY,
			org_id          INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			report_type     VARCHAR(32) NOT NULL,
			title           VARCHAR(255) NOT NULL,
			period_from     TIMESTAMPTZ,
			period_to       TIMESTAMPTZ,
			status          VARCHAR(16) NOT NULL DEFAULT 'generating',
			format          VARCHAR(8)  NOT NULL DEFAULT 'json',
			content         JSONB,
			file_path       TEXT,
			generated_by    INT REFERENCES users(id),
			generated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			error           TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_compliance_reports_org ON compliance_reports(org_id, generated_at DESC)`,
	}
	for _, stmt := range reportTables {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: compliance_reports migration: %v", err)
		}
	}

	// Migration: NIS2 Art.21 expanded checklist — 30 items covering (a-j).
	// Adds auto_assessable + article_ref columns and seeds the full 30-item list.
	nis2ExpandCols := []string{
		`ALTER TABLE compliance_requirements ADD COLUMN IF NOT EXISTS auto_assessable BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE compliance_requirements ADD COLUMN IF NOT EXISTS article_ref VARCHAR(32)`,
	}
	for _, stmt := range nis2ExpandCols {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: compliance_requirements alter: %v", err)
		}
	}

	// Seed 30-item NIS2 checklist (Art.21 a-j). ON CONFLICT DO NOTHING is idempotent.
	_, _ = db.ExecContext(context.Background(), `
		INSERT INTO compliance_requirements (framework_id, req_code, category, title, weight, auto_assessable, article_ref)
		SELECT f.id, r.code, r.cat, r.title, r.w::int, r.aa::boolean, r.aref
		FROM compliance_frameworks f,
		(VALUES
		  ('NIS2-A1','Governance',     'Politica di sicurezza',                              '2','false','Art.21(a)'),
		  ('NIS2-A2','Risk Management','Analisi del rischio periodica',                      '3','true', 'Art.21(a)'),
		  ('NIS2-B1','Incident',       'Rilevamento degli incidenti',                        '3','true', 'Art.21(b)'),
		  ('NIS2-B2','Incident',       'Procedura di risposta agli incidenti',               '3','false','Art.21(b)'),
		  ('NIS2-B3','Incident',       'Notifica Art.23 al CSIRT (24h early warning)',       '3','true', 'Art.21(b)/Art.23'),
		  ('NIS2-B4','Incident',       'Rapporto finale incidente (30 giorni)',              '2','true', 'Art.21(b)/Art.23'),
		  ('NIS2-C1','Continuity',     'Piano di continuita operativa (BCP)',                '2','false','Art.21(c)'),
		  ('NIS2-C2','Continuity',     'Piano di disaster recovery (DRP)',                   '2','false','Art.21(c)'),
		  ('NIS2-C3','Continuity',     'Test periodici del DRP',                             '2','false','Art.21(c)'),
		  ('NIS2-D1','Supply Chain',   'Inventario fornitori critici',                       '2','true', 'Art.21(d)'),
		  ('NIS2-D2','Supply Chain',   'Valutazione sicurezza fornitori',                    '3','true', 'Art.21(d)'),
		  ('NIS2-D3','Supply Chain',   'Clausole di sicurezza nei contratti',                '2','true', 'Art.21(d)'),
		  ('NIS2-E1','Acquisition',    'Sicurezza nello sviluppo e acquisizione sistemi',    '1','false','Art.21(e)'),
		  ('NIS2-E2','Acquisition',    'Protocolli sicuri nelle comunicazioni OT',           '3','true', 'Art.21(e)'),
		  ('NIS2-F1','Audit',          'Audit e valutazione dell efficacia',                 '1','false','Art.21(f)'),
		  ('NIS2-F2','Audit',          'Indicatori KPI di sicurezza',                        '1','false','Art.21(f)'),
		  ('NIS2-G1','Awareness',      'Formazione cybersecurity per il personale',          '1','false','Art.21(g)'),
		  ('NIS2-G2','Awareness',      'Consapevolezza sui rischi cyber (awareness)',        '1','false','Art.21(g)'),
		  ('NIS2-G3','Vulnerability',  'Gestione delle vulnerabilita (patch)',               '3','true', 'Art.21(g)'),
		  ('NIS2-H1','Cryptography',   'Uso di crittografia per dati in transito',          '2','true', 'Art.21(h)'),
		  ('NIS2-H2','Cryptography',   'Uso di crittografia per dati a riposo',             '2','false','Art.21(h)'),
		  ('NIS2-H3','Cryptography',   'Gestione delle chiavi crittografiche',              '1','false','Art.21(h)'),
		  ('NIS2-I1','HR Security',    'Procedure di onboarding sicuro del personale',      '1','false','Art.21(i)'),
		  ('NIS2-I2','HR Security',    'Revoca accessi alla cessazione del rapporto',       '2','false','Art.21(i)'),
		  ('NIS2-I3','HR Security',    'Gestione degli accessi privilegiati (PAM)',         '2','false','Art.21(i)'),
		  ('NIS2-J1','Access Control', 'Autenticazione multi-fattore (MFA)',                '3','true', 'Art.21(j)'),
		  ('NIS2-J2','Access Control', 'Gestione identita e accessi (IAM)',                 '2','false','Art.21(j)'),
		  ('NIS2-J3','Access Control', 'Segregazione delle reti OT/IT',                    '3','true', 'Art.21(j)'),
		  ('NIS2-J4','Access Control', 'Monitoraggio degli accessi e audit log',            '2','true', 'Art.21(j)'),
		  ('NIS2-J5','Access Control', 'Gestione sessioni sicure',                          '1','false','Art.21(j)')
		) AS r(code, cat, title, w, aa, aref)
		WHERE f.code = 'NIS2'
		ON CONFLICT (framework_id, req_code) DO NOTHING
	`)

	// Update article_ref for the original 10 NIS2 items (idempotent).
	nis2ArticleRefs := []string{
		`UPDATE compliance_requirements SET article_ref='Art.21(a)' WHERE req_code='NIS2-A' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(b)' WHERE req_code='NIS2-B' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(c)' WHERE req_code='NIS2-C' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(d)' WHERE req_code='NIS2-D' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(e)' WHERE req_code='NIS2-E' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(f)' WHERE req_code='NIS2-F' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(g)' WHERE req_code='NIS2-G' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(h)' WHERE req_code='NIS2-H' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(i)' WHERE req_code='NIS2-I' AND article_ref IS NULL`,
		`UPDATE compliance_requirements SET article_ref='Art.21(j)' WHERE req_code='NIS2-L' AND article_ref IS NULL`,
	}
	for _, stmt := range nis2ArticleRefs {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: NIS2 article_ref update: %v", err)
		}
	}

	// Migration: CSIRT incidents (Art.23 NIS2 — incident reporting with legal deadlines).
	csirtMigration := []string{
		`CREATE TABLE IF NOT EXISTS csirt_incidents (
			id                     SERIAL PRIMARY KEY,
			org_id                 INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			threat_event_id        INT REFERENCES ot_threat_events(id) ON DELETE SET NULL,
			title                  VARCHAR(255) NOT NULL,
			description            TEXT,
			severity               VARCHAR(10) NOT NULL DEFAULT 'high',
			status                 VARCHAR(20) NOT NULL DEFAULT 'open',
			affected_systems       TEXT,
			impact_description     TEXT,
			early_warning_due      TIMESTAMPTZ NOT NULL,
			notification_due       TIMESTAMPTZ NOT NULL,
			final_report_due       TIMESTAMPTZ NOT NULL,
			early_warning_sent_at  TIMESTAMPTZ,
			notification_sent_at   TIMESTAMPTZ,
			final_report_sent_at   TIMESTAMPTZ,
			root_cause             TEXT,
			remediation            TEXT,
			created_by             INT REFERENCES users(id),
			closed_by              INT REFERENCES users(id),
			closed_at              TIMESTAMPTZ,
			created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_csirt_incidents_org  ON csirt_incidents(org_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_csirt_incidents_open ON csirt_incidents(org_id, status) WHERE status != 'closed'`,
	}
	for _, stmt := range csirtMigration {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: csirt_incidents migration: %v", err)
		}
	}

	// Migration: OT vendors (Art.18 NIS2 — supply chain vendor risk).
	vendorMigration := []string{
		`CREATE TABLE IF NOT EXISTS ot_vendors (
			id                 SERIAL PRIMARY KEY,
			org_id             INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			name               VARCHAR(128) NOT NULL,
			country            VARCHAR(64),
			website            VARCHAR(255),
			contact_email      VARCHAR(255),
			products_used      TEXT,
			has_iso27001       BOOLEAN NOT NULL DEFAULT false,
			has_soc2           BOOLEAN NOT NULL DEFAULT false,
			has_iec62443       BOOLEAN NOT NULL DEFAULT false,
			last_audit_date    DATE,
			data_access_level  VARCHAR(20) NOT NULL DEFAULT 'none',
			network_access     BOOLEAN NOT NULL DEFAULT false,
			remote_access      BOOLEAN NOT NULL DEFAULT false,
			contract_start     DATE,
			contract_end       DATE,
			security_clauses   BOOLEAN NOT NULL DEFAULT false,
			risk_score         INT NOT NULL DEFAULT 50,
			criticality        VARCHAR(10) NOT NULL DEFAULT 'medium',
			notes              TEXT,
			auto_imported      BOOLEAN NOT NULL DEFAULT false,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (org_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ot_vendors_org ON ot_vendors(org_id, risk_score)`,
	}
	for _, stmt := range vendorMigration {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("Warning: ot_vendors migration: %v", err)
		}
	}

	// MFA recovery codes
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS mfa_recovery_codes (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at TIMESTAMPTZ
)`); err != nil {
		log.Printf("Warning: mfa_recovery_codes migration: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `CREATE INDEX IF NOT EXISTS idx_mfa_recovery_codes_user ON mfa_recovery_codes(user_id)`); err != nil {
		log.Printf("Warning: mfa_recovery_codes index: %v", err)
	}
	// MFA required per org
	if _, err := db.ExecContext(context.Background(), `ALTER TABLE organizations ADD COLUMN IF NOT EXISTS mfa_required BOOLEAN NOT NULL DEFAULT false`); err != nil {
		log.Printf("Warning: org mfa_required migration: %v", err)
	}

	// Migration: at most one open alarm_events row per (tag_id, definition_id).
	ensureSingleOpenAlarmEvent(db)

	log.Println("[DB] Auto-migrations completed successfully")
	return nil
}

// ensureSingleOpenAlarmEvent enforces "at most one open alarm_events row per
// (tag_id, definition_id)".
//
// internal/alarms claimed to rely on a partial unique index called
// alarm_events_active_unique to deduplicate its INSERTs, but that index existed
// in no migration: the INSERT never failed, the duplicate-handling branch was
// dead code and nothing stopped two ACTIVE rows for the same alarm — one of
// which could then never be cleared, leaving a phantom alarm on the operator's
// screen forever.
//
// The migration runs in two steps and is safe to re-run:
//
//  1. De-duplicate. Existing installations may already hold duplicate open rows
//     and CREATE UNIQUE INDEX would simply fail on them, leaving the index
//     permanently absent. So the most recent open row per (tag_id, definition_id)
//     is kept and the older ones are closed (status CLEARED, clear_time set to
//     their own trigger_time so no fake alarm duration appears in reports).
//     This runs in its own transaction: either every stale duplicate is closed
//     or none is — the table is never left half-migrated.
//
//  2. Create the index. On TimescaleDB alarm_events is a hypertable partitioned
//     on trigger_time, and Postgres only accepts unique indexes on a partitioned
//     table when they contain the partitioning column — which would defeat the
//     purpose. There the CREATE fails and we log it as an explicit warning
//     rather than aborting startup: internal/alarms also enforces the invariant
//     in its INSERT (conditional on WHERE NOT EXISTS), so the index is the
//     backstop, not the only line of defence. A plain non-unique partial index
//     is created either way to keep that WHERE NOT EXISTS probe cheap.
func ensureSingleOpenAlarmEvent(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Step 0: nothing to do if the alarm pipeline has not been provisioned yet
	// (fresh DB where migrations/20250308_schema.sql has not run).
	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'alarm_events'
		)`).Scan(&exists); err != nil || !exists {
		return
	}

	// Step 1: close the duplicates, atomically.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[DB] alarm_events dedup: cannot start transaction: %v", err)
		return
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE alarm_events ae
		SET status = 'CLEARED', clear_time = ae.trigger_time
		FROM (
			SELECT tag_id, definition_id, MAX(trigger_time) AS keep_time
			FROM alarm_events
			WHERE status = 'ACTIVE' AND clear_time IS NULL
			GROUP BY tag_id, definition_id
			HAVING COUNT(*) > 1
		) dup
		WHERE ae.tag_id = dup.tag_id
		  AND ae.definition_id = dup.definition_id
		  AND ae.status = 'ACTIVE'
		  AND ae.clear_time IS NULL
		  AND ae.trigger_time < dup.keep_time`)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("[DB] alarm_events dedup failed, rolled back — %s index NOT created: %v",
			"alarm_events_active_unique", err)
		return
	}
	closed, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		log.Printf("[DB] alarm_events dedup: commit failed, nothing changed: %v", err)
		return
	}
	if closed > 0 {
		log.Printf("[DB] alarm_events: closed %d duplicate open alarm rows (kept the most recent per tag+definition)", closed)
	}

	// Step 2a: lookup index for the application-level duplicate check.
	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS alarm_events_active_lookup
		ON alarm_events (tag_id, definition_id)
		WHERE status = 'ACTIVE' AND clear_time IS NULL`); err != nil {
		log.Printf("Warning: alarm_events_active_lookup index: %v", err)
	}

	// Step 2b: the real constraint. Best-effort — see the doc comment.
	if _, err := db.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS alarm_events_active_unique
		ON alarm_events (tag_id, definition_id)
		WHERE status = 'ACTIVE' AND clear_time IS NULL`); err != nil {
		log.Printf("[DB] alarm_events_active_unique NOT created (%v) — duplicate ACTIVE alarms "+
			"remain prevented by the conditional INSERT in internal/alarms; this is expected on "+
			"TimescaleDB, where a unique index on the alarm_events hypertable would have to "+
			"include trigger_time", err)
	}
}

// isHypertable reports whether a table is managed by TimescaleDB.
//
// It matters because the two cases need different tools, and using the wrong
// one is slow rather than wrong — which is the failure mode that only shows up
// once a plant has been running for a year.
func isHypertable(db *sql.DB, table string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var exists bool
	// timescaledb_information.hypertables is absent entirely when the extension
	// is not installed, so the query erroring IS the answer.
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM timescaledb_information.hypertables
			WHERE hypertable_name = $1
		)`, table).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// EnsureRetentionPolicies hands aging and compression to TimescaleDB.
//
// What was here before: the hypertables were created without a single policy —
// migrations/20250308_schema.sql carried add_compression_policy and
// add_retention_policy, but nothing ever executed that file, so on every real
// install the historian was an uncompressed hypertable that grew without
// bound. Aging was a Go worker issuing DELETE ... WHERE time < cutoff once a
// day.
//
// Why that is not good enough for a plant. DELETE on a hypertable rewrites
// rows, leaves dead tuples across every chunk, and needs a VACUUM afterwards
// that does not return the space to the filesystem; drop_chunks unlinks whole
// chunk tables and returns the space immediately. And without compression the
// historian occupies several times what it needs to — for append-only numeric
// series ordered by time, which is the case compression was designed for.
//
// The policies are removed and re-added rather than created if_not_exists,
// because the retention window is operator-configurable: if_not_exists would
// leave yesterday's interval in place and silently ignore the new setting.
func EnsureRetentionPolicies(db *sql.DB, historianRetentionDays int) {
	if !isHypertable(db, "tag_history") {
		log.Printf("[RETENTION] tag_history is not a hypertable — TimescaleDB policies skipped; " +
			"the fallback cleanup worker will age data with DELETE instead")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Compression pays off most on tag_history: append-only, ordered by time,
	// and read almost exclusively one tag at a time. Segmenting by tag_id keeps
	// a single-tag trend query reading one compressed batch instead of
	// decompressing the whole chunk.
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE tag_history SET (
			timescaledb.compress,
			timescaledb.compress_segmentby = 'tag_id',
			timescaledb.compress_orderby   = 'time DESC'
		)`); err != nil {
		log.Printf("[RETENTION] could not enable compression on tag_history: %v", err)
	} else {
		// Seven days uncompressed: long enough that the recent window most
		// dashboards read stays uncompressed and fast, short enough that the
		// bulk of the history is compressed.
		_, _ = db.ExecContext(ctx, `SELECT remove_compression_policy('tag_history', if_exists => TRUE)`)
		if _, err := db.ExecContext(ctx,
			`SELECT add_compression_policy('tag_history', INTERVAL '7 days')`); err != nil {
			log.Printf("[RETENTION] could not add compression policy: %v", err)
		} else {
			log.Printf("[RETENTION] tag_history compressed after 7 days")
		}
	}

	// Retention, per table. system_events is operational chatter — gateway up
	// and down — and is worth far less than the tag values, so it is aged more
	// aggressively. alarm_events follows the historian window because operators
	// report on alarm history over the same period they trend values.
	policies := []struct {
		table string
		days  int
	}{
		{"tag_history", historianRetentionDays},
		{"alarm_events", historianRetentionDays},
		{"system_events", 90},
	}

	for _, p := range policies {
		if p.days <= 0 {
			// Explicitly disabled. Drop any policy left over from a previous
			// setting, or the old window would silently keep deleting.
			if isHypertable(db, p.table) {
				_, _ = db.ExecContext(ctx,
					fmt.Sprintf(`SELECT remove_retention_policy('%s', if_exists => TRUE)`, p.table))
				log.Printf("[RETENTION] %s: retention disabled, data kept indefinitely", p.table)
			}
			continue
		}
		if !isHypertable(db, p.table) {
			continue
		}
		_, _ = db.ExecContext(ctx,
			fmt.Sprintf(`SELECT remove_retention_policy('%s', if_exists => TRUE)`, p.table))
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf(`SELECT add_retention_policy('%s', INTERVAL '%d days')`, p.table, p.days)); err != nil {
			log.Printf("[RETENTION] could not set retention on %s: %v", p.table, err)
			continue
		}
		log.Printf("[RETENTION] %s: chunks older than %d days dropped automatically", p.table, p.days)
	}
}

// StartHistorianRetentionWorker runs a daily cleanup of old historian data.
// retentionDays <= 0 disables cleanup.
//
// This remains as the fallback for a deployment where TimescaleDB is absent and
// tag_history is an ordinary table. When it IS a hypertable, TimescaleDB's own
// retention job owns aging and this worker drops old chunks rather than
// deleting rows — see runHistorianCleanup.
func StartHistorianRetentionWorker(db *sql.DB, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	go func() {
		time.Sleep(5 * time.Minute) // delay initial run
		for {
			runHistorianCleanup(db, retentionDays)
			time.Sleep(24 * time.Hour)
		}
	}()
}

// StartOAuthCleanupWorker prunes spent OAuth rows.
//
// Codes live two minutes and refresh tokens thirty days, but neither table
// deletes anything on its own: a consumed code is kept so a replay stays
// detectable, and a revoked refresh token is kept for the same reason. Without
// this, both grow for the life of the installation.
//
// The delay is generous — a replay arrives within seconds, not days — and the
// point is only that these tables stop growing, not that they stay small.
func StartOAuthCleanupWorker(db *sql.DB) {
	go func() {
		time.Sleep(10 * time.Minute)
		for {
			runOAuthCleanup(db)
			time.Sleep(6 * time.Hour)
		}
	}()
}

func runOAuthCleanup(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for _, stmt := range []string{
		`DELETE FROM oauth_authorization_codes WHERE expires_at < NOW() - INTERVAL '1 day'`,
		`DELETE FROM oauth_refresh_tokens
		  WHERE expires_at < NOW() - INTERVAL '7 days'
		     OR (revoked_at IS NOT NULL AND revoked_at < NOW() - INTERVAL '7 days')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			log.Printf("[OAUTH] cleanup: %v", err)
		}
	}
}

func runHistorianCleanup(db *sql.DB, retentionDays int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// On a hypertable, drop the chunks. DELETE would rewrite rows and leave
	// dead tuples spread across every chunk, and the VACUUM that follows does
	// not hand the space back to the filesystem — so a disk that filled up
	// stays full even after the cleanup "succeeded". drop_chunks unlinks whole
	// chunk tables, which is both far cheaper and actually reclaims space.
	//
	// This normally finds nothing to do: TimescaleDB's own retention policy,
	// installed by EnsureRetentionPolicies, gets there first. It stays as the
	// backstop for the case where the background scheduler is not running.
	if isHypertable(db, "tag_history") {
		if _, err := db.ExecContext(ctx,
			`SELECT drop_chunks('tag_history', older_than => $1::timestamptz)`, cutoff); err != nil {
			log.Printf("[HISTORIAN] drop_chunks failed: %v", err)
		}
		return
	}

	result, err := db.ExecContext(ctx, `DELETE FROM tag_history WHERE time < $1`, cutoff)
	if err != nil {
		log.Printf("[HISTORIAN] Cleanup error: %v", err)
		return
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("[HISTORIAN] Removed %d rows older than %d days", rows, retentionDays)
		_, _ = db.ExecContext(ctx, `VACUUM ANALYZE tag_history`)
	}
}

// BootstrapAdmin creates the initial administrator when the users table is
// empty, then reports whether any global admin still uses the built-in default
// password.
//
// It is EXPORTED and called explicitly by core-api instead of running inside
// runAutoMigrations, because every service — driver-manager and all six drivers
// included — calls Connect and therefore ran the migrations. Whichever process
// reached an empty users table first created the account, and only core-api's
// container is given OPENEDGE_INITIAL_ADMIN_PASSWORD: in practice
// driver-manager usually won the race and created 'admin' with the default
// password, silently defeating the variable on every fresh install.
//
// Creating login credentials is core-api's job. Adding the variable to the
// other containers would paper over that, and would still leave the outcome
// dependent on a start-up race.
func BootstrapAdmin(db *sql.DB) {
	if err := bootstrapAdminIfMissing(db); err != nil {
		log.Printf("Warning: bootstrap admin check failed: %v", err)
	}
	warnAboutDefaultAdminPassword(db)
}

// bootstrapAdminIfMissing assicura che esista almeno un utente admin
// quando lo schema base è già stato creato ma il seed iniziale non è
// stato applicato (es. volume Postgres riusato da un'installazione
// precedente).
//
// La password iniziale viene da OPENEDGE_INITIAL_ADMIN_PASSWORD: la
// variabile è documentata in .env.example, README e nei compose file ma
// non veniva letta da nessuna parte, quindi ogni installazione restava
// raggiungibile con admin/admin123 — incluse quelle di operatori
// convinti di aver impostato una password forte. Senza la variabile
// l'admin di default viene comunque creato (altrimenti il primo accesso
// sarebbe impossibile) ma con un avviso esplicito a log.
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

	initialPassword := os.Getenv("OPENEDGE_INITIAL_ADMIN_PASSWORD")
	usingDefault := initialPassword == ""
	if usingDefault {
		initialPassword = defaultInitialAdminPassword
	}
	if len(initialPassword) < 8 {
		return fmt.Errorf("OPENEDGE_INITIAL_ADMIN_PASSWORD must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(initialPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash initial admin password: %w", err)
	}

	if _, err := db.Exec(`
		INSERT INTO users (username, password_hash, role, full_name, org_id)
		VALUES ('admin', $1, 'admin', 'System Administrator', NULL)
		ON CONFLICT (username) DO NOTHING`, string(hash)); err != nil {
		return err
	}

	if usingDefault {
		warnDefaultAdminPassword("created the admin account with the DEFAULT password")
	} else {
		log.Println("[DB] Bootstrap: created admin user with the password from OPENEDGE_INITIAL_ADMIN_PASSWORD")
	}
	return nil
}

// warnAboutDefaultAdminPassword checks, at every startup, whether any account
// still authenticates with the built-in default password and says so loudly.
//
// Existing installations were all seeded with it, and rotating a live
// credential automatically would be worse than reporting it — so this only
// reports. It runs after bootstrap and is best-effort: any error is ignored,
// since this must never block startup.
func warnAboutDefaultAdminPassword(db *sql.DB) {
	rows, err := db.Query(`SELECT username, password_hash FROM users WHERE role = 'admin' AND org_id IS NULL`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var username, hash string
		if err := rows.Scan(&username, &hash); err != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(defaultInitialAdminPassword)) == nil {
			warnDefaultAdminPassword(fmt.Sprintf("global admin %q still uses the DEFAULT password", username))
			return
		}
	}
}

func warnDefaultAdminPassword(what string) {
	log.Printf("[DB] ############################################################")
	log.Printf("[DB] SECURITY WARNING: %s (%q).", what, defaultInitialAdminPassword)
	log.Printf("[DB] This account is a GLOBAL ADMIN: it can read every tenant's data")
	log.Printf("[DB] and write setpoints to every PLC. Change it now from the UI, or")
	log.Printf("[DB] set OPENEDGE_INITIAL_ADMIN_PASSWORD before the first start.")
	log.Printf("[DB] ############################################################")
}

// intMinDB is a local min for truncating statements in log lines.
func intMinDB(a, b int) int {
	if a < b {
		return a
	}
	return b
}
