package lakeserver_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeserver"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
)

// startRawTestServer starts a Quack Server backed by a local DuckLake
// catalog file and returns the *lakeserver.Server itself (unlike
// lakeservertest.NewLocal, which only returns a client lake.Config) — tests
// that need the server's own raw catalog connection (Server.DB()), such as
// PruneEmptyInlinedTables, need the Server handle.
func startRawTestServer(t *testing.T) *lakeserver.Server {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
	})
	if err != nil {
		t.Fatalf("start quack server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

// ensureInlinedDataTablesRegistry creates ducklake_inlined_data_tables if it
// doesn't already exist. DuckLake only lazily creates this registry table
// the first time a real inlined write happens against a catalog, so a fresh
// test catalog (which has never had a real inlined write) starts without
// it. Schema confirmed by inspecting a real catalog after a forced inlined
// write: (table_id BIGINT, table_name VARCHAR, schema_version BIGINT).
func ensureInlinedDataTablesRegistry(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		"CREATE TABLE IF NOT EXISTS ducklake_inlined_data_tables (table_id BIGINT, table_name VARCHAR, schema_version BIGINT)",
	); err != nil {
		t.Fatalf("ensure ducklake_inlined_data_tables registry: %v", err)
	}
}

func testSpan(projectID, spanID string, start time.Time) *domain.Span {
	return &domain.Span{
		SpanID:       spanID,
		TraceID:      "trace-" + spanID,
		ProjectID:    projectID,
		ServiceName:  "svc",
		Name:         "llm-call",
		Kind:         domain.SpanKind("llm"),
		StartTime:    start,
		EndTime:      start.Add(time.Second),
		Model:        "gpt-4o",
		Input:        `[{"role":"user","content":"hi"}]`,
		Output:       `{"role":"assistant","content":"hello"}`,
		InputTokens:  10,
		OutputTokens: 5,
		CostUSD:      0.001,
		StatusCode:   "OK",
		Attributes:   map[string]any{"k": "v"},
	}
}

// TestRunMaintenanceRetentionDisabled proves that with no RetentionConfig (or
// Enabled: false), RunMaintenance behaves exactly as before: no rows are
// deleted regardless of age.
func TestRunMaintenanceRetentionDisabled(t *testing.T) {
	ctx := context.Background()
	cfg, _ := lakeservertest.NewLocal(t)

	l, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer l.Close()

	old := time.Now().AddDate(0, 0, -30)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "old", old)}); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	if _, err := lakeserver.RunMaintenance(ctx, l.DB(), lakeserver.MaintenanceTables, lakeserver.RetentionConfig{}); err != nil {
		t.Fatalf("run maintenance: %v", err)
	}

	var n int
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("spans after maintenance with retention disabled: got %d, want 1", n)
	}
}

// TestRunMaintenanceRetentionDeletesOldRowsAndReclaimsFiles proves that, with
// retention enabled and a low MaxAgeDays, RunMaintenance deletes spans/scores
// older than the cutoff (by start_time / span_start_time), leaves newer rows
// untouched, and physically reclaims the Parquet files backing the deleted
// rows in the same pass.
func TestRunMaintenanceRetentionDeletesOldRowsAndReclaimsFiles(t *testing.T) {
	ctx := context.Background()
	cfg, dataDir := lakeservertest.NewLocal(t)

	l, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer l.Close()

	old := time.Now().AddDate(0, 0, -30)
	recent := time.Now()

	if err := l.InsertSpans(ctx, []*domain.Span{
		testSpan("proj-a", "old-span", old),
		testSpan("proj-a", "new-span", recent),
	}); err != nil {
		t.Fatalf("insert spans: %v", err)
	}
	if err := l.InsertScores(ctx, []*domain.Score{
		{ScoreID: "old-score", SpanID: "old-span", TraceID: "trace-old-span", ProjectID: "proj-a",
			EvalName: "e", Value: 1, CreatedAt: old, SpanStartTime: old},
		{ScoreID: "new-score", SpanID: "new-span", TraceID: "trace-new-span", ProjectID: "proj-a",
			EvalName: "e", Value: 1, CreatedAt: recent, SpanStartTime: recent},
	}); err != nil {
		t.Fatalf("insert scores: %v", err)
	}

	// Do NOT call FlushInlinedData before RunMaintenance: per
	// internal/lake/quack_spike6 (see RunMaintenance's doc comment),
	// ducklake_rewrite_data_files fails with "Scanning a DuckLake table
	// after the transaction has ended" if it runs in the same session after
	// ducklake_flush_inlined_data has already run — even with no DELETE in
	// between. RunMaintenance's own flush step (last) covers this.
	retCfg := lakeserver.RetentionConfig{Enabled: true, MaxAgeDays: 7}
	result, err := lakeserver.RunMaintenance(ctx, l.DB(), lakeserver.MaintenanceTables, retCfg)
	if err != nil {
		t.Fatalf("run maintenance: %v", err)
	}

	if result.Retention.SpansDeleted != 1 {
		t.Errorf("spans deleted: got %d, want 1", result.Retention.SpansDeleted)
	}
	if result.Retention.ScoresDeleted != 1 {
		t.Errorf("scores deleted: got %d, want 1", result.Retention.ScoresDeleted)
	}
	if result.Retention.Duration <= 0 {
		t.Error("retention duration not recorded")
	}

	var n int
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE span_id = 'old-span'").Scan(&n); err != nil {
		t.Fatalf("count old span: %v", err)
	}
	if n != 0 {
		t.Errorf("old-span after maintenance: got %d, want 0", n)
	}
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.scores WHERE score_id = 'old-score'").Scan(&n); err != nil {
		t.Fatalf("count old score: %v", err)
	}
	if n != 0 {
		t.Errorf("old-score after maintenance: got %d, want 0", n)
	}

	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE span_id = 'new-span'").Scan(&n); err != nil {
		t.Fatalf("count new span: %v", err)
	}
	if n != 1 {
		t.Errorf("new-span after maintenance: got %d, want 1", n)
	}
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.scores WHERE score_id = 'new-score'").Scan(&n); err != nil {
		t.Fatalf("count new score: %v", err)
	}
	if n != 1 {
		t.Errorf("new-score after maintenance: got %d, want 1", n)
	}

	// Data path is walkable after maintenance — no corrupted/orphaned
	// state (#91, same pattern as TestDeleteProject).
	if err := filepath.WalkDir(filepath.Join(dataDir, "data"), func(path string, d fs.DirEntry, err error) error {
		return err
	}); err != nil {
		t.Errorf("walk data path after retention: %v", err)
	}
}

// TestRunMaintenanceFlushOrderingPreserved proves that flush_inlined_data
// still runs last even when retention is enabled — retention's DELETE +
// rewrite must not break the #105 ordering constraint documented in
// RunMaintenance's doc comment.
func TestRunMaintenanceFlushOrderingPreserved(t *testing.T) {
	ctx := context.Background()
	cfg, _ := lakeservertest.NewLocal(t)

	l, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer l.Close()

	old := time.Now().AddDate(0, 0, -30)
	recent := time.Now()
	if err := l.InsertSpans(ctx, []*domain.Span{
		testSpan("proj-a", "old-span", old),
		testSpan("proj-a", "new-span", recent),
	}); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	// Do NOT flush before maintenance: retention's DELETE + rewrite must
	// handle inlined data itself (mirrors reclaim()/quack_spike6), and flush
	// must still run last without erroring.
	retCfg := lakeserver.RetentionConfig{Enabled: true, MaxAgeDays: 7}
	if _, err := lakeserver.RunMaintenance(ctx, l.DB(), lakeserver.MaintenanceTables, retCfg); err != nil {
		t.Fatalf("run maintenance: %v", err)
	}

	var n int
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("spans after maintenance: got %d, want 1", n)
	}
}

// countActiveSpansFiles returns the number of data files DuckLake's catalog
// considers part of the current "spans" snapshot.
func countActiveSpansFiles(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM ducklake_list_files('lake', 'spans')").Scan(&n); err != nil {
		t.Fatalf("count active spans files: %v", err)
	}
	return n
}

// TestRunMaintenanceMergeWithCapStillConvergesCorrectly proves the
// max_compacted_files cap (lakeserver.MaxCompactedFilesPerMerge) added to
// the ducklake_merge_adjacent_files call doesn't change correctness: rows
// survive, and the table still ends up fully merged to one active file.
//
// max_compacted_files bounds how many source files get combined into any
// one merge *group* — confirmed empirically (this test included) that it
// does not make ducklake_merge_adjacent_files stop early and return after
// processing only that many files total; at small scale a single capped
// call still merges the whole table down to one file, same as uncapped.
// The fix's value is in the production incident's actual failure mode: with
// no cap, the call tried to combine an entire ~25k-file backlog into one
// merge group (effectively one gigantic rewrite) and ran for 45+ minutes
// without completing. Capping the group size keeps every individual merge
// operation's cost bounded regardless of total backlog size, which is what
// let the equivalent call finish at all once deployed.
func TestRunMaintenanceMergeWithCapStillConvergesCorrectly(t *testing.T) {
	ctx := context.Background()
	cfg, _ := lakeservertest.NewLocal(t)

	l, err := lake.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer l.Close()

	// Each InsertSpans call commits its own snapshot/data file — mirrors the
	// production fragmentation pattern (many small Commit Cadence flushes).
	const fileCount = 6
	for i := 0; i < fileCount; i++ {
		span := testSpan("proj-a", fmt.Sprintf("s%d", i), time.Now())
		if err := l.InsertSpans(ctx, []*domain.Span{span}); err != nil {
			t.Fatalf("insert span %d: %v", i, err)
		}
	}

	before := countActiveSpansFiles(t, ctx, l.DB())
	if before < fileCount {
		t.Fatalf("expected at least %d active spans files before any merge, got %d", fileCount, before)
	}

	old := lakeserver.MaxCompactedFilesPerMerge
	lakeserver.MaxCompactedFilesPerMerge = 2
	defer func() { lakeserver.MaxCompactedFilesPerMerge = old }()

	if _, err := lakeserver.RunMaintenance(ctx, l.DB(), []string{"spans"}, lakeserver.RetentionConfig{}); err != nil {
		t.Fatalf("run maintenance: %v", err)
	}

	final := countActiveSpansFiles(t, ctx, l.DB())
	if final != 1 {
		t.Errorf("active spans files after capped merge: got %d, want 1 (fully converged)", final)
	}

	var n int
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count spans after merge: %v", err)
	}
	if n != fileCount {
		t.Errorf("spans row count after merge: got %d, want %d (merge must not lose rows)", n, fileCount)
	}
}

// TestDefaultMaintenanceIntervalIs15Minutes pins the documented fallback
// interval. 5m was too aggressive for light workloads: with skip-when-idle
// (RunMaintenanceLoop) doing most of the work to avoid wasted passes, a
// longer baseline cadence further reduces how many merge-lineage
// generations ever get created in the first place, independent of whether
// cleanup successfully reclaims them.
func TestDefaultMaintenanceIntervalIs15Minutes(t *testing.T) {
	if lakeserver.DefaultMaintenanceInterval != 15*time.Minute {
		t.Errorf("DefaultMaintenanceInterval: got %v, want 15m", lakeserver.DefaultMaintenanceInterval)
	}
}

// TestRunMaintenanceLoopFallsBackToDefaultInterval proves RunMaintenanceLoop
// uses DefaultMaintenanceInterval when given a non-positive interval, by
// checking the configured-interval is reflected back in its startup log
// would require log capture; instead this proves it via behavior: a
// canceled context returns promptly without ever firing a tick at an
// effectively-zero interval (which would instead busy-loop).
func TestRunMaintenanceLoopFallsBackToDefaultInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg, _ := lakeservertest.NewLocal(t)
	l, err := lake.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	done := make(chan error, 1)
	go func() {
		done <- lakeserver.RunMaintenanceLoop(ctx, l.DB(), lakeserver.MaintenanceTables, 0, lakeserver.RetentionConfig{}, nil)
	}()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("RunMaintenanceLoop with interval=0 and canceled ctx: got err %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunMaintenanceLoop did not return promptly with an already-canceled context")
	}
}

// TestPruneEmptyInlinedTables proves PruneEmptyInlinedTables drops only the
// empty catalog-resident inlined-data tables (and deregisters them), leaving
// non-empty ones untouched — this is the cleanup routine for the husk
// tables DuckLake's own ducklake_flush_inlined_data leaves behind forever.
func TestPruneEmptyInlinedTables(t *testing.T) {
	srv := startRawTestServer(t)
	ctx := context.Background()
	db := srv.DB()
	ensureInlinedDataTablesRegistry(t, ctx, db)

	if _, err := db.ExecContext(ctx, "CREATE TABLE ducklake_inlined_data_1_1 (x INT)"); err != nil {
		t.Fatalf("create empty inlined table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO ducklake_inlined_data_tables (table_id, table_name, schema_version) VALUES (1, 'ducklake_inlined_data_1_1', 1)",
	); err != nil {
		t.Fatalf("register empty inlined table: %v", err)
	}

	if _, err := db.ExecContext(ctx, "CREATE TABLE ducklake_inlined_data_2_1 (x INT)"); err != nil {
		t.Fatalf("create non-empty inlined table: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO ducklake_inlined_data_2_1 VALUES (42)"); err != nil {
		t.Fatalf("populate non-empty inlined table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO ducklake_inlined_data_tables (table_id, table_name, schema_version) VALUES (2, 'ducklake_inlined_data_2_1', 1)",
	); err != nil {
		t.Fatalf("register non-empty inlined table: %v", err)
	}

	result, err := lakeserver.PruneEmptyInlinedTables(ctx, db)
	if err != nil {
		t.Fatalf("PruneEmptyInlinedTables: %v", err)
	}
	if result.TablesDropped != 1 {
		t.Errorf("TablesDropped: got %d, want 1", result.TablesDropped)
	}

	var emptyStillRegistered int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM ducklake_inlined_data_tables WHERE table_name = 'ducklake_inlined_data_1_1'",
	).Scan(&emptyStillRegistered); err != nil {
		t.Fatalf("check empty table registry row: %v", err)
	}
	if emptyStillRegistered != 0 {
		t.Errorf("empty inlined table's registry row still present, want removed")
	}

	if _, err := db.ExecContext(ctx, "SELECT * FROM ducklake_inlined_data_1_1"); err == nil {
		t.Errorf("empty inlined table ducklake_inlined_data_1_1 still exists, want dropped")
	}

	var nonEmptyStillRegistered int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM ducklake_inlined_data_tables WHERE table_name = 'ducklake_inlined_data_2_1'",
	).Scan(&nonEmptyStillRegistered); err != nil {
		t.Fatalf("check non-empty table registry row: %v", err)
	}
	if nonEmptyStillRegistered != 1 {
		t.Errorf("non-empty inlined table's registry row missing, want preserved")
	}

	var nonEmptyRowCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM ducklake_inlined_data_2_1").Scan(&nonEmptyRowCount); err != nil {
		t.Fatalf("non-empty inlined table no longer queryable: %v", err)
	}
	if nonEmptyRowCount != 1 {
		t.Errorf("non-empty inlined table row count: got %d, want 1", nonEmptyRowCount)
	}
}

// TestPruneEmptyInlinedTablesRejectsUnexpectedTableName proves
// PruneEmptyInlinedTables refuses to touch a registry row whose table_name
// doesn't match DuckLake's machine-generated inlined-table naming pattern,
// rather than blindly DROPping whatever name is in the table — defense in
// depth against a corrupted or unexpected registry row.
func TestPruneEmptyInlinedTablesRejectsUnexpectedTableName(t *testing.T) {
	srv := startRawTestServer(t)
	ctx := context.Background()
	db := srv.DB()
	ensureInlinedDataTablesRegistry(t, ctx, db)

	if _, err := db.ExecContext(ctx, "CREATE TABLE spans_backup (x INT)"); err != nil {
		t.Fatalf("create decoy table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO ducklake_inlined_data_tables (table_id, table_name, schema_version) VALUES (99, 'spans_backup', 1)",
	); err != nil {
		t.Fatalf("register decoy table: %v", err)
	}

	if _, err := lakeserver.PruneEmptyInlinedTables(ctx, db); err == nil {
		t.Fatal("PruneEmptyInlinedTables: got nil error for non-matching table_name, want error")
	}

	var exists int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM spans_backup").Scan(&exists); err != nil {
		t.Fatalf("decoy table no longer queryable, want untouched: %v", err)
	}
}

// TestPruneEmptyInlinedTablesRemovesOrphanedRegistryRows proves
// PruneEmptyInlinedTables deregisters a registry row whose table doesn't
// physically exist at all, rather than erroring out and aborting the whole
// pass. Confirmed in production: DuckLake itself can leave a registry row
// behind referencing a table that was already dropped/never materialized
// (table_id 3, an old schema_version, while a newer schema_version for the
// same table_id existed and was fine) — with ~4362 real entries, the
// previous behavior (return an error on the first such row) meant the
// entire cleanup sweep silently did nothing.
func TestPruneEmptyInlinedTablesRemovesOrphanedRegistryRows(t *testing.T) {
	srv := startRawTestServer(t)
	ctx := context.Background()
	db := srv.DB()
	ensureInlinedDataTablesRegistry(t, ctx, db)

	if _, err := db.ExecContext(ctx,
		"INSERT INTO ducklake_inlined_data_tables (table_id, table_name, schema_version) VALUES (3, 'ducklake_inlined_data_3_2086', 2086)",
	); err != nil {
		t.Fatalf("register orphaned row: %v", err)
	}

	if _, err := db.ExecContext(ctx, "CREATE TABLE ducklake_inlined_data_4_1 (x INT)"); err != nil {
		t.Fatalf("create empty inlined table: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO ducklake_inlined_data_tables (table_id, table_name, schema_version) VALUES (4, 'ducklake_inlined_data_4_1', 1)",
	); err != nil {
		t.Fatalf("register empty inlined table: %v", err)
	}

	result, err := lakeserver.PruneEmptyInlinedTables(ctx, db)
	if err != nil {
		t.Fatalf("PruneEmptyInlinedTables: %v", err)
	}
	if result.OrphanedRowsRemoved != 1 {
		t.Errorf("OrphanedRowsRemoved: got %d, want 1", result.OrphanedRowsRemoved)
	}
	if result.TablesDropped != 1 {
		t.Errorf("TablesDropped: got %d, want 1", result.TablesDropped)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM ducklake_inlined_data_tables WHERE table_name = 'ducklake_inlined_data_3_2086'",
	).Scan(&n); err != nil {
		t.Fatalf("check orphaned row: %v", err)
	}
	if n != 0 {
		t.Errorf("orphaned registry row still present, want removed")
	}
}

// startRawTestServerWithClient is startRawTestServer plus a client
// lake.Config attached to the same server, for tests (like
// TestRepairMissingDataFiles) that need both: a normal client to insert
// data and a raw server connection to mutate catalog tables directly.
func startRawTestServerWithClient(t *testing.T) (*lakeserver.Server, lake.Config, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
	})
	if err != nil {
		t.Fatalf("start quack server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	clientCfg := lake.Config{
		QuackAddr:  fmt.Sprintf("localhost:%d", port),
		QuackToken: srv.Token(),
		DataPath:   filepath.Join(dir, "data"),
	}
	return srv, clientCfg, dir
}

// TestRepairMissingDataFiles proves RepairMissingDataFiles finds a data
// file the catalog still references but whose physical Parquet file is
// gone from storage, and marks it removed (end_snapshot = begin_snapshot)
// so it stops breaking queries — without touching any other, still-good
// file's rows.
//
// Reproduces the 2026-06-25 production incident's end state directly
// (physically deleting a committed file out from under the catalog)
// rather than the concurrent-jobs race that caused it — the race is hard
// to reproduce deterministically, but its end state (catalog row present,
// physical file gone) is exactly what RepairMissingDataFiles must detect
// and fix.
func TestRepairMissingDataFiles(t *testing.T) {
	ctx := context.Background()
	srv, clientCfg, _ := startRawTestServerWithClient(t)

	l, err := lake.Open(ctx, clientCfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer l.Close()

	// Two separate commits -> two separate data files, so deleting one
	// physically can be proven not to affect the other's rows.
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "keep-me", time.Now())}); err != nil {
		t.Fatalf("insert keep-me span: %v", err)
	}
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "lose-me", time.Now())}); err != nil {
		t.Fatalf("insert lose-me span: %v", err)
	}

	// Find the specific file containing "lose-me" (not just "the first
	// file alphabetically" — InsertSpans' UUIDv7-based filenames are
	// time-ordered, so that would deterministically pick keep-me's file
	// instead, since it was inserted first). read_parquet doesn't accept a
	// table-function lateral-join column as its path argument, so this
	// has to loop in Go rather than correlate in one query.
	pathRows, err := l.DB().QueryContext(ctx, "SELECT data_file FROM ducklake_list_files('lake', 'spans')")
	if err != nil {
		t.Fatalf("list spans files: %v", err)
	}
	var allPaths []string
	for pathRows.Next() {
		var p string
		if err := pathRows.Scan(&p); err != nil {
			t.Fatalf("scan path: %v", err)
		}
		allPaths = append(allPaths, p)
	}
	pathRows.Close()

	var pathToDelete string
	for _, p := range allPaths {
		var matches int
		if err := l.DB().QueryRowContext(ctx,
			"SELECT count(*) FROM read_parquet(?) WHERE span_id = 'lose-me'", p,
		).Scan(&matches); err != nil {
			t.Fatalf("probe %s for lose-me: %v", p, err)
		}
		if matches > 0 {
			pathToDelete = p
			break
		}
	}
	if pathToDelete == "" {
		t.Fatalf("could not find lose-me's file among %v", allPaths)
	}
	if err := os.Remove(pathToDelete); err != nil {
		t.Fatalf("delete file %s from storage: %v", pathToDelete, err)
	}

	result, err := lakeserver.RepairMissingDataFiles(ctx, l.DB(), srv.DB(), lakeserver.MaintenanceTables)
	if err != nil {
		t.Fatalf("RepairMissingDataFiles: %v", err)
	}
	if len(result.RepairedPaths) != 1 {
		t.Fatalf("RepairedPaths: got %d (%v), want 1", len(result.RepairedPaths), result.RepairedPaths)
	}
	if result.RepairedPaths[0] != pathToDelete {
		t.Errorf("RepairedPaths[0]: got %s, want %s", result.RepairedPaths[0], pathToDelete)
	}
	if result.FilesChecked < 2 {
		t.Errorf("FilesChecked: got %d, want at least 2", result.FilesChecked)
	}

	// The table is queryable again (no 404 on the deleted file), and only
	// the surviving span's row remains.
	var n int
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count spans after repair: %v", err)
	}
	if n != 1 {
		t.Errorf("spans row count after repair: got %d, want 1", n)
	}
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE span_id = 'keep-me'").Scan(&n); err != nil {
		t.Fatalf("count keep-me: %v", err)
	}
	if n != 1 {
		t.Errorf("keep-me after repair: got %d, want 1 (must survive)", n)
	}

	// Running it again is a no-op: the file is already marked removed
	// (end_snapshot IS NOT NULL), so it's no longer in
	// ducklake_list_files's active set and won't be re-checked.
	result2, err := lakeserver.RepairMissingDataFiles(ctx, l.DB(), srv.DB(), lakeserver.MaintenanceTables)
	if err != nil {
		t.Fatalf("RepairMissingDataFiles (second run): %v", err)
	}
	if len(result2.RepairedPaths) != 0 {
		t.Errorf("RepairedPaths on second run: got %v, want none", result2.RepairedPaths)
	}
}
