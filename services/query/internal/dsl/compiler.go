package dsl

import (
	"fmt"
	"strings"
	"time"
)

// TruncUnit is an allowlisted time-truncation granularity for GroupBy fields.
type TruncUnit string

const (
	TruncHour  TruncUnit = "hour"
	TruncDay   TruncUnit = "day"
	TruncWeek  TruncUnit = "week"
	TruncMonth TruncUnit = "month"
)

// validTruncUnits is the allowlist of valid truncation units.
var validTruncUnits = map[TruncUnit]struct{}{
	TruncHour: {}, TruncDay: {}, TruncWeek: {}, TruncMonth: {},
}

// FilterOp is an allowlisted comparison operator.
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

// validFilterOps is the allowlist of valid comparison operators.
var validFilterOps = map[FilterOp]struct{}{
	OpEq: {}, OpNeq: {}, OpGt: {}, OpGte: {}, OpLt: {}, OpLte: {}, OpIn: {},
}

// AggFunc is an allowlisted aggregation function.
type AggFunc string

const (
	AggSum   AggFunc = "sum"
	AggCount AggFunc = "count"
	AggAvg   AggFunc = "avg"
	AggMin   AggFunc = "min"
	AggMax   AggFunc = "max"
	AggP50   AggFunc = "p50"
	AggP95   AggFunc = "p95"
	AggP99   AggFunc = "p99"
)

// validAggFuncs is the allowlist of valid aggregation functions.
var validAggFuncs = map[AggFunc]struct{}{
	AggSum: {}, AggCount: {}, AggAvg: {}, AggMin: {}, AggMax: {},
	AggP50: {}, AggP95: {}, AggP99: {},
}

// spanColumns is the allowlist of valid span table columns for aggregations,
// filters, group-by, and order-by clauses.
var spanColumns = map[string]struct{}{
	"project_id":     {},
	"trace_id":       {},
	"span_id":        {},
	"service_name":   {},
	"name":           {},
	"kind":           {},
	"model":          {},
	"start_time":     {},
	"end_time":       {},
	"input_tokens":   {},
	"output_tokens":  {},
	"cost_usd":       {},
	"prompt_name":    {},
	"prompt_version": {},
	"status_code":    {},
	"status_message": {},
	// duration_ms is a virtual field — not a real column, but allowed.
	"duration_ms": {},
}

// durationMsVirtualField is the canonical virtual field name.
const durationMsVirtualField = "duration_ms"

// isDurationMs checks whether a field name refers to the duration_ms virtual
// field.
func isDurationMs(field string) bool {
	return field == durationMsVirtualField
}

// durationMsExpr returns the DuckDB SQL expression for the duration_ms virtual
// field.
func durationMsExpr() string {
	return "EPOCH_MS(end_time) - EPOCH_MS(start_time)"
}

// aggExprForField maps a (function, field) pair to a SQL expression fragment
// (without alias). It handles the duration_ms virtual field and percentile
// aggregations that wrap approx_quantile.
func aggExprForField(fn AggFunc, field string) (string, error) {
	if _, ok := validAggFuncs[fn]; !ok {
		return "", fmt.Errorf("dsl: unknown aggregation function %q", fn)
	}

	if field == "*" {
		if fn != AggCount {
			return "", fmt.Errorf("dsl: field %q is only valid with count()", "*")
		}
		return "COUNT(*)", nil
	}

	colExpr := field
	if isDurationMs(field) {
		colExpr = durationMsExpr()
	} else if _, ok := spanColumns[field]; !ok {
		return "", fmt.Errorf("dsl: unknown field %q for aggregation", field)
	}

	switch fn {
	case AggP50:
		return fmt.Sprintf("APPROX_QUANTILE(%s, 0.50)", colExpr), nil
	case AggP95:
		return fmt.Sprintf("APPROX_QUANTILE(%s, 0.95)", colExpr), nil
	case AggP99:
		return fmt.Sprintf("APPROX_QUANTILE(%s, 0.99)", colExpr), nil
	case AggCount:
		return fmt.Sprintf("COUNT(%s)", colExpr), nil
	case AggSum:
		return fmt.Sprintf("SUM(%s)", colExpr), nil
	case AggAvg:
		return fmt.Sprintf("AVG(%s)", colExpr), nil
	case AggMin:
		return fmt.Sprintf("MIN(%s)", colExpr), nil
	case AggMax:
		return fmt.Sprintf("MAX(%s)", colExpr), nil
	default:
		return "", fmt.Errorf("dsl: unhandled aggregation function %q", fn)
	}
}

// filterOpSQL maps FilterOp names to SQL operator symbols.
var filterOpSQL = map[FilterOp]string{
	OpEq:  "=",
	OpNeq: "!=",
	OpGt:  ">",
	OpGte: ">=",
	OpLt:  "<",
	OpLte: "<=",
}

// filterExprForField resolves a field name to its SQL representation for use in
// WHERE clauses. For duration_ms, it emits the EPOCH_MS expression.
func filterExprForField(field string) (string, error) {
	if isDurationMs(field) {
		return durationMsExpr(), nil
	}
	if _, ok := spanColumns[field]; !ok {
		return "", fmt.Errorf("dsl: unknown field %q", field)
	}
	return field, nil
}

// aliasFor extracts the column alias from a select clause. If the clause
// contains " AS <alias>", returns the alias; otherwise returns the full
// clause verbatim.
func aliasFor(clause string) string {
	if idx := strings.Index(clause, " AS "); idx >= 0 {
		return clause[idx+4:]
	}
	return clause
}

// groupByFieldExpr returns the SQL expression for a group-by field, applying
// date_trunc when a truncation unit is specified.
func groupByFieldExpr(field string, trunc TruncUnit) string {
	if trunc != "" {
		return fmt.Sprintf("date_trunc('%s', %s)", trunc, field)
	}
	return field
}

// Compile translates a Query into a parameterized DuckDB SQL string and its
// positional arguments. projectID is always injected as a mandatory filter.
// Raw SQL is never accepted from clients; all field and operator references
// are validated against allowlists before emission.
func Compile(projectID string, q Query) (sql string, args []any, err error) {
	// Validate and build the SELECT clause (aggregations).
	selectClauses := make([]string, 0, len(q.Aggregations)+len(q.GroupBy))
	for _, agg := range q.Aggregations {
		expr, err := aggExprForField(agg.Function, agg.Field)
		if err != nil {
			return "", nil, err
		}
		if agg.Alias != "" {
			selectClauses = append(selectClauses, fmt.Sprintf("%s AS %s", expr, agg.Alias))
		} else {
			selectClauses = append(selectClauses, expr)
		}
	}

	// Group-by columns must appear in the SELECT clause for SQL validity.
	for _, gb := range q.GroupBy {
		if _, ok := spanColumns[gb.Field]; !ok {
			return "", nil, fmt.Errorf("dsl: unknown group-by field %q", gb.Field)
		}
		if _, ok := validTruncUnits[gb.Truncate]; !ok && gb.Truncate != "" {
			return "", nil, fmt.Errorf("dsl: unknown truncation unit %q", gb.Truncate)
		}
		selectClauses = append(selectClauses, groupByFieldExpr(gb.Field, gb.Truncate))
	}

	// Default: SELECT COUNT(*) if no aggregations and no group-by.
	if len(selectClauses) == 0 {
		selectClauses = append(selectClauses, "COUNT(*) AS count")
	}

	// Validate order-by fields: must reference an aggregation alias, a span
	// column, or a group-by field.
	for _, ob := range q.OrderBy {
		isAlias := false
		for _, agg := range q.Aggregations {
			if agg.Alias == ob.Field {
				isAlias = true
				break
			}
		}
		if !isAlias && !isValidOrderByField(ob.Field, q.GroupBy) {
			return "", nil, fmt.Errorf("dsl: unknown order-by field %q", ob.Field)
		}
	}

	// Build the WHERE clause.
	// project_id is always injected from the session — never from client input.
	whereClauses := []string{"project_id = ?"}
	args = []any{projectID}

	if !q.From.IsZero() && !q.To.IsZero() {
		whereClauses = append(whereClauses, "start_time >= ? AND start_time <= ?")
		args = append(args, q.From, q.To)
	}

	for _, f := range q.Filters {
		expr, err := filterExprForField(f.Field)
		if err != nil {
			return "", nil, err
		}
		if _, ok := validFilterOps[f.Op]; !ok {
			return "", nil, fmt.Errorf("dsl: unknown operator %q", f.Op)
		}
		if f.Value == nil {
			return "", nil, fmt.Errorf("dsl: filter value must not be nil")
		}

		opSymbol := filterOpSQL[f.Op]
		if f.Op == OpIn {
			// IN operator: value must be a slice.
			slice, ok := f.Value.([]any)
			if !ok {
				return "", nil, fmt.Errorf("dsl: in operator requires a []any value")
			}
			if len(slice) == 0 {
				whereClauses = append(whereClauses, fmt.Sprintf("%s IS NULL", expr))
			} else {
				placeholders := make([]string, len(slice))
				for i := range slice {
					placeholders[i] = "?"
					args = append(args, slice[i])
				}
				whereClauses = append(whereClauses, fmt.Sprintf("%s IN (%s)", expr, strings.Join(placeholders, ", ")))
			}
		} else {
			whereClauses = append(whereClauses, fmt.Sprintf("%s %s ?", expr, opSymbol))
			args = append(args, f.Value)
		}
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "\n  WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Build the GROUP BY clause.
	groupSQL := ""
	if len(q.GroupBy) > 0 {
		groupParts := make([]string, len(q.GroupBy))
		for i, gb := range q.GroupBy {
			groupParts[i] = groupByFieldExpr(gb.Field, gb.Truncate)
		}
		groupSQL = "\n  GROUP BY " + strings.Join(groupParts, ", ")
	}

	// Build the ORDER BY clause.
	orderParts := make([]string, 0, len(q.OrderBy))
	for _, ob := range q.OrderBy {
		dir := "ASC"
		if ob.Desc {
			dir = "DESC"
		}
		orderParts = append(orderParts, fmt.Sprintf("%s %s", ob.Field, dir))
	}

	limitClause := ""
	if q.Limit > 0 {
		limitClause = fmt.Sprintf("\n  LIMIT %d", q.Limit)
	}

	// --- Build hot + cold UNION ---
	selectSQL := strings.Join(selectClauses, ", ")

	// Hot side: local DuckDB snapshot.
	hotSide := fmt.Sprintf(
		"SELECT %s FROM spans%s%s",
		selectSQL, whereSQL, groupSQL,
	)

	// Cold side: no-op stub (zero rows) that mirrors the column aliases.
	coldSide := buildColdSide(selectClauses)

	// Outer query wraps the UNION and projects the inner results.
	outerSelects := make([]string, len(selectClauses))
	for i, sc := range selectClauses {
		outerSelects[i] = aliasFor(sc)
	}
	outerSQL := fmt.Sprintf(
		"SELECT %s FROM (\n%s\nUNION ALL\n%s\n) AS _unioned",
		strings.Join(outerSelects, ", "),
		hotSide,
		coldSide,
	)

	if len(orderParts) > 0 {
		outerSQL += "\n  ORDER BY " + strings.Join(orderParts, ", ")
	} else if len(q.GroupBy) > 0 {
		// Default ordering for grouped queries: order by first group-by expression.
		outerSQL += "\n  ORDER BY " + groupByFieldExpr(q.GroupBy[0].Field, q.GroupBy[0].Truncate) + " ASC"
	}
	outerSQL += limitClause

	return outerSQL, args, nil
}

// isValidOrderByField reports whether a field is valid in an ORDER BY clause.
// It checks group-by fields and span table columns.
func isValidOrderByField(field string, groupBy []GroupByField) bool {
	for _, gb := range groupBy {
		if gb.Field == field {
			return true
		}
	}
	if _, ok := spanColumns[field]; ok {
		return true
	}
	return false
}

// buildColdSide constructs the no-op cold side of the UNION.
// It mirrors the column aliases of the select clause with CAST(NULL AS VARCHAR).
func buildColdSide(selectClauses []string) string {
	aliases := make([]string, len(selectClauses))
	for i, sc := range selectClauses {
		aliases[i] = aliasFor(sc)
	}

	// Build the VALUES clause with CAST(NULL AS VARCHAR) for each column.
	values := make([]string, len(selectClauses))
	for i := range selectClauses {
		values[i] = "CAST(NULL AS VARCHAR)"
	}

	return fmt.Sprintf(
		"SELECT %s FROM (VALUES (%s) LIMIT 0) AS t(%s)",
		strings.Join(aliases, ", "),
		strings.Join(values, ", "),
		strings.Join(aliases, ", "),
	)
}

// Filter is a single predicate in the analytics DSL.
type Filter struct {
	Field string   `json:"field"`
	Op    FilterOp `json:"op"`
	Value any      `json:"value"`
}

// GroupByField is a structured group-by clause. If Truncate is set the
// compiler emits date_trunc(Truncate, Field) rather than the bare column.
type GroupByField struct {
	Field    string    `json:"field"`
	Truncate TruncUnit `json:"truncate,omitempty"`
}

// Aggregation specifies a metric to compute. duration_ms is a virtual field
// compiled to EPOCH_MS(end_time) - EPOCH_MS(start_time).
type Aggregation struct {
	Function AggFunc `json:"function"`
	Field    string  `json:"field"`
	Alias    string  `json:"alias"`
}

// OrderClause specifies sort direction for an output column.
type OrderClause struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc"`
}

// Query is the structured analytics query accepted from clients.
// From and To are absolute UTC timestamps resolved by the caller before
// sending — the server never interprets relative time strings.
type Query struct {
	From         time.Time      `json:"from"`
	To           time.Time      `json:"to"`
	Filters      []Filter       `json:"filters"`
	Aggregations []Aggregation  `json:"aggregations"`
	GroupBy      []GroupByField `json:"group_by"`
	OrderBy      []OrderClause  `json:"order_by"`
	Limit        int            `json:"limit"`
}
