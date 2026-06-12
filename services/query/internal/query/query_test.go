package query

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/storage"
	"github.com/omneval/omneval/services/query/internal/cursor"
)

func TestNewSpanQuery_Valid(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
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

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
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

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
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

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
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

	_, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
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

	_, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
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

	_, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err == nil {
		t.Error("expected error for invalid filter op")
	}
}

func TestSQL_FirstPage(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if len(args) == 0 {
		t.Error("expected at least one arg for first page")
	}

	// Should contain UNION ALL.
	if !strings.Contains(sql, "UNION ALL") {
		t.Error("expected UNION ALL in SQL")
	}

	// Should contain ORDER BY start_time DESC.
	if !strings.Contains(sql, "start_time DESC") {
		t.Error("expected ORDER BY start_time DESC")
	}

	// Should contain LIMIT.
	if !strings.Contains(sql, "LIMIT") {
		t.Error("expected LIMIT in SQL")
	}

	// Should not contain cursor predicates on first page.
	if strings.Contains(sql, "cursor") {
		t.Error("should not have cursor predicates on first page")
	}
}

func TestSQL_CursorPage(t *testing.T) {
	req := SpanQueryRequest{
		From:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit:  10,
		Cursor: "eyJzdGFydF90aW1lIjoiMjAyNS0wMS0wMVQxMDowMDowMFoiLCJzcGFuX2lkIjoic3BhbjAxIn0",
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Cursor page should have cursor predicate args (start_time, start_time, span_id, limit).
	if len(args) < 4 {
		t.Errorf("expected at least 4 args for cursor page, got %d", len(args))
	}

	if !strings.Contains(sql, "start_time < ?") {
		t.Error("expected cursor start_time < predicate")
	}
}

func TestSQL_ProjectIDInjected(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	q, err := NewSpanQuery("proj-secret", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain project_id = ? with proj-secret as arg.
	expected := "project_id = ?"
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SQL", expected)
	}

	// First arg should be project ID.
	if len(args) < 1 || args[0] != "proj-secret" {
		t.Errorf("first arg should be project ID, got %v", args[0])
	}
}

func TestSQL_TimeRangeFilter(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "start_time >= ?") {
		t.Error("expected start_time >= filter")
	}
	if !strings.Contains(sql, "start_time <= ?") {
		t.Error("expected start_time <= filter")
	}

	// Should have at least 2 args for time range.
	if len(args) < 2 {
		t.Errorf("expected at least 2 args for time range, got %d", len(args))
	}
}

func TestSQL_FilterModelEq(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "model", Op: "eq", Value: "gpt-4"},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "model = ?") {
		t.Error("expected model = ? filter")
	}

	// model = ? should come after project_id and time range.
	// Count args: project_id + 2 time args + model arg + limit = 4 min.
	if len(args) < 4 {
		t.Errorf("expected at least 4 args, got %d", len(args))
	}
}

func TestSQL_FilterModelIn(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "model", Op: "in", Value: []any{"gpt-4", "gpt-3.5-turbo"}},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "model IN") {
		t.Error("expected model IN filter")
	}
}

func TestSQL_ColdSideStub(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Cold side stub uses VALUES with CAST(NULL ...).
	if !strings.Contains(sql, "CAST(NULL AS") {
		t.Error("expected cold side stub with CAST(NULL AS...)")
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

func TestSQL_LimitArg_IsPlusOne(t *testing.T) {
	// The SQL query must request limit+1 rows so we can distinguish
	// "exactly limit results (last page)" from "limit results and more exist".
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	_, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// The last arg is the LIMIT value — it must be limit+1.
	limitArg := args[len(args)-1].(int)
	if limitArg != 11 {
		t.Errorf("SQL LIMIT arg: got %d, want %d (limit+1)", limitArg, 11)
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

func TestCountSQL(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.CountSQL()
	if err != nil {
		t.Fatalf("CountSQL error: %v", err)
	}

	if !strings.Contains(sql, "SELECT COUNT(*)") {
		t.Error("expected SELECT COUNT(*)")
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

func TestSQL_NoTimeRange_DefaultsTo30Days(t *testing.T) {
	// When from/to are omitted (zero time.Time), the query should
	// default to the last 30 days — not return all time.
	req := SpanQueryRequest{
		Limit: 50,
	}

	now := time.Now()
	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain time range filters (defaults to last 30 days).
	if !strings.Contains(sql, "start_time >= ?") {
		t.Error("expected start_time >= filter with default 30-day from")
	}
	if !strings.Contains(sql, "start_time <= ?") {
		t.Error("expected start_time <= filter with default now as to")
	}

	// Should have project_id + from + to + limit = 4 args.
	if len(args) < 4 {
		t.Errorf("expected at least 4 args (project_id + from + to + limit), got %d", len(args))
	}

	// Verify the default time range is approximately last 30 days.
	if len(args) >= 2 {
		from, ok := args[1].(time.Time)
		if !ok {
			t.Fatalf("arg[1] expected time.Time, got %T", args[1])
		}
		to := args[2].(time.Time)

		expectedFrom := now.Add(-defaultTimeRange)
		// Allow 2-second drift due to test execution time.
		if from.Sub(expectedFrom).Abs() > 2*time.Second {
			t.Errorf("default from: got %v, want ~%v", from, expectedFrom)
		}
		// to should be approximately now (within 2 seconds).
		if to.Sub(now).Abs() > 2*time.Second {
			t.Errorf("default to: got %v, want ~%v", to, now)
		}
	}

	if args[0] != "proj-abc" {
		t.Errorf("first arg should be project ID, got %v", args[0])
	}
}

func TestNewSpanQuery_FromAfterTo_ReturnsError(t *testing.T) {
	// When from is after to, return a 400 error.
	req := SpanQueryRequest{
		From:  time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit: 50,
	}

	_, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err == nil {
		t.Fatal("expected error when from > to")
	}
	if !strings.Contains(err.Error(), "from must not be after to") {
		t.Errorf("error message should mention 'from must not be after to', got: %v", err)
	}
}

func TestSQL_FromZeroToSet(t *testing.T) {
	// When only 'to' is set, apply only an upper bound.
	to := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	req := SpanQueryRequest{
		To:    to,
		Limit: 50,
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain upper bound only.
	if !strings.Contains(sql, "start_time <= ?") {
		t.Error("expected start_time <= filter")
	}
	if strings.Contains(sql, "start_time >= ?") {
		t.Error("expected no start_time >= filter when from is zero")
	}

	// Should have exactly 3 args: project_id + upper bound + limit.
	if len(args) != 3 {
		t.Errorf("expected 3 args (project_id + to + limit), got %d: %v", len(args), args)
	}
	if len(args) >= 2 && !args[1].(time.Time).Equal(to) {
		t.Errorf("second arg should be 'to' time, got %v", args[1])
	}
}

func TestSQL_FromSetToZero(t *testing.T) {
	// When only 'from' is set, apply only a lower bound.
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	req := SpanQueryRequest{
		From:  from,
		Limit: 50,
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain lower bound only.
	if !strings.Contains(sql, "start_time >= ?") {
		t.Error("expected start_time >= filter")
	}
	if strings.Contains(sql, "start_time <= ?") {
		t.Error("expected no start_time <= filter when to is zero")
	}

	// Should have exactly 3 args: project_id + lower bound + limit.
	if len(args) != 3 {
		t.Errorf("expected 3 args (project_id + from + limit), got %d: %v", len(args), args)
	}
	if len(args) >= 2 && !args[1].(time.Time).Equal(from) {
		t.Errorf("second arg should be 'from' time, got %v", args[1])
	}
}

func TestSQL_FilterDurationMs(t *testing.T) {
	// duration_ms is a virtual field — it should compile to
	// (EXTRACT(EPOCH FROM (end_time - start_time)) * 1000) with the operator.
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 50,
		Filters: []SpanQueryFilter{
			{Field: "duration_ms", Op: "gte", Value: 1000},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain the duration_ms expression.
	if !strings.Contains(sql, "duration_ms") && !strings.Contains(sql, "EPOCH") && !strings.Contains(sql, "end_time") {
		t.Errorf("expected duration_ms expression in SQL, got:\n%s", sql)
	}
}

func TestSQL_FilterDurationMsLt(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 50,
		Filters: []SpanQueryFilter{
			{Field: "duration_ms", Op: "lt", Value: 500},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain duration_ms < expression.
	if !strings.Contains(sql, "< ?") {
		t.Errorf("expected '< ?' in SQL for duration_ms lt filter, got:\n%s", sql)
	}
}

func TestSQL_FilterCostUsdRange(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 50,
		Filters: []SpanQueryFilter{
			{Field: "cost_usd", Op: "gte", Value: 0.01},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain cost_usd >= ? filter.
	if !strings.Contains(sql, "cost_usd >= ?") {
		t.Errorf("expected 'cost_usd >= ?' in SQL, got:\n%s", sql)
	}
}

func TestSQL_FilterInputTokensRange(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 50,
		Filters: []SpanQueryFilter{
			{Field: "input_tokens", Op: "gte", Value: int64(100)},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	// Should contain input_tokens >= ? filter.
	if !strings.Contains(sql, "input_tokens >= ?") {
		t.Errorf("expected 'input_tokens >= ?' in SQL, got:\n%s", sql)
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

	_, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery should accept contains op: %v", err)
	}
}

func TestSQL_FilterContainsName(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "name", Op: "contains", Value: "error"},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "name LIKE ?") {
		t.Errorf("expected 'name LIKE ?' in SQL, got:\n%s", sql)
	}

	// Value should be wildcarded.
	if len(args) > 0 {
		expectedVal := "%error%"
		if args[len(args)-2] != expectedVal {
			t.Errorf("contains value should be %q, got %q", expectedVal, args[len(args)-2])
		}
	}
}

func TestSQL_FilterContainsOnModel(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "model", Op: "contains", Value: "gpt"},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "model LIKE ?") {
		t.Errorf("expected 'model LIKE ?' in SQL, got:\n%s", sql)
	}
}

func TestSQL_FilterContainsOnInput(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "input", Op: "contains", Value: "user prompt"},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "input LIKE ?") {
		t.Errorf("expected 'input LIKE ?' in SQL, got:\n%s", sql)
	}
}

func TestSQL_FilterContainsOnOutput(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "output", Op: "contains", Value: "response"},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "output LIKE ?") {
		t.Errorf("expected 'output LIKE ?' in SQL, got:\n%s", sql)
	}
}

func TestSQL_FilterContainsEmptyString(t *testing.T) {
	// Empty string should still work — matches everything with LIKE '%%'
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "name", Op: "contains", Value: ""},
		},
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "name LIKE ?") {
		t.Errorf("expected 'name LIKE ?' in SQL, got:\n%s", sql)
	}
}

// ── Cold-side filtering (issue #66) ─────────────────────────────────────────
//
// The cold (Parquet-on-S3) branch of the UNION must carry the same WHERE
// clause as the hot branch. Without it, GET /api/v1/traces/{traceId} pulled
// every archived span of the project and built its tree from unrelated spans
// — rendering the same trace regardless of which id was requested.

type fakeObjectStore struct{}

func (fakeObjectStore) Put(_ context.Context, _ string, _ io.Reader) error { return nil }
func (fakeObjectStore) PutSized(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return nil
}
func (fakeObjectStore) Get(_ context.Context, _ string) (io.ReadCloser, error) { return nil, nil }
func (fakeObjectStore) Delete(_ context.Context, _ string) error               { return nil }
func (fakeObjectStore) ListPrefix(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (fakeObjectStore) Stat(_ context.Context, _ string) (*storage.ObjectStat, error) {
	return nil, nil
}

func TestSQL_ColdSideCarriesWhereClause_WhenS3Configured(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "trace_id", Op: "eq", Value: "b81fa355ec7328cdb0addd47ef044f9d"},
		},
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, fakeObjectStore{}, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, "read_parquet") {
		t.Fatalf("expected cold read_parquet branch in SQL, got:\n%s", sql)
	}
	// Both UNION branches must filter on trace_id.
	if got := strings.Count(sql, "trace_id = ?"); got != 2 {
		t.Errorf("expected trace_id filter on hot AND cold sides (2 occurrences), got %d:\n%s", got, sql)
	}
	if got := strings.Count(sql, "project_id = ?"); got != 2 {
		t.Errorf("expected project_id predicate on hot AND cold sides (2 occurrences), got %d:\n%s", got, sql)
	}
	// Args: hot (project_id, from, to, trace_id) + cold (same 4) + limit.
	if len(args) != 9 {
		t.Errorf("expected 9 args (4 hot + 4 cold + limit), got %d: %v", len(args), args)
	}
	// The trace_id value must appear twice (hot + cold).
	traceArgs := 0
	for _, a := range args {
		if a == "b81fa355ec7328cdb0addd47ef044f9d" {
			traceArgs++
		}
	}
	if traceArgs != 2 {
		t.Errorf("expected trace_id arg twice (hot + cold), got %d", traceArgs)
	}
}

func TestSQL_ColdSideNoWhere_WhenS3Absent(t *testing.T) {
	// Without S3 the cold branch is a zero-row VALUES stub — no extra args.
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "trace_id", Op: "eq", Value: "abc"},
		},
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if strings.Contains(sql, "read_parquet") {
		t.Error("did not expect read_parquet without S3")
	}
	// Args: hot (project_id, from, to, trace_id) + limit.
	if len(args) != 5 {
		t.Errorf("expected 5 args (4 hot + limit), got %d: %v", len(args), args)
	}
}

func TestSQL_ColdSideBookmarkedFilterResolves_WhenS3Configured(t *testing.T) {
	// The special bookmarked filter references spans.trace_id; the cold
	// wrapper is aliased "spans" so that correlated subquery still resolves.
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "bookmarked", Op: "eq", Value: true},
		},
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, fakeObjectStore{}, "/tmp/test.duckdb")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}
	q.SetBookmarkedTraceIDs([]string{"trace-starred"})

	sql, args, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL error: %v", err)
	}

	if !strings.Contains(sql, ") AS spans") {
		t.Errorf("expected cold wrapper aliased AS spans, got:\n%s", sql)
	}
	if got := strings.Count(sql, "spans.trace_id IN (?)"); got != 2 {
		t.Errorf("expected bookmarked IN-list on both sides (2 occurrences), got %d\n%s", got, sql)
	}
	// The starred trace ID must appear as an arg once per side.
	var idArgs int
	for _, a := range args {
		if a == "trace-starred" {
			idArgs++
		}
	}
	if idArgs != 2 {
		t.Errorf("expected starred trace ID arg on both sides, got %d occurrences in %v", idArgs, args)
	}
}

// ─── Issue #85: Lake single-table reads ───

func TestLakeSQL_FirstPage(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req, nil, "")
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
	if got := strings.Count(sql, "WHERE"); got != 1 {
		t.Errorf("expected exactly one WHERE clause, got %d:\n%s", got, sql)
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

	q, err := NewSpanQuery("proj-abc", req, nil, "")
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args, err := q.LakeSQL()
	if err != nil {
		t.Fatalf("LakeSQL error: %v", err)
	}

	// The cursor predicate must join the filter WHERE clause with AND — a
	// second WHERE is a syntax error on the single-table read.
	if got := strings.Count(sql, "WHERE"); got != 1 {
		t.Errorf("expected exactly one WHERE clause, got %d:\n%s", got, sql)
	}
	if !strings.Contains(sql, "AND (start_time < ? OR (start_time = ? AND span_id < ?))") {
		t.Errorf("expected keyset cursor predicate ANDed into WHERE, got:\n%s", sql)
	}
	// args: project_id, from, to, cursor start_time x2, cursor span_id, limit+1.
	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %v", len(args), args)
	}
}

func TestLakeTraceSpansSQL_Dedupes(t *testing.T) {
	q, err := NewSpanQuery("proj-abc", SpanQueryRequest{}, nil, "")
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
	dir := t.TempDir()

	lk, err := lake.Open(ctx, lake.Config{
		CatalogDriver: lake.CatalogDriverLocal,
		CatalogDSN:    dir + "/catalog/lake.ducklake",
		DataPath:      dir + "/data",
	})
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
		q, err := NewSpanQuery("proj-page", req, nil, "")
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
