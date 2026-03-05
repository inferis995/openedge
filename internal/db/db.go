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
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
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

	log.Println("[DB] Auto-migrations completed successfully")
	return nil
}
