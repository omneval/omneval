package query

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/services/query/internal/cursor"
)

// FilterOp is an allowlisted comparison operator for span queries.
type FilterOp string

const (
	OpEq       FilterOp = "eq"
	OpNeq      FilterOp = "neq"
	OpGt       FilterOp = "gt"
	OpGte      FilterOp = "gte"
	OpLt       FilterOp = "lt"
	OpLte      FilterOp = "lte"
	OpIn       FilterOp = "in"
	OpContains FilterOp = "contains"
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
	fieldProjectID      spanField = "project_id"
	fieldTraceID        spanField = "trace_id"
	fieldSpanID         spanField = "span_id"
	fieldConversationID spanField = "conversation_id"
	fieldServiceName    spanField = "service_name"
	fieldName           spanField = "name"
	fieldKind           spanField = "kind"
	fieldModel          spanField = "model"
	fieldStartTime      spanField = "start_time"
	fieldEndTime        spanField = "end_time"
	fieldInputTokens    spanField = "input_tokens"
	fieldOutputTokens   spanField = "output_tokens"
	fieldCostUSD        spanField = "cost_usd"
	fieldPromptName     spanField = "prompt_name"
	fieldStatusCode     spanField = "status_code"
)

// specialFilterFields are filter fields handled specially (not simple column comparisons).
var specialFilterFields = map[string]struct{}{
	"bookmarked":  {},
	"duration_ms": {},
}

// allSpanFields is the full allowlist of valid span fields.
var allSpanFields = map[spanField]struct{}{
	fieldProjectID: {}, fieldTraceID: {}, fieldSpanID: {}, fieldConversationID: {},
	fieldServiceName: {}, fieldName: {}, fieldKind: {},
	fieldModel: {}, fieldStartTime: {}, fieldEndTime: {},
	fieldInputTokens: {}, fieldOutputTokens: {}, fieldCostUSD: {},
	fieldPromptName: {}, fieldStatusCode: {},
	// input and output are JSON columns — allowed for contains (LIKE) filters.
	"input":  {},
	"output": {},
}

// validOps is the set of allowed comparison operators.
var validOps = map[FilterOp]struct{}{
	OpEq: {}, OpNeq: {}, OpGt: {}, OpGte: {}, OpLt: {}, OpLte: {}, OpIn: {}, OpContains: {},
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
//	eq, neq, gt, gte, lt, lte, in, contains
//
// For "in" operator, value must be a JSON array of values.
// For "contains" operator, value is a substring matched via SQL LIKE '%value%'.
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
//
// Default time range: when from/to are omitted (zero time.Time), the query
// defaults to the last 30 days (from = now - 30d, to = now).
// Time range validation: if from is explicitly set after to, an error is
// returned so the caller can respond with 400.
func NewSpanQuery(projectID string, req SpanQueryRequest) (*SpanQuery, error) {
	from := req.From
	to := req.To

	// Apply default time range when both bounds are omitted.
	if from.IsZero() && to.IsZero() {
		from = time.Now().Add(-defaultTimeRange)
		to = time.Now()
	}

	// Validate that from is not after to.
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, fmt.Errorf("query: from must not be after to (got from=%v, to=%v)", from, to)
	}

	q := &SpanQuery{
		projectID: projectID,
		from:      from,
		to:        to,
		filters:   make([]SpanQueryFilter, len(req.Filters)),
		limit:     DefaultLimit,
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

// defaultTimeRange is the time range applied when from and to are omitted
// from a span query request. The default is the last 30 days.
const defaultTimeRange = 30 * 24 * time.Hour

// SpanQuery builds a parameterized SQL query for POST /api/v1/spans/query.
// It compiles to a single-table read against the Lake (lake.spans, ADR-0004)
// with keyset cursor pagination and field-level filters with allowlisted
// operators. See LakeSQL.
type SpanQuery struct {
	projectID string
	from      time.Time
	to        time.Time
	filters   []SpanQueryFilter
	cursor    cursor.Cursor
	limit     int
	// bookmarkedTraceIDs is the project's starred trace IDs, resolved from
	// the Metadata Store by the handler before SQL compilation. Bookmarks
	// no longer live in DuckDB (ADR-0004), so "bookmarked" filters compile
	// to an inline trace_id IN (...) list instead of a join.
	bookmarkedTraceIDs []string
}

// NeedsBookmarks reports whether any filter requires the project's
// bookmarked trace IDs to compile. Callers should resolve them from the
// Metadata Store and call SetBookmarkedTraceIDs before LakeSQL().
func (q *SpanQuery) NeedsBookmarks() bool {
	for _, f := range q.filters {
		if f.Field == "bookmarked" {
			return true
		}
	}
	return false
}

// SetBookmarkedTraceIDs supplies the project's starred trace IDs for
// "bookmarked" filter compilation.
func (q *SpanQuery) SetBookmarkedTraceIDs(ids []string) {
	q.bookmarkedTraceIDs = ids
}

// EffectiveLimit returns the limit actually used by this query. This is the
// validated, bounded value — not the raw client input.
func (q *SpanQuery) EffectiveLimit() int {
	return q.limit
}

// buildWhereClause assembles the WHERE clause from filters.
func (q *SpanQuery) buildWhereClause() ([]any, string) {
	var clauses []string
	var args []any

	// project_id is always injected from the session.
	clauses = append(clauses, "project_id = ?")
	args = append(args, q.projectID)

	// Time range filter — only applied when the corresponding bound is set.
	if !q.from.IsZero() {
		clauses = append(clauses, "start_time >= ?")
		args = append(args, q.from)
	}
	if !q.to.IsZero() {
		clauses = append(clauses, "start_time <= ?")
		args = append(args, q.to)
	}

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

	// Contains operator: LIKE '%value%'
	if op == OpContains {
		args = append(args, "%"+fmt.Sprint(f.Value)+"%")
		return fmt.Sprintf("%s LIKE ?", field), args, nil
	}

	// All other operators use a single placeholder.
	opSymbol := operatorSQL[op]
	args = append(args, f.Value)
	return fmt.Sprintf("%s %s ?", field, opSymbol), args, nil
}

// durationMsExpr returns the DuckDB SQL expression for the duration_ms
// virtual field — computed as milliseconds between end_time and start_time.
func durationMsExpr() string {
	return "(EXTRACT(EPOCH FROM (end_time - start_time)) * 1000)"
}

// compileSpecialFilter generates SQL for non-column filters that require
// custom logic beyond simple column comparisons. Currently supports
// "bookmarked" (an inline trace_id IN list resolved from the Metadata
// Store) and "duration_ms" (computed from end_time - start_time).
//
// The caller (buildWhereClause) guarantees that the filter field is a known
// special field. This method returns an empty string for unsupported
// operators or values, causing the filter to be silently skipped.
func (q *SpanQuery) compileSpecialFilter(f SpanQueryFilter) (string, []any) {
	switch f.Field {
	case "bookmarked":
		// Only equality operator is supported for bookmarked filters.
		if FilterOp(f.Op) != OpEq {
			return "", nil
		}

		bookmarked, ok := f.Value.(bool)
		if !ok {
			return "", nil
		}

		// Bookmarks live in the Metadata Store, not DuckDB; the handler
		// resolves the project's starred trace IDs up front and the filter
		// compiles to an inline IN list. Starred sets are small (manual
		// user action), so the list stays well under placeholder limits.
		if len(q.bookmarkedTraceIDs) == 0 {
			if bookmarked {
				return "FALSE", nil
			}
			return "TRUE", nil
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(q.bookmarkedTraceIDs)), ", ")
		args := make([]any, len(q.bookmarkedTraceIDs))
		for i, id := range q.bookmarkedTraceIDs {
			args[i] = id
		}
		sqlClause := "spans.trace_id IN (" + placeholders + ")"
		if !bookmarked {
			sqlClause = "spans.trace_id NOT IN (" + placeholders + ")"
		}
		return sqlClause, args

	case "duration_ms":
		ops, ok := operatorSQL[FilterOp(f.Op)]
		if !ok {
			return "", nil
		}
		return fmt.Sprintf("%s %s ?", durationMsExpr(), ops), []any{f.Value}

	default:
		return "", nil
	}
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
// Returns an empty string if there are no more pages.
//
// Callers must pass the raw DB result (fetched with LIMIT limit+1). If the
// result has more than limit items, there is at least one more page — return
// a cursor encoding the last item of the current page (index limit-1). If
// the result has limit or fewer items, it is the last page — return "".
func NextCursor(spans []*domain.Span, limit int) string {
	if len(spans) == 0 || len(spans) <= limit {
		return ""
	}
	// len(spans) > limit: there is a next page.
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

// parseKindCounts decodes the kind_counts column — a JSON object (e.g.
// `{"llm":2,"tool":1}`) produced by to_json(MAP(...)) in LakeSQL — into a
// map[string]int64. Returns (nil, nil) for NULL/empty values (e.g.
// span-level reads that don't select this column).
func parseKindCounts(v any) (map[string]int64, error) {
	switch s := v.(type) {
	case []byte:
		if len(s) == 0 {
			return nil, nil
		}
		var asFloat map[string]float64
		if err := json.Unmarshal(s, &asFloat); err != nil {
			return nil, err
		}
		out := make(map[string]int64, len(asFloat))
		for k, n := range asFloat {
			out[k] = int64(n)
		}
		return out, nil
	case string:
		if s == "" {
			return nil, nil
		}
		var asFloat map[string]float64
		if err := json.Unmarshal([]byte(s), &asFloat); err != nil {
			return nil, err
		}
		out := make(map[string]int64, len(asFloat))
		for k, n := range asFloat {
			out[k] = int64(n)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]int64, len(s))
		for k, n := range s {
			switch num := n.(type) {
			case int64:
				out[k] = num
			case float64:
				out[k] = int64(num)
			default:
				return nil, fmt.Errorf("kind_counts[%q]: unexpected value type %T", k, n)
			}
		}
		return out, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected type %T", v)
	}
}

// ScanRows converts raw rows from the query result into domain.Span objects.
// Expected column order: span_id, trace_id, parent_id, conversation_id, project_id,
// service_name, name, kind, start_time, end_time, model, input, output,
// input_tokens, output_tokens, cost_usd, prompt_name, prompt_version,
// status_code, status_message
//
// Trace-rollup reads (LakeSQL, issue #136) append a 21st and 22nd column,
// span_count and kind_counts — the number of spans in the row's trace, and
// a JSON object mapping each observation kind present in the trace to its
// count. Span-level reads (e.g. LakeTraceSpansSQL) produce exactly 20
// columns and leave SpanCount/KindCounts at their zero values.
func ScanRows(rows [][]any) ([]*domain.Span, error) {
	spans := make([]*domain.Span, len(rows))
	for i, row := range rows {
		if len(row) < 20 {
			return nil, fmt.Errorf("scan: expected at least 20 columns, got %d", len(row))
		}
		s := &domain.Span{}
		s.SpanID = strVal(row[0])
		s.TraceID = strVal(row[1])
		s.ParentID = strVal(row[2])
		s.ConversationID = strVal(row[3])
		s.ProjectID = strVal(row[4])
		s.ServiceName = strVal(row[5])
		s.Name = strVal(row[6])
		s.Kind = domain.SpanKind(strVal(row[7]))
		if v, ok := row[8].(time.Time); ok {
			s.StartTime = v
		}
		if v, ok := row[9].(time.Time); ok {
			s.EndTime = v
		}
		s.Model = strVal(row[10])
		s.Input = strVal(row[11])
		s.Output = strVal(row[12])
		if v, ok := row[13].(int64); ok {
			s.InputTokens = v
		}
		if v, ok := row[14].(int64); ok {
			s.OutputTokens = v
		}
		if v, ok := row[15].(float64); ok {
			s.CostUSD = v
		}
		s.PromptName = strVal(row[16])
		if v, ok := row[17].(int64); ok {
			s.PromptVersion = v
		}
		s.StatusCode = strVal(row[18])
		s.StatusMessage = strVal(row[19])
		if len(row) >= 21 {
			if v, ok := row[20].(int64); ok {
				s.SpanCount = v
			}
		}
		if len(row) >= 22 {
			if kc, err := parseKindCounts(row[21]); err != nil {
				return nil, fmt.Errorf("scan: kind_counts: %w", err)
			} else if kc != nil {
				s.KindCounts = kc
			}
		}
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

// MarshalResponse serializes a SpanResponse to JSON bytes.
func MarshalResponse(r SpanResponse) ([]byte, error) {
	return json.Marshal(r)
}

// LakeSQL returns the SQL for the Traces list: one row per distinct
// trace_id, representing that trace's root span (the span with no
// parent_id, or the earliest-starting span if no explicit root is present)
// annotated with trace-level rollups (issue #136):
//
//   - input_tokens, output_tokens, cost_usd are overridden with the SUM
//     across all spans in the trace (not just the root span's own values).
//   - end_time is overridden with the MAX end_time across all spans in the
//     trace, so duration_ms reflects the trace's true end-to-end latency
//     even when child spans outlive the root span.
//   - status_code is overridden with the "worst" status across the trace's
//     spans: "error" wins if any span errored, else the root span's status.
//   - span_count (a new trailing column) is the number of spans in the
//     trace.
//
// Filters and the time range apply to the underlying spans before
// rollup — e.g. a kind=llm filter selects traces containing at least one
// llm span, and rollups are computed over that filtered span set. This
// mirrors the pre-#136 filter semantics applied at the span level.
//
// Like the prior flat-span query, this reads only from the single Lake
// table (lake.spans, ADR-0004) — no hot+cold UNION — and does not dedupe
// residual duplicate spans (tolerated outside trace detail).
func (q *SpanQuery) LakeSQL() (sql string, args []any, err error) {
	// buildWhereClause always emits a WHERE clause — project_id is
	// unconditionally injected.
	whereArgs, where := q.buildWhereClause()

	var sb strings.Builder
	sb.WriteString("WITH filtered AS (\n")
	sb.WriteString("  SELECT * FROM lake.spans")
	sb.WriteString(where)
	sb.WriteString("\n)")
	args = append(args, whereArgs...)

	sb.WriteString(`,
ranked AS (
  SELECT *,
    ROW_NUMBER() OVER (
      PARTITION BY trace_id
      ORDER BY (CASE WHEN parent_id IS NULL OR parent_id = '' THEN 0 ELSE 1 END), start_time ASC, span_id ASC
    ) AS rn
  FROM filtered
),
rollups AS (
  SELECT
    trace_id,
    CAST(COUNT(*) AS BIGINT) AS span_count,
    CAST(COALESCE(SUM(input_tokens), 0) AS BIGINT) AS total_input_tokens,
    CAST(COALESCE(SUM(output_tokens), 0) AS BIGINT) AS total_output_tokens,
    COALESCE(SUM(cost_usd), 0) AS total_cost_usd,
    MAX(end_time) AS trace_end_time,
    MAX(CASE WHEN status_code = 'error' THEN 1 ELSE 0 END) AS has_error
  FROM filtered
  GROUP BY trace_id
),
kind_rollups AS (
  SELECT
    trace_id,
    to_json(MAP(LIST(kind), LIST(kind_count))) AS kind_counts
  FROM (
    SELECT trace_id, kind, CAST(COUNT(*) AS BIGINT) AS kind_count
    FROM filtered
    GROUP BY trace_id, kind
  )
  GROUP BY trace_id
)
SELECT
  r.span_id, r.trace_id, r.parent_id, r.conversation_id, r.project_id,
  r.service_name, r.name, r.kind, r.start_time,
  ru.trace_end_time AS end_time,
  r.model, r.input, r.output,
  ru.total_input_tokens AS input_tokens,
  ru.total_output_tokens AS output_tokens,
  ru.total_cost_usd AS cost_usd,
  r.prompt_name, r.prompt_version,
  CASE WHEN ru.has_error = 1 THEN 'error' ELSE r.status_code END AS status_code,
  r.status_message,
  ru.span_count,
  kr.kind_counts
FROM ranked r
JOIN rollups ru USING (trace_id)
JOIN kind_rollups kr USING (trace_id)
WHERE r.rn = 1`)

	if !q.cursor.StartTime.IsZero() || q.cursor.SpanID != "" {
		sb.WriteString(" AND (r.start_time < ? OR (r.start_time = ? AND r.span_id < ?))")
		args = append(args, q.cursor.StartTime, q.cursor.StartTime, q.cursor.SpanID)
	}
	sb.WriteString("\n  ORDER BY r.start_time DESC, r.span_id ASC")
	sb.WriteString("\n  LIMIT ?")
	args = append(args, q.limit+1)

	return sb.String(), args, nil
}

// LakeTraceSpansSQL returns a SQL query that fetches all spans for a single
// trace from the Lake table, deduplicated on (trace_id, span_id) keeping one
// row per pair — the read-time residual-duplicate policy from ADR-0004
// (duplicates survive only a crash between Lake commit and ledger insert).
func (q *SpanQuery) LakeTraceSpansSQL(traceID string) (sql string, args []any) {
	sql = "SELECT * FROM (" +
		"SELECT *, ROW_NUMBER() OVER (PARTITION BY trace_id, span_id ORDER BY start_time DESC) AS rn" +
		" FROM lake.spans WHERE trace_id = ? AND project_id = ?" +
		") AS deduped WHERE rn = 1 ORDER BY start_time ASC"
	return sql, []any{traceID, q.projectID}
}
