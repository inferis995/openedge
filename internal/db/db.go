package db

import (
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
	db.SetConnMaxIdleTime(10 * time.Minute)

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
		('kpi_target_logins_24h_min',  '0',   'Soglia minima login nelle 24h.')
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

	log.Println("[DB] Auto-migrations completed successfully")
	return nil
}

