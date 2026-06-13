package duckdb

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
)

//go:embed schema.sql
var schemaSQL string

//go:embed migrations/0001_add_conversation_id.up.sql
var migrationSQL001 string

// Open opens (or creates) a DuckDB database at the given path.
// It applies the embedded schema and any pending migrations.
func Open(path string) (*sql.DB, error) {
	// Ensure parent directory exists (skip for bare filenames).
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("duckdb: create dir %s: %w", dir, err)
		}
	}

	dsn := path

	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, fmt.Errorf("duckdb: open %s: %w", path, err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("duckdb: ping: %w", err)
	}

	// Apply schema.
	if err := ApplySchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("duckdb: apply schema: %w", err)
	}

	// Apply migrations.
	if err := ApplyMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("duckdb: apply migrations: %w", err)
	}

	return db, nil
}

// ApplySchema runs the embedded schema SQL.
func ApplySchema(db *sql.DB) error {
	statements := splitStatements(schemaSQL)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("duckdb: exec: %s: %w", stmt, err)
		}
	}
	return nil
}

// ApplyMigrations creates the migrations tracking table and runs any pending
// migrations that have not yet been applied. Each migration is applied within
// a transaction so that both the schema change and the tracking insert are
// atomic.
func ApplyMigrations(db *sql.DB) error {
	// Create migrations tracking table.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS _schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("duckdb: create migrations table: %w", err)
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{1, migrationSQL001},
	}

	for _, m := range migrations {
		var count int
		err := db.QueryRowContext(
			context.Background(),
			"SELECT count(*) FROM _schema_migrations WHERE version = ?",
			m.version,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("duckdb: check migration %d status: %w", m.version, err)
		}
		if count == 0 {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				return fmt.Errorf("duckdb: begin migration %d: %w", m.version, err)
			}
			if _, err := tx.ExecContext(context.Background(), m.sql); err != nil {
				tx.Rollback()
				return fmt.Errorf("duckdb: apply migration %d: %w", m.version, err)
			}
			if _, err := tx.ExecContext(
				context.Background(),
				"INSERT INTO _schema_migrations (version) VALUES (?)",
				m.version,
			); err != nil {
				tx.Rollback()
				return fmt.Errorf("duckdb: record migration %d: %w", m.version, err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("duckdb: commit migration %d: %w", m.version, err)
			}
		}
	}

	return nil
}

// splitStatements splits SQL on semicolons, handling basic cases.
func splitStatements(sql string) []string {
	var parts []string
	var current strings.Builder

	for _, r := range sql {
		if r == ';' {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	last := current.String()
	if last = strings.TrimSpace(last); last != "" {
		parts = append(parts, last)
	}
	return parts
}
