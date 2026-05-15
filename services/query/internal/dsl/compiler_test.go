package dsl

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func makeQuery(opts ...func(*Query)) Query {
	q := Query{
		From:         time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:           time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters:      []Filter{},
		Aggregations: []Aggregation{},
		GroupBy:      []GroupByField{},
		OrderBy:      []OrderClause{},
		Limit:        0,
	}
	for _, o := range opts {
		o(&q)
	}
	return q
}

// --- Compile: basic ---

func TestCompile_WithoutAggregation(t *testing.T) {
	q := makeQuery()
	sql, args, err := Compile("proj-abc", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if sql == "" {
		t.Fatal("expected non-empty SQL")
	}

	// project_id is injected
	if !strings.Contains(sql, "project_id = ?") {
		t.Error("expected project_id = ? in SQL")
	}
	// first arg should be project ID
	if len(args) == 0 || args[0] != "proj-abc" {
		t.Errorf("first arg should be proj-abc, got %v", args[0])
	}

	// Time range
	if !strings.Contains(sql, "start_time >= ?") {
		t.Error("expected start_time >= ?")
	}
	if !strings.Contains(sql, "start_time <= ?") {
		t.Error("expected start_time <= ?")
	}

	// Always emits UNION ALL
	if !strings.Contains(sql, "UNION ALL") {
		t.Error("expected UNION ALL in SQL")
	}
}

func TestCompile_ProjectIDInjectedRegardlessOfClientInput(t *testing.T) {
	q := makeQuery(
		withFilter(Filter{Field: "project_id", Op: OpEq, Value: "hacker-proj"}),
	)
	sql, args, err := Compile("real-proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	// The compiler MUST inject the real project_id.
	// The client's filter on project_id should be rejected.
	if !strings.Contains(sql, "project_id = ?") {
		t.Fatal("expected project_id = ?")
	}
	if len(args) == 0 || args[0] != "real-proj" {
		t.Errorf("first arg should be real-proj, got %v", args[0])
	}
}

func TestCompile_ColdSideStub(t *testing.T) {
	q := makeQuery()
	sql, _, err := Compile("proj-abc", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	// Cold side must use CAST(NULL ...) pattern.
	if !strings.Contains(sql, "CAST(NULL AS") {
		t.Error("expected cold side stub with CAST(NULL AS...)")
	}
}

// --- Filters ---

func TestCompile_FilterModelEq(t *testing.T) {
	q := makeQuery(withFilter(Filter{Field: "model", Op: OpEq, Value: "gpt-4"}))
	sql, args, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "model = ?") {
		t.Error("expected model = ?")
	}
	// Args: project_id + 2 time + model = 4
	if len(args) < 4 {
		t.Errorf("expected at least 4 args, got %d", len(args))
	}
}

func TestCompile_FilterModelIn(t *testing.T) {
	q := makeQuery(withFilter(Filter{Field: "model", Op: OpIn, Value: []any{"gpt-4", "claude"}}))
	sql, args, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "model IN") {
		t.Error("expected model IN")
	}
	if len(args) < 5 {
		t.Errorf("expected at least 5 args (proj + 2 time + 2 in), got %d", len(args))
	}
}

func TestCompile_FilterUnknownFieldRejected(t *testing.T) {
	q := makeQuery(withFilter(Filter{Field: "nonexistent", Op: OpEq, Value: "x"}))
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestCompile_FilterUnknownOpRejected(t *testing.T) {
	q := makeQuery(withFilter(Filter{Field: "model", Op: FilterOp("regex"), Value: ".*"}))
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for unknown operator")
	}
}

func TestCompile_AllFilterOperators(t *testing.T) {
	// Non-IN operators use a single value.
	for _, op := range []FilterOp{OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte} {
		q := makeQuery(withFilter(Filter{Field: "model", Op: op, Value: "x"}))
		_, _, err := Compile("proj", q)
		if err != nil {
			t.Errorf("operator %q should be valid: %v", op, err)
		}
	}
	// IN operator requires a slice value.
	q := makeQuery(withFilter(Filter{Field: "model", Op: OpIn, Value: []any{"x"}}))
	_, _, err := Compile("proj", q)
	if err != nil {
		t.Errorf("operator %q should be valid: %v", OpIn, err)
	}
}

func TestCompile_FilterValueNil(t *testing.T) {
	q := makeQuery(withFilter(Filter{Field: "model", Op: OpEq, Value: nil}))
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for nil filter value")
	}
}

// --- Aggregations ---

func TestCompile_AggSum(t *testing.T) {
	q := makeQuery(
		withAgg(AggSum, "cost_usd", "total_cost"),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "SUM(cost_usd)") {
		t.Error("expected SUM(cost_usd)")
	}
	if !strings.Contains(sql, "AS total_cost") {
		t.Error("expected AS total_cost alias")
	}
}

func TestCompile_AggCount(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "span_count"),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "COUNT(*)") {
		t.Error("expected COUNT(*)")
	}
	if !strings.Contains(sql, "AS span_count") {
		t.Error("expected AS span_count alias")
	}
}

func TestCompile_AggAvg(t *testing.T) {
	q := makeQuery(withAgg(AggAvg, "input_tokens", "avg_input"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "AVG(input_tokens)") {
		t.Error("expected AVG(input_tokens)")
	}
}

func TestCompile_AggMin(t *testing.T) {
	q := makeQuery(withAgg(AggMin, "output_tokens", "min_output"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "MIN(output_tokens)") {
		t.Error("expected MIN(output_tokens)")
	}
}

func TestCompile_AggMax(t *testing.T) {
	q := makeQuery(withAgg(AggMax, "cost_usd", "max_cost"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "MAX(cost_usd)") {
		t.Error("expected MAX(cost_usd)")
	}
}

func TestCompile_AggUnknownFuncRejected(t *testing.T) {
	q := makeQuery(withAgg(AggFunc("median"), "cost_usd", "m"))
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for unknown aggregation function")
	}
}

// TestCompile_AggUnknownFuncErrorMessage verifies that the error message
// for an unknown aggregation function includes the list of accepted functions
// and hints about the "fn" vs "function" field name.
func TestCompile_AggUnknownFuncErrorMessage(t *testing.T) {
	q := makeQuery(withAgg(AggFunc("median"), "cost_usd", "m"))
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Fatal("expected error for unknown aggregation function")
	}
	errMsg := err.Error()
	// Should list accepted functions
	accepted := []string{"sum", "count", "avg", "min", "max", "p50", "p95", "p99"}
	for _, fn := range accepted {
		if !strings.Contains(errMsg, fn) {
			t.Errorf("error message should mention accepted function %q, got: %s", fn, errMsg)
		}
	}
	// Should hint about the correct field name
	if !strings.Contains(errMsg, "function") {
		t.Errorf("error message should hint about 'function' field name, got: %s", errMsg)
	}
}

// TestCompile_AggFnAliasDeserialization verifies that the "fn" key is
// accepted as an alias for "function" in the JSON request body.
func TestCompile_AggFnAliasDeserialization(t *testing.T) {
	jsonStr := `{"aggregations":[{"fn":"count","field":"*","alias":"total"}]}`
	var req Query
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if len(req.Aggregations) != 1 {
		t.Fatalf("expected 1 aggregation, got %d", len(req.Aggregations))
	}
	if req.Aggregations[0].Function != AggCount {
		t.Errorf("expected function count, got %q", req.Aggregations[0].Function)
	}
}

// TestCompile_AggFnAliasEndToEnd verifies that sending "fn" in the request
// produces valid SQL (end-to-end through Compile).
func TestCompile_AggFnAliasEndToEnd(t *testing.T) {
	jsonStr := `{"aggregations":[{"fn":"count","field":"*","alias":"total"}]}`
	var req Query
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	sql, _, err := Compile("proj", req)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	if !strings.Contains(sql, "COUNT(*)") {
		t.Errorf("expected COUNT(*) in SQL, got:\n%s", sql)
	}
}

// TestCompile_AggFunctionPreferredOverFn verifies that if both "function"
// and "fn" are provided, "function" takes precedence.
func TestCompile_AggFunctionPreferredOverFn(t *testing.T) {
	jsonStr := `{"aggregations":[{"fn":"sum","function":"count","field":"*","alias":"total"}]}`
	var req Query
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if req.Aggregations[0].Function != AggCount {
		t.Errorf(`expected function count ("function" wins over "fn"), got %q`, req.Aggregations[0].Function)
	}
}

func TestCompile_AggUnknownFieldRejected(t *testing.T) {
	q := makeQuery(withAgg(AggSum, "nonexistent", "m"))
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for unknown aggregation field")
	}
}

// --- duration_ms virtual field ---

func TestCompile_AggDurationMs(t *testing.T) {
	q := makeQuery(withAgg(AggSum, "duration_ms", "total_duration"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	expected := "EPOCH_MS(end_time) - EPOCH_MS(start_time)"
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SQL, got:\n%s", expected, sql)
	}
}

func TestCompile_AggAvgDurationMs(t *testing.T) {
	q := makeQuery(withAgg(AggAvg, "duration_ms", "avg_duration"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	expected := "AVG(EPOCH_MS(end_time) - EPOCH_MS(start_time))"
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SQL, got:\n%s", expected, sql)
	}
}

func TestCompile_FilterDurationMs(t *testing.T) {
	// duration_ms can also appear in filters — should compile to the expression.
	q := makeQuery(withFilter(Filter{Field: "duration_ms", Op: OpGt, Value: 1000}))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	expected := "EPOCH_MS(end_time) - EPOCH_MS(start_time) > ?"
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SQL, got:\n%s", expected, sql)
	}
}

// --- Percentile aggregations ---

func TestCompile_AggP50(t *testing.T) {
	q := makeQuery(withAgg(AggP50, "duration_ms", "p50_duration"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	expected := "APPROX_QUANTILE(EPOCH_MS(end_time) - EPOCH_MS(start_time), 0.50)"
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SQL, got:\n%s", expected, sql)
	}
}

func TestCompile_AggP95(t *testing.T) {
	q := makeQuery(withAgg(AggP95, "cost_usd", "p95_cost"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	expected := "APPROX_QUANTILE(cost_usd, 0.95)"
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SQL, got:\n%s", expected, sql)
	}
}

func TestCompile_AggP99(t *testing.T) {
	q := makeQuery(withAgg(AggP99, "input_tokens", "p99_input"))
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	expected := "APPROX_QUANTILE(input_tokens, 0.99)"
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SQL, got:\n%s", expected, sql)
	}
}

// --- Group by ---

func TestCompile_GroupByHour(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("start_time", TruncHour),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "date_trunc") {
		t.Error("expected date_trunc for group_by")
	}
	if !strings.Contains(sql, "'hour'") {
		t.Error("expected 'hour' in date_trunc")
	}
}

func TestCompile_GroupByDay(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("start_time", TruncDay),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "'day'") {
		t.Error("expected 'day' in date_trunc")
	}
}

func TestCompile_GroupByWeek(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("start_time", TruncWeek),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "'week'") {
		t.Error("expected 'week' in date_trunc")
	}
}

func TestCompile_GroupByMonth(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("start_time", TruncMonth),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "'month'") {
		t.Error("expected 'month' in date_trunc")
	}
}

func TestCompile_GroupByRawField(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("model", ""),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	// Should reference model column directly (no date_trunc).
	// The select should contain model as a raw field in GROUP BY.
	if !strings.Contains(sql, "model") {
		t.Error("expected model column in GROUP BY")
	}
}

func TestCompile_GroupByUnknownFieldRejected(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("nonexistent", ""),
	)
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for unknown group-by field")
	}
}

func TestCompile_GroupByUnknownTruncRejected(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("start_time", TruncUnit("year")),
	)
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for unknown trunc unit")
	}
}

// --- Order by ---

func TestCompile_OrderByField(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withOrderBy("count", false),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "count ASC") {
		t.Error("expected count ASC in ORDER BY")
	}
}

func TestCompile_OrderByDesc(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withOrderBy("count", true),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "count DESC") {
		t.Error("expected count DESC in ORDER BY")
	}
}

func TestCompile_OrderByUnknownFieldRejected(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withOrderBy("nonexistent", false),
	)
	_, _, err := Compile("proj", q)
	if err == nil {
		t.Error("expected error for unknown order-by field")
	}
}

func TestCompile_OrderByAggAlias(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "span_count"),
		withOrderBy("span_count", true),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	// Order by should reference the alias.
	if !strings.Contains(sql, "span_count DESC") {
		t.Error("expected span_count DESC in ORDER BY")
	}
}

// --- Limit ---

func TestCompile_Limit(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withLimit(10),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "LIMIT 10") {
		t.Error("expected LIMIT 10")
	}
}

// --- Multiple aggregations ---

func TestCompile_MultipleAggregations(t *testing.T) {
	q := makeQuery(
		withAgg(AggSum, "cost_usd", "total_cost"),
		withAgg(AggCount, "*", "count"),
		withAgg(AggAvg, "input_tokens", "avg_input"),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !strings.Contains(sql, "SUM(cost_usd)") || !strings.Contains(sql, "COUNT(*)") || !strings.Contains(sql, "AVG(input_tokens)") {
		t.Error("expected all three aggregation functions")
	}
}

// --- Parameterization ---

func TestCompile_IsParameterized(t *testing.T) {
	q := makeQuery(
		withFilter(Filter{Field: "model", Op: OpEq, Value: "gpt-4"}),
		withAgg(AggSum, "cost_usd", "total"),
		withLimit(42),
	)
	sql, args, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	// Should use ? placeholders, not literal values.
	if strings.Contains(sql, "gpt-4") {
		t.Error("SQL should not contain literal filter value")
	}
	// Args should hold the values.
	if len(args) < 1 {
		t.Error("expected at least one arg")
	}
	// LIMIT is embedded directly in the SQL (compile-time constant).
	if !strings.Contains(sql, "LIMIT 42") {
		t.Error("expected LIMIT 42 in SQL")
	}
}

// --- Helpers ---

func withFilter(f Filter) func(*Query) {
	return func(q *Query) {
		q.Filters = append(q.Filters, f)
	}
}

func withAgg(fn AggFunc, field, alias string) func(*Query) {
	return func(q *Query) {
		q.Aggregations = append(q.Aggregations, Aggregation{Function: fn, Field: field, Alias: alias})
	}
}

func withGroupBy(field string, trunc TruncUnit) func(*Query) {
	return func(q *Query) {
		q.GroupBy = append(q.GroupBy, GroupByField{Field: field, Truncate: trunc})
	}
}

func withOrderBy(field string, desc bool) func(*Query) {
	return func(q *Query) {
		q.OrderBy = append(q.OrderBy, OrderClause{Field: field, Desc: desc})
	}
}

func withLimit(n int) func(*Query) {
	return func(q *Query) {
		q.Limit = n
	}
}

// --- Dashboard-specific tests (issue #92) ---

// TestCompile_GroupByTruncOrderByRawField verifies that when the group-by
// field uses a truncation unit (e.g. "hour"), an order-by clause referencing
// the raw field name ("start_time" instead of the truncated expression)
// is correctly resolved to the truncated expression.
//
// This is the "Traces over Time" pattern: group by start_time truncated to
// hour, ordered by start_time. The outer query projects the truncated
// expression, so ORDER BY must also use the truncated expression.
func TestCompile_GroupByTruncOrderByRawField(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("start_time", TruncHour),
		withOrderBy("start_time", false),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	// The outer ORDER BY must reference the truncated expression, not the
	// raw field. The outer SELECT aliases the group-by expression directly
	// (no explicit alias), so the column name in the outer query is the
	// full expression.
	if !strings.Contains(sql, "date_trunc('hour', start_time) ASC") {
		t.Errorf("expected ORDER BY date_trunc('hour', start_time) ASC in SQL:\n%s", sql)
	}
}

// TestCompile_GroupByOrderByAggAlias verifies that ordering by an
// aggregation alias works correctly (the existing behavior).
func TestCompile_GroupByOrderByAggAlias(t *testing.T) {
	q := makeQuery(
		withAgg(AggSum, "cost_usd", "total_cost"),
		withGroupBy("model", ""),
		withOrderBy("total_cost", true),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	// The ORDER BY should reference the alias "total_cost".
	if !strings.Contains(sql, "total_cost DESC") {
		t.Errorf("expected ORDER BY total_cost DESC in SQL:\n%s", sql)
	}
}

// TestCompile_GroupByOrderByGroupByFieldNoTrunc verifies that when a
// group-by field has no truncation, the order-by field matching the
// raw group-by field is correctly resolved.
func TestCompile_GroupByOrderByGroupByFieldNoTrunc(t *testing.T) {
	q := makeQuery(
		withAgg(AggCount, "*", "count"),
		withGroupBy("model", ""),
		withOrderBy("model", true),
	)
	sql, _, err := Compile("proj", q)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	// The ORDER BY should reference the model column directly.
	if !strings.Contains(sql, "model DESC") {
		t.Errorf("expected ORDER BY model DESC in SQL:\n%s", sql)
	}
}
