package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var migrations = "migrations"

func Open() (*sql.DB, error) {
	dbPath, err := DefaultDBPath()
	if err != nil {
		return nil, err
	}
	return OpenAt(dbPath)
}

func OpenAt(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	// SQLite allows one writer at a time, and ticket create/update hold a
	// transaction across label and dependency resolution. Two settings are
	// needed for a second writer — typically an agent using the MCP server
	// while someone uses the web UI — to wait its turn instead of failing:
	//
	//   busy_timeout   installs a busy handler; without it the driver has none
	//                  and a contended write returns SQLITE_BUSY immediately.
	//   _txlock        makes db.Begin() issue BEGIN IMMEDIATE, taking the write
	//                  lock up front. This one is not optional: a deferred
	//                  transaction that reads first and writes later cannot use
	//                  the busy handler when it upgrades to a writer, because
	//                  waiting there could deadlock, so SQLite fails it at once.
	//                  Our write transactions all read before they write.
	db, err := sql.Open("sqlite", dbPath+
		"?_pragma=journal_mode(WAL)"+
		"&_pragma=foreign_keys(1)"+
		"&_pragma=busy_timeout(5000)"+
		"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

func DefaultDBPath() (string, error) {
	dataDir, err := os.UserConfigDir()
	if err != nil {
		home, err2 := os.UserHomeDir()
		if err2 != nil {
			return "", fmt.Errorf("finding home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".config")
	}
	return filepath.Join(dataDir, "taskboard", "taskboard.db"), nil
}

func runMigrations(database *sql.DB) error {
	_, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir(migrations)
	if err != nil {
		return fmt.Errorf("reading migrations: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		var count int
		err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", name).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		content, err := migrationsFS.ReadFile(filepath.Join(migrations, name))
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		tx, err := database.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", name, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", name, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", name); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", name, err)
		}
	}

	return nil
}
