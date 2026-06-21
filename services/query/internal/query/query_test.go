package query

import (
	"context"
	"database/sql"
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

// TestBuildWhereClause_StatusCodeIn_OK verifies that filtering by
// status_code = "OK" compiles to a plain IN clause on the status_code
// column — no special-casing needed when "UNSET" is not among the values.
func TestBuildWhereClause_StatusCodeIn_OK(t *testing.T) {
	q, err := NewSpanQuery("proj-1", SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "status_code", Op: "in", Value: []any{"OK"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	args, where, err := q.buildWhereClause()
	if err != nil {
		t.Fatalf("buildWhereClause error: %v", err)
	}

	if !strings.Contains(where, "status_code IN (?)") {
		t.Errorf("expected where clause to contain 'status_code IN (?)', got: %s", where)
	}
	if strings.Contains(where, "IS NULL") {
		t.Errorf("did not expect NULL handling for OK-only filter, got: %s", where)
	}

	found := false
	for _, a := range args {
		if a == "OK" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected args to contain %q, got: %v", "OK", args)
	}
}

// TestBuildWhereClause_StatusCodeIn_UNSET verifies that filtering by
// status_code = "UNSET" also matches rows where status_code is NULL or the
// empty string. Pre-#135 data never populates status_code, so it is stored
// as NULL/'' rather than the literal string "UNSET"; the filter must treat
// those as "Unset" too, or selecting "Unset" would show zero traces for all
// existing data.
func TestBuildWhereClause_StatusCodeIn_UNSET(t *testing.T) {
	q, err := NewSpanQuery("proj-1", SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "status_code", Op: "in", Value: []any{"UNSET"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	args, where, err := q.buildWhereClause()
	if err != nil {
		t.Fatalf("buildWhereClause error: %v", err)
	}

	if !strings.Contains(where, "status_code IN (?)") {
		t.Errorf("expected where clause to still match literal 'UNSET' rows, got: %s", where)
	}
	if !strings.Contains(where, "status_code IS NULL") {
		t.Errorf("expected where clause to also match NULL status_code, got: %s", where)
	}
	if !strings.Contains(where, "status_code = ''") {
		t.Errorf("expected where clause to also match empty-string status_code, got: %s", where)
	}

	found := false
	for _, a := range args {
		if a == "UNSET" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected args to contain %q, got: %v", "UNSET", args)
	}
}

// TestBuildWhereClause_StatusCodeIn_OKAndUnset verifies that combining "OK"
// with "UNSET" ORs the NULL/empty handling together with the literal value
// match, all within a single parenthesized predicate so it ANDs correctly
// with other filters (e.g. project_id, time range).
func TestBuildWhereClause_StatusCodeIn_OKAndUnset(t *testing.T) {
	q, err := NewSpanQuery("proj-1", SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "status_code", Op: "in", Value: []any{"OK", "UNSET"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	args, where, err := q.buildWhereClause()
	if err != nil {
		t.Fatalf("buildWhereClause error: %v", err)
	}

	if !strings.Contains(where, "status_code IN (?, ?)") {
		t.Errorf("expected where clause to contain 'status_code IN (?, ?)', got: %s", where)
	}
	if !strings.Contains(where, "status_code IS NULL") {
		t.Errorf("expected where clause to also match NULL status_code, got: %s", where)
	}
	if !strings.Contains(where, "status_code = ''") {
		t.Errorf("expected where clause to also match empty-string status_code, got: %s", where)
	}

	// The combined predicate must be wrapped in parentheses so it ANDs
	// correctly with the surrounding project_id / time-range clauses.
	if !strings.Contains(where, "(status_code IN (?, ?) OR status_code IS NULL OR status_code = '')") {
		t.Errorf("expected status_code predicate to be a single OR-wrapped group, got: %s", where)
	}

	wantArgs := map[string]bool{"OK": false, "UNSET": false}
	for _, a := range args {
		if s, ok := a.(string); ok {
			if _, exists := wantArgs[s]; exists {
				wantArgs[s] = true
			}
		}
	}
	for v, found := range wantArgs {
		if !found {
			t.Errorf("expected args to contain %q, got: %v", v, args)
		}
	}
}

// TestBuildWhereClause_StatusCodeIn_ERROR verifies the ERROR-only filter
// compiles to a plain IN clause without NULL/empty-string handling.
func TestBuildWhereClause_StatusCodeIn_ERROR(t *testing.T) {
	q, err := NewSpanQuery("proj-1", SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "status_code", Op: "in", Value: []any{"ERROR"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	args, where, err := q.buildWhereClause()
	if err != nil {
		t.Fatalf("buildWhereClause error: %v", err)
	}

	if !strings.Contains(where, "status_code IN (?)") {
		t.Errorf("expected where clause to contain 'status_code IN (?)', got: %s", where)
	}
	if strings.Contains(where, "IS NULL") {
		t.Errorf("did not expect NULL handling for ERROR-only filter, got: %s", where)
	}

	found := false
	for _, a := range args {
		if a == "ERROR" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected args to contain %q, got: %v", "ERROR", args)
	}
}

// TestBuildWhereClause_StatusCodeIn_OKAndError verifies that combining "OK"
// and "ERROR" (no "UNSET") compiles to a plain IN clause without NULL/empty
// handling — existing behavior should be unchanged for this common case.
func TestBuildWhereClause_StatusCodeIn_OKAndError(t *testing.T) {
	q, err := NewSpanQuery("proj-1", SpanQueryRequest{
		From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: []SpanQueryFilter{
			{Field: "status_code", Op: "in", Value: []any{"OK", "ERROR"}},
		},
	})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	args, where, err := q.buildWhereClause()
	if err != nil {
		t.Fatalf("buildWhereClause error: %v", err)
	}

	if !strings.Contains(where, "status_code IN (?, ?)") {
		t.Errorf("expected where clause to contain 'status_code IN (?, ?)', got: %s", where)
	}
	if strings.Contains(where, "IS NULL") {
		t.Errorf("did not expect NULL handling for OK+ERROR filter, got: %s", where)
	}

	wantArgs := map[string]bool{"OK": false, "ERROR": false}
	for _, a := range args {
		if s, ok := a.(string); ok {
			if _, exists := wantArgs[s]; exists {
				wantArgs[s] = true
			}
		}
	}
	for v, found := range wantArgs {
		if !found {
			t.Errorf("expected args to contain %q, got: %v", v, args)
		}
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

func TestPhaseOneSQL_FirstPage(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args := q.PhaseOneSQL()

	// Phase 1 must never read the wide input/output LLM payload columns —
	// it's the cheap scan that resolves which trace_ids the page contains
	// (issue #229).
	if strings.Contains(sql, "input") || strings.Contains(sql, "output") {
		t.Errorf("phase1 query must not read input/output columns, got:\n%s", sql)
	}
	if !strings.Contains(sql, "SELECT trace_id, span_id, parent_id, start_time, project_id FROM lake.spans") {
		t.Errorf("expected narrow 5-column scan, got:\n%s", sql)
	}
	if !strings.Contains(sql, "ROW_NUMBER() OVER") {
		t.Errorf("expected window function for root-span selection, got:\n%s", sql)
	}
	// args: project_id, from, to, limit+1 (no cursor).
	if len(args) != 4 {
		t.Fatalf("expected 4 args (project_id, from, to, limit+1), got %d: %v", len(args), args)
	}
	if args[len(args)-1] != 11 {
		t.Errorf("last arg should be limit+1 (11), got %v", args[len(args)-1])
	}
}

func TestPhaseOneSQL_WithCursor(t *testing.T) {
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

	sql, args := q.PhaseOneSQL()

	if !strings.Contains(sql, "AND (start_time < ? OR (start_time = ? AND span_id < ?))") {
		t.Errorf("expected keyset cursor predicate, got:\n%s", sql)
	}
	// args: project_id, from, to, cursor x3, limit+1.
	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %v", len(args), args)
	}
	if args[len(args)-1] != 11 {
		t.Errorf("last arg should be limit+1 (11), got %v", args[len(args)-1])
	}
}

func TestMainSQL_FirstPage(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	traceIDs := []string{"trace-1", "trace-2", "trace-3"}
	sql, args, err := q.MainSQL(traceIDs)
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	if !strings.Contains(sql, "FROM lake.spans") {
		t.Errorf("expected single-table read from lake.spans, got:\n%s", sql)
	}
	if strings.Contains(sql, "UNION") {
		t.Errorf("Lake reads must not UNION hot+cold, got:\n%s", sql)
	}
	if !strings.Contains(sql, "GROUP BY trace_id") {
		t.Errorf("expected trace-level rollup aggregation, got:\n%s", sql)
	}
	if !strings.Contains(sql, "ROW_NUMBER() OVER") {
		t.Errorf("expected window function for root-span selection, got:\n%s", sql)
	}
	if !strings.Contains(sql, "FROM (\n  SELECT *,") {
		t.Errorf("expected ranked inline subquery, got:\n%s", sql)
	}
	if !strings.Contains(sql, ") r\n") {
		t.Errorf("expected ranked alias 'r', got:\n%s", sql)
	}
	if !strings.Contains(sql, ") ru ON r.trace_id = ru.trace_id\n") {
		t.Errorf("expected rollups alias 'ru' joined on trace_id, got:\n%s", sql)
	}
	if !strings.Contains(sql, ") kr ON r.trace_id = kr.trace_id\n") {
		t.Errorf("expected kind_rollups alias 'kr' joined on trace_id, got:\n%s", sql)
	}
	if !strings.Contains(sql, "WHERE r.rn = 1") {
		t.Errorf("expected root-span selection WHERE r.rn = 1, got:\n%s", sql)
	}
	// MainSQL no longer inlines the phase1 subquery — traceIDs is filtered
	// as a literal `trace_id IN (?, ?, ...)` list, scoped 3 times (ranked,
	// rollup, kind_rollup) — see PhaseOneSQL's doc for why a literal list
	// (rather than a subquery) is the actual fix for issue #229.
	if got := strings.Count(sql, "trace_id IN"); got != 3 {
		t.Errorf("expected 3 literal trace_id IN clauses (ranked, rollup, kind_rollup), got %d:\n%s", got, sql)
	}
	// Each of the 3 sites contributes whereArgs (project_id, from, to = 3)
	// + len(traceIDs) (3) = 6 args; 3 sites × 6 = 18.
	if len(args) != 18 {
		t.Fatalf("expected 18 args, got %d: %v", len(args), args)
	}
}

// TestMainSQL_NoCursorNeeded verifies MainSQL's output is identical whether
// or not the originating SpanQuery had a cursor set — cursor filtering is
// fully resolved by PhaseOneSQL, so MainSQL only needs the already-resolved
// trace_ids.
func TestMainSQL_NoCursorNeeded(t *testing.T) {
	base := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}
	withCursor := base
	withCursor.Cursor = "eyJzdGFydF90aW1lIjoiMjAyNS0wMS0wMVQxMDowMDowMFoiLCJzcGFuX2lkIjoic3BhbjAxIn0"

	traceIDs := []string{"trace-1", "trace-2"}

	q1, err := NewSpanQuery("proj-abc", base)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}
	sql1, args1, err := q1.MainSQL(traceIDs)
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	q2, err := NewSpanQuery("proj-abc", withCursor)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}
	sql2, args2, err := q2.MainSQL(traceIDs)
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	if sql1 != sql2 {
		t.Errorf("MainSQL output must not depend on the cursor:\nwithout cursor:\n%s\nwith cursor:\n%s", sql1, sql2)
	}
	if len(args1) != len(args2) {
		t.Errorf("MainSQL args must not depend on the cursor: got %d vs %d", len(args1), len(args2))
	}
}

// TestMainSQL_NoCTEs verifies that the traces-list main query uses inline
// subqueries (derived tables) rather than CTEs — so that DuckDB does NOT
// materialize the full filtered span set before applying the LIMIT
// (issue #154). The query must not contain the "WITH" keyword.
func TestMainSQL_NoCTEs(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.MainSQL([]string{"trace-1"})
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	if strings.Contains(strings.ToUpper(sql), "WITH ") {
		t.Errorf("query must not use CTEs (no WITH clause); got:\n%s", sql)
	}
	// Verify the query starts with SELECT (no CTE preamble).
	if !strings.HasPrefix(strings.TrimSpace(sql), "SELECT") {
		t.Errorf("query should start directly with SELECT, got:\n%s", sql)
	}
}

// TestMainSQL_NoFilteredCTE verifies that the old "filtered" CTE has been
// inlined — there is no standalone CTE named filtered in the generated SQL.
func TestMainSQL_NoFilteredCTE(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.MainSQL([]string{"trace-1"})
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	// The old query had: "WITH filtered AS (SELECT * FROM lake.spans WHERE ...)"
	// The new query should have: "SELECT * FROM lake.spans" inside inline
	// subqueries, not as a named CTE.
	if strings.Contains(sql, "WITH filtered") ||
		strings.Contains(sql, "filtered AS") {
		t.Errorf("query must not have a named 'filtered' CTE; got:\n%s", sql)
	}
}

// TestMainSQL_NoRankedCTE verifies that the "ranked" CTE has been replaced
// with an inline subquery alias.
func TestMainSQL_NoRankedCTE(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.MainSQL([]string{"trace-1"})
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	if strings.Contains(sql, "ranked AS") {
		t.Errorf("query must not have a named 'ranked' CTE; got:\n%s", sql)
	}
	if !strings.Contains(sql, "AS rn\n  FROM (\n") {
		t.Errorf("expected rn window alias in inline subquery; got:\n%s", sql)
	}
}

// TestMainSQL_NoRollupCTEs verifies that "rollups" and "kind_rollups" are
// inline subqueries, not CTEs.
func TestMainSQL_NoRollupCTEs(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.MainSQL([]string{"trace-1"})
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	if strings.Contains(sql, "rollups AS") {
		t.Errorf("query must not have a named 'rollups' CTE; got:\n%s", sql)
	}
	if strings.Contains(sql, "kind_rollups AS") {
		t.Errorf("query must not have a named 'kind_rollups' CTE; got:\n%s", sql)
	}
	// Verify they appear as JOIN subqueries with proper aliases.
	if !strings.Contains(sql, "JOIN (") {
		t.Errorf("expected rollup and kind_rollup JOIN subqueries; got:\n%s", sql)
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
	// args: traceID, projectID, from, to, limit.
	if len(args) != 5 || args[0] != "trace-1" || args[1] != "proj-abc" {
		t.Errorf("args: got %v, want [trace-1 proj-abc <from> <to> <limit>]", args)
	}
}

func TestLakeTraceSpansSQL_HasLimitClause(t *testing.T) {
	q, err := NewSpanQuery("proj-abc", SpanQueryRequest{Limit: 100})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args := q.LakeTraceSpansSQL("trace-1")
	if !strings.Contains(sql, "LIMIT ?") {
		t.Errorf("trace detail SQL must include LIMIT clause, got:\n%s", sql)
	}
	// args: traceID, projectID, from, to, limit.
	if len(args) != 5 || args[4] != 100 {
		t.Errorf("limit argument: got %v, want [trace-1 proj-abc <from> <to> 100]", args)
	}
}

func TestLakeTraceSpansSQL_FallbackToHardCapWhenZero(t *testing.T) {
	// Construct the query directly (bypassing NewSpanQuery's DefaultLimit
	// assignment) to verify LakeTraceSpansSQL falls back to MaxTraceSpansLimit
	// when q.limit is 0.
	q := &SpanQuery{
		projectID: "proj-abc",
		limit:     0,
	}

	_, args := q.LakeTraceSpansSQL("trace-1")
	// When q.limit == 0, effective limit falls back to MaxTraceSpansLimit (10000)
	// so trace detail queries don't load unbounded spans into memory.
	if len(args) != 5 || args[4] != MaxTraceSpansLimit {
		t.Errorf("limit argument with zero limit: got %v, want [trace-1 proj-abc <from> <to> %d]", args, MaxTraceSpansLimit)
	}
}

func TestLakeTraceSpansSQL_CappedAtHardLimit(t *testing.T) {
	// Construct the query directly (bypassing NewSpanQuery's MaxLimit validation)
	// to verify LakeTraceSpansSQL caps at MaxTraceSpansLimit regardless.
	q := &SpanQuery{
		projectID: "proj-abc",
		limit:     50000, // exceeds MaxTraceSpansLimit
	}

	_, args := q.LakeTraceSpansSQL("trace-1")
	if len(args) != 5 || args[4] != 10000 {
		t.Errorf("limit argument capped at 10000: got %v", args)
	}
}

// TestLakeTraceSpansSQL_HasTimeBounds verifies that when a SpanQuery
// carries from/to bounds, LakeTraceSpansSQL includes them in the generated
// SQL so that DuckDB can prune partitions during execution (issue #153).
func TestLakeTraceSpansSQL_HasTimeBounds(t *testing.T) {
	now := time.Now()
	req := SpanQueryRequest{
		From: now.Add(-24 * time.Hour),
		To:   now,
		Limit: 100,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, args := q.LakeTraceSpansSQL("trace-1")

	if !strings.Contains(sql, "start_time >= ?") {
		t.Errorf("trace detail SQL must include start_time >= bound, got:\n%s", sql)
	}
	if !strings.Contains(sql, "start_time <= ?") {
		t.Errorf("trace detail SQL must include start_time <= bound, got:\n%s", sql)
	}
	// args: traceID, projectID, from, to, limit.
	if len(args) != 5 {
		t.Errorf("args: got %d args, want 5 (traceID, projectID, from, to, limit)", len(args))
	}
	if len(args) >= 5 && !args[2].(time.Time).Equal(q.from) {
		t.Errorf("from arg: got %v, want %v", args[2], q.from)
	}
	if len(args) >= 5 && !args[3].(time.Time).Equal(q.to) {
		t.Errorf("to arg: got %v, want %v", args[3], q.to)
	}
}

// TestLakeTraceSpansSQL_TimeBoundsWithDedup checks that time bounds appear
// before the ORDER BY and LIMIT — i.e. they are part of the WHERE clause
// inside the subquery, not after dedup.
func TestLakeTraceSpansSQL_TimeBoundsBeforeDedup(t *testing.T) {
	now := time.Now()
	q, err := NewSpanQuery("proj-abc", SpanQueryRequest{
		From: now.Add(-1 * time.Hour),
		To:   now,
	})
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _ := q.LakeTraceSpansSQL("trace-abc")

	// The WHERE clause must be inside the subquery (before the outer
	// "AS deduped WHERE rn = 1"), not after it.  Check that "start_time >="
	// appears before "AS deduped".
	beforeDedup := strings.Index(sql, "start_time >=")
	dedupIdx := strings.Index(sql, "AS deduped")
	if beforeDedup < 0 {
		t.Fatal("expected start_time >= bound in SQL")
	}
	if dedupIdx < 0 {
		t.Fatal("expected AS deduped in SQL")
	}
	if beforeDedup > dedupIdx {
		t.Errorf("start_time bound appears after dedup alias; must be in the inner WHERE, got:\n%s", sql)
	}
}

// execLakeSQL runs the two-phase Lake query (PhaseOneSQL then MainSQL)
// against db and returns the scanned spans. This mirrors what
// querybuild.QueryBuilder.ExecuteSpan does in production — duplicated here
// rather than imported because querybuild imports this package.
func execLakeSQL(ctx context.Context, db *sql.DB, q *SpanQuery) ([]*domain.Span, error) {
	phase1SQL, phase1Args := q.PhaseOneSQL()
	phase1Rows, err := db.QueryContext(ctx, phase1SQL, phase1Args...)
	if err != nil {
		return nil, fmt.Errorf("phase1 query: %w (sql=%s)", err, phase1SQL)
	}
	var traceIDs []string
	for phase1Rows.Next() {
		var id string
		if err := phase1Rows.Scan(&id); err != nil {
			phase1Rows.Close()
			return nil, fmt.Errorf("phase1 scan: %w", err)
		}
		traceIDs = append(traceIDs, id)
	}
	if err := phase1Rows.Err(); err != nil {
		phase1Rows.Close()
		return nil, fmt.Errorf("phase1 rows iteration: %w", err)
	}
	phase1Rows.Close()

	if len(traceIDs) == 0 {
		return nil, nil
	}

	sqlStr, args, err := q.MainSQL(traceIDs)
	if err != nil {
		return nil, fmt.Errorf("MainSQL: %w", err)
	}
	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("main query: %w (sql=%s)", err, sqlStr)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}
	var raw [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		raw = append(raw, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return ScanRows(raw)
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
		pageSpans, err := execLakeSQL(ctx, lk.DB(), q)
		if err != nil {
			t.Fatalf("page %d query: %v", page, err)
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

	traces, err := execLakeSQL(ctx, lk.DB(), q)
	if err != nil {
		t.Fatalf("execLakeSQL: %v", err)
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

// TestMainSQL_RollupScopedToLimit verifies that MainSQL scopes its rollup
// and kind_rollup subqueries to the resolved set of trace_ids rather than
// scanning the full time range. This prevents OOM on wide time-range queries
// (issue #188).
func TestMainSQL_RollupScopedToLimit(t *testing.T) {
	req := SpanQueryRequest{
		From:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}

	q, err := NewSpanQuery("proj-abc", req)
	if err != nil {
		t.Fatalf("NewSpanQuery error: %v", err)
	}

	sql, _, err := q.MainSQL([]string{"trace-1", "trace-2"})
	if err != nil {
		t.Fatalf("MainSQL error: %v", err)
	}

	// The SQL must contain `trace_id IN` for the ranked/rollup/kind_rollup
	// subqueries, scoping them to the resolved trace_ids rather than the
	// full time range (issue #188, and issue #229 for why it's a literal
	// list rather than an inlined phase1 subquery).
	if got := strings.Count(sql, "trace_id IN"); got != 3 {
		t.Errorf("MainSQL subqueries must scope to resolved trace_ids via `trace_id IN (...)`;\nthis prevents OOM on wide time ranges (issue #188).\ngot %d occurrences:\n%s", got, sql)
	}
}

// TestLakeSQL_RollupExecution verifies the two-phase query pattern works
// end-to-end: the rollup subqueries are correctly scoped and return correct
// results.
func TestLakeSQL_RollupExecution(t *testing.T) {
	ctx := context.Background()

	cfg, _ := lakeservertest.NewLocal(t)
	lk, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	defer lk.Close()

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Create 3 traces in the same project, each with a root span and children.
	traces := []struct {
		traceID     string
		spanID      string
		childIDs    []string
		spanCount   int64
		inputTokens int64
	}{
		{traceID: "trace-A", spanID: "a-root", childIDs: []string{"a-c1", "a-c2", "a-c3"}, spanCount: 4, inputTokens: 400},
		{traceID: "trace-B", spanID: "b-root", childIDs: []string{"b-c1"}, spanCount: 2, inputTokens: 200},
		{traceID: "trace-C", spanID: "c-root", childIDs: []string{"c-c1", "c-c2"}, spanCount: 3, inputTokens: 300},
	}

	var spans []*domain.Span
	for _, tr := range traces {
		// Root span
		spans = append(spans, &domain.Span{
			SpanID:      tr.spanID,
			TraceID:     tr.traceID,
			ParentID:    "",
			ProjectID:   "proj-limit",
			Name:        "agent.run",
			Kind:        domain.SpanKindAgent,
			StartTime:   base,
			EndTime:     base.Add(time.Duration(tr.spanCount) * time.Second),
			InputTokens: tr.inputTokens,
			StatusCode:  "ok",
		})
		// Child spans
		for _, cid := range tr.childIDs {
			spans = append(spans, &domain.Span{
				SpanID:      cid,
				TraceID:     tr.traceID,
				ParentID:    tr.spanID,
				ProjectID:   "proj-limit",
				Name:        "tool.call",
				Kind:        domain.SpanKindTool,
				StartTime:   base.Add(time.Second),
				EndTime:     base.Add(time.Duration(tr.spanCount) * time.Second),
				InputTokens: 10,
				StatusCode:  "ok",
			})
		}
	}

	if err := lk.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	req := SpanQueryRequest{
		From:  base.Add(-1 * time.Hour),
		To:    base.Add(1 * time.Hour),
		Limit: 2, // Only fetch 2 traces
	}
	q, err := NewSpanQuery("proj-limit", req)
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}

	result, err := execLakeSQL(ctx, lk.DB(), q)
	if err != nil {
		t.Fatalf("execLakeSQL: %v", err)
	}

	// We requested limit=2, but the query uses limit+1 for NextCursor detection,
	// so we may get up to 3 rows. The handler trims to limit.
	if len(result) > 3 {
		t.Fatalf("expected at most 3 traces (limit+1 for cursor detection), got %d", len(result))
	}

	// Trim to effective limit (simulates handler behavior).
	if len(result) > q.EffectiveLimit() {
		result = result[:q.EffectiveLimit()]
	}

	// Each trace's span_count must reflect its true span count, not contaminated by other traces.
	spanCountMap := make(map[string]int64)
	for _, s := range result {
		spanCountMap[s.TraceID] = s.SpanCount
	}

	expected := map[string]int64{
		"trace-C": 3,
		"trace-B": 2,
		"trace-A": 4,
	}
	for traceID, wantCount := range expected {
		// Only check traces that are in the result (we only get 2).
		if gotCount, ok := spanCountMap[traceID]; ok {
			if gotCount != wantCount {
				t.Errorf("trace %q span_count: got %d, want %d", traceID, gotCount, wantCount)
			}
		}
	}
}
