// Command quack_spike2 is a throwaway program for issue #105: it tests how
// the Quack Server's underlying catalog persistence works.
//
// The #111 spike (internal/lake/quack_spike) proved that the SERVER side of
// `quack_serve()` is a PLAIN DuckDB session — it does NOT itself ATTACH a
// DuckLake catalog. Clients attach via `ducklake:quack:<host>:<port>` and
// DuckLake's "quack" catalog driver talks to that plain session.
//
// But the #111 spike's server session was `sql.Open("duckdb", "")` — an
// IN-MEMORY database. For #105 we need the Quack Server's catalog state to
// be DURABLE (Postgres in prod, a local file in demo). This spike asks:
//
//  1. If the Quack Server opens its underlying session against a persistent
//     DuckDB FILE (sql.Open("duckdb", "/path/to/catalog.db")) instead of an
//     in-memory database, does the DuckLake catalog metadata that a
//     `ducklake:quack:` client creates (CREATE TABLE lake.spans, INSERT,
//     etc.) actually get persisted into that file? I.e. does DuckLake's
//     "quack" catalog driver create its catalog schema/tables INSIDE the
//     server's default/main database via RPC, the same way it would create
//     them inside a postgres catalog DSN via the "postgres" catalog driver?
//
//  2. After stopping the first server process (closing serverDB) and
//     starting a NEW server process pointed at the SAME catalog file, does
//     a fresh client see the previously-committed data? This simulates a
//     Quack Server restart / redeploy.
//
//  3. Can the Quack Server's underlying session ALSO have the "postgres"
//     extension loaded and BE a postgres-attached database (i.e. does
//     quack_serve() work when run against an ATTACHed postgres database
//     as the default, vs. the top-level in-memory "memory" database)?
//     This tells us whether CatalogDriverPostgres can be wired straight
//     into the Quack Server's session.
//
// Run with: go run ./internal/lake/quack_spike2
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

func startServer(ctx context.Context, catalogPath, port string) (*sql.DB, chan string) {
	serverDB, err := sql.Open("duckdb", catalogPath)
	if err != nil {
		log.Fatal(err)
	}
	must("SERVER", serverDB, ctx, "INSTALL quack")
	must("SERVER", serverDB, ctx, "LOAD quack")

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
		if err := rows.Err(); err != nil {
			fmt.Printf("quack_serve rows error: %v\n", err)
		}
	}()

	time.Sleep(750 * time.Millisecond)
	return serverDB, tokenCh
}

func attachClient(ctx context.Context, label, port, token, dataPath string) *sql.DB {
	client, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	must(label, client, ctx, "INSTALL ducklake")
	must(label, client, ctx, "LOAD ducklake")
	must(label, client, ctx, "INSTALL quack")
	must(label, client, ctx, "LOAD quack")
	must(label, client, ctx, fmt.Sprintf("CREATE SECRET quack_auth (TYPE quack, TOKEN '%s')", token))
	attachStmt := fmt.Sprintf("ATTACH 'ducklake:quack:localhost:%s' AS lake (DATA_PATH '%s')", port, dataPath)
	if err := tryExec(label, client, ctx, attachStmt); err != nil {
		log.Fatal(err)
	}
	return client
}

func main() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "quackspike2")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		log.Fatal(err)
	}
	catalogPath := filepath.Join(dir, "catalog.db")

	const port = "9496"

	// --- Phase 1: start a server with a PERSISTENT catalog file (not
	// in-memory). Attach a client, create a table, insert rows. ---
	fmt.Println("=== Phase 1: server with persistent catalog file ===")
	serverDB1, tokenCh1 := startServer(ctx, catalogPath, port)
	token := <-tokenCh1
	fmt.Printf("token: %s\n", token)

	client1 := attachClient(ctx, "CLIENT-1", port, token, data)
	must("CLIENT-1", client1, ctx, "CREATE TABLE IF NOT EXISTS lake.spans (id INT, project_id VARCHAR)")
	must("CLIENT-1", client1, ctx, "INSERT INTO lake.spans VALUES (1, 'proj-a'), (2, 'proj-b')")
	var n int
	if err := client1.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		fmt.Printf("CLIENT-1 count: ERROR: %v\n", err)
	} else {
		fmt.Printf("CLIENT-1 count after insert: %d\n", n)
	}

	// Close client and server (simulate Quack Server shutdown).
	client1.Close()
	serverDB1.Close()
	fmt.Println("server1 + client1 closed")

	// Inspect the catalog file directly: does it contain DuckLake metadata
	// tables now?
	fmt.Println("=== Phase 1b: inspect catalog file directly (no quack) ===")
	inspectDB, err := sql.Open("duckdb", catalogPath)
	if err != nil {
		log.Fatal(err)
	}
	rows, err := inspectDB.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		fmt.Printf("inspect SHOW TABLES: ERROR: %v\n", err)
	} else {
		for rows.Next() {
			var name string
			rows.Scan(&name)
			fmt.Printf("catalog file table: %s\n", name)
		}
		rows.Close()
	}
	inspectDB.Close()

	// --- Phase 2: start a NEW server process against the SAME catalog
	// file/port. Does a fresh client see Phase 1's data? ---
	fmt.Println("=== Phase 2: restart server against same catalog file ===")
	serverDB2, tokenCh2 := startServer(ctx, catalogPath, port)
	token2 := <-tokenCh2
	if token2 == "" {
		token2 = token
	}
	fmt.Printf("token2: %s\n", token2)

	client2 := attachClient(ctx, "CLIENT-2", port, token2, data)
	if err := client2.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		fmt.Printf("CLIENT-2 count: ERROR: %v\n", err)
	} else if n == 2 {
		fmt.Println("RESULT: catalog persisted across server restart (2 rows)")
	} else {
		fmt.Printf("RESULT: catalog NOT persisted, got %d rows (want 2)\n", n)
	}

	client2.Close()
	serverDB2.Close()
	fmt.Println("spike2 done")
}
