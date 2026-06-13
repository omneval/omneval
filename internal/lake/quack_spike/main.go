// Command quack_spike is a throwaway program for issue #111: it spins up a
// quack_serve() instance attached to a local DuckLake catalog, attaches to
// it as a quack:// client, and tests whether DELETE followed by
// ducklake_rewrite_data_files over the SAME quack:// client session avoids
// the "Not implemented Error: Scanning a DuckLake table after the
// transaction has ended" error that a fresh database/sql connection hits
// against a direct ducklake: attachment.
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

func main() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "quackspike")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	catalog := filepath.Join(dir, "catalog", "lake.ducklake")
	if err := os.MkdirAll(filepath.Dir(catalog), 0755); err != nil {
		log.Fatal(err)
	}
	data := filepath.Join(dir, "data")

	// --- Server side: open a db, attach ducklake, start quack_serve. ---
	serverDB, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer serverDB.Close()

	must("SERVER", serverDB, ctx, "INSTALL ducklake")
	must("SERVER", serverDB, ctx, "LOAD ducklake")
	must("SERVER", serverDB, ctx, "INSTALL quack")
	must("SERVER", serverDB, ctx, "LOAD quack")
	must("SERVER", serverDB, ctx,
		fmt.Sprintf("ATTACH 'ducklake:%s' AS lake (DATA_PATH '%s')", catalog, data))
	must("SERVER", serverDB, ctx,
		"CREATE TABLE IF NOT EXISTS lake.spans (id INT, project_id VARCHAR)")
	must("SERVER", serverDB, ctx,
		"ALTER TABLE lake.spans SET PARTITIONED BY (project_id)")
	must("SERVER", serverDB, ctx,
		"INSERT INTO lake.spans VALUES (1, 'proj-a'), (2, 'proj-b')")
	must("SERVER", serverDB, ctx, "CALL ducklake_flush_inlined_data('lake')")

	tokenCh := make(chan string, 1)
	go func() {
		rows, err := serverDB.QueryContext(ctx, "SELECT * FROM quack_serve('quack://127.0.0.1:9494')")
		if err != nil {
			fmt.Printf("quack_serve query error: %v\n", err)
			tokenCh <- ""
			return
		}
		defer rows.Close()
		for rows.Next() {
			var a, b, c sql.NullString
			if err := rows.Scan(&a, &b, &c); err != nil {
				fmt.Printf("quack_serve scan error: %v\n", err)
				tokenCh <- ""
				return
			}
			fmt.Printf("quack_serve row: %v %v %v\n", a, b, c)
			tokenCh <- c.String
		}
		if err := rows.Err(); err != nil {
			fmt.Printf("quack_serve rows error: %v\n", err)
		}
		fmt.Println("quack_serve goroutine done")
	}()

	// give the server a moment to bind
	time.Sleep(500 * time.Millisecond)
	token := <-tokenCh
	fmt.Printf("CLIENT using token: %s\n", token)

	// --- Client side: a SEPARATE *sql.DB attaching over quack://. ---
	clientDB, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer clientDB.Close()

	must("CLIENT", clientDB, ctx, "INSTALL quack")
	must("CLIENT", clientDB, ctx, "LOAD quack")
	// Try several plausible ways of supplying the Quack auth token before
	// the bare ATTACH. Record which (if any) works.
	candidates := []string{
		fmt.Sprintf("CREATE SECRET quack_auth (TYPE quack, TOKEN '%s')", token),
		fmt.Sprintf("SET quack_token='%s'", token),
		fmt.Sprintf("ATTACH 'quack:127.0.0.1:9494' AS lake (TOKEN '%s')", token),
	}
	attached := false
	for _, c := range candidates {
		if err := tryExec("CLIENT", clientDB, ctx, c); err != nil {
			continue
		}
		// if this was a secret/set, still need a bare ATTACH
		if !attached {
			if err := tryExec("CLIENT", clientDB, ctx, "ATTACH 'quack:127.0.0.1:9494' AS lake"); err == nil {
				attached = true
				break
			}
		} else {
			attached = true
			break
		}
	}
	if !attached {
		fmt.Println("RESULT: ATTACH quack:// failed via all candidate auth methods, cannot proceed with spike")
		return
	}

	// Debug: what schemas/tables does the quack:// attachment expose?
	if rows, err := clientDB.QueryContext(ctx, "SHOW ALL TABLES"); err == nil {
		cols, _ := rows.Columns()
		fmt.Printf("CLIENT SHOW ALL TABLES cols: %v\n", cols)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			fmt.Printf("CLIENT table row: %v\n", vals)
		}
		rows.Close()
	} else {
		fmt.Printf("CLIENT SHOW ALL TABLES: ERROR: %v\n", err)
	}

	// Try PRAGMA database_list and SHOW TABLES FROM lake / pure attached-DB queries.
	if rows, err := clientDB.QueryContext(ctx, "PRAGMA database_list"); err == nil {
		cols, _ := rows.Columns()
		fmt.Printf("CLIENT database_list cols: %v\n", cols)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			fmt.Printf("CLIENT database_list row: %v\n", vals)
		}
		rows.Close()
	} else {
		fmt.Printf("CLIENT database_list: ERROR: %v\n", err)
	}

	if rows, err := clientDB.QueryContext(ctx, "SHOW TABLES FROM lake"); err == nil {
		cols, _ := rows.Columns()
		fmt.Printf("CLIENT SHOW TABLES FROM lake cols: %v\n", cols)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			fmt.Printf("CLIENT SHOW TABLES FROM lake row: %v\n", vals)
		}
		rows.Close()
	} else {
		fmt.Printf("CLIENT SHOW TABLES FROM lake: ERROR: %v\n", err)
	}

	var n int
	if err := clientDB.QueryRowContext(ctx, "SELECT count(*) FROM lake.main.spans").Scan(&n); err != nil {
		fmt.Printf("CLIENT count before delete (lake.main.spans): ERROR: %v\n", err)
		return
	}
	fmt.Printf("CLIENT count before delete: %d\n", n)

	// THE KEY TEST: DELETE then ducklake_rewrite_data_files over the SAME
	// quack:// client session (same *sql.DB / same underlying connection).
	if err := tryExec("CLIENT", clientDB, ctx, "DELETE FROM lake.main.spans WHERE project_id = 'proj-a'"); err != nil {
		return
	}

	rewriteErr := tryExec("CLIENT", clientDB, ctx, "CALL ducklake_rewrite_data_files('lake', 'spans')")

	if rewriteErr == nil {
		fmt.Println("RESULT: DELETE -> ducklake_rewrite_data_files over the SAME quack:// session SUCCEEDED")
	} else {
		fmt.Println("RESULT: DELETE -> ducklake_rewrite_data_files over the SAME quack:// session FAILED with the same/similar error as a fresh database/sql connection")
	}

	// Verify final state.
	if err := clientDB.QueryRowContext(ctx, "SELECT count(*) FROM lake.main.spans WHERE project_id = 'proj-a'").Scan(&n); err != nil {
		fmt.Printf("CLIENT count proj-a after delete: ERROR: %v\n", err)
	} else {
		fmt.Printf("CLIENT count proj-a after delete: %d\n", n)
	}
}
