package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
	"github.com/omneval/omneval/services/query/internal/cursor"
)

func TestNewSpanQuery_Valid(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}
	if q.projectID != "proj-abc" {
		t.Errorf("projectID: got %q, want %q", q.projectID, "proj-abc")
	}
	if q.limit != 10 {
		t.Errorf("limit: got %d, want %d", q.limit, 10)
	}
}

func TestNewSpanQuery_DefaultLimit(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Now(),
		To:   time.Now().Add(time.Hour),
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}
	if q.limit != DefaultLimit {
		t.Errorf("limit: got %d, want %d", q.limit, DefaultLimit)
	}
}

func TestNewSpanQuery_LimitCapped(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Now(),
		To:    time.Now().Add(time.Hour),
		Limit: 1000, // exceeds MaxLimit
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}
	// Values exceeding MaxLimit fall back to DefaultLimit.
	if q.limit != DefaultLimit {
		t.Errorf("limit: got %d, want %d (DefaultLimit)", q.limit, DefaultLimit)
	}
}

func TestNewSpanQuery_LimitWithinMax(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Now(),
		To:    time.Now().Add(time.Hour),
		Limit: 100,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}
	if q.limit != 100 {
		t.Errorf("limit: got %d, want 100", q.limit)
	}
}

func TestNewSpanQuery_InvalidCursor(t *testing.T) {
	req := SpanQueryRequest{
		From:   time.Now(),
		To:     time.Now().Add(time.Hour),
		Cursor: "not-valid-base64!!!",
		Limit:  10,
	}

	_, err := NewSpanQuery("proj-abc", req)
	if err == nil {
		t.Error("expected error for invalid cursor")
	}
}

func TestNewSpanQuery_InvalidFilterField(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Now(),
		To:   time.Now().Add(time.Hour),
		Filters: []SpanQueryFilter{
			{Field: "nonexistent_field", Op: "eq", Value: "x"},
		},
	}

	_, err := NewSpanQuery("proj-abc", req)
	if err == nil {
		t.Error("expected error for invalid filter field")
	}
}

func TestNewSpanQuery_InvalidFilterOp(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Now(),
		To:   time.Now().Add(time.Hour),
		Filters: []SpanQueryFilter{
			{Field: "model", Op: "regex", Value: ".*"},
		},
	}

	_, err := NewSpanQuery("proj-abc", req)
	if err == nil {
		t.Error("expected error for invalid filter op")
	}
}

func TestNextCursor_MoreSpansThanLimit(t *testing.T) {
	now := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := make([]*domain.Span, DefaultLimit+1)
	for i := range spans {
		spans[i] = &domain.Span{
			SpanID:    fmt.Sprintf("span-%03d", i),
			StartTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	next := NextCursor(spans, DefaultLimit)
	if next == "" {
		t.Error("expected non-empty next cursor when more spans than limit")
	}

	// Cursor should encode the last span in the page (index DefaultLimit-1).
	expectedLast := spans[DefaultLimit-1]
	decoded, err := cursor.Decode(next)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if !decoded.StartTime.Equal(expectedLast.StartTime) {
		t.Errorf("cursor start_time: got %v, want %v", decoded.StartTime, expectedLast.StartTime)
	}
	if decoded.SpanID != expectedLast.SpanID {
		t.Errorf("cursor span_id: got %q, want %q", decoded.SpanID, expectedLast.SpanID)
	}
}

func TestNextCursor_ExactlyLimit_IsLastPage(t *testing.T) {
	// When the DB returns exactly `limit` rows (not limit+1), there are no
	// more results — next cursor must be empty.
	// Under limit+1 semantics: DB fetches limit+1; if only limit come back,
	// it's the last page.
	limit := 10
	spans := make([]*domain.Span, limit)
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	for i := range spans {
		spans[i] = &domain.Span{
			SpanID:    fmt.Sprintf("span-%03d", i),
			StartTime: base.Add(time.Duration(i) * time.Second),
		}
	}

	next := NextCursor(spans, limit)
	if next != "" {
		t.Errorf("expected empty next cursor when result count == limit (last page), got %q", next)
	}
}

func TestNextCursor_FewerSpansThanLimit(t *testing.T) {
	spans := []*domain.Span{
		{SpanID: "span-001", StartTime: time.Now()},
	}

	next := NextCursor(spans, 50)
	if next != "" {
		t.Errorf("expected empty next cursor with fewer spans than limit, got %q", next)
	}
}

func TestNextCursor_EmptySpans(t *testing.T) {
	next := NextCursor([]*domain.Span{}, 50)
	if next != "" {
		t.Errorf("expected empty next cursor with empty spans, got %q", next)
	}
}

func TestValidateFilter_AllowlistedFields(t *testing.T) {
	// All fields in allSpanFields should pass validation.
	for field := range allSpanFields {
		f := SpanQueryFilter{Field: string(field), Op: "eq", Value: "x"}
		if err := validateFilter(f); err != nil {
			t.Errorf("field %q should be valid: %v", field, err)
		}
	}
}

func TestValidateFilter_RejectsUnknownField(t *testing.T) {
	f := SpanQueryFilter{Field: "unknown_field", Op: "eq", Value: "x"}
	if err := validateFilter(f); err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestValidateFilter_RejectsUnknownOp(t *testing.T) {
	f := SpanQueryFilter{Field: "model", Op: "regex", Value: ".*"}
	if err := validateFilter(f); err == nil {
		t.Error("expected error for unknown operator")
	}
}

func TestValidateFilter_RejectsNilValue(t *testing.T) {
	f := SpanQueryFilter{Field: "model", Op: "eq", Value: nil}
	if err := validateFilter(f); err == nil {
		t.Error("expected error for nil value")
	}
}

func TestValidateFilter_UnknownFieldListsAcceptedFields(t *testing.T) {
	f := SpanQueryFilter{Field: "unknown_field", Op: "eq", Value: "x"}
	err := validateFilter(f)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "accepted fields") {
		t.Errorf("error should list accepted fields, got: %q", errMsg)
	}
	for _, valid := range []string{"model", "name", "trace_id"} {
		if !strings.Contains(errMsg, valid) {
			t.Errorf("error should mention field %q, got: %q", valid, errMsg)
		}
	}
}

func TestValidateFilter_UnknownOpListsAcceptedOperators(t *testing.T) {
	f := SpanQueryFilter{Field: "model", Op: "regex", Value: ".*"}
	err := validateFilter(f)
	if err == nil {
		t.Fatal("expected error for unknown operator")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "accepted operators") {
		t.Errorf("error should list accepted operators, got: %q", errMsg)
	}
	for _, valid := range []string{"eq", "neq", "in"} {
		if !strings.Contains(errMsg, valid) {
			t.Errorf("error should mention operator %q, got: %q", valid, errMsg)
		}
	}
}

func TestValidSpanQueryFilters_ReturnsFields(t *testing.T) {
	fields := ValidSpanQueryFilters()
	if len(fields) == 0 {
		t.Fatal("expected non-empty list of fields")
	}
	expectedFields := []string{"model", "name", "trace_id", "service_name"}
	fieldSet := make(map[string]bool)
	for _, f := range fields {
		fieldSet[f] = true
	}
	for _, exp := range expectedFields {
		if !fieldSet[exp] {
			t.Errorf("expected field %q in list", exp)
		}
	}
}

func TestValidFilterOperators_ReturnsOps(t *testing.T) {
	ops := ValidFilterOperators()
	if len(ops) == 0 {
		t.Fatal("expected non-empty list of operators")
	}
	expectedOps := []string{"eq", "neq", "in", "lt", "lte"}
	for _, exp := range expectedOps {
		found := false
		for _, op := range ops {
			if op == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected operator %q in list", exp)
		}
	}
}

func TestCompileFilter_InWithEmptySlice(t *testing.T) {
	f := SpanQueryFilter{Field: "model", Op: "in", Value: []any{}}
	sql, args, err := compileFilter(f)
	if err != nil {
		t.Fatalf("compileFilter error: %v", err)
	}
	if sql != "model IS NULL" {
		t.Errorf("expected 'model IS NULL' for empty in, got %q", sql)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args for empty in, got %d", len(args))
	}
}

func TestCompileFilter_StringSliceIn(t *testing.T) {
	f := SpanQueryFilter{Field: "model", Op: "in", Value: []string{"gpt-4", "gpt-3.5"}}
	sql, args, err := compileFilter(f)
	if err != nil {
		t.Fatalf("compileFilter error: %v", err)
	}
	if !strings.Contains(sql, "model IN") {
		t.Error("expected model IN filter")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args for 2-element in, got %d", len(args))
	}
}

func TestMarshalResponse(t *testing.T) {
	resp := SpanResponse{
		Spans: []*domain.Span{},
		Limit: 50,
	}

	data, err := MarshalResponse(resp)
	if err != nil {
		t.Fatalf("MarshalResponse error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON")
	}
}

func TestNextCursor_PageBoundary(t *testing.T) {
	// Verify cursor encodes the LAST element of the page, not the first.
	// This ensures the next page picks up from the right position.
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := make([]*domain.Span, 10)
	for i := range spans {
		spans[i] = &domain.Span{
			SpanID:    fmt.Sprintf("span-%03d", i),
			StartTime: base.Add(time.Duration(i) * time.Second),
		}
	}

	next := NextCursor(spans, 5)
	if next == "" {
		t.Fatal("expected non-empty cursor")
	}

	decoded, err := cursor.Decode(next)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	// Last page element is index 4.
	expected := spans[4]
	if !decoded.StartTime.Equal(expected.StartTime) {
		t.Errorf("cursor start_time: got %v, want %v", decoded.StartTime, expected.StartTime)
	}
	if decoded.SpanID != expected.SpanID {
		t.Errorf("cursor span_id: got %q, want %q", decoded.SpanID, expected.SpanID)
	}
}

func TestNewSpanQuery_FromAfterTo_ReturnsError(t *testing.T) {
	// When from is after to, return a 400 error.
	req := SpanQueryRequest{
		From:  time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit: 50,
	}

	_, err := NewSpanQuery("proj-abc", req)
	if err == nil {
		t.Fatal("expected error when from > to")
	}
	if !strings.Contains(err.Error(), "from must not be after to") {
		t.Errorf("error message should mention 'from must not be after to', got: %v", err)
	}
}

func TestNextCursor_SameStartTimeDifferentSpanID(t *testing.T) {
	// When multiple spans share the same start_time, the cursor should
	// encode span_id as tiebreaker.
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		{SpanID: "aaa", StartTime: base},
		{SpanID: "bbb", StartTime: base},
		{SpanID: "ccc", StartTime: base},
	}

	next := NextCursor(spans, 2)
	if next == "" {
		t.Fatal("expected non-empty cursor")
	}

	decoded, err := cursor.Decode(next)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	// Last page element is "bbb" (index 1).
	if decoded.SpanID != "bbb" {
		t.Errorf("cursor span_id: got %q, want %q", decoded.SpanID, "bbb")
	}
}

// --- contains filter operator (issue #15) ---

func TestNewSpanQuery_ContainsOpAccepted(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "name", Op: "contains", Value: "qa-"},
		},
	}

	_, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery should accept contains op: %v", err)
	}
}

// ─── Issue #85: Lake single-table reads ───

func TestLakeSQL_FirstPage(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.LakeSQL()
	if err != nil {
		t.Fatalf("LakeSQL error: %v", err)
	}

	if !strings.Contains(sql, "FROM lake.spans") {
		t.Errorf("expected single-table read from lake.spans, got:\n%s", sql)
	}
	if strings.Contains(sql, "UNION") {
		t.Errorf("Lake reads must not UNION hot+cold, got:\n%s", sql)
	}
	// The traces-list query (issue #136) filters the underlying spans in the
	// "filtered" CTE WHERE clause, then selects rn=1 (root span) rows from
	// "ranked" with a second WHERE — two WHERE clauses is expected here.
	if got := strings.Count(sql, "WHERE"); got != 2 {
		t.Errorf("expected two WHERE clauses (filtered CTE + root-span selection), got %d:\n%s", got, sql)
	}
	if !strings.Contains(sql, "GROUP BY trace_id") {
		t.Errorf("expected trace-level rollup aggregation, got:\n%s", sql)
	}
	// args: project_id, from, to, limit+1.
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[len(args)-1] != 11 {
		t.Errorf("last arg should be limit+1 (11), got %v", args[len(args)-1])
	}
}

func TestLakeSQL_CursorPage(t *testing.T) {
	req := SpanQueryRequest{
		From:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit:  10,
		Cursor: "eyJzdGFydF90aW1lIjoiMjAyNS0wMS0wMVQxMDowMDowMFoiLCJzcGFuX2lkIjoic3BhbjAxIn0",
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.LakeSQL()
	if err != nil {
		t.Fatalf("LakeSQL error: %v", err)
	}

	// The cursor predicate is ANDed onto the root-span selection WHERE
	// (r.rn = 1), the second of the two WHERE clauses (see
	// TestLakeSQL_FirstPage for why two are expected post-#136).
	if got := strings.Count(sql, "WHERE"); got != 2 {
		t.Errorf("expected two WHERE clauses (filtered CTE + root-span selection), got %d:\n%s", got, sql)
	}
	if !strings.Contains(sql, "AND (r.start_time < ? OR (r.start_time = ? AND r.span_id < ?))") {
		t.Errorf("expected keyset cursor predicate ANDed into root-span WHERE, got:\n%s", sql)
	}
	// args: project_id, from, to, cursor start_time x2, cursor span_id, limit+1.
	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %v", len(args), args)
	}
}

func TestLakeTraceSpansSQL_Dedupes(t *testing.T) {
	q, err := NewSpanQuery("proj-abc", SpanQueryRequest{})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args := q.LakeTraceSpansSQL("trace-1")
	if !strings.Contains(sql, "ROW_NUMBER() OVER (PARTITION BY trace_id, span_id") {
		t.Errorf("trace detail must dedupe on (trace_id, span_id), got:\n%s", sql)
	}
	if len(args) != 2 || args[0] != "trace-1" || args[1] != "proj-abc" {
		t.Errorf("args: got %v, want [trace-1 proj-abc]", args)
	}
}

// TestLakeSQL_CursorPagination_Integration walks a multi-page span list
// against a real local Lake and verifies keyset pagination behaves like the
// legacy path: every span appears exactly once, newest first.
func TestLakeSQL_CursorPagination_Integration(t *testing.T) {
	ctx := context.Background()

	cfg, _ := lakeservertest.NewLocal(t)
	lk, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	defer lk.Close()

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var spans []*domain.Span
	for i := 0; i < 5; i++ {
		spans = append(spans, &domain.Span{
			SpanID:    fmt.Sprintf("span-%02d", i),
			TraceID:   fmt.Sprintf("trace-%02d", i),
			ProjectID: "proj-page",
			Name:      "op",
			StartTime: base.Add(time.Duration(i) * time.Minute),
			EndTime:   base.Add(time.Duration(i)*time.Minute + time.Second),
		})
	}
	if err := lk.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	var seen []string
	cursorStr := ""
	for page := 0; page < 10; page++ {
		req := SpanQueryRequest{Limit: 2, Cursor: cursorStr}
		q, err := NewSpanQuery("proj-page", req)
		if err != nil {
			t.Fatalf("NewSpanQuery: %v", err)
		}
		sqlStr, args, err := q.LakeSQL()
		if err != nil {
			t.Fatalf("LakeSQL: %v", err)
		}
		rows, err := lk.DB().QueryContext(ctx, sqlStr, args...)
		if err != nil {
			t.Fatalf("page %d query: %v\nSQL:\n%s", page, err, sqlStr)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("columns: %v", err)
		}
		var raw [][]any
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("scan: %v", err)
			}
			raw = append(raw, vals)
		}
		rows.Close()

		pageSpans, err := ScanRows(raw)
		if err != nil {
			t.Fatalf("ScanRows: %v", err)
		}

		next := NextCursor(pageSpans, q.EffectiveLimit())
		if len(pageSpans) > q.EffectiveLimit() {
			pageSpans = pageSpans[:q.EffectiveLimit()]
		}
		for _, s := range pageSpans {
			seen = append(seen, s.SpanID)
		}
		if next == "" {
			break
		}
		cursorStr = next
	}

	want := []string{"span-04", "span-03", "span-02", "span-01", "span-00"}
	if len(seen) != len(want) {
		t.Fatalf("paginated spans: got %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("paginated spans out of order: got %v, want %v", seen, want)
		}
	}
}

// TestLakeSQL_OneRowPerTrace verifies that the traces-list query (LakeSQL)
// returns exactly one row per distinct trace_id — the root span (the span
// with no parent_id) annotated with trace-level rollups — rather than a flat
// row per span (issue #136).
func TestLakeSQL_OneRowPerTrace(t *testing.T) {
	ctx := context.Background()

	cfg, _ := lakeservertest.NewLocal(t)
	lk, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	defer lk.Close()

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// A single trace with a root span and three child spans.
	root := &domain.Span{
		SpanID:       "root-span",
		TraceID:      "trace-rollup",
		ParentID:     "",
		ProjectID:    "proj-rollup",
		Name:         "agent.run",
		Kind:         domain.SpanKindAgent,
		StartTime:    base,
		EndTime:      base.Add(10 * time.Second),
		InputTokens:  10,
		OutputTokens: 20,
		CostUSD:      0.01,
		StatusCode:   "ok",
	}
	child1 := &domain.Span{
		SpanID:       "child-1",
		TraceID:      "trace-rollup",
		ParentID:     "root-span",
		ProjectID:    "proj-rollup",
		Name:         "litellm.completion",
		Kind:         domain.SpanKindLLM,
		StartTime:    base.Add(1 * time.Second),
		EndTime:      base.Add(3 * time.Second),
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.05,
		StatusCode:   "ok",
	}
	child2 := &domain.Span{
		SpanID:       "child-2",
		TraceID:      "trace-rollup",
		ParentID:     "root-span",
		ProjectID:    "proj-rollup",
		Name:         "tool.call",
		Kind:         domain.SpanKindTool,
		StartTime:    base.Add(3 * time.Second),
		EndTime:      base.Add(4 * time.Second),
		InputTokens:  5,
		OutputTokens: 5,
		CostUSD:      0.0,
		StatusCode:   "ok",
	}
	// A child that ends after the root span's own end_time, so the trace's
	// overall end_time (and therefore duration) must reflect this span, not
	// just the root span's own end_time. Also has an error status, so the
	// trace's overall status must reflect the worst status across spans.
	child3 := &domain.Span{
		SpanID:       "child-3",
		TraceID:      "trace-rollup",
		ParentID:     "root-span",
		ProjectID:    "proj-rollup",
		Name:         "tool.call.slow",
		Kind:         domain.SpanKindTool,
		StartTime:    base.Add(4 * time.Second),
		EndTime:      base.Add(15 * time.Second),
		InputTokens:  1,
		OutputTokens: 1,
		CostUSD:      0.001,
		StatusCode:   "error",
	}

	// A second, unrelated trace in the same project — must appear as its own
	// row and must not have its rollups contaminated by trace-rollup's spans.
	other := &domain.Span{
		SpanID:    "other-root",
		TraceID:   "trace-other",
		ParentID:  "",
		ProjectID: "proj-rollup",
		Name:      "agent.run",
		Kind:      domain.SpanKindAgent,
		StartTime: base.Add(-1 * time.Minute),
		EndTime:   base.Add(-1*time.Minute + 2*time.Second),
		StatusCode: "ok",
	}

	if err := lk.InsertSpans(ctx, []*domain.Span{root, child1, child2, child3, other}); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	req := SpanQueryRequest{Limit: 10}
	q, err := NewSpanQuery("proj-rollup", req)
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}

	sqlStr, args, err := q.LakeSQL()
	if err != nil {
		t.Fatalf("LakeSQL: %v", err)
	}

	rows, err := lk.DB().QueryContext(ctx, sqlStr, args...)
	if err != nil {
		t.Fatalf("query: %v\nSQL:\n%s", err, sqlStr)
	}
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	var raw [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		raw = append(raw, vals)
	}
	rows.Close()

	traces, err := ScanRows(raw)
	if err != nil {
		t.Fatalf("ScanRows: %v", err)
	}

	if len(traces) != 2 {
		t.Fatalf("expected one row per trace (2 traces), got %d rows", len(traces))
	}

	// Newest trace (trace-rollup) first — ORDER BY start_time DESC.
	got := traces[0]
	if got.TraceID != "trace-rollup" {
		t.Fatalf("expected first row to be trace-rollup, got %q", got.TraceID)
	}
	if got.SpanID != "root-span" {
		t.Errorf("expected root span (root-span) to represent the trace, got span_id %q", got.SpanID)
	}
	if got.SpanCount != 4 {
		t.Errorf("span_count: got %d, want 4", got.SpanCount)
	}
	if got.InputTokens != 116 { // 10 + 100 + 5 + 1
		t.Errorf("total input_tokens: got %d, want 116", got.InputTokens)
	}
	if got.OutputTokens != 76 { // 20 + 50 + 5 + 1
		t.Errorf("total output_tokens: got %d, want 76", got.OutputTokens)
	}
	if want := 0.061; got.CostUSD < want-0.0001 || got.CostUSD > want+0.0001 {
		t.Errorf("total cost_usd: got %v, want ~%v", got.CostUSD, want)
	}
	// Trace end_time must be the max end_time across all spans (child-3's,
	// 15s after base), not just the root span's own end_time (10s).
	wantEnd := base.Add(15 * time.Second)
	if !got.EndTime.Equal(wantEnd) {
		t.Errorf("trace end_time: got %v, want %v", got.EndTime, wantEnd)
	}
	// Overall status must reflect the worst status across spans — child-3
	// errored, so the trace-level status must be "error" even though the
	// root span itself was "ok".
	if got.StatusCode != "error" {
		t.Errorf("overall status_code: got %q, want %q", got.StatusCode, "error")
	}

	// KindCounts must reflect the per-kind breakdown across all spans in the
	// trace (root agent.run + 2 tool.call + 1 litellm.completion), so the
	// Traces list "Levels" column can render observation pills without a
	// flat span list (issue #136).
	wantKindCounts := map[string]int64{
		string(domain.SpanKindAgent): 1,
		string(domain.SpanKindLLM):   1,
		string(domain.SpanKindTool):  2,
	}
	if len(got.KindCounts) != len(wantKindCounts) {
		t.Errorf("kind_counts: got %v, want %v", got.KindCounts, wantKindCounts)
	}
	for k, want := range wantKindCounts {
		if got.KindCounts[k] != want {
			t.Errorf("kind_counts[%q]: got %d, want %d", k, got.KindCounts[k], want)
		}
	}

	other2 := traces[1]
	if other2.TraceID != "trace-other" {
		t.Fatalf("expected second row to be trace-other, got %q", other2.TraceID)
	}
	if other2.SpanCount != 1 {
		t.Errorf("trace-other span_count: got %d, want 1 (must not be contaminated by trace-rollup's spans)", other2.SpanCount)
	}
	wantOtherKindCounts := map[string]int64{string(domain.SpanKindAgent): 1}
	if len(other2.KindCounts) != len(wantOtherKindCounts) || other2.KindCounts[string(domain.SpanKindAgent)] != 1 {
		t.Errorf("trace-other kind_counts: got %v, want %v", other2.KindCounts, wantOtherKindCounts)
	}
}
