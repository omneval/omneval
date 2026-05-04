package query

import (
	"encoding/json"
	"fmt"
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

// spanField is an allowlisted column name for span queries.
type spanField string

const (
	fieldProjectID     spanField = "project_id"
	fieldTraceID       spanField = "trace_id"
	fieldSpanID        spanField = "span_id"
	fieldServiceName   spanField = "service_name"
	fieldName          spanField = "name"
	fieldKind          spanField = "kind"
	fieldModel         spanField = "model"
	fieldStartTime     spanField = "start_time"
	fieldEndTime       spanField = "end_time"
	fieldInputTokens   spanField = "input_tokens"
	fieldOutputTokens  spanField = "output_tokens"
	fieldCostUSD       spanField = "cost_usd"
	fieldPromptName    spanField = "prompt_name"
	fieldStatusCode    spanField = "status_code"
)

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

// SpanQueryRequest is the body accepted by POST /api/v1/spans/query.
type SpanQueryRequest struct {
	From    time.Time         `json:"from"`
	To      time.Time         `json:"to"`
	Filters []SpanQueryFilter `json:"filters,omitempty"`
	Cursor  string            `json:"cursor,omitempty"`
	Limit   int               `json:"limit,omitempty"`
}

// SpanQueryFilter is a single predicate on a span field.
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
)

// SpanQuery builds a parameterized SQL query for POST /api/v1/spans/query.
// It handles hot+cold UNION (cold side is a no-op stub in this slice),
// keyset cursor pagination, and field-level filters with allowlisted operators.
type SpanQuery struct {
	projectID  string
	from       time.Time
	to         time.Time
	filters    []SpanQueryFilter
	cursor     cursor.Cursor
	limit      int
	s3Store    storage.ObjectStore // nil when S3 not configured
	snapshotDB string               // local DuckDB snapshot path
}

// SQL returns the compiled SQL string and positional arguments.
// The query always emits a hot+cold UNION. The cold side is a no-op stub
// (always false) in this slice since Parquet archival is not yet implemented.
func (q *SpanQuery) SQL() (sql string, args []any, err error) {
	// --- Hot side: local DuckDB snapshot ---
	hotSQL, hotArgs, err := q.hotSQL()
	if err != nil {
		return "", nil, err
	}

	// --- Cold side: Parquet stub (no-op) ---
	// This stub always returns zero rows, establishing the UNION pattern
	// for when Parquet archival is implemented.
	// Must match the 19 columns of the hot query.
	coldSQL := `SELECT span_id, trace_id, parent_id, project_id, service_name, name, kind, start_time, end_time, model, input, output, input_tokens, output_tokens, cost_usd, prompt_name, prompt_version, status_code, status_message FROM (VALUES (CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR), CAST(NULL AS TIMESTAMPTZ), CAST(NULL AS TIMESTAMPTZ), CAST(NULL AS VARCHAR), CAST(NULL AS JSON), CAST(NULL AS JSON), CAST(NULL AS BIGINT), CAST(NULL AS BIGINT), CAST(NULL AS DOUBLE), CAST(NULL AS VARCHAR), CAST(NULL AS BIGINT), CAST(NULL AS VARCHAR), CAST(NULL AS VARCHAR)) LIMIT 0) AS t(span_id, trace_id, parent_id, project_id, service_name, name, kind, start_time, end_time, model, input, output, input_tokens, output_tokens, cost_usd, prompt_name, prompt_version, status_code, status_message)`

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

	// User-provided filters.
	for _, f := range q.filters {
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
func compileFilter(f SpanQueryFilter) (sql string, args []any, err error) {
	field := spanField(f.Field)
	if _, ok := allSpanFields[field]; !ok {
		return "", nil, fmt.Errorf("query: unknown field %q", f.Field)
	}

	op := FilterOp(f.Op)
	if _, ok := validOps[op]; !ok {
		return "", nil, fmt.Errorf("query: unknown operator %q", f.Op)
	}

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

// validateFilter checks that a SpanQueryFilter has a valid field and operator.
func validateFilter(f SpanQueryFilter) error {
	if _, ok := allSpanFields[spanField(f.Field)]; !ok {
		return fmt.Errorf("unknown field %q", f.Field)
	}
	if _, ok := validOps[FilterOp(f.Op)]; !ok {
		return fmt.Errorf("unknown operator %q", f.Op)
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
		if v, ok := row[0].([]byte); ok {
			s.SpanID = string(v)
		}
		if v, ok := row[1].([]byte); ok {
			s.TraceID = string(v)
		}
		if v, ok := row[2].([]byte); ok {
			s.ParentID = string(v)
		}
		if v, ok := row[3].([]byte); ok {
			s.ProjectID = string(v)
		}
		if v, ok := row[4].([]byte); ok {
			s.ServiceName = string(v)
		}
		if v, ok := row[5].([]byte); ok {
			s.Name = string(v)
		}
		if v, ok := row[6].([]byte); ok {
			s.Kind = domain.SpanKind(v)
		}
		if v, ok := row[7].(time.Time); ok {
			s.StartTime = v
		}
		if v, ok := row[8].(time.Time); ok {
			s.EndTime = v
		}
		if v, ok := row[9].([]byte); ok {
			s.Model = string(v)
		}
		if v, ok := row[10].([]byte); ok {
			_ = v
		}
		if v, ok := row[11].([]byte); ok {
			_ = v
		}
		if v, ok := row[12].(int64); ok {
			s.InputTokens = v
		}
		if v, ok := row[13].(int64); ok {
			s.OutputTokens = v
		}
		if v, ok := row[14].(float64); ok {
			s.CostUSD = v
		}
		if v, ok := row[15].([]byte); ok {
			s.PromptName = string(v)
		}
		if v, ok := row[16].(int64); ok {
			s.PromptVersion = v
		}
		if v, ok := row[17].([]byte); ok {
			s.StatusCode = string(v)
		}
		if v, ok := row[18].([]byte); ok {
			s.StatusMessage = string(v)
		}
		spans[i] = s
	}
	return spans, nil
}

// SpanResponse is the JSON body returned by POST /api/v1/spans/query.
type SpanResponse struct {
	Spans  []*domain.Span `json:"spans"`
	Next   string         `json:"next,omitempty"`
	Total  int64          `json:"total,omitempty"`
	Limit  int            `json:"limit"`
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
