package querybuild

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/dsl"
	"github.com/omneval/omneval/services/query/internal/query"
)

// ValidationError indicates that the query was rejected because of invalid
// client input rather than an unexpected server error.  Callers can use
// [errors.Is] with this sentinel to distinguish 400-level API errors from
// 500-level server errors.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return nil }

// IsValidationError checks whether an error is (or wraps) a ValidationError.
// Exported so the handler package can use it to distinguish 400 from 500.
func IsValidationError(err error) bool {
	return isValidation(err)
}

// isValidation checks whether an error is (or wraps) a ValidationError.
func isValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// QueryBuilder encapsulates the full DSL → SQL → execute → scan pipeline
// for both span and analytics queries. It owns the Lake connection, query
// compilation, row scanning, and cursor computation.
type QueryBuilder struct {
	Lake          DBHandle
	BookmarkStore metadata.BookmarkStore
}

// DBHandle is the minimal database interface used by the query builder.
type DBHandle interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// SpanQueryResult is the unified result for a span query execution.
type SpanQueryResult struct {
	Spans []*domain.Span
	Next  string
	Limit int
}

// AnalyticsQueryResult is the unified result for an analytics query execution.
type AnalyticsQueryResult struct {
	Rows []map[string]any
}

// QueryResult is the unified result returned by Execute.  For span queries the
// Spans, Next, and Limit fields are populated; for analytics queries the Rows
// field is populated.  The caller inspects which fields are non-zero to
// determine the result type.
type QueryResult struct {
	Spans []*domain.Span
	Rows  []map[string]any
	Next  string
	Limit int
}

// ExecuteSpan handles the full span query pipeline:
//
//	1. Build SpanQuery from request and projectID (handles validation, default time range, cursor decode)
//	2. Inject bookmarked trace IDs if a "bookmarked" filter is present
//	3. Resolve the page's trace_ids via PhaseOneSQL (cheap, narrow-column scan)
//	4. Compile and execute MainSQL against those resolved trace_ids
//	5. Scan rows into domain spans
//	6. Compute next cursor and truncate to effective limit
//	7. Return SpanResponse
//
// Phase 1 is a separate round trip rather than an inlined subquery — see
// query.SpanQuery.PhaseOneSQL's doc for why (issue #229: inlining let DuckDB
// read the wide input/output LLM payload columns across the full query time
// range instead of just the resolved page).
func (qb *QueryBuilder) ExecuteSpan(ctx context.Context, req query.SpanQueryRequest, projectID string) (*query.SpanResponse, error) {
	// Build the query — projectID is validated by the caller before calling ExecuteSpan.
	q, err := query.NewSpanQuery(projectID, req)
	if err != nil {
		return nil, &ValidationError{Message: fmt.Sprintf("querybuild: build span query: %s", err)}
	}

	// "bookmarked" filters need the project's starred trace IDs from the
	// Metadata Store before SQL compilation.
	if q.NeedsBookmarks() && qb.BookmarkStore != nil {
		ids, err := qb.BookmarkStore.ListBookmarkedTraceIDs(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("querybuild: bookmark lookup: %w", err)
		}
		q.SetBookmarkedTraceIDs(ids)
	}

	// Phase 1: resolve the page's candidate trace_ids first.
	phase1SQL, phase1Args := q.PhaseOneSQL()
	phase1Rows, err := qb.Lake.Query(phase1SQL, phase1Args...)
	if err != nil {
		return nil, fmt.Errorf("querybuild: execute phase1 query: %w", err)
	}
	traceIDs, err := scanTraceIDs(phase1Rows)
	if err != nil {
		return nil, fmt.Errorf("querybuild: scan phase1 trace_ids: %w", err)
	}

	if len(traceIDs) == 0 {
		return &query.SpanResponse{Limit: q.EffectiveLimit()}, nil
	}

	// Compile and execute the main query against the resolved trace_ids.
	sqlStr, args, err := q.MainSQL(traceIDs)
	if err != nil {
		return nil, &ValidationError{Message: fmt.Sprintf("querybuild: compile span query: %s", err)}
	}

	rows, err := qb.Lake.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("querybuild: execute span query: %w", err)
	}
	defer rows.Close()

	// Scan rows.
	spanRows, err := scanAllRows(rows)
	if err != nil {
		return nil, fmt.Errorf("querybuild: scan span rows: %w", err)
	}

	spans, err := query.ScanRows(spanRows)
	if err != nil {
		return nil, fmt.Errorf("querybuild: convert span rows: %w", err)
	}

	// Compute next cursor using the effective limit from the query.
	// The SQL fetches limit+1 rows; NextCursor determines whether more
	// pages exist based on whether we got more than limit results.
	next := query.NextCursor(spans, q.EffectiveLimit())

	// Truncate to the requested page size — the extra row was only used to
	// detect whether a next page exists and must not be returned to callers.
	if len(spans) > q.EffectiveLimit() {
		spans = spans[:q.EffectiveLimit()]
	}

	return &query.SpanResponse{
		Spans: spans,
		Next:  next,
		Limit: q.EffectiveLimit(),
	}, nil
}

// scanTraceIDs scans a single-column (trace_id) result set returned by
// PhaseOneSQL into a string slice, closing rows when done.
func scanTraceIDs(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan: trace_id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan: rows iteration: %w", err)
	}
	return ids, nil
}

// ExecuteAnalytics handles the full analytics query pipeline:
//
//	1. Validate the query (applies default time range)
//	2. Compile SQL via the DSL compiler
//	3. Execute SQL against the Lake
//	4. Scan rows into column headers + [][]any
//	5. Return AnalyticsResponse
func (qb *QueryBuilder) ExecuteAnalytics(ctx context.Context, req dsl.Query, projectID string) (*AnalyticsQueryResult, error) {
	// Validate and apply default time range.
	if err := req.Validate(); err != nil {
		return nil, &ValidationError{Message: fmt.Sprintf("querybuild: validate analytics query: %s", err)}
	}

	// Compile the query against the single Lake table set.
	sqlStr, args, err := dsl.CompileLake(projectID, req)
	if err != nil {
		return nil, &ValidationError{Message: fmt.Sprintf("querybuild: compile analytics query: %s", err)}
	}

	// Execute.
	rows, err := qb.Lake.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("querybuild: execute analytics query: %w", err)
	}
	defer rows.Close()

	// Scan rows into column headers + map rows.
	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("querybuild: scan column types: %w", err)
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("querybuild: scan analytics row: %w", err)
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col.Name()] = values[i]
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("querybuild: analytics row iteration: %w", err)
	}

	return &AnalyticsQueryResult{
		Rows: result,
	}, nil
}

// scanAllRows scans all database rows into [][]any.
func scanAllRows(rows *sql.Rows) ([][]any, error) {
	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("scan: column types: %w", err)
	}

	var result [][]any
	for rows.Next() {
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan: row: %w", err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan: rows iteration: %w", err)
	}
	return result, nil
}

// Execute is the unified entry point that dispatches to the correct
// pipeline (span or analytics) based on the query's QueryType.  It
// encapsulates the full DSL → SQL → execute → scan pipeline and returns
// a single QueryResult type regardless of the underlying query kind.
func (qb *QueryBuilder) Execute(ctx context.Context, q *dsl.Query) (*QueryResult, error) {
	switch q.QueryType {
	case dsl.QueryTypeSpan:
		return qb.executeSpan(ctx, q)
	case dsl.QueryTypeAnalytics:
		return qb.executeAnalytics(ctx, q)
	default:
		return nil, &ValidationError{Message: fmt.Sprintf("querybuild: unknown query type %q", q.QueryType)}
	}
}

// executeSpan handles the span query pipeline using a dsl.Query directly.
// It converts the dsl.Query fields (From, To, Filters, Limit, Cursor) into
// a SpanQueryRequest and delegates to ExecuteSpan, then wraps the result.
func (qb *QueryBuilder) executeSpan(ctx context.Context, q *dsl.Query) (*QueryResult, error) {
	// Convert dsl.Query → SpanQueryRequest.
	req := query.SpanQueryRequest{
		From:  q.From,
		To:    q.To,
		Limit: q.Limit,
		Cursor: q.Cursor,
	}

	// Convert dsl.Filter → query.SpanQueryFilter.
	for _, f := range q.Filters {
		req.Filters = append(req.Filters, query.SpanQueryFilter{
			Field: f.Field,
			Op:    string(f.Op),
			Value: f.Value,
		})
	}

	resp, err := qb.ExecuteSpan(ctx, req, q.ProjectID)
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Spans: resp.Spans,
		Next:  resp.Next,
		Limit: resp.Limit,
	}, nil
}

// executeAnalytics handles the analytics query pipeline by delegating to
// ExecuteAnalytics and wrapping the result in a QueryResult.
func (qb *QueryBuilder) executeAnalytics(ctx context.Context, q *dsl.Query) (*QueryResult, error) {
	result, err := qb.ExecuteAnalytics(ctx, *q, q.ProjectID)
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Rows: result.Rows,
	}, nil
}