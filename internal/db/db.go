package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// Config holds database connection configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// Connect creates a new database connection
func Connect(cfg Config) (*sql.DB, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
