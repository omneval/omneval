package query

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
)

// TestSQL_LakeMode_SingleTable proves lake mode compiles one SELECT over
// the spans view: no hot+cold UNION, no read_parquet — even when an S3
// store is configured.
func TestSQL_LakeMode_SingleTable(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, fakeObjectStore{}, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}
	q.EnableLakeMode()

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL: %v", err)
	}

	if strings.Contains(sql, "UNION ALL") {
		t.Errorf("lake mode must not emit UNION ALL:\n%s", sql)
	}
	if strings.Contains(sql, "read_parquet") {
		t.Errorf("lake mode must not emit read_parquet:\n%s", sql)
	}
	if !strings.Contains(sql, "FROM spans") {
		t.Errorf("expected FROM spans:\n%s", sql)
	}
	if !strings.Contains(sql, "ORDER BY start_time DESC, span_id ASC") {
		t.Errorf("expected keyset ordering:\n%s", sql)
	}
	// Args: project_id + from + to + limit.
	if len(args) != 4 {
		t.Errorf("args: got %d (%v), want 4", len(args), args)
	}
}

// TestSQL_LakeMode_CursorParity proves the keyset cursor predicate and
// argument order are identical to the legacy path.
func TestSQL_LakeMode_CursorParity(t *testing.T) {
	req := SpanQueryRequest{
		From:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit:  10,
		Cursor: "eyJzdGFydF90aW1lIjoiMjAyNS0wMS0wMVQxMDowMDowMFoiLCJzcGFuX2lkIjoic3BhbjAxIn0",
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "")
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}
	q.EnableLakeMode()

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL: %v", err)
	}

	if !strings.Contains(sql, "WHERE (start_time < ? OR (start_time = ? AND span_id < ?))") {
		t.Errorf("expected keyset cursor predicate:\n%s", sql)
	}
	// Args: project_id, from, to, cursor start_time ×2, cursor span_id, limit.
	if len(args) != 7 {
		t.Errorf("args: got %d (%v), want 7", len(args), args)
	}
	if args[len(args)-1] != 11 { // limit+1
		t.Errorf("last arg should be limit+1=11, got %v", args[len(args)-1])
	}
}

// TestLakeMode_TraceDedupe ingests the same span twice into a real Lake
// and proves the trace-detail query returns one row while the undeduped
// list returns two — the Batch Ledger residual-duplicate policy.
func TestLakeMode_TraceDedupe(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	lk, err := lake.Open(ctx, lake.Config{
		CatalogDriver: lake.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog.ducklake"),
		DataPath:      filepath.Join(dir, "data"),
	})
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer lk.Close()

	span := &domain.Span{
		SpanID:    "s1",
		TraceID:   "t1",
		ProjectID: "p1",
		Name:      "chat",
		StartTime: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 6, 1, 10, 0, 1, 0, time.UTC),
	}
	// Same span committed twice — a redelivered batch that slipped past
	// the Batch Ledger.
	if err := lk.InsertSpans(ctx, []*domain.Span{span}); err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	if err := lk.InsertSpans(ctx, []*domain.Span{span}); err != nil {
		t.Fatalf("insert 2: %v", err)
	}

	buildQuery := func(dedupe bool) (string, []any) {
		req := SpanQueryRequest{
			From: time.Time{},
			To:   time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
			Filters: []SpanQueryFilter{
				{Field: "trace_id", Op: "eq", Value: "t1"},
			},
			Limit: 100,
		}
		q, err := NewSpanQuery("p1", req, nil, "")
		if err != nil {
			t.Fatalf("NewSpanQuery: %v", err)
		}
		q.EnableLakeMode()
		if dedupe {
			q.EnableTraceDedupe()
		}
		sqlStr, args, err := q.SQL()
		if err != nil {
			t.Fatalf("SQL: %v", err)
		}
		return sqlStr, args
	}

	countRows := func(sqlStr string, args []any) int {
		rows, err := lk.DB().QueryContext(ctx, sqlStr, args...)
		if err != nil {
			t.Fatalf("query: %v\nsql: %s", err, sqlStr)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		return n
	}

	sqlStr, args := buildQuery(false)
	if got := countRows(sqlStr, args); got != 2 {
		t.Errorf("without dedupe: got %d rows, want 2 (duplicate present)", got)
	}

	sqlStr, args = buildQuery(true)
	if got := countRows(sqlStr, args); got != 1 {
		t.Errorf("with dedupe: got %d rows, want 1", got)
	}
}
