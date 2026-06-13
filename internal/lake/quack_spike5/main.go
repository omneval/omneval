// Command quack_spike5 checks the allow_other_hostname kwarg for quack_serve.
// Run with: go run ./internal/lake/quack_spike5
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

	rows, err := db.QueryContext(ctx, "SELECT * FROM quack_serve('quack://0.0.0.0:9499', allow_other_hostname=true)")
	if err != nil {
		fmt.Println("0.0.0.0 with allow_other_hostname=true: ERROR:", err)
		return
	}
	cols, _ := rows.Columns()
	fmt.Println("0.0.0.0 with allow_other_hostname=true: OK, cols:", cols)
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		fmt.Println("row:", vals)
	}
	rows.Close()
}
