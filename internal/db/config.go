package db

// Config holds the PostgreSQL connection configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}
