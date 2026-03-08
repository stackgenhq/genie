// Package db provides a central GORM-based database layer that supports
// both SQLite (default, file-based) and PostgreSQL (DSN-based).
// All persistent tables (e.g. HITL approvals) are registered here as GORM
// models and auto-migrated via [AutoMigrate].
//
// SQLite is selected when Config.DBFile is set (or defaulted).
// PostgreSQL is selected when Config.DSN is set.
// If both are set, DSN takes precedence.
//
// Usage (SQLite):
//
//	gormDB, err := db.OpenConfig(db.DefaultConfig())
//	db.AutoMigrate(gormDB)
//
// Usage (PostgreSQL):
//
//	gormDB, err := db.OpenConfig(db.Config{DSN: "postgres://user:pass@host:5432/genie"})
//	db.AutoMigrate(gormDB)
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stackgenhq/genie/pkg/osutils"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	// Ensure the pure-Go SQLite driver is registered.
	// This may already be imported by other packages (e.g. whatsapp adapter),
	// but the import is idempotent via sql.Register's dedup.
	_ "modernc.org/sqlite"
)

// Config holds the database connection configuration.
// Set DSN for PostgreSQL, or DBFile for SQLite (the default).
// If both are set, DSN takes precedence.
type Config struct {
	// DBFile is the path to the SQLite database file.
	// Defaults to ~/.genie/genie.db when neither DBFile nor DSN is set.
	DBFile string `json:"db_file" toml:"db_file,omitempty" yaml:"db_file,omitempty"`

	// DSN is a PostgreSQL connection string (e.g. "postgres://user:pass@host:5432/dbname?sslmode=disable").
	// When set, PostgreSQL is used instead of SQLite.
	DSN string `json:"dsn,omitempty" toml:"dsn,omitempty" yaml:"dsn,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		DBFile: defaultPath(),
	}
}

// isPostgres returns true when the configuration targets PostgreSQL.
func (c Config) isPostgres() bool {
	return c.DSN != ""
}

// DisplayPath returns a human-readable string describing the database
// location — either the DSN (with password masked) or the SQLite file path.
func (c Config) DisplayPath() string {
	if c.isPostgres() {
		return "postgres"
	}
	if c.DBFile != "" {
		return c.DBFile
	}
	return defaultPath()
}

// DefaultPath returns the default database file path: ~/.genie/genie.db.
// It creates the ~/.genie directory if it does not exist.
func defaultPath() string {
	return filepath.Join(osutils.GenieDir(), "genie.db")
}

// Open opens (or creates) a SQLite database at dbPath using GORM.
// It uses the pure-Go modernc.org/sqlite driver (already registered as "sqlite").
// WAL journal mode is enabled for better concurrent read/write performance.
//
// Deprecated: Use OpenConfig instead, which supports both SQLite and PostgreSQL.
func Open(dbPath string) (*gorm.DB, error) {
	return OpenConfig(Config{DBFile: dbPath})
}

// OpenConfig opens a database connection based on the given Config.
// If DSN is set, PostgreSQL is used. Otherwise, SQLite is used with the
// DBFile path (defaulting to ~/.genie/genie.db).
func OpenConfig(cfg Config) (*gorm.DB, error) {
	if cfg.isPostgres() {
		return openPostgres(cfg.DSN)
	}
	return openSQLite(cfg.DBFile)
}

// openSQLite opens (or creates) a SQLite database at dbPath.
func openSQLite(dbPath string) (*gorm.DB, error) {
	if dbPath == "" {
		dbPath = defaultPath()
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
	}

	// Open raw sql.DB with the modernc.org/sqlite driver.
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database %s: %w", dbPath, err)
	}

	// Enable WAL mode for better concurrent read/write performance.
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Set busy timeout so concurrent writers retry instead of returning
	// SQLITE_BUSY immediately. 15 seconds allows heavy bursts (e.g. many
	// HITL approval inserts plus session/checkpoint writes) to drain.
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=15000"); err != nil {
		sqlDB.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Wrap with GORM using the sqlite dialector pointed at the existing connection.
	dialector := sqlite.Dialector{
		DriverName: "sqlite",
		DSN:        dbPath,
		Conn:       sqlDB,
	}

	gormDB, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		sqlDB.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to initialize GORM: %w", err)
	}

	return gormDB, nil
}

// openPostgres opens a PostgreSQL database using the given DSN.
func openPostgres(dsn string) (*gorm.DB, error) {
	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Configure connection pool for production use.
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Sensible defaults for a single-replica app with moderate concurrency.
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)

	return gormDB, nil
}

// AutoMigrate runs GORM auto-migration for all registered models.
// Call this after Open() to ensure all tables exist with the correct schema.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Approval{},
		&Memory{},
		&CronTask{},
		&CronHistory{},
		&ShortMemory{},
		&Clarification{},
		// Session persistence (conversation history + state).
		&SessionRow{},
		&SessionEvent{},
		&SessionState{},
		&AppState{},
		&UserState{},
		&SessionSummaryRow{},
	)
}

// Close closes the underlying database connection.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}
