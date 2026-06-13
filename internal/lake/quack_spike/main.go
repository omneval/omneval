// Command quack_spike is a throwaway program for issue #111: it tests
// whether DuckLake can use a Quack server as its CATALOG backend — i.e.
// `ATTACH 'ducklake:quack:<host>:<port>' AS lake (DATA_PATH '<dir>')` —
// rather than treating quack:// as a generic remote-table proxy (which the
// earlier version of this spike found does not expose DuckLake tables at
// all: SHOW TABLES FROM lake returned nothing, lake.spans errored "Table
// with name spans does not exist", and PRAGMA database_list errored
// "Not implemented Error: InMemory not implemented yet").
//
// Per DuckDB's 1.5.3 announcement and the Quack docs, the correct pattern
// is: the Quack server just serves a plain DuckDB session (it does NOT
// itself ATTACH the DuckLake catalog as "lake" — it acts as the metadata
// store that DuckLake's "quack:" catalog driver talks to). Clients then run
//
//	ATTACH 'ducklake:quack:<host>:<port>' AS lake (DATA_PATH '<dir>')
//
// analogous to today's `ducklake:postgres:<dsn>`. DATA_PATH is still read
// directly by each client; Quack only carries catalog metadata.
//
// This spike also re-tests the other open #111 blocker: does
// DELETE FROM lake.<table> ... followed by
// CALL ducklake_rewrite_data_files('lake', '<table>') now work when the
// catalog is Quack-backed (vs. the "Not implemented Error: Scanning a
// DuckLake table after the transaction has ended" seen with a direct local
// catalog)?
//
// Run with: go run ./internal/lake/quack_spike
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

func showRows(label string, db *sql.DB, ctx context.Context, query string) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		fmt.Printf("%s %s: ERROR: %v\n", label, query, err)
		return
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	fmt.Printf("%s %s cols: %v\n", label, query, cols)
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			fmt.Printf("%s scan error: %v\n", label, err)
			return
		}
		fmt.Printf("%s row: %v\n", label, vals)
	}
	if err := rows.Err(); err != nil {
		fmt.Printf("%s rows error: %v\n", label, err)
	}
}

func main() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "quackspike")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		log.Fatal(err)
	}

	const port = "9495"
	token := "spike-token"

	// --- Server side: a PLAIN DuckDB session serving quack_serve(). It does
	// NOT attach DuckLake itself — it is only the catalog metadata store. ---
	serverDB, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer serverDB.Close()

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
		fmt.Println("quack_serve goroutine done")
	}()

	// give the server a moment to bind and report its auth token
	time.Sleep(750 * time.Millisecond)
	select {
	case t := <-tokenCh:
		if t != "" {
			token = t
		}
	default:
	}
	fmt.Printf("CLIENT using server-issued token: %s\n", token)

	// --- Client A: attach DuckLake using Quack as the CATALOG backend. ---
	clientA, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer clientA.Close()

	must("CLIENT-A", clientA, ctx, "INSTALL ducklake")
	must("CLIENT-A", clientA, ctx, "LOAD ducklake")
	must("CLIENT-A", clientA, ctx, "INSTALL quack")
	must("CLIENT-A", clientA, ctx, "LOAD quack")

	// The bare "ducklake:quack:host:port?token=..." and TOKEN '...' attach
	// options both failed (see history). Try registering a quack secret on
	// the client first, matching the server's CREATE SECRET ... TYPE quack,
	// then a bare ducklake:quack:host:port ATTACH.
	must("CLIENT-A", clientA, ctx,
		fmt.Sprintf("CREATE SECRET quack_auth (TYPE quack, TOKEN '%s')", token))

	attachStmt := fmt.Sprintf(
		"ATTACH 'ducklake:quack:localhost:%s' AS lake (DATA_PATH '%s')",
		port, data)
	if err := tryExec("CLIENT-A", clientA, ctx, attachStmt); err != nil {
		// try alternate syntaxes if the first form errors
		alt1 := fmt.Sprintf(
			"ATTACH 'ducklake:quack:127.0.0.1:%s' AS lake (DATA_PATH '%s')",
			port, data)
		if err2 := tryExec("CLIENT-A", clientA, ctx, alt1); err2 != nil {
			alt2 := fmt.Sprintf(
				"ATTACH 'ducklake:quack://localhost:%s' AS lake (DATA_PATH '%s')",
				port, data)
			if err3 := tryExec("CLIENT-A", clientA, ctx, alt2); err3 != nil {
				fmt.Println("RESULT: ATTACH 'ducklake:quack:...' failed under all attempted syntaxes")
				return
			}
			attachStmt = alt2
		} else {
			attachStmt = alt1
		}
	}

	// Create a minimal test table and insert rows.
	must("CLIENT-A", clientA, ctx, "CREATE TABLE IF NOT EXISTS lake.spans (id INT, project_id VARCHAR)")
	tryExec("CLIENT-A", clientA, ctx, "ALTER TABLE lake.spans SET PARTITIONED BY (project_id)")
	must("CLIENT-A", clientA, ctx, "INSERT INTO lake.spans VALUES (1, 'proj-a'), (2, 'proj-b'), (3, 'proj-a')")

	showRows("CLIENT-A", clientA, ctx, "PRAGMA database_list")
	showRows("CLIENT-A", clientA, ctx, "SHOW TABLES FROM lake")
	showRows("CLIENT-A", clientA, ctx, "SELECT * FROM lake.spans ORDER BY id")

	// --- Client B: a SEPARATE connection, attaching the SAME way, to
	// confirm it sees Client A's committed data via the shared Quack
	// catalog. ---
	clientB, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer clientB.Close()

	must("CLIENT-B", clientB, ctx, "INSTALL ducklake")
	must("CLIENT-B", clientB, ctx, "LOAD ducklake")
	must("CLIENT-B", clientB, ctx, "INSTALL quack")
	must("CLIENT-B", clientB, ctx, "LOAD quack")
	must("CLIENT-B", clientB, ctx,
		fmt.Sprintf("CREATE SECRET quack_auth (TYPE quack, TOKEN '%s')", token))
	must("CLIENT-B", clientB, ctx, attachStmt)

	showRows("CLIENT-B", clientB, ctx, "SHOW TABLES FROM lake")
	showRows("CLIENT-B", clientB, ctx, "SELECT * FROM lake.spans ORDER BY id")

	var n int
	if err := clientB.QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		fmt.Printf("CLIENT-B count: ERROR: %v\n", err)
	} else if n == 3 {
		fmt.Println("RESULT: CLIENT-B sees CLIENT-A's committed rows via shared ducklake:quack: catalog (3 rows)")
	} else {
		fmt.Printf("RESULT: CLIENT-B sees %d rows (expected 3) via shared ducklake:quack: catalog\n", n)
	}

	// --- THE KEY TEST: DELETE then ducklake_rewrite_data_files. ---
	// Attempt 1: separate auto-committed statements on CLIENT-A.
	fmt.Println("--- Attempt 1: DELETE then rewrite as separate autocommit statements (CLIENT-A) ---")
	if err := tryExec("CLIENT-A", clientA, ctx, "DELETE FROM lake.spans WHERE project_id = 'proj-a'"); err == nil {
		rewriteErr := tryExec("CLIENT-A", clientA, ctx, "CALL ducklake_rewrite_data_files('lake', 'spans')")
		if rewriteErr == nil {
			fmt.Println("RESULT (separate statements): DELETE -> ducklake_rewrite_data_files SUCCEEDED")
		} else {
			fmt.Println("RESULT (separate statements): DELETE -> ducklake_rewrite_data_files FAILED")
		}
	}

	showRows("CLIENT-A", clientA, ctx, "SELECT * FROM lake.spans ORDER BY id")

	// Attempt 2: explicit transaction wrapping both DELETE and rewrite, on a
	// fresh table, using CLIENT-B.
	fmt.Println("--- Attempt 2: DELETE then rewrite inside an explicit transaction (CLIENT-B) ---")
	must("CLIENT-B", clientB, ctx, "CREATE TABLE IF NOT EXISTS lake.spans2 (id INT, project_id VARCHAR)")
	tryExec("CLIENT-B", clientB, ctx, "ALTER TABLE lake.spans2 SET PARTITIONED BY (project_id)")
	must("CLIENT-B", clientB, ctx, "INSERT INTO lake.spans2 VALUES (1, 'proj-a'), (2, 'proj-b'), (3, 'proj-a')")
	tryExec("CLIENT-B", clientB, ctx, "CALL ducklake_flush_inlined_data('lake')")

	tx, err := clientB.BeginTx(ctx, nil)
	if err != nil {
		fmt.Printf("CLIENT-B begin tx: ERROR: %v\n", err)
	} else {
		if _, err := tx.ExecContext(ctx, "DELETE FROM lake.spans2 WHERE project_id = 'proj-a'"); err != nil {
			fmt.Printf("CLIENT-B tx DELETE: ERROR: %v\n", err)
			tx.Rollback()
		} else {
			fmt.Println("CLIENT-B tx DELETE: OK")
			if _, err := tx.ExecContext(ctx, "CALL ducklake_rewrite_data_files('lake', 'spans2')"); err != nil {
				fmt.Printf("CLIENT-B tx rewrite: ERROR: %v\n", err)
				tx.Rollback()
			} else {
				fmt.Println("CLIENT-B tx rewrite: OK")
				if err := tx.Commit(); err != nil {
					fmt.Printf("CLIENT-B tx commit: ERROR: %v\n", err)
				} else {
					fmt.Println("RESULT (explicit transaction): DELETE -> ducklake_rewrite_data_files -> COMMIT SUCCEEDED")
				}
			}
		}
	}

	showRows("CLIENT-B", clientB, ctx, "SELECT * FROM lake.spans2 ORDER BY id")

	// Attempt 3: fresh client (CLIENT-C), new ATTACH, after CLIENT-A's
	// committed DELETE — does a brand-new session hit the "Scanning a
	// DuckLake table after the transaction has ended" error?
	fmt.Println("--- Attempt 3: fresh client + ducklake_rewrite_data_files after a prior committed DELETE ---")
	clientC, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer clientC.Close()
	must("CLIENT-C", clientC, ctx, "INSTALL ducklake")
	must("CLIENT-C", clientC, ctx, "LOAD ducklake")
	must("CLIENT-C", clientC, ctx, "INSTALL quack")
	must("CLIENT-C", clientC, ctx, "LOAD quack")
	must("CLIENT-C", clientC, ctx,
		fmt.Sprintf("CREATE SECRET quack_auth (TYPE quack, TOKEN '%s')", token))
	must("CLIENT-C", clientC, ctx, attachStmt)
	rewriteErr := tryExec("CLIENT-C", clientC, ctx, "CALL ducklake_rewrite_data_files('lake', 'spans')")
	if rewriteErr == nil {
		fmt.Println("RESULT (fresh client after committed delete): ducklake_rewrite_data_files SUCCEEDED")
	} else {
		fmt.Println("RESULT (fresh client after committed delete): ducklake_rewrite_data_files FAILED")
	}

	fmt.Println("spike done")
}
