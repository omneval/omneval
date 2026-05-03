package dsl

import "time"

// TruncUnit is an allowlisted time-truncation granularity for GroupBy fields.
type TruncUnit string

const (
	TruncHour  TruncUnit = "hour"
	TruncDay   TruncUnit = "day"
	TruncWeek  TruncUnit = "week"
	TruncMonth TruncUnit = "month"
)

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

// Compile translates a Query into a parameterized DuckDB SQL string and its
// positional arguments. projectID is always injected as a mandatory filter.
// Raw SQL is never accepted from clients; all field and operator references
// are validated against allowlists before emission.
func Compile(projectID string, q Query) (sql string, args []any, err error) {
	panic("not implemented")
}
