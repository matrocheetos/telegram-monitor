package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	_ "github.com/ncruces/go-sqlite3/driver"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func OpenDB(ctx context.Context, dataSourceName string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("sqlite: failed to open database: %w", err)
	}

	if dataSourceName == ":memory:" || strings.Contains(dataSourceName, "mode=memory") {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	}

	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite: failed to execute %s: %w", pragma, err)
		}
	}

	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: failed to run migrations: %w", err)
	}

	return db, nil
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		sqlContent, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", entry.Name(), err)
		}

		if _, err := db.ExecContext(ctx, string(sqlContent)); err != nil {
			return fmt.Errorf("executing migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}
