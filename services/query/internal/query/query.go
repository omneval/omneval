package query

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/storage"
	"github.com/zbloss/lantern/services/query/internal/cursor"
)

// FilterOp is an allowlisted comparison operator for span queries.
type FilterOp string

const (
	OpEq  FilterOp = "eq"
	OpNeq FilterOp = "neq"
	OpGt  FilterOp = "gt"
	OpGte FilterOp = "gte"
	OpLt  FilterOp = "lt"
	OpLte FilterOp = "lte"
	OpIn  FilterOp = "in"
)

// ValidSpanQueryFilters returns the list of accepted filter fields for
// use in error messages and API documentation.
func ValidSpanQueryFilters() []string {
	fields := make([]string, 0, len(allSpanFields))
	for f := range allSpanFields {
		fields = append(fields, string(f))
	}
	sort.Strings(fields)
	return fields
}

// ValidFilterOperators returns the list of accepted filter operators for
// use in error messages and API documentation.
func ValidFilterOperators() []string {
	ops := make([]string, 0, len(validOps))
	for op := range validOps {
		ops = append(ops, string(op))
	}
	sort.Strings(ops)
	return ops
}

// acceptedFieldsMessage returns a human-readable string of all accepted
// filter fields, suitable for inclusion in error messages.
func acceptedFieldsMessage() string {
	return strings.Join(ValidSpanQueryFilters(), ", ")
}

// acceptedOperatorsMessage returns a human-readable string of all accepted
// operators, suitable for inclusion in error messages.
func acceptedOperatorsMessage() string {
	return strings.Join(ValidFilterOperators(), ", ")
}

// spanField is an allowlisted column name for span queries.
type spanField string

const (
	fieldProjectID    spanField = "project_id"
	fieldTraceID      spanField = "trace_id"
	fieldSpanID       spanField = "span_id"
	fieldServiceName  spanField = "service_name"
	fieldName         spanField = "name"
	fieldKind         spanField = "kind"
	fieldModel        spanField = "model"
	fieldStartTime    spanField = "start_time"
	fieldEndTime      spanField = "end_time"
	fieldInputTokens  spanField = "input_tokens"
	fieldOutputTokens spanField = "output_tokens"
	fieldCostUSD      spanField = "cost_usd"
	fieldPromptName   spanField = "prompt_name"
	fieldStatusCode   spanField = "status_code"
)

// specialFilterFields are filter fields handled specially (not simple column comparisons).
var specialFilterFields = map[string]struct{}{
	"bookmarked": {},
}

// allSpanFields is the full allowlist of valid span fields.
var allSpanFields = map[spanField]struct{}{
	fieldProjectID: {}, fieldTraceID: {}, fieldSpanID: {},
	fieldServiceName: {}, fieldName: {}, fieldKind: {},
	fieldModel: {}, fieldStartTime: {}, fieldEndTime: {},
	fieldInputTokens: {}, fieldOutputTokens: {}, fieldCostUSD: {},
	fieldPromptName: {}, fieldStatusCode: {},
}

// validOps is the set of allowed comparison operators.
var validOps = map[FilterOp]struct{}{
	OpEq: {}, OpNeq: {}, OpGt: {}, OpGte: {}, OpLt: {}, OpLte: {}, OpIn: {},
}

// SpanQueryRequest is the JSON body accepted by POST /api/v1/spans/query.
//
// # API Schema
//
// The request body must be a JSON object with the following fields:
//
//	{
//	  "from": "2025-01-01T00:00:00Z",     // RFC 3339 start time (optional)
//	  "to": "2025-01-02T00:00:00Z",       // RFC 3339 end time (optional)
//	  "filters": [                         // array of filter predicates (optional)
//	    {
//	      "field": "model",                // one of: project_id, trace_id, span_id,
//	                                       //   service_name, name, kind, model,
//	                                       //   start_time, end_time, input_tokens,
//	                                       //   output_tokens, cost_usd, prompt_name,
//	                                       //   status_code, bookmarked
//	      "op": "eq",                      // one of: eq, neq, gt, gte, lt, lte, in
//	      "value": "gpt-4o"                // value to compare against (any JSON type)
//	    }
//	  ],
//	  "cursor": "...",                     // base64-encoded keyset cursor for pagination (optional)
//	  "limit": 50                          // page size, 1-500 (optional, defaults to 50)
//	}
//
// # Filter Format
//
// Filters MUST be an array of objects with "field", "op", and "value" keys.
// Passing a non-array value (e.g., an object or string) will return a 400 error
// with a message describing the correct format.
//
// # Accepted Fields
//
// The following fields are supported:
//
//	project_id, trace_id, span_id, service_name, name, kind, model,
//	start_time, end_time, input_tokens, output_tokens, cost_usd,
//	prompt_name, status_code, bookmarked
//
// # Accepted Operators
//
// The following operators are supported:
//
//	eq, neq, gt, gte, lt, lte, in
//
// For "in" operator, value must be a JSON array of values.
// For "bookmarked" field, value must be a boolean (true/false).
// All other fields accept the operator's natural value type (string, number, etc.).
type SpanQueryRequest struct {
	From    time.Time         `json:"from"`
	To      time.Time         `json:"to"`
	Filters []SpanQueryFilter `json:"filters,omitempty"`
	Cursor  string            `json:"cursor,omitempty"`
	Limit   int               `json:"limit,omitempty"`
}

// SpanQueryFilter is a single predicate on a span field, used within
// SpanQueryRequest.Filters as an object with three keys: "field", "op", "value".
type SpanQueryFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// NewSpanQuery creates a SpanQuery from a SpanQueryRequest.
// The projectID is always injected — it is never read from client input.
func NewSpanQuery(projectID string, req SpanQueryRequest, s3Store storage.ObjectStore, snapshotDB string) (*SpanQuery, error) {
	q := &SpanQuery{
		projectID:  projectID,
		from:       req.From,
		to:         req.To,
		filters:    make([]SpanQueryFilter, len(req.Filters)),
		limit:      DefaultLimit,
		s3Store:    s3Store,
		snapshotDB: snapshotDB,
	}

	for i, f := range req.Filters {
		if err := validateFilter(f); err != nil {
			return nil, fmt.Errorf("query: invalid filter %d: %w", i, err)
		}
		q.filters[i] = f
	}

	if req.Cursor != "" {
		c, err := cursor.Decode(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("query: invalid cursor: %w", err)
		}
		q.cursor = c
	}

	if req.Limit > 0 && req.Limit <= MaxLimit {
		q.limit = req.Limit
	}

	return q, nil
}

const (
	// DefaultLimit is the default page size for span queries.
	DefaultLimit = 50
	// MaxLimit is the maximum page size allowed.
	MaxLimit = 500
	// lanternBucket is the default S3 bucket for both the Writer's flusher
	// and the Query API's cold Parquet reads. Must match config.Storage.Bucket.
	lanternBucket = "lantern"
)

// SpanQuery builds a parameterized SQL query for POST /api/v1/spans/query.
// It handles hot+cold UNION with keyset cursor pagination and
// field-level filters with allowlisted operators.
// The cold side reads Hive-partitioned Parquet files from S3 via
// DuckDB's read_parquet with hive_partitioning=true.
type SpanQuery struct {
	projectID  string
	from       time.Time
	to         time.Time
	filters    []SpanQueryFilter
	cursor     cursor.Cursor
	limit      int
	s3Store    storage.ObjectStore // nil when S3 not configured
	snapshotDB string              // local DuckDB snapshot path
}

// EffectiveLimit returns the limit actually used by this query. This is the
// validated, bounded value — not the raw client input.
func (q *SpanQuery) EffectiveLimit() int {
	return q.limit
}

// SQL returns the compiled SQL string and positional arguments.
// The query always emits a hot+cold UNION. The cold side reads
// Hive-partitioned Parquet files from S3 via read_parquet with
// hive_partitioning=true.
func (q *SpanQuery) SQL() (sql string, args []any, err error) {
	// --- Hot side: local DuckDB snapshot ---
	hotSQL, hotArgs, err := q.hotSQL()
	if err != nil {
		return "", nil, err
	}

	// --- Cold side: Parquet on S3 via read_parquet ---
	coldSQL := q.coldSQL()

	parts := []string{hotSQL, coldSQL}
	unionSQL := strings.Join(parts, "\nUNION ALL\n")

	// Wrap in outer query for cursor, ordering, and limit.
	outerSQL := q.outerSQL(unionSQL)

	// Collect all args: hot side args + cursor args.
	args = hotArgs
	if q.cursor.StartTime.IsZero() && q.cursor.SpanID == "" {
		// First page: pass limit as arg for the outer LIMIT.
		args = append(args, q.limit)
	} else {
		// Cursor page: pass (start_time, start_time, span_id, limit).
		args = append(args, q.cursor.StartTime, q.cursor.StartTime, q.cursor.SpanID, q.limit)
	}

	return outerSQL, args, nil
}

// hotSQL builds the SELECT for the local DuckDB snapshot.
// It returns just the SELECT with WHERE — ORDER BY and LIMIT are applied
// by the outer query to support UNION properly.
func (q *SpanQuery) hotSQL() (string, []any, error) {
	var sb strings.Builder
	sb.WriteString("SELECT span_id, trace_id, parent_id, project_id, service_name, name, kind, start_time, end_time, model, input, output, input_tokens, output_tokens, cost_usd, prompt_name, prompt_version, status_code, status_message FROM spans")

	args, where := q.buildWhereClause()
	sb.WriteString(where)

	return sb.String(), args, nil
}

// coldSQL builds the SELECT for the Parquet archive on S3.
// It uses read_parquet with hive_partitioning=true to read Hive-partitioned
// Parquet files from s3://bucket/archive/project_id={id}/date={date}/spans/.
// Only reads when S3 is configured (s3Store != nil).
// The bucket defaults to "lantern" — matching the Writer's s3URL default.
func (q *SpanQuery) coldSQL() string {
	if q.s3Store == nil {
		// No S3 configured: return a no-op that produces zero rows.
		return `SELECT span_id, trace_id, parent_id, project_id, service_name, name, kind, start_time, end_time, model, input, output, input_tokens, output_tokens, cost_usd, prompt_name, prompt_version, status_code, status_message FROM (VALUES (CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS TIMESTAMPTZ), CAST(NULL AS TIMESTAMPTZ), CAST(NULL AS VARCHAR), CAST(NULL AS JSON), CAST(NULL AS JSON), CAST(NULL AS BIGINT), CAST(NULL AS BIGINT), CAST(NULL AS DOUBLE), CAST(NULL AS VARCHAR), CAST(NULL AS BIGINT), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR)) LIMIT 0) AS t(span_id, trace_id, parent_id, project_id, service_name, name, kind, start_time, end_time, model, input, output, input_tokens, output_tokens, cost_usd, prompt_name, prompt_version, status_code, status_message)`
	}

	parquetPrefix := fmt.Sprintf(
		"s3://%s/archive/project_id=%s/**/*.parquet",
		lanternBucket,
		q.projectID,
	)

	return fmt.Sprintf(`
		SELECT span_id, trace_id, parent_id, project_id, service_name, name, kind,
		       start_time, end_time, model, input, output,
		       input_tokens, output_tokens, cost_usd,
		       prompt_name, prompt_version,
		       status_code, status_message
		FROM read_parquet(['%s'], hive_partitioning=true)
	`, parquetPrefix)
}

// outerSQL wraps the UNION result for keyset cursor pagination.
// ORDER BY and LIMIT are applied to the outer query.
func (q *SpanQuery) outerSQL(innerSQL string) string {
	var sb strings.Builder
	sb.WriteString("SELECT * FROM (")
	sb.WriteString(innerSQL)
	sb.WriteString(") AS _unioned")

	if q.cursor.StartTime.IsZero() && q.cursor.SpanID == "" {
		sb.WriteString("\n  ORDER BY start_time DESC, span_id ASC")
		sb.WriteString("\n  LIMIT ?")
		return sb.String()
	}

	sb.WriteString("\n  WHERE (start_time < ? OR (start_time = ? AND span_id < ?))")
	sb.WriteString("\n  ORDER BY start_time DESC, span_id ASC")
	sb.WriteString("\n  LIMIT ?")
	return sb.String()
}

// buildWhereClause assembles the WHERE clause from filters.
func (q *SpanQuery) buildWhereClause() ([]any, string) {
	var clauses []string
	var args []any

	// project_id is always injected from the session.
	clauses = append(clauses, "project_id = ?")
	args = append(args, q.projectID)

	// Time range filter.
	clauses = append(clauses, "start_time >= ? AND start_time <= ?")
	args = append(args, q.from, q.to)

	// Special filters (e.g., bookmarked) require custom SQL beyond simple
	// column comparisons.
	for _, f := range q.filters {
		if _, ok := specialFilterFields[f.Field]; ok {
			compiled, specialArgs := q.compileSpecialFilter(f)
			if compiled != "" {
				clauses = append(clauses, compiled)
				args = append(args, specialArgs...)
			}
		}
	}

	// Regular filters map directly to span columns.
	for _, f := range q.filters {
		if _, ok := specialFilterFields[f.Field]; ok {
			continue
		}
		compiled, a, err := compileFilter(f)
		if err != nil {
			continue // Skip invalid filters rather than failing the whole query.
		}
		clauses = append(clauses, compiled)
		args = append(args, a...)
	}

	where := "\n  WHERE " + strings.Join(clauses, " AND ")
	return args, where
}

// operatorSQL maps FilterOp names to SQL operator symbols.
var operatorSQL = map[FilterOp]string{
	OpEq:  "=",
	OpNeq: "!=",
	OpGt:  ">",
	OpGte: ">=",
	OpLt:  "<",
	OpLte: "<=",
}

// compileFilter translates a single Filter into SQL with placeholder arguments.
// Validation of field and operator names is guaranteed by NewSpanQuery's
// validateFilter calls — this function assumes the filter has already been
// validated and focuses solely on SQL compilation.
func compileFilter(f SpanQueryFilter) (sql string, args []any, err error) {
	field := spanField(f.Field)
	op := FilterOp(f.Op)

	// Handle IN specially — value is a slice.
	if op == OpIn {
		slice, ok := f.Value.([]any)
		if !ok {
			switch v := f.Value.(type) {
			case []string:
				slice = make([]any, len(v))
				for i, s := range v {
					slice[i] = s
				}
			case []int64:
				slice = make([]any, len(v))
				for i, n := range v {
					slice[i] = n
				}
			case []float64:
				slice = make([]any, len(v))
				for i, n := range v {
					slice[i] = n
				}
			default:
				return "", nil, fmt.Errorf("query: in operator requires a slice value")
			}
		}
		if len(slice) == 0 {
			return fmt.Sprintf("%s IS NULL", field), nil, nil
		}
		placeholders := make([]string, len(slice))
		for i := range slice {
			placeholders[i] = "?"
			args = append(args, slice[i])
		}
		return fmt.Sprintf("%s IN (%s)", field, strings.Join(placeholders, ", ")), args, nil
	}

	// All other operators use a single placeholder.
	opSymbol := operatorSQL[op]
	args = append(args, f.Value)
	return fmt.Sprintf("%s %s ?", field, opSymbol), args, nil
}

// compileSpecialFilter generates SQL for non-column filters that require
// custom logic beyond simple column comparisons. Currently only supports
// "bookmarked", which checks the bookmarks table for existence.
//
// The caller (buildWhereClause) guarantees that the filter field is a known
// special field. This method returns an empty string for unsupported
// operators or values, causing the filter to be silently skipped.
func (q *SpanQuery) compileSpecialFilter(f SpanQueryFilter) (string, []any) {
	// Only equality operator is supported for bookmarked filters.
	if FilterOp(f.Op) != OpEq {
		return "", nil
	}

	bookmarked, ok := f.Value.(bool)
	if !ok {
		return "", nil
	}

	sqlClause := "EXISTS (SELECT 1 FROM bookmarks b WHERE b.trace_id = spans.trace_id AND b.project_id = ?)"
	if !bookmarked {
		sqlClause = "NOT EXISTS (SELECT 1 FROM bookmarks b WHERE b.trace_id = spans.trace_id AND b.project_id = ?)"
	}
	return sqlClause, []any{q.projectID}
}

// validateFilter checks that a SpanQueryFilter has a valid field and operator.
// Special fields (like "bookmarked") bypass the spanFields allowlist.
// Error messages include lists of accepted fields/operators for discoverability.
func validateFilter(f SpanQueryFilter) error {
	if _, ok := allSpanFields[spanField(f.Field)]; !ok {
		if _, ok := specialFilterFields[f.Field]; !ok {
			return fmt.Errorf("unknown field %q; accepted fields: %s", f.Field, acceptedFieldsMessage())
		}
	}
	if _, ok := validOps[FilterOp(f.Op)]; !ok {
		return fmt.Errorf("unknown operator %q; accepted operators: %s", f.Op, acceptedOperatorsMessage())
	}
	if f.Value == nil {
		return fmt.Errorf("value must not be nil")
	}
	return nil
}

// NextCursor extracts the next page cursor from a list of spans.
// Returns an empty string if there are no more pages (fewer spans than limit).
// The cursor encodes the last element of the *page* (at index min(len-1, limit-1)),
// not the last element of the full input slice.
func NextCursor(spans []*domain.Span, limit int) string {
	if len(spans) == 0 || len(spans) < limit {
		return ""
	}
	// The last element of this page is at index limit-1.
	last := spans[limit-1]
	if last.StartTime.IsZero() && last.SpanID == "" {
		return ""
	}
	return cursor.Encode(cursor.Cursor{
		StartTime: last.StartTime,
		SpanID:    last.SpanID,
	})
}

// strVal extracts a string from a column value, handling both []byte and string.
// DuckDB returns VARCHAR as string, not []byte.
func strVal(v any) string {
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

// ScanRows converts raw rows from the query result into domain.Span objects.
// Expected column order: span_id, trace_id, parent_id, project_id, service_name,
// name, kind, start_time, end_time, model, input, output, input_tokens,
// output_tokens, cost_usd, prompt_name, prompt_version, status_code, status_message
func ScanRows(rows [][]any) ([]*domain.Span, error) {
	spans := make([]*domain.Span, len(rows))
	for i, row := range rows {
		if len(row) < 19 {
			return nil, fmt.Errorf("scan: expected 19 columns, got %d", len(row))
		}
		s := &domain.Span{}
		s.SpanID = strVal(row[0])
		s.TraceID = strVal(row[1])
		s.ParentID = strVal(row[2])
		s.ProjectID = strVal(row[3])
		s.ServiceName = strVal(row[4])
		s.Name = strVal(row[5])
		s.Kind = domain.SpanKind(strVal(row[6]))
		if v, ok := row[7].(time.Time); ok {
			s.StartTime = v
		}
		if v, ok := row[8].(time.Time); ok {
			s.EndTime = v
		}
		s.Model = strVal(row[9])
		s.Input = strVal(row[10])
		s.Output = strVal(row[11])
		if v, ok := row[12].(int64); ok {
			s.InputTokens = v
		}
		if v, ok := row[13].(int64); ok {
			s.OutputTokens = v
		}
		if v, ok := row[14].(float64); ok {
			s.CostUSD = v
		}
		s.PromptName = strVal(row[15])
		if v, ok := row[16].(int64); ok {
			s.PromptVersion = v
		}
		s.StatusCode = strVal(row[17])
		s.StatusMessage = strVal(row[18])
		spans[i] = s
	}
	return spans, nil
}

// SpanResponse is the JSON body returned by POST /api/v1/spans/query.
type SpanResponse struct {
	Spans []*domain.Span `json:"spans"`
	Next  string         `json:"next,omitempty"`
	Total int64          `json:"total,omitempty"`
	Limit int            `json:"limit"`
}

// CountSQL returns a COUNT(*) query for the same filter set.
func (q *SpanQuery) CountSQL() (sql string, args []any, err error) {
	_, where := q.buildWhereClause()
	return fmt.Sprintf("SELECT COUNT(*) FROM spans%s", where), nil, nil
}

// MarshalResponse serializes a SpanResponse to JSON bytes.
func MarshalResponse(r SpanResponse) ([]byte, error) {
	return json.Marshal(r)
}
