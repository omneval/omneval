// Command quack_spike4 checks whether quack_serve() accepts a pre-set token
// (e.g. `token => '...'`) or only returns an auto-generated one.
// Run with: go run ./internal/lake/quack_spike4
package main

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/duckdb/duckdb-go/v2"
	_ "github.com/omneval/omneval/internal/duckdbfix"
)

func main() {
	ctx := context.Background()
	db, _ := sql.Open("duckdb", "")
	db.ExecContext(ctx, "INSTALL quack")
	db.ExecContext(ctx, "LOAD quack")
	rows, err := db.QueryContext(ctx, "SELECT * FROM quack_serve('quack://localhost:9498', token => 'mytoken')")
	if err != nil {
		fmt.Println("with token kwarg: ERROR:", err)
	} else {
		fmt.Println("with token kwarg: OK")
		rows.Close()
	}
}
