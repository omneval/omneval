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

// ExecuteSpan handles the full span query pipeline:
//
//	1. Build SpanQuery from request and projectID (handles validation, default time range, cursor decode)
//	2. Inject bookmarked trace IDs if a "bookmarked" filter is present
//	3. Compile SQL via LakeSQL()
//	4. Execute SQL against the Lake
//	5. Scan rows into domain spans
//	6. Compute next cursor and truncate to effective limit
//	7. Return SpanResponse
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

	// Compile SQL.
	sqlStr, args, err := q.LakeSQL()
	if err != nil {
		return nil, &ValidationError{Message: fmt.Sprintf("querybuild: compile span query: %s", err)}
	}

	// Execute.
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