package db

// Config holds the PostgreSQL connection configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string

	// Pool bounds; zero means the defaults (25 open, 10 idle). The
	// DB_MAX_OPEN_CONNS and DB_MAX_IDLE_CONNS environment variables override
	// them either way.
	MaxOpenConns int
	MaxIdleConns int

	// SkipMigrations leaves the schema to the services that own it (core-api,
	// driver-manager). Set for drivers: one per gateway, so a site with a
	// hundred devices would otherwise run a hundred migration passes at once
	// on every restart.
	SkipMigrations bool
}

// ForDriver is the configuration a driver connects with. A driver writes a
// handful of values at a time, so it needs a couple of connections, not 25:
// Postgres admits a fixed number in total, and with one driver per device the
// default pool would exhaust it long before the machine ran out of anything
// else.
func ForDriver(cfg Config) Config {
	cfg.MaxOpenConns = 2
	cfg.MaxIdleConns = 1
	cfg.SkipMigrations = true
	return cfg
}
