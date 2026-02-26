// Package db provides a central GORM-based database layer backed by SQLite.
// All persistent tables (e.g. HITL approvals) are registered here as GORM models
// and auto-migrated via [AutoMigrate]. The default database path is ~/.genie/genie.db.
//
// Usage:
//
//	gormDB, err := db.Open(db.DefaultPath())
//	db.AutoMigrate(gormDB)
//	store := hitl.NewStore(gormDB)
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	// Ensure the pure-Go SQLite driver is registered.
	// This may already be imported by other packages (e.g. whatsapp adapter),
	// but the import is idempotent via sql.Register's dedup.
	_ "modernc.org/sqlite"
)

type Config struct {
	DBFile string `json:"db_file" toml:"db_file,omitempty" yaml:"db_file,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		DBFile: defaultPath(),
	}
}

// DefaultPath returns the default database file path: ~/.genie/genie.db.
// It creates the ~/.genie directory if it does not exist.
func defaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "genie.db"
	}
	dir := filepath.Join(home, ".genie")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "genie.db"
	}
	return filepath.Join(dir, "genie.db")
}

// Open opens (or creates) a SQLite database at dbPath using GORM.
// It uses the pure-Go modernc.org/sqlite driver (already registered as "sqlite").
// WAL journal mode is enabled for better concurrent read/write performance.
func Open(dbPath string) (*gorm.DB, error) {
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
