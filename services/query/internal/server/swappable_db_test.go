package server

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// makeTestDB creates a DuckDB file at path with a single-column table
// containing n rows. It returns the path for convenience.
func makeTestDB(t *testing.T, path string, n int) string {
	t.Helper()

	db, err := sql.Open("duckdb", path+"?access_mode=read_write")
	if err != nil {
		t.Fatalf("makeTestDB: open %s: %v", path, err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE items (id INTEGER)`); err != nil {
		t.Fatalf("makeTestDB: create table: %v", err)
	}
	for i := range n {
		if _, err := db.Exec(`INSERT INTO items VALUES (?)`, i+1); err != nil {
			t.Fatalf("makeTestDB: insert row %d: %v", i+1, err)
		}
	}
	return path
}

// countItems queries the items table through sdb and returns the row count.
func countItems(t *testing.T, sdb *SwappableDB) int {
	t.Helper()
	var count int
	if err := sdb.QueryRow("SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatalf("countItems: %v", err)
	}
	return count
}

// TestSwappableDB_SwapSeesNewData is the canonical regression test for issue #20.
//
// It verifies that after SwappableDB.Swap() the caller sees the new snapshot's
// data — not the cached in-memory state of the old file at the same path.
// Two swaps are performed so that the test exercises the A/B cycle where slot A
// is reused (the actual cache-collision scenario).
func TestSwappableDB_SwapSeesNewData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Snapshot A: 1 row. This is the initial snapshot.
	snapA := makeTestDB(t, filepath.Join(dir, "initial.duckdb"), 1)

	// Open SwappableDB on the initial snapshot.
	sdb, err := NewSwappableDB(snapA)
	if err != nil {
		t.Fatalf("NewSwappableDB: %v", err)
	}
	defer sdb.Close()

	// Sanity check: we should see 1 row before any swap.
	if got := countItems(t, sdb); got != 1 {
		t.Errorf("before first swap: got %d rows, want 1", got)
	}

	// Snapshot B: 2 rows. Simulates the writer uploading a new snapshot to S3
	// and the poller downloading it to a staging path.
	snap2 := makeTestDB(t, filepath.Join(dir, "snap2.duckdb"), 2)

	// First swap — should see 2 rows.
	if err := sdb.Swap(snap2); err != nil {
		t.Fatalf("first Swap: %v", err)
	}
	if got := countItems(t, sdb); got != 2 {
		t.Errorf("after first swap: got %d rows, want 2 (path-cache bug?)", got)
	}

	// Second swap — cycles back to the slot used initially, which is the actual
	// cache-collision scenario described in issue #20. Should see 3 rows.
	snap3 := makeTestDB(t, filepath.Join(dir, "snap3.duckdb"), 3)
	if err := sdb.Swap(snap3); err != nil {
		t.Fatalf("second Swap: %v", err)
	}
	if got := countItems(t, sdb); got != 3 {
		t.Errorf("after second swap: got %d rows, want 3 (path-cache bug?)", got)
	}

	// Third swap — another cycle, ensures alternation is stable beyond 2 swaps.
	snap4 := makeTestDB(t, filepath.Join(dir, "snap4.duckdb"), 4)
	if err := sdb.Swap(snap4); err != nil {
		t.Fatalf("third Swap: %v", err)
	}
	if got := countItems(t, sdb); got != 4 {
		t.Errorf("after third swap: got %d rows, want 4", got)
	}
}

// TestSwappableDB_OldConnectionClosed verifies that after a Swap the old
// file can be deleted — implying no dangling file-descriptor leak.
func TestSwappableDB_OldConnectionClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	snapA := makeTestDB(t, filepath.Join(dir, "initial.duckdb"), 1)

	sdb, err := NewSwappableDB(snapA)
	if err != nil {
		t.Fatalf("NewSwappableDB: %v", err)
	}
	defer sdb.Close()

	snap2 := makeTestDB(t, filepath.Join(dir, "snap2.duckdb"), 2)
	if err := sdb.Swap(snap2); err != nil {
		t.Fatalf("Swap: %v", err)
	}

	// On Windows, removing a file that still has an open handle fails.
	// This assertion catches file-descriptor leaks.
	if err := os.Remove(snapA); err != nil {
		t.Errorf("remove old snapshot file after Swap: %v (file descriptor leak?)", err)
	}
}
