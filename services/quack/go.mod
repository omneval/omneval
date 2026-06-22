module github.com/omneval/omneval/services/quack

go 1.25.0

require (
	github.com/duckdb/duckdb-go/v2 v2.10503.1
	github.com/omneval/omneval/internal v0.0.0
	go.uber.org/automaxprocs v1.6.0
)

replace github.com/omneval/omneval/internal => ../../internal
