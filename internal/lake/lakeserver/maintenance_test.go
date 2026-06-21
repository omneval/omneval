package lakeserver_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net"
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
