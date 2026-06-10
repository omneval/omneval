package duckdb

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestOpen_CreatesDatabase(t *testing.T) {
	db, err := Open("test_open.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	defer cleanupTestFiles(t, "test_open.db")

	// Verify the spans table exists.
	var count int
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM spans").Scan(&count)
	if err != nil {
		t.Fatalf("querying spans table: %v", err)
	}
	if count != 0 {
		t.Errorf("spans table has %d rows, want 0", count)
	}

	// Verify the scores table exists.
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM scores").Scan(&count)
	if err != nil {
		t.Fatalf("querying scores table: %v", err)
	}
}

func TestOpen_IdempotentSchema(t *testing.T) {
	db, err := Open("test_idempotent.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	defer cleanupTestFiles(t, "test_idempotent.db")

	// Open again with same path should not error.
	db2, err := Open("test_idempotent.db")
	if err != nil {
		t.Fatalf("Open second time: %v", err)
	}
	defer db2.Close()
}

func TestWriteSpans(t *testing.T) {
	db, err := Open("test_write.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	defer cleanupTestFiles(t, "test_write.db")

	spans := []struct {
		spanID, traceID, projectID string
		name                       string
		model                      string
		inputTokens, outputTokens  int64
		kind                       string
	}{
		{"span-1", "trace-1", "proj-1", "chat", "gpt-4o", 10, 5, "llm"},
		{"span-2", "trace-1", "proj-1", "tool-call", "gpt-4o", 3, 1, "tool"},
	}

	stmt, err := db.Prepare(`
		INSERT INTO spans (span_id, trace_id, project_id, name, model,
		                   input_tokens, output_tokens, cost_usd, kind,
		                   start_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0.001, ?, ?)
	`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, s := range spans {
		_, err := stmt.Exec(s.spanID, s.traceID, s.projectID, s.name, s.model,
			s.inputTokens, s.outputTokens, s.kind, now)
		if err != nil {
			t.Fatalf("exec span %s: %v", s.spanID, err)
		}
	}

	// Verify count.
	var count int
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM spans").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 2 {
		t.Errorf("spans count: got %d, want 2", count)
	}
}

func TestUpsert_Idempotent(t *testing.T) {
	db, err := Open("test_upsert.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	defer cleanupTestFiles(t, "test_upsert.db")

	now := time.Now()

	// Insert first time.
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO spans (span_id, trace_id, project_id, name, model,
		                   input_tokens, output_tokens, cost_usd, kind,
		                   start_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "span-1", "trace-1", "proj-1", "chat", "gpt-4o", 10, 5, 0.001, "llm", now)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Insert second time with same PK (should replace, not duplicate).
	_, err = db.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO spans (span_id, trace_id, project_id, name, model,
		                   input_tokens, output_tokens, cost_usd, kind,
		                   start_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "span-1", "trace-1", "proj-1", "chat", "gpt-4o-v2", 15, 8, 0.002, "llm", now)
	if err != nil {
		t.Fatalf("insert second: %v", err)
	}

	// Verify only one row exists.
	var count int
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM spans").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("spans count: got %d, want 1 (upsert should not duplicate)", count)
	}

	// Verify updated values.
	var model string
	var cost float64
	err = db.QueryRowContext(context.Background(),
		"SELECT model, cost_usd FROM spans WHERE span_id = ?", "span-1").Scan(&model, &cost)
	if err != nil {
		t.Fatalf("query updated: %v", err)
	}
	if model != "gpt-4o-v2" {
		t.Errorf("model: got %q, want %q", model, "gpt-4o-v2")
	}
	if cost != 0.002 {
		t.Errorf("cost: got %f, want %f", cost, 0.002)
	}
}

func TestBookmarksTableExists(t *testing.T) {
	db, err := Open("test_bookmarks.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	defer os.Remove("test_bookmarks.db")

	// Verify the bookmarks table exists.
	var count int
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM bookmarks").Scan(&count)
	if err != nil {
		t.Fatalf("querying bookmarks table: %v", err)
	}

	// Insert a bookmark.
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO bookmarks (trace_id, project_id, created_at)
		VALUES (?, ?, ?)
	`, "trace-1", "proj-1", time.Now())
	if err != nil {
		t.Fatalf("insert bookmark: %v", err)
	}

	// Verify count.
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM bookmarks").Scan(&count)
	if err != nil {
		t.Fatalf("query bookmarks: %v", err)
	}
	if count != 1 {
		t.Errorf("bookmarks count: got %d, want 1", count)
	}
}

func cleanupTestFiles(t *testing.T, paths ...string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			t.Logf("cleanup: %s: %v", p, err)
		}
	}
}

// TestOpen_UpgradesLegacyDatabase simulates a database created before
// conversation_id existed (issue #67): Open must succeed — the schema pass
// must not reference the column (it runs before migrations), and migration
// 0001 must add the column + index.
func TestOpen_UpgradesLegacyDatabase(t *testing.T) {
	path := "test_legacy.db"
	defer cleanupTestFiles(t, path)

	// Build a legacy database: spans table without conversation_id.
	legacy, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(), `
		CREATE TABLE spans (
			span_id    VARCHAR NOT NULL,
			trace_id   VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			start_time TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (trace_id, span_id)
		)`); err != nil {
		legacy.Close()
		t.Fatalf("create legacy spans: %v", err)
	}
	legacy.Close()

	// Open must apply schema + migrations without error.
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open on legacy db: %v", err)
	}
	defer db.Close()

	// conversation_id must now exist.
	var n int
	if err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM spans WHERE conversation_id IS NULL OR conversation_id IS NOT NULL",
	).Scan(&n); err != nil {
		t.Fatalf("conversation_id column missing after Open: %v", err)
	}
}
