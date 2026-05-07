package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// TestIntegration_QueryWithDuckDB tests the full query flow with a real DuckDB database.
func TestIntegration_QueryWithDuckDB(t *testing.T) {
	// Create a temp DuckDB file path.
	tmpDir, err := os.MkdirTemp("", "lantern-integration")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpPath := tmpDir + "/test.duckdb"

	// Open the database (creates it if it doesn't exist).
	db, err := sql.Open("duckdb", tmpPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	// Create the spans table.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE spans (
			span_id        VARCHAR NOT NULL,
			trace_id       VARCHAR NOT NULL,
			parent_id      VARCHAR,
			project_id     VARCHAR NOT NULL,
			service_name   VARCHAR,
			name           VARCHAR,
			kind           VARCHAR,
			start_time     TIMESTAMPTZ NOT NULL,
			end_time       TIMESTAMPTZ,
			model          VARCHAR,
			input          JSON,
			output         JSON,
			input_tokens   BIGINT,
			output_tokens  BIGINT,
			cost_usd       DOUBLE,
			prompt_name    VARCHAR,
			prompt_version BIGINT,
			status_code    VARCHAR,
			status_message VARCHAR,
			attributes     JSON,
			PRIMARY KEY (trace_id, span_id)
		);
		CREATE INDEX idx_spans_project_time ON spans (project_id, start_time);
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert test spans.
	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%03d", i), "trace-abc", "proj-123", "gpt-4",
			baseTime.Add(time.Duration(i)*time.Minute),
			baseTime.Add(time.Duration(i)*time.Minute).Add(10*time.Second)); err != nil {
			t.Fatalf("insert span %d: %v", i, err)
		}
	}

	// Query page 1.
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-123", req, nil, tmpPath)
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}

	sqlStr, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL: %v", err)
	}

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	var page1Rows [][]any
	for rows.Next() {
		cols, _ := rows.ColumnTypes()
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		page1Rows = append(page1Rows, values)
	}
	rows.Close()

	if len(page1Rows) != 10 {
		t.Errorf("page 1: got %d rows, want 10", len(page1Rows))
	}

	// Compute next cursor from page 1.
	spans, _ := ScanRows(page1Rows)
	next := NextCursor(spans, req.Limit)
	if next == "" {
		t.Fatal("expected non-empty next cursor on page 1")
	}

	// Query page 2 with cursor.
	req2 := SpanQueryRequest{
		From:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit:  10,
		Cursor: next,
	}

	q2, err := NewSpanQuery("proj-123", req2, nil, tmpPath)
	if err != nil {
		t.Fatalf("NewSpanQuery page 2: %v", err)
	}

	sqlStr2, args2, err := q2.SQL()
	if err != nil {
		t.Fatalf("SQL page 2: %v", err)
	}

	rows2, err := db.Query(sqlStr2, args2...)
	if err != nil {
		t.Fatalf("query page 2: %v", err)
	}

	var page2Rows [][]any
	for rows2.Next() {
		cols, _ := rows2.ColumnTypes()
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows2.Scan(valuePtrs...); err != nil {
			t.Fatalf("scan page 2: %v", err)
		}
		page2Rows = append(page2Rows, values)
	}
	rows2.Close()

	if len(page2Rows) != 10 {
		t.Errorf("page 2: got %d rows, want 10", len(page2Rows))
	}

	// Compute next cursor from page 2.
	page2Spans, _ := ScanRows(page2Rows)
	next2 := NextCursor(page2Spans, 10)
	if next2 == "" {
		t.Fatal("expected non-empty next cursor on page 2")
	}

	// Verify no overlap between page 1 and page 2.
	page1SpanIDs := make(map[string]bool, len(page1Rows))
	for _, row := range page1Rows {
		page1SpanIDs[asString(row[0])] = true
	}

	for _, row := range page2Rows {
		spanID := asString(row[0])
		if page1SpanIDs[spanID] {
			t.Errorf("page 2 contains span %q from page 1 — cursor pagination is broken", spanID)
		}
	}

	// Query page 3 (last page should have fewer rows).
	req3 := SpanQueryRequest{
		From:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit:  10,
		Cursor: next2,
	}

	q3, err := NewSpanQuery("proj-123", req3, nil, tmpPath)
	if err != nil {
		t.Fatalf("NewSpanQuery page 3: %v", err)
	}

	sqlStr3, args3, err := q3.SQL()
	if err != nil {
		t.Fatalf("SQL page 3: %v", err)
	}

	rows3, err := db.Query(sqlStr3, args3...)
	if err != nil {
		t.Fatalf("query page 3: %v", err)
	}

	var page3Rows [][]any
	for rows3.Next() {
		cols, _ := rows3.ColumnTypes()
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows3.Scan(valuePtrs...); err != nil {
			t.Fatalf("scan page 3: %v", err)
		}
		page3Rows = append(page3Rows, values)
	}
	rows3.Close()

	// With 25 spans and 10 per page: page 3 should have 5 rows.
	if len(page3Rows) != 5 {
		t.Errorf("page 3: got %d rows, want 5", len(page3Rows))
	}

	// Verify no overlap between page 2 and page 3.
	page2SpanIDs := make(map[string]bool, len(page2Rows))
	for _, row := range page2Rows {
		page2SpanIDs[asString(row[0])] = true
	}
	for _, row := range page3Rows {
		spanID := asString(row[0])
		if page2SpanIDs[spanID] {
			t.Errorf("page 3 contains span %q from page 2 — cursor pagination is broken", spanID)
		}
	}

	// Next cursor for page 3 should be empty (last page).
	spans3, _ := ScanRows(page3Rows)
	next3 := NextCursor(spans3, 10)
	if next3 != "" {
		t.Errorf("expected empty next cursor on last page, got %q", next3)
	}
}

// asString converts a column value to a string, handling both []byte and string.
func asString(v any) string {
	switch s := v.(type) {
	case []byte:
		return string(s)
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
