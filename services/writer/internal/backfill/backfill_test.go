package backfill

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/lake"
)

// seedHotDB creates a legacy hot DuckDB file with the given spans/scores.
// Each span is (spanID, project, date); each score is (scoreID, spanID).
func seedHotDB(t *testing.T, path string, spans [][3]string, scores [][2]string) {
	t.Helper()
	db, err := duckdb.Open(path)
	if err != nil {
		t.Fatalf("open hot duckdb: %v", err)
	}
	defer db.Close()
	seedSpans(t, db, "spans", spans)
	for _, sc := range scores {
		_, err := db.Exec(`INSERT INTO scores (score_id, span_id, trace_id, project_id, eval_name, value, created_at)
			SELECT ?, span_id, trace_id, project_id, 'accuracy', 0.9, start_time + INTERVAL 1 HOUR FROM spans WHERE span_id = ?`,
			sc[0], sc[1])
		if err != nil {
			t.Fatalf("insert hot score %s: %v", sc[0], err)
		}
	}
}

func seedSpans(t *testing.T, db *sql.DB, table string, spans [][3]string) {
	t.Helper()
	for _, s := range spans {
		_, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (span_id, trace_id, project_id, name, kind, start_time, end_time, input_tokens, output_tokens, cost_usd)
			VALUES (?, ?, ?, 'op', 'llm', CAST(? AS TIMESTAMPTZ), CAST(? AS TIMESTAMPTZ) + INTERVAL 1 SECOND, 10, 5, 0.01)`, table),
			s[0], "trace-"+s[0], s[1], s[2]+" 12:00:00+00", s[2]+" 12:00:00+00")
		if err != nil {
			t.Fatalf("insert span %s: %v", s[0], err)
		}
	}
}

// seedColdArchive writes Hive-partitioned Parquet files in the legacy
// archive layout for the given spans (and optional scores), using the hot
// DuckDB schema as the source of truth for column shape.
func seedColdArchive(t *testing.T, root string, spans [][3]string, scores [][2]string) {
	t.Helper()
	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()
	seedSpans(t, db, "spans", spans)
	for _, sc := range scores {
		if _, err := db.Exec(`INSERT INTO scores (score_id, span_id, trace_id, project_id, eval_name, value, created_at)
			SELECT ?, span_id, trace_id, project_id, 'accuracy', 0.9, start_time + INTERVAL 1 HOUR FROM spans WHERE span_id = ?`,
			sc[0], sc[1]); err != nil {
			t.Fatalf("insert cold score %s: %v", sc[0], err)
		}
	}

	type pk struct{ project, date string }
	parts := map[pk]bool{}
	for _, s := range spans {
		parts[pk{s[1], s[2]}] = true
	}
	for p := range parts {
		dir := filepath.Join(root,
			fmt.Sprintf("project_id=%s", p.project), fmt.Sprintf("date=%s", p.date))
		for _, typ := range []string{"spans", "scores"} {
			if err := os.MkdirAll(filepath.Join(dir, typ), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf(
			`COPY (SELECT %s FROM spans WHERE project_id = '%s' AND CAST(start_time AS DATE) = '%s')
			 TO '%s' (FORMAT PARQUET)`,
			spanColumns, p.project, p.date, filepath.Join(dir, "spans", "spans.parquet"))); err != nil {
			t.Fatalf("copy cold spans: %v", err)
		}
		if _, err := db.Exec(fmt.Sprintf(
			`COPY (SELECT %s FROM scores WHERE project_id = '%s' AND CAST(created_at AS DATE) = '%s')
			 TO '%s' (FORMAT PARQUET)`,
			scoreColumns, p.project, p.date, filepath.Join(dir, "scores", "scores.parquet"))); err != nil {
			t.Fatalf("copy cold scores: %v", err)
		}
	}
}

func openTestLake(t *testing.T) (*lake.Lake, lake.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := lake.Config{
		CatalogDriver: lake.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog.ducklake"),
		DataPath:      filepath.Join(dir, "data"),
	}
	lk, err := lake.Open(context.Background(), cfg)
	if err != nil {
		t.Skipf("lake.Open: %v (ducklake extension unavailable)", err)
	}
	t.Cleanup(func() { lk.Close() })
	return lk, cfg
}

func countLake(t *testing.T, lk *lake.Lake, table string) int {
	t.Helper()
	var n int
	if err := lk.DB().QueryRow("SELECT count(*) FROM lake." + table).Scan(&n); err != nil {
		t.Fatalf("count lake.%s: %v", table, err)
	}
	return n
}

// TestBackfill_AllTierCombinations covers partitions that exist only in
// the hot DuckDB, only in cold Parquet, and in both (the hot-window
// overlap), plus score backfill with span-derived partitioning.
func TestBackfill_AllTierCombinations(t *testing.T) {
	ctx := context.Background()
	lk, _ := openTestLake(t)
	dir := t.TempDir()
	hotPath := filepath.Join(dir, "hot.db")
	archiveRoot := filepath.Join(dir, "archive")

	// Hot tier: a hot-only partition (June 10) and the overlap span s2.
	seedHotDB(t, hotPath, [][3]string{
		{"s1", "proj-a", "2026-06-10"},
		{"s2", "proj-a", "2026-06-08"}, // also archived (hot-window overlap)
	}, [][2]string{{"sc1", "s1"}})

	// Cold tier: a cold-only partition (June 5, proj-b) and the same s2.
	seedColdArchive(t, archiveRoot, [][3]string{
		{"s2", "proj-a", "2026-06-08"},
		{"s3", "proj-b", "2026-06-05"},
	}, [][2]string{{"sc3", "s3"}})

	report, err := RunWithLake(ctx, lk, Options{HotDBPath: hotPath, ArchiveRoot: archiveRoot})
	if err != nil {
		t.Fatalf("RunWithLake: %v", err)
	}

	// s1, s2 (deduped to one row), s3 → 3 lake spans across 3 partitions.
	if got := countLake(t, lk, "spans"); got != 3 {
		t.Errorf("lake spans: got %d, want 3", got)
	}
	if got := countLake(t, lk, "scores"); got != 2 {
		t.Errorf("lake scores: got %d, want 2", got)
	}
	if len(report.Mismatched()) != 0 {
		report.Print(os.Stderr)
		t.Fatalf("unexpected mismatches: %+v", report.Mismatched())
	}
	if len(report.Partitions) != 3 {
		t.Errorf("partitions: got %d, want 3 (%+v)", len(report.Partitions), report.Partitions)
	}

	// The overlap partition must hold exactly one copy of s2.
	var s2 int
	if err := lk.DB().QueryRow("SELECT count(*) FROM lake.spans WHERE span_id = 's2'").Scan(&s2); err != nil {
		t.Fatalf("count s2: %v", err)
	}
	if s2 != 1 {
		t.Errorf("overlap span s2: got %d rows, want 1 (dedupe)", s2)
	}

	// A score's partition follows the annotated span's date (ADR-0002).
	var scDate string
	if err := lk.DB().QueryRow(
		"SELECT CAST(CAST(span_start_time AS DATE) AS VARCHAR) FROM lake.scores WHERE score_id = 'sc1'").Scan(&scDate); err != nil {
		t.Fatalf("score partition date: %v", err)
	}
	if scDate != "2026-06-10" {
		t.Errorf("score sc1 span date: got %s, want 2026-06-10", scDate)
	}
}

// TestBackfill_Idempotent runs the backfill twice and proves the Lake row
// counts are identical — the issue's idempotency criterion.
func TestBackfill_Idempotent(t *testing.T) {
	ctx := context.Background()
	lk, _ := openTestLake(t)
	dir := t.TempDir()
	hotPath := filepath.Join(dir, "hot.db")

	seedHotDB(t, hotPath, [][3]string{
		{"s1", "proj-a", "2026-06-10"},
		{"s2", "proj-a", "2026-06-10"},
		{"s3", "proj-b", "2026-06-11"},
	}, [][2]string{{"sc1", "s1"}, {"sc2", "s3"}})

	for run := 1; run <= 2; run++ {
		report, err := RunWithLake(ctx, lk, Options{HotDBPath: hotPath})
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if len(report.Mismatched()) != 0 {
			t.Fatalf("run %d mismatches: %+v", run, report.Mismatched())
		}
		if got := countLake(t, lk, "spans"); got != 3 {
			t.Errorf("run %d lake spans: got %d, want 3", run, got)
		}
		if got := countLake(t, lk, "scores"); got != 2 {
			t.Errorf("run %d lake scores: got %d, want 2", run, got)
		}
	}
}

// TestBackfill_HealsLakeDuplicates: pre-existing duplicate rows in the
// Lake partition are replaced by the delete-and-rewrite.
func TestBackfill_HealsLakeDuplicates(t *testing.T) {
	ctx := context.Background()
	lk, _ := openTestLake(t)
	dir := t.TempDir()
	hotPath := filepath.Join(dir, "hot.db")

	seedHotDB(t, hotPath, [][3]string{{"s1", "proj-a", "2026-06-10"}}, nil)

	// Simulate residual duplicates in the Lake from a ledger crash window.
	for i := 0; i < 2; i++ {
		if _, err := lk.DB().Exec(
			`INSERT INTO lake.spans (span_id, trace_id, project_id, start_time)
			 VALUES ('s1', 'trace-s1', 'proj-a', CAST('2026-06-10 12:00:00+00' AS TIMESTAMPTZ))`); err != nil {
			t.Fatalf("seed duplicate: %v", err)
		}
	}

	report, err := RunWithLake(ctx, lk, Options{HotDBPath: hotPath})
	if err != nil {
		t.Fatalf("RunWithLake: %v", err)
	}
	if len(report.Mismatched()) != 0 {
		t.Fatalf("mismatches: %+v", report.Mismatched())
	}
	if got := countLake(t, lk, "spans"); got != 1 {
		t.Errorf("lake spans after rewrite: got %d, want 1", got)
	}
}

// TestBackfill_NoSourcesErrors: nothing to read is an explicit error, not
// a silent no-op.
func TestBackfill_NoSourcesErrors(t *testing.T) {
	lk, _ := openTestLake(t)
	_, err := RunWithLake(context.Background(), lk, Options{
		HotDBPath:   filepath.Join(t.TempDir(), "missing.db"),
		ArchiveRoot: filepath.Join(t.TempDir(), "missing-archive"),
	})
	if err == nil {
		t.Fatal("expected error when no legacy sources exist")
	}
}
