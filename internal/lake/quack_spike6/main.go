// Command quack_spike6: reorder maintenance — rewrite/merge first, flush
// LAST (nothing after it in the same session needs catalog scans).
package main

import (
	"context"
	"database/sql"
	"fmt"
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

func newClient(ctx context.Context, port, data string) *sql.DB {
	client, _ := sql.Open("duckdb", "")
	client.SetMaxOpenConns(1)
	must("C", client, ctx, "INSTALL ducklake")
	must("C", client, ctx, "LOAD ducklake")
	must("C", client, ctx, "INSTALL quack")
	must("C", client, ctx, "LOAD quack")
	must("C", client, ctx, "CREATE SECRET quack_auth (TYPE quack, TOKEN 'spike6')")
	must("C", client, ctx, fmt.Sprintf("ATTACH 'ducklake:quack:localhost:%s' AS lake (DATA_PATH '%s')", port, data))
	return client
}

func main() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "quackspike6")
	defer os.RemoveAll(dir)
	data := filepath.Join(dir, "data")
	os.MkdirAll(data, 0755)
	const port = "9491"

	serverDB, _ := sql.Open("duckdb", "")
	defer serverDB.Close()
	serverDB.ExecContext(ctx, "INSTALL quack")
	serverDB.ExecContext(ctx, "LOAD quack")
	go func() {
		serverDB.QueryContext(ctx, fmt.Sprintf("SELECT * FROM quack_serve('quack://localhost:%s', token => 'spike6')", port))
	}()
	time.Sleep(500 * time.Millisecond)

	admin := newClient(ctx, port, data)
	defer admin.Close()
	must("ADMIN", admin, ctx, "CREATE TABLE IF NOT EXISTS lake.t1 (id INT, project_id VARCHAR)")
	tryExec("ADMIN", admin, ctx, "ALTER TABLE lake.t1 SET PARTITIONED BY (project_id)")
	must("ADMIN", admin, ctx, "INSERT INTO lake.t1 VALUES (1, 'proj-a'), (2, 'proj-b')")
	must("ADMIN", admin, ctx, "DELETE FROM lake.t1 WHERE project_id = 'proj-a'")
	must("ADMIN", admin, ctx, "CALL ducklake_rewrite_data_files('lake', 't1')")

	// maintLake: fresh session. New order: rewrite + merge first, flush LAST.
	maint := newClient(ctx, port, data)
	defer maint.Close()
	tryExec("MAINT", maint, ctx, "CALL ducklake_rewrite_data_files('lake', 't1')")
	tryExec("MAINT", maint, ctx, "CALL ducklake_merge_adjacent_files('lake', 't1')")
	tryExec("MAINT", maint, ctx, "CALL ducklake_expire_snapshots('lake', older_than => now())")
	tryExec("MAINT", maint, ctx, "CALL ducklake_delete_orphaned_files('lake', cleanup_all => true)")
	tryExec("MAINT", maint, ctx, "CALL ducklake_cleanup_old_files('lake', cleanup_all => true)")
	tryExec("MAINT", maint, ctx, "CALL ducklake_flush_inlined_data('lake')")

	query := newClient(ctx, port, data)
	defer query.Close()
	var n int
	query.QueryRowContext(ctx, "SELECT count(*) FROM lake.t1 WHERE project_id = 'proj-a'").Scan(&n)
	fmt.Println("proj-a count after maintenance:", n)
}
