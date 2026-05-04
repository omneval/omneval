package duckdb

import (
	"context"
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

func cleanupTestFiles(t *testing.T, paths ...string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			t.Logf("cleanup: %s: %v", p, err)
		}
	}
}
