package querybuild

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/omneval/omneval/internal/domain"
	_ "github.com/omneval/omneval/internal/duckdbfix"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/laketest"
	"github.com/omneval/omneval/services/query/internal/dsl"
	"github.com/omneval/omneval/services/query/internal/query"
)

// duckdbConvertPlaceholders converts `?` placeholders to DuckDB's `$N` style
// for use with plain DuckDB in tests.  The production code runs against
// DuckLake which handles `?` natively, so we only convert in tests.
// It also strips monotonic clock offsets from time.Time values, which the
// DuckDB driver serializes into strings that DuckDB cannot parse as TIMESTAMPTZ
// when bound as parameters.
func duckdbConvertPlaceholders(sql string, args []any) (string, []any) {
	var sb strings.Builder
	argIdx := 1
	for _, ch := range sql {
		if ch == '?' {
			sb.WriteString(fmt.Sprintf("$%d", argIdx))
			argIdx++
		} else {
			sb.WriteRune(ch)
		}
	}
	converted := make([]any, len(args))
	copy(converted, args)
	for i, arg := range args {
		if t, ok := arg.(time.Time); ok {
			converted[i] = time.Date(t.Year(), t.Month(), t.Day(),
				t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
		}
	}
	return sb.String(), converted
}

// testDBHandle wraps a *sql.DB, converting `?` → `$N` so the query builder's
// `?`-placeholder SQL works correctly with plain DuckDB in tests.  DuckLake
// handles `?` natively, so production code uses DBHandle directly.
// We use a named field (not embedding) because *sql.DB has its own Query()
// method which would shadow our custom implementation.
type testDBHandle struct {
	DB *sql.DB
}

func (t *testDBHandle) Query(query string, args ...any) (*sql.Rows, error) {
	concrete, convertedArgs := duckdbConvertPlaceholders(query, args)
	fmt.Fprintf(os.Stderr, "[testDBHandle.Query] SQL=%s args=%v\n", concrete, convertedArgs)
	return t.DB.QueryContext(context.Background(), concrete, convertedArgs...)
}

func (t *testDBHandle) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	concrete, convertedArgs := duckdbConvertPlaceholders(query, args)
	// DuckDB's driver serializes time.Time values differently for ExecContext
	// vs QueryContext. For consistency, we execute DDL/DML via a query and use
	// LastId/RowsAffected manually. But since the driver should handle this,
	// we fall back to ExecContext here.
	return t.DB.ExecContext(ctx, concrete, convertedArgs...)
}

// testLakeDB creates a file-based DuckDB with the lake schema and returns it.
// File-based DB is required because in-memory DuckDB can behave differently
// with schema/view resolution, and file-based matches the handler tests.
func testLakeDB(t *testing.T) *testDBHandle {
	t.Helper()
	tmpDir := t.TempDir()
	path := tmpDir + "/test.duckdb"
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	// DuckDB file mode requires single-connection to avoid concurrency issues.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE spans (
			span_id         VARCHAR NOT NULL,
			trace_id        VARCHAR NOT NULL,
			parent_id       VARCHAR,
			conversation_id VARCHAR,
			project_id      VARCHAR NOT NULL,
			service_name    VARCHAR,
			name            VARCHAR,
			kind            VARCHAR,
			start_time      TIMESTAMPTZ NOT NULL,
			end_time        TIMESTAMPTZ,
			model           VARCHAR,
			input           JSON,
			output          JSON,
			input_tokens    BIGINT,
			output_tokens   BIGINT,
			cost_usd        DOUBLE,
			prompt_name     VARCHAR,
			prompt_version  BIGINT,
			status_code     VARCHAR,
			status_message  VARCHAR,
			attributes      JSON,
			PRIMARY KEY (trace_id, span_id)
		);
		CREATE SCHEMA IF NOT EXISTS lake;
		CREATE VIEW lake.spans AS SELECT * FROM main.spans;
	`); err != nil {
		t.Fatalf("create spans table: %v", err)
	}
	return &testDBHandle{DB: db}
}

// testLakeServer creates a Lake server using laketest for integration tests.
func testLakeServer(t *testing.T) *lake.Lake {
	t.Helper()
	return laketest.NewLocal(t)
}

// fakeBookmarkStore returns bookmarked trace IDs for the "bookmarked" filter.
type fakeBookmarkStore struct {
	traceIDs []string
}

func (f *fakeBookmarkStore) ListBookmarkedTraceIDs(ctx context.Context, projectID string) ([]string, error) {
	return f.traceIDs, nil
}

func (f *fakeBookmarkStore) IsBookmarked(ctx context.Context, projectID, traceID string) (bool, error) {
	for _, id := range f.traceIDs {
		if id == traceID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeBookmarkStore) SetBookmark(ctx context.Context, b *domain.Bookmark) error {
	return nil
}

func (f *fakeBookmarkStore) RemoveBookmark(ctx context.Context, projectID, traceID string) error {
	return nil
}

func (f *fakeBookmarkStore) RemoveBookmarksForProject(ctx context.Context, projectID string) error {
	return nil
}

// TestExecuteSpan_SingleSpan verifies that ExecuteSpan returns exactly one
// span when the Lake table contains a single row matching the project filter.
func TestExecuteSpan_SingleSpan(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	now := time.Now().UTC()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, model, start_time, end_time, name, kind, status_code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"span-001", "trace-001", "test-proj", "gpt-4",
		now, now.Add(10*time.Second), "agent.run", "llm", "ok"); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	resp, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 50,
	}, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 1 {
		t.Errorf("spans: got %d, want 1", len(resp.Spans))
	}
	if resp.Spans[0].SpanID != "span-001" {
		t.Errorf("span_id: got %q, want %q", resp.Spans[0].SpanID, "span-001")
	}
	if resp.Limit != query.DefaultLimit {
		t.Errorf("limit: got %d, want %d", resp.Limit, query.DefaultLimit)
	}
}

// TestExecuteSpan_MultipleSpans verifies pagination: with limit=1 and two spans,
// the response should include one span and a non-empty next cursor.
func TestExecuteSpan_Pagination(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	spans := []struct {
		spanID string
		time   time.Time
	}{
		{"span-001", baseTime},
		{"span-002", baseTime.Add(time.Second)},
	}
	for _, s := range spans {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, status_code) VALUES (?, ?, ?, ?, ?)`,
			s.spanID, "trace-"+s.spanID, "test-proj", s.time, "ok"); err != nil {
			t.Fatalf("insert span %s: %v", s.spanID, err)
		}
	}

	// Explicit time bounds prevent race with NewSpanQuery's default time range.
	resp, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 1,
		From:  baseTime.Add(-time.Minute),
		To:    baseTime.Add(2 * time.Second),
	}, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 1 {
		t.Errorf("spans: got %d, want 1", len(resp.Spans))
	}
	if resp.Next == "" {
		t.Error("expected non-empty next cursor with more than limit spans")
	}
}

// TestExecuteSpan_NoSpans verifies the path when no rows match the project filter.
func TestExecuteSpan_NoSpans(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	resp, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 50,
	}, "nonexistent-proj")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 0 {
		t.Errorf("spans: got %d, want 0", len(resp.Spans))
	}
	if resp.Limit != query.DefaultLimit {
		t.Errorf("limit: got %d, want %d", resp.Limit, query.DefaultLimit)
	}
}

// TestExecuteSpan_WithBookmarks verifies that "bookmarked" filters are
// correctly resolved from the BookmarkStore and applied in SQL.
func TestExecuteSpan_WithBookmarks(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{
		Lake:          db,
		BookmarkStore: &fakeBookmarkStore{traceIDs: []string{"trace-bookmarked"}},
	}

	baseTime := time.Now().UTC()
	for _, tid := range []string{"trace-bookmarked", "trace-other"} {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, status_code) VALUES (?, ?, ?, ?, ?)`,
			"span-"+tid, tid, "test-proj", baseTime, "ok"); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	resp, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 50,
		Filters: []query.SpanQueryFilter{
			{Field: "bookmarked", Op: "eq", Value: true},
		},
	}, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 1 {
		t.Fatalf("spans: got %d, want 1 (bookmarked filter)", len(resp.Spans))
	}
	if resp.Spans[0].TraceID != "trace-bookmarked" {
		t.Errorf("trace_id: got %q, want %q", resp.Spans[0].TraceID, "trace-bookmarked")
	}
}

// TestExecuteSpan_InvalidQuery verifies that build errors are propagated.
func TestExecuteSpan_InvalidQuery(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	// Invalid cursor should cause an error.
	_, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Cursor: "invalid!!!",
	}, "test-proj")
	if err == nil {
		t.Fatal("expected error for invalid cursor")
	}

	// Invalid filter field should cause an error.
	_, err = qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 10,
		Filters: []query.SpanQueryFilter{
			{Field: "nonexistent", Op: "eq", Value: "x"},
		},
	}, "test-proj")
	if err == nil {
		t.Fatal("expected error for invalid filter field")
	}
}

// TestExecuteSpan_WithLakeIntegration verifies the full path end-to-end using
// a real Lake server, not just an in-memory DuckDB.
func TestExecuteSpan_WithLakeIntegration(t *testing.T) {
	ctx := context.Background()
	lk := testLakeServer(t)

	base := time.Now().UTC()
	spans := []*domain.Span{
		{
			SpanID: "span-a", TraceID: "trace-a", ProjectID: "proj-int",
			Name: "agent.run", Kind: domain.SpanKindAgent,
			StartTime: base, EndTime: base.Add(5 * time.Second),
			InputTokens: 100, OutputTokens: 50, CostUSD: 0.05, StatusCode: "ok",
		},
		{
			SpanID: "span-b", TraceID: "trace-b", ProjectID: "proj-int",
			Name: "tool.call", Kind: domain.SpanKindTool,
			StartTime: base.Add(time.Second), EndTime: base.Add(3 * time.Second),
			InputTokens: 20, OutputTokens: 5, CostUSD: 0.01, StatusCode: "ok",
		},
	}
	if err := lk.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	qb := &QueryBuilder{Lake: lk}

	// Explicit time bounds prevent race with NewSpanQuery's default time range.
	resp, err := qb.ExecuteSpan(ctx, query.SpanQueryRequest{
		Limit: 10,
		From:  base.Add(-time.Minute),
		To:    base.Add(6 * time.Second),
	}, "proj-int")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 2 {
		t.Fatalf("spans: got %d, want 2", len(resp.Spans))
	}
	if resp.Limit != 10 {
		t.Errorf("limit: got %d, want 10", resp.Limit)
	}
	// Both spans should be from proj-int.
	for _, s := range resp.Spans {
		if s.ProjectID != "proj-int" {
			t.Errorf("project_id: got %q, want %q", s.ProjectID, "proj-int")
		}
	}
}

// TestExecuteAnalytics_SingleRow verifies that ExecuteAnalytics compiles,
// executes, and scans a simple analytics query with a single aggregation.
func TestExecuteAnalytics_SingleRow(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	// Insert 3 spans.
	for i := 0; i < 3; i++ {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, input_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), 100+i, float64(i))
		if err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	// Debug: verify raw count with different queries
	var rawCountMain, rawCountLake int
	db.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM main.spans WHERE project_id='test-proj'").Scan(&rawCountMain)
	t.Logf("raw count in main.spans: %d", rawCountMain)
	db.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM lake.spans WHERE project_id='test-proj'").Scan(&rawCountLake)
	t.Logf("raw count in lake.spans: %d", rawCountLake)
	// Try the exact same SQL the compiler produces
	db.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM lake.spans WHERE project_id = $1", "test-proj").Scan(&rawCountLake)
	t.Logf("lake.spans with parameterized query: %d", rawCountLake)

	// Build query with explicit time bounds covering all inserted spans.
	// The DSL compiler defaults to "last 30 days" from time.Now(), but
	// spans are inserted at time.Now() (with 0-2s offsets). A narrow
	// time window at query-compile time could exclude spans. Use a wide
	// explicit window to avoid race conditions.
	req := dsl.Query{
		From: baseTime.Add(-time.Minute),
		To:   baseTime.Add(3 * time.Second),
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
	}

	// Debug: compile SQL manually
	sqlStr, args, err := dsl.CompileLake("test-proj", req)
	if err != nil {
		t.Fatalf("CompileLake: %v", err)
	}
	t.Logf("Compiled SQL:\n%s", sqlStr)
	t.Logf("CompileLake args: %v", args)
	// Raw execute with ? placeholders
	rows, err := db.DB.QueryContext(context.Background(), sqlStr, args...)
	if err != nil {
		t.Fatalf("raw query with ?: %v", err)
	}
	for rows.Next() {
		var cnt int64
		rows.Scan(&cnt)
		t.Logf("Count with ? placeholder: %d", cnt)
	}
	rows.Close()
	// Raw execute with $N placeholders (DuckDB native)
	rows2, err := db.DB.QueryContext(context.Background(),
		"SELECT COUNT(*) AS count FROM lake.spans WHERE project_id = $1", "test-proj")
	if err != nil {
		t.Fatalf("raw query with $1: %v", err)
	}
	for rows2.Next() {
		var cnt int64
		rows2.Scan(&cnt)
		t.Logf("Count with $1 placeholder: %d", cnt)
	}
	rows2.Close()

	// Debug: query actual start_time values stored in lake.spans
	timestamps := make([]time.Time, 3)
	rows3, err := db.DB.QueryContext(context.Background(),
		"SELECT start_time FROM lake.spans WHERE project_id=$1 ORDER BY start_time LIMIT 3", "test-proj")
	if err != nil {
		t.Fatalf("timestamp query: %v", err)
	}
	idx := 0
	for rows3.Next() && idx < 3 {
		var ts time.Time
		rows3.Scan(&ts)
		timestamps[idx] = ts
		t.Logf("lake.spans[%d] start_time: %v", idx, ts)
		idx++
	}
	rows3.Close()

	// Debug: try the exact wrapper query
	wrapperSQL, wrapperArgs := duckdbConvertPlaceholders(sqlStr, args)
	t.Logf("Wrapper SQL: %s", wrapperSQL)
	t.Logf("Wrapper args: %v", wrapperArgs)
	wrapperRows, err := db.Query(wrapperSQL, wrapperArgs...)
	if err != nil {
		t.Fatalf("wrapper query: %v", err)
	}
	for wrapperRows.Next() {
		var wrapperCount int64
		wrapperRows.Scan(&wrapperCount)
		t.Logf("Wrapper query count: %d", wrapperCount)
	}
	wrapperRows.Close()

	resp, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteAnalytics: %v", err)
	}

	if len(resp.Rows) != 1 {
		t.Fatalf("rows: got %d, want 1", len(resp.Rows))
	}
	countVal, ok := resp.Rows[0]["count"].(int64)
	if !ok {
		t.Errorf("count: expected int64, got %T", resp.Rows[0]["count"])
	}
	if countVal != 3 {
		t.Errorf("count: got %d, want 3", countVal)
	}
}

// TestExecuteAnalytics_GroupBy verifies that grouping works through the full pipeline.
func TestExecuteAnalytics_GroupBy(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	models := []string{"gpt-4", "gpt-3.5", "gpt-4"}
	for i, model := range models {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, model) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), model); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	req := dsl.Query{
		// Explicit time bounds prevent race with CompileLake's default time range.
		From: baseTime.Add(-time.Minute),
		To:   baseTime.Add(3 * time.Second),
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
		GroupBy: []dsl.GroupByField{
			{Field: "model"},
		},
	}
	resp, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteAnalytics: %v", err)
	}

	if len(resp.Rows) != 2 {
		t.Fatalf("rows: got %d, want 2 (gpt-4 and gpt-3.5)", len(resp.Rows))
	}

	// Build a map from model -> count.
	modelCounts := make(map[string]int64)
	for _, row := range resp.Rows {
		model := row["model"].(string)
		count := row["count"].(int64)
		modelCounts[model] = count
	}

	if modelCounts["gpt-4"] != 2 {
		t.Errorf("gpt-4 count: got %d, want 2", modelCounts["gpt-4"])
	}
	if modelCounts["gpt-3.5"] != 1 {
		t.Errorf("gpt-3.5 count: got %d, want 1", modelCounts["gpt-3.5"])
	}
}

// TestExecuteAnalytics_NoRows verifies that aggregation queries return
// one row with count=0 when no data matches (COUNT(*) always produces a row).
func TestExecuteAnalytics_NoRows(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	req := dsl.Query{
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
	}
	resp, err := qb.ExecuteAnalytics(context.Background(), req, "nonexistent-proj")
	if err != nil {
		t.Fatalf("ExecuteAnalytics: %v", err)
	}

	// COUNT(*) always returns one row, even with zero matching data.
	if len(resp.Rows) != 1 {
		t.Fatalf("rows: got %d, want 1 (COUNT(*) always returns a row)", len(resp.Rows))
	}
	countVal := resp.Rows[0]["count"].(int64)
	if countVal != 0 {
		t.Errorf("count: got %d, want 0", countVal)
	}
}

// TestExecuteAnalytics_ValidationError verifies that Validate() errors are propagated.
func TestExecuteAnalytics_ValidationError(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	// from after to should fail validation.
	req := dsl.Query{
		From: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
	}
	_, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err == nil {
		t.Fatal("expected error for from after to")
	}
}

// TestExecuteSpan_DefaultTimeRange verifies that when from/to are zero,
// the default 30-day range is applied.
func TestExecuteSpan_DefaultTimeRange(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	now := time.Now()
	baseTime := now.Add(-15 * 24 * time.Hour) // 15 days ago, within default range

	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, start_time, status_code) VALUES (?, ?, ?, ?, ?)`,
		"span-default", "trace-default", "test-proj",
		baseTime, "ok"); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	resp, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 50,
		// from and to omitted — should default to last 30 days.
	}, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 1 {
		t.Errorf("spans: got %d, want 1 (span within default time range)", len(resp.Spans))
	}
}

// TestExecuteSpan_DefaultTimeRangeExcluded verifies that spans outside the
// default 30-day window are not returned when from/to are omitted.
func TestExecuteSpan_DefaultTimeRangeExcluded(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().Add(-60 * 24 * time.Hour) // 60 days ago, outside default range

	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO spans (span_id, trace_id, project_id, start_time, status_code) VALUES (?, ?, ?, ?, ?)`,
		"span-old", "trace-old", "test-proj",
		baseTime, "ok"); err != nil {
		t.Fatalf("insert span: %v", err)
	}

	resp, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 50,
	}, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 0 {
		t.Errorf("spans: got %d, want 0 (span outside default time range)", len(resp.Spans))
	}
}

// TestExecuteSpan_FullPagination verifies that the full pagination loop
// (cursor → next cursor → next page) works through the full pipeline.
func TestExecuteSpan_FullPagination(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, status_code) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%02d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), "ok"); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	req := query.SpanQueryRequest{
		Limit: 2,
		From:  baseTime.Add(-time.Minute),
		To:    baseTime.Add(5 * time.Second),
	}
	var allSpanIDs []string
	cursor := ""
	for attempt := 0; attempt < 5; attempt++ {
		req.Cursor = cursor
		req.Limit = 2
		resp, err := qb.ExecuteSpan(context.Background(), req, "test-proj")
		if err != nil {
			t.Fatalf("ExecuteSpan page %d: %v", attempt, err)
		}
		for _, s := range resp.Spans {
			allSpanIDs = append(allSpanIDs, s.SpanID)
		}
		if resp.Next == "" {
			break
		}
		cursor = resp.Next
	}

	if len(allSpanIDs) != 5 {
		t.Fatalf("total spans across pages: got %d, want 5", len(allSpanIDs))
	}
}

// TestExecuteAnalytics_WithFilters verifies that analytics filters are applied correctly.
func TestExecuteAnalytics_WithFilters(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	for i, model := range []string{"gpt-4", "gpt-3.5", "gpt-4"} {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, model) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), model); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	req := dsl.Query{
		// Explicit time bounds prevent race with CompileLake's default time range.
		From: baseTime.Add(-time.Minute),
		To:   baseTime.Add(3 * time.Second),
		Filters: []dsl.Filter{
			{Field: "model", Op: dsl.OpEq, Value: "gpt-4"},
		},
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
	}
	resp, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteAnalytics: %v", err)
	}

	if len(resp.Rows) != 1 {
		t.Fatalf("rows: got %d, want 1", len(resp.Rows))
	}
	countVal := resp.Rows[0]["count"].(int64)
	if countVal != 2 {
		t.Errorf("count: got %d, want 2 (only gpt-4)", countVal)
	}
}

// TestCompileSpanQuery_ErrorPropagates verifies that SQL compilation errors
// (e.g. invalid filter) propagate through the full ExecuteSpan pipeline.
func TestCompileSpanQuery_ErrorPropagates(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	// An invalid filter field should cause an error during LakeSQL() compilation,
	// which should propagate through ExecuteSpan.
	_, err := qb.ExecuteSpan(context.Background(), query.SpanQueryRequest{
		Limit: 10,
		Filters: []query.SpanQueryFilter{
			{Field: "nonexistent_field", Op: "eq", Value: "x"},
		},
	}, "test-proj")
	if err == nil {
		t.Fatal("expected error for invalid filter field")
	}
}

// TestExecuteAnalytics_Limit verifies that the analytics query limit is applied.
func TestExecuteAnalytics_Limit(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	for i := 0; i < 10; i++ {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, model) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%02d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), fmt.Sprintf("model-%d", i)); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	req := dsl.Query{
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
		Limit: 3,
	}
	resp, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteAnalytics: %v", err)
	}

	if len(resp.Rows) != 1 {
		t.Fatalf("rows: got %d, want 1 (count is a single aggregate)", len(resp.Rows))
	}
}

// TestExecuteAnalytics_InvalidField verifies that the DSL compiler rejects
// unknown fields in aggregations.
func TestExecuteAnalytics_InvalidField(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	req := dsl.Query{
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggSum, Field: "nonexistent_field", Alias: "sum"},
		},
	}
	_, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err == nil {
		t.Fatal("expected error for invalid aggregation field")
	}
}

// TestExecuteAnalytics_Sum verifies that SUM aggregations work through the pipeline.
func TestExecuteAnalytics_Sum(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	costs := []float64{0.1, 0.2, 0.3}
	for i, cost := range costs {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, cost_usd) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), cost); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	req := dsl.Query{
		// Explicit time bounds prevent race with CompileLake's default time range.
		From: baseTime.Add(-time.Minute),
		To:   baseTime.Add(3 * time.Second),
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggSum, Field: "cost_usd", Alias: "total_cost"},
		},
	}
	resp, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteAnalytics: %v", err)
	}

	if len(resp.Rows) != 1 {
		t.Fatalf("rows: got %d, want 1", len(resp.Rows))
	}
	totalCost := resp.Rows[0]["total_cost"].(float64)
	if totalCost < 0.599 || totalCost > 0.601 {
		t.Errorf("total_cost: got %f, want ~0.6", totalCost)
	}
}

// TestExecuteAnalytics_Avg verifies that AVG aggregations work through the pipeline.
func TestExecuteAnalytics_Avg(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	inputs := []int64{100, 200, 300}
	for i, input := range inputs {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, input_tokens) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), input); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	req := dsl.Query{
		// Explicit time bounds prevent race with CompileLake's default time range.
		From: baseTime.Add(-time.Minute),
		To:   baseTime.Add(3 * time.Second),
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggAvg, Field: "input_tokens", Alias: "avg_input"},
		},
	}
	resp, err := qb.ExecuteAnalytics(context.Background(), req, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteAnalytics: %v", err)
	}

	if len(resp.Rows) != 1 {
		t.Fatalf("rows: got %d, want 1", len(resp.Rows))
	}
	avgInput := resp.Rows[0]["avg_input"].(float64)
	// 100 + 200 + 300 = 600 / 3 = 200
	if avgInput < 199.9 || avgInput > 200.1 {
		t.Errorf("avg_input: got %f, want 200", avgInput)
	}
}

// TestQueryBuilder_LakeServerIntegration verifies the full span pipeline
// end-to-end through a real Lake server.
func TestQueryBuilder_LakeServerIntegration(t *testing.T) {
	ctx := context.Background()
	lk := testLakeServer(t)

	// Relative to now so the spans always fall inside the query's default
	// time window (a fixed date rots out of the window as the calendar
	// advances). Truncated to seconds to avoid timestamp-precision drift.
	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	spans := []*domain.Span{
		{
			SpanID: "span-001", TraceID: "trace-001", ProjectID: "proj-lake",
			Name: "agent.run", Kind: domain.SpanKindAgent,
			StartTime: base, EndTime: base.Add(5 * time.Second),
			InputTokens: 100, OutputTokens: 50, CostUSD: 0.05, StatusCode: "ok", Model: "gpt-4",
		},
		{
			SpanID: "span-002", TraceID: "trace-002", ProjectID: "proj-lake",
			Name: "llm.completion", Kind: domain.SpanKindLLM,
			StartTime: base.Add(time.Second), EndTime: base.Add(3 * time.Second),
			InputTokens: 200, OutputTokens: 100, CostUSD: 0.10, StatusCode: "error", Model: "gpt-3.5",
		},
		{
			SpanID: "span-003", TraceID: "trace-003", ProjectID: "proj-lake",
			Name: "tool.call", Kind: domain.SpanKindTool,
			StartTime: base.Add(2 * time.Second), EndTime: base.Add(4 * time.Second),
			InputTokens: 50, OutputTokens: 25, CostUSD: 0.02, StatusCode: "ok", Model: "claude",
		},
	}
	if err := lk.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	qb := &QueryBuilder{Lake: lk}

	// Execute a span query with a filter.
	resp, err := qb.ExecuteSpan(ctx, query.SpanQueryRequest{
		Limit: 10,
		Filters: []query.SpanQueryFilter{
			{Field: "status_code", Op: "eq", Value: "ok"},
		},
	}, "proj-lake")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 2 {
		t.Fatalf("spans: got %d, want 2 (only status_code=ok)", len(resp.Spans))
	}

	// Verify that both spans have status "ok".
	for _, s := range resp.Spans {
		if s.StatusCode != "ok" {
			t.Errorf("status_code: got %q, want %q", s.StatusCode, "ok")
		}
	}
}

// TestHandlerRefactoring_CompileExecuteScanVerified verifies that the full
// compile → execute → scan path through QueryBuilder works correctly,
// which mirrors what the handler does.
func TestHandlerRefactoring_CompileExecuteScanVerified(t *testing.T) {
	// This test validates the full pipeline that the handler should delegate to:
	// Request → NewSpanQuery → LakeSQL → Execute → ScanRows → NextCursor → Response
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	now := time.Now()
	for i := 0; i < 7; i++ {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, name, kind, status_code, input_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%02d", i), fmt.Sprintf("trace-%02d", i), "test-proj",
			now.Add(time.Duration(i*10)*time.Second),
			fmt.Sprintf("agent.%d", i), "agent", "ok",
			int64(100+i*10), float64(0.01)*float64(i)); err != nil {
			t.Fatalf("insert span %d: %v", i, err)
		}
	}

	// Full pipeline as a handler would execute it.
	// Explicit time bounds prevent race with NewSpanQuery's default time range.
	req := query.SpanQueryRequest{
		Limit: 3,
		From:  now.Add(-time.Minute),
		To:    now.Add(65 * time.Second),
	}
	resp, err := qb.ExecuteSpan(context.Background(), req, "test-proj")
	if err != nil {
		t.Fatalf("ExecuteSpan: %v", err)
	}

	if len(resp.Spans) != 3 {
		t.Errorf("spans: got %d, want 3", len(resp.Spans))
	}
	// There should be more results → next cursor is non-empty.
	if resp.Next == "" {
		t.Error("expected non-empty next cursor")
	}

	// Verify the response shape matches what the handler returns.
	if resp.Limit != 3 {
		t.Errorf("limit: got %d, want 3", resp.Limit)
	}

	// Verify spans are ordered by start_time DESC (newest first).
	if len(resp.Spans) >= 2 {
		prevTime := resp.Spans[0].StartTime
		for i := 1; i < len(resp.Spans); i++ {
			if resp.Spans[i].StartTime.After(prevTime) {
				t.Errorf("spans not ordered DESC: span[%d] %s > span[%d] %s",
					i, resp.Spans[i].StartTime, i-1, prevTime)
			}
			prevTime = resp.Spans[i].StartTime
		}
	}
}

// TestUnifiedExecute_Analytics verifies that the unified Execute method
// correctly dispatches analytics queries: validates, compiles SQL,
// executes against the Lake, scans rows, and returns a QueryResult.
func TestUnifiedExecute_Analytics(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	models := []string{"gpt-4", "gpt-3.5", "gpt-4"}
	for i, model := range models {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, model) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), model); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	// Build a dsl.Query with type "analytics" — this is what the handler
	// would construct from the incoming JSON payload.
	req := dsl.Query{
		From:    baseTime.Add(-time.Minute),
		To:      baseTime.Add(3 * time.Second),
		ProjectID: "test-proj",
		QueryType: dsl.QueryTypeAnalytics,
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
		GroupBy: []dsl.GroupByField{
			{Field: "model"},
		},
	}

	// Call the unified Execute method.
	result, err := qb.Execute(context.Background(), &req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Verify the result has rows and they are in QueryResult.
	if result == nil {
		t.Fatal("Execute returned nil QueryResult")
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows: got %d, want 2 (gpt-4 and gpt-3.5)", len(result.Rows))
	}

	// Build a map from model -> count.
	modelCounts := make(map[string]int64)
	for _, row := range result.Rows {
		model := row["model"].(string)
		count := row["count"].(int64)
		modelCounts[model] = count
	}

	if modelCounts["gpt-4"] != 2 {
		t.Errorf("gpt-4 count: got %d, want 2", modelCounts["gpt-4"])
	}
	if modelCounts["gpt-3.5"] != 1 {
		t.Errorf("gpt-3.5 count: got %d, want 1", modelCounts["gpt-3.5"])
	}

	// SpanQueryResult fields should be zero for analytics.
	if len(result.Spans) != 0 {
		t.Errorf("spans: got %d, want 0 (analytics query returns no spans)", len(result.Spans))
	}
}

// TestUnifiedExecute_Span verifies that the unified Execute method
// correctly dispatches span queries: builds a SpanQuery, compiles SQL,
// executes against the Lake, scans rows, computes cursor, and returns
// a QueryResult with populated spans.
func TestUnifiedExecute_Span(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, status_code) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), "ok"); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	// Build a dsl.Query with type "span" — the handler converts the
	// SpanQueryRequest into a dsl.Query before calling Execute.
	req := dsl.Query{
		From:      baseTime.Add(-time.Minute),
		To:        baseTime.Add(3 * time.Second),
		ProjectID: "test-proj",
		QueryType: dsl.QueryTypeSpan,
		Filters: []dsl.Filter{
			{Field: "status_code", Op: dsl.OpEq, Value: "ok"},
		},
		Limit: 50,
	}

	result, err := qb.Execute(context.Background(), &req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result == nil {
		t.Fatal("Execute returned nil QueryResult")
	}

	// Verify spans are populated.
	if len(result.Spans) != 3 {
		t.Fatalf("spans: got %d, want 3", len(result.Spans))
	}

	// Verify Next cursor is empty when results < limit (no more pages).
	if result.Next != "" {
		t.Errorf("Next cursor: got %q, want empty (results < limit)", result.Next)
	}

	// Verify Limit is populated.
	if result.Limit != 50 {
		t.Errorf("Limit: got %d, want 50", result.Limit)
	}

	// Rows should be empty for span queries.
	if len(result.Rows) != 0 {
		t.Errorf("rows: got %d, want 0 (span query returns spans)", len(result.Rows))
	}
}

// TestUnifiedExecute_SpanWithNextCursor verifies that Next is set when there
// are more results than the effective limit.
func TestUnifiedExecute_SpanWithNextCursor(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	baseTime := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO spans (span_id, trace_id, project_id, start_time, status_code) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("span-%d", i), "trace-"+fmt.Sprint(i), "test-proj",
			baseTime.Add(time.Duration(i)*time.Second), "ok"); err != nil {
			t.Fatalf("insert span: %v", err)
		}
	}

	req := dsl.Query{
		From:      baseTime.Add(-time.Minute),
		To:        baseTime.Add(5 * time.Second),
		ProjectID: "test-proj",
		QueryType: dsl.QueryTypeSpan,
		Limit:     2,
	}

	result, err := qb.Execute(context.Background(), &req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Should have exactly limit=2 spans, but Next is non-empty because 5 total > 2 limit.
	if len(result.Spans) != 2 {
		t.Fatalf("spans: got %d, want 2", len(result.Spans))
	}
	if result.Next == "" {
		t.Fatal("Next cursor should be set when results exceed limit")
	}
	if result.Limit != 2 {
		t.Errorf("Limit: got %d, want 2", result.Limit)
	}
}

// TestUnifiedExecute_UnknownType verifies that Execute returns a
// ValidationError when the QueryType is not recognized.
func TestUnifiedExecute_UnknownType(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	req := dsl.Query{
		QueryType: dsl.QueryType("unknown"),
	}

	_, err := qb.Execute(context.Background(), &req)
	if err == nil {
		t.Fatal("Execute: expected error for unknown query type")
	}
	if !IsValidationError(err) {
		t.Errorf("expected ValidationError for unknown query type, got %T: %v", err, err)
	}
}

// TestUnifiedExecute_AnalyticsValidation verifies that Execute propagates
// validation errors from analytics queries (e.g. from after to).
func TestUnifiedExecute_AnalyticsValidation(t *testing.T) {
	db := testLakeDB(t)
	qb := &QueryBuilder{Lake: db}

	req := dsl.Query{
		QueryType: dsl.QueryTypeAnalytics,
		From:      time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Aggregations: []dsl.Aggregation{
			{Function: dsl.AggCount, Field: "*", Alias: "count"},
		},
	}

	_, err := qb.Execute(context.Background(), &req)
	if err == nil {
		t.Fatal("expected ValidationError for from after to")
	}
	if !IsValidationError(err) {
		t.Errorf("expected ValidationError, got %T: %v", err, err)
	}
}