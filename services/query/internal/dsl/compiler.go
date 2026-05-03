package dsl

// Query is the structured analytics query accepted from clients.
type Query struct {
	Filters     []Filter
	Aggregations []Aggregation
	GroupBy     []string
	OrderBy     []OrderClause
	Limit       int
}

// Filter is a single predicate in the analytics DSL.
type Filter struct {
	Field    string
	Operator string
	Value    any
}

// Aggregation specifies a metric to compute.
type Aggregation struct {
	Function string
	Field    string
	Alias    string
}

// OrderClause specifies sort direction for a field.
type OrderClause struct {
	Field string
	Desc  bool
}

// Compile translates a Query into a parameterized DuckDB SQL string.
// projectID is always injected as a mandatory filter; raw SQL is never
// accepted from clients.
func Compile(projectID string, q Query) (sql string, args []any, err error) {
	panic("not implemented")
}
