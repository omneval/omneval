package query

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/services/query/internal/cursor"
)

func TestNewSpanQuery_Valid(t *testing.T) {
	req := SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
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
		From:    time.Now(),
		To:      time.Now().Add(time.Hour),
		Cursor:  "not-valid-base64!!!",
		Limit:   10,
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
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
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

func TestCompileFilter_UnknownField(t *testing.T) {
	f := SpanQueryFilter{Field: "nonexistent", Op: "eq", Value: "x"}
	_, _, err := compileFilter(f)
	if err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestCompileFilter_UnknownOp(t *testing.T) {
	f := SpanQueryFilter{Field: "model", Op: "regex", Value: ".*"}
	_, _, err := compileFilter(f)
	if err == nil {
		t.Error("expected error for unknown operator")
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

