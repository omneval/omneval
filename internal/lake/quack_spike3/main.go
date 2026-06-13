// Command quack_spike3 is a throwaway program for issue #105: it tests
// whether the Quack Server's underlying session can be backed by an
// ATTACHed Postgres database (for the prod CatalogDriverPostgres case),
// the same way quack_spike2 proved a local DuckDB file works.
//
// Approach: open a plain DuckDB session, INSTALL/LOAD postgres, ATTACH the
// Postgres DSN as the default database (or use it directly), then
// INSTALL/LOAD quack and call quack_serve(). A client then attaches via
// ducklake:quack:<host>:<port> and creates a table — does DuckLake's quack
// catalog driver create its ducklake_* metadata tables INSIDE the attached
// Postgres database?
//
// Requires a reachable Postgres. Set TEST_PG_DSN env var, e.g.:
//
//	TEST_PG_DSN="host=localhost port=5432 user=postgres password=postgres dbname=quackcatalog"
//
// If TEST_PG_DSN is unset, the spike exits early with instructions.
//
// Run with: go run ./internal/lake/quack_spike3
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	_ "github.com/omneval/omneval/internal/duckdbfix"
)

func must(label string, db *sql.DB, ctx context.Context, stmt string) {
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		fmt.Printf("%s %s: ERROR: %v\n", label, stmt, err)
		os.Exit(1)
	}
	fmt.Printf("%s %s: OK\n", label, stmt)
}

func tryExec(label string, db *sql.DB, ctx context.Context, stmt string) error {
	_, err := db.ExecContext(ctx, stmt)
	if err != nil {
		fmt.Printf("%s %s: ERROR: %v\n", label, stmt, err)
		return err
	}
	fmt.Printf("%s %s: OK\n", label, stmt)
	return nil
}

func main() {
	ctx := context.Background()

	pgDSN := os.Getenv("TEST_PG_DSN")
	if pgDSN == "" {
		fmt.Println("TEST_PG_DSN not set — skipping. Example:")
		fmt.Println(`  TEST_PG_DSN="host=localhost port=5432 user=postgres password=postgres dbname=quackcatalog"`)
		return
	}

	dir, err := os.MkdirTemp("", "quackspike3")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		log.Fatal(err)
	}

	const port = "9497"

	// --- Server: plain session, attach postgres as default db, then serve quack. ---
	serverDB, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	must("SERVER", serverDB, ctx, "INSTALL postgres")
	must("SERVER", serverDB, ctx, "LOAD postgres")
	must("SERVER", serverDB, ctx, "INSTALL quack")
	must("SERVER", serverDB, ctx, "LOAD quack")

	// Attach postgres DB and try to USE it as the default so quack_serve's
	// RPC operations land there.
	attachStmt := fmt.Sprintf("ATTACH '%s' AS pgcatalog (TYPE postgres)", strings.ReplaceAll(pgDSN, "'", "''"))
	if err := tryExec("SERVER", serverDB, ctx, attachStmt); err != nil {
		log.Fatal(err)
	}
	if err := tryExec("SERVER", serverDB, ctx, "USE pgcatalog"); err != nil {
		log.Fatal(err)
	}

	tokenCh := make(chan string, 1)
	go func() {
		rows, err := serverDB.QueryContext(ctx,
			fmt.Sprintf("SELECT * FROM quack_serve('quack://localhost:%s')", port))
		if err != nil {
			fmt.Printf("quack_serve query error: %v\n", err)
			tokenCh <- ""
			return
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		fmt.Printf("quack_serve cols: %v\n", cols)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			fmt.Printf("quack_serve row: %v\n", vals)
			if len(vals) >= 3 {
				if s, ok := vals[2].(string); ok {
					tokenCh <- s
					continue
				}
			}
		}
	}()

	time.Sleep(750 * time.Millisecond)
	token := <-tokenCh

	// --- Client: attach via ducklake:quack:, create table, insert. ---
	client, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	must("CLIENT", client, ctx, "INSTALL ducklake")
	must("CLIENT", client, ctx, "LOAD ducklake")
	must("CLIENT", client, ctx, "INSTALL quack")
	must("CLIENT", client, ctx, "LOAD quack")
	must("CLIENT", client, ctx, fmt.Sprintf("CREATE SECRET quack_auth (TYPE quack, TOKEN '%s')", token))
	attachClientStmt := fmt.Sprintf("ATTACH 'ducklake:quack:localhost:%s' AS lake (DATA_PATH '%s')", port, data)
	if err := tryExec("CLIENT", client, ctx, attachClientStmt); err != nil {
		log.Fatal(err)
	}

	must("CLIENT", client, ctx, "CREATE TABLE IF NOT EXISTS lake.spans (id INT, project_id VARCHAR)")
	must("CLIENT", client, ctx, "INSERT INTO lake.spans VALUES (1, 'proj-a'), (2, 'proj-b')")

	var n int
	if err := client.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		fmt.Printf("CLIENT count: ERROR: %v\n", err)
	} else {
		fmt.Printf("CLIENT count after insert: %d\n", n)
	}

	client.Close()
	serverDB.Close()

	// --- Inspect Postgres directly for ducklake_* tables. ---
	pgDB, err := sql.Open("pgx", pgDSN)
	if err == nil {
		rows, err := pgDB.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_name LIKE 'ducklake_%'")
		if err != nil {
			fmt.Printf("inspect postgres: ERROR: %v\n", err)
		} else {
			for rows.Next() {
				var name string
				rows.Scan(&name)
				fmt.Printf("postgres ducklake table: %s\n", name)
			}
			rows.Close()
		}
		pgDB.Close()
	} else {
		fmt.Printf("open pgx: %v (skipping direct inspection)\n", err)
	}

	fmt.Println("spike3 done")
}
