package query

import (
	"strings"
	"testing"
)

func TestCompileSpecialFilter_Bookmarked(t *testing.T) {
	cases := []struct {
		name      string
		bookmarks []string
		filter    SpanQueryFilter
		wantSQL   string
		wantArgs  []any
	}{
		{
			name:      "bookmarked true with starred traces",
			bookmarks: []string{"t1", "t2"},
			filter:    SpanQueryFilter{Field: "bookmarked", Op: "eq", Value: true},
			wantSQL:   "spans.trace_id IN (?, ?)",
			wantArgs:  []any{"t1", "t2"},
		},
		{
			name:      "bookmarked false with starred traces",
			bookmarks: []string{"t1"},
			filter:    SpanQueryFilter{Field: "bookmarked", Op: "eq", Value: false},
			wantSQL:   "spans.trace_id NOT IN (?)",
			wantArgs:  []any{"t1"},
		},
		{
			name:      "bookmarked true with no starred traces matches nothing",
			bookmarks: nil,
			filter:    SpanQueryFilter{Field: "bookmarked", Op: "eq", Value: true},
			wantSQL:   "FALSE",
			wantArgs:  nil,
		},
		{
			name:      "bookmarked false with no starred traces matches everything",
			bookmarks: nil,
			filter:    SpanQueryFilter{Field: "bookmarked", Op: "eq", Value: false},
			wantSQL:   "TRUE",
			wantArgs:  nil,
		},
		{
			name:      "unsupported operator",
			bookmarks: []string{"t1"},
			filter:    SpanQueryFilter{Field: "bookmarked", Op: "neq", Value: true},
			wantSQL:   "",
			wantArgs:  nil,
		},
		{
			name:      "non-bool value",
			bookmarks: []string{"t1"},
			filter:    SpanQueryFilter{Field: "bookmarked", Op: "eq", Value: "yes"},
			wantSQL:   "",
			wantArgs:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &SpanQuery{projectID: "test-project"}
			q.SetBookmarkedTraceIDs(tc.bookmarks)

			gotSQL, gotArgs := q.compileSpecialFilter(tc.filter)
			if gotSQL != tc.wantSQL {
				t.Errorf("SQL: got %q, want %q", gotSQL, tc.wantSQL)
			}
			if len(gotArgs) != len(tc.wantArgs) {
				t.Errorf("args: got %v, want %v", gotArgs, tc.wantArgs)
			} else {
				for i := range gotArgs {
					if gotArgs[i] != tc.wantArgs[i] {
						t.Errorf("arg[%d]: got %v, want %v", i, gotArgs[i], tc.wantArgs[i])
					}
				}
			}
		})
	}
}

func TestValidateFilter_AcceptsBookmarked(t *testing.T) {
	filter := SpanQueryFilter{Field: "bookmarked", Op: "eq", Value: true}
	if err := validateFilter(filter); err != nil {
		t.Errorf("validateFilter(bookmarked): unexpected error: %v", err)
	}
}

func TestNewSpanQuery_BookmarkedFilter(t *testing.T) {
	req := SpanQueryRequest{
		Filters: []SpanQueryFilter{
			{Field: "bookmarked", Op: "eq", Value: true},
		},
	}

	q, err := NewSpanQuery("test-project", req)
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}
	if q == nil {
		t.Fatal("expected non-nil query")
	}
	if !q.NeedsBookmarks() {
		t.Error("NeedsBookmarks: want true for a query with a bookmarked filter")
	}
}

func TestNeedsBookmarks_FalseWithoutFilter(t *testing.T) {
	q, err := NewSpanQuery("test-project", SpanQueryRequest{})
	if err != nil {
		t.Fatalf("NewSpanQuery: %v", err)
	}
	if q.NeedsBookmarks() {
		t.Error("NeedsBookmarks: want false for a query without a bookmarked filter")
	}
}

func TestSpanQuery_BookmarkedFilterInWhereClause(t *testing.T) {
	q := &SpanQuery{
		projectID: "my-project",
		filters: []SpanQueryFilter{
			{Field: "bookmarked", Op: "eq", Value: true},
		},
	}
	q.SetBookmarkedTraceIDs([]string{"trace-a", "trace-b"})

	args, where, err := q.buildWhereClause()
	if err != nil {
		t.Fatalf("buildWhereClause error: %v", err)
	}

	wantClause := "spans.trace_id IN (?, ?)"
	if !strings.Contains(where, wantClause) {
		t.Errorf("WHERE clause missing bookmark filter.\nGot: %s\nWant substring: %s", where, wantClause)
	}
	// project_id arg + the two trace IDs.
	if len(args) != 3 {
		t.Errorf("args: got %v, want 3 entries", args)
	}
}

func TestSpanQuery_BookmarkedFalseFilterInWhereClause(t *testing.T) {
	q := &SpanQuery{
		projectID: "my-project",
		filters: []SpanQueryFilter{
			{Field: "bookmarked", Op: "eq", Value: false},
		},
	}
	q.SetBookmarkedTraceIDs([]string{"trace-a"})

	_, where, err := q.buildWhereClause()
	if err != nil {
		t.Fatalf("buildWhereClause error: %v", err)
	}

	wantClause := "spans.trace_id NOT IN (?)"
	if !strings.Contains(where, wantClause) {
		t.Errorf("WHERE clause missing unbookmarked filter.\nGot: %s\nWant substring: %s", where, wantClause)
	}
}

