package duckdb

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/marcboeker/go-duckdb/v2"
)

//go:embed schema.sql
var schemaSQL string

// Open opens (or creates) a DuckDB database at the given path.
// It applies the embedded schema if the spans table does not exist.
func Open(path string) (*sql.DB, error) {
	// Ensure parent directory exists (skip for bare filenames).
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("duckdb: create dir %s: %w", dir, err)
		}
	}

	dsn := path
	// For in-memory, use the file-based approach to avoid extension issues.
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared"
	}

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
