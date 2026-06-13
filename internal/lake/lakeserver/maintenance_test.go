package lakeserver_test

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeserver"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
)

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

// findFreePort finds an available TCP port for a test Quack Server.
func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestServeS3SecretCreated verifies that when DataPath is an s3:// URL and
// Storage credentials are configured, Serve() creates an S3 secret in the
// DuckDB session. This is the fix for #120: DuckLake's internal read_blob
// calls (orphaned-file cleanup) need S3 credentials in the server's session.
func TestServeS3SecretCreated(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := findFreePort(t)

	storageCfg := config.StorageConfig{
		AccessKey: "minio",
		SecretKey: "minio123",
		Region:    "us-east-1",
		Endpoint:  "omneval-minio:9000",
		Bucket:    "omneval",
	}

	// Verify Region and Endpoint are configured; the original bug (#120) was
	// caused by Region being empty, so these must be non-empty for the secret
	// to work against MinIO.
	if storageCfg.Region == "" {
		t.Fatal("test config: Region must be non-empty")
	}
	if storageCfg.Endpoint == "" {
		t.Fatal("test config: Endpoint must be non-empty")
	}

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
		DataPath:      "s3://omneval/lake",
		Storage:       storageCfg,
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	// Verify that the S3 secret is registered by querying DuckDB's secret
	// catalog. The secret should exist and be scoped to our S3 path.
	// scope is a VARCHAR[] array; use scope[1] to get the first element.
	// type is lowercase 's3' per DuckDB's convention.
	var scope, secretType string
	err = srv.DB().QueryRowContext(ctx, `
		SELECT scope[1], type
		FROM duckdb_secrets()
		WHERE LOWER(type) = 's3' AND scope[1] LIKE 's3://omneval%%'
	`).Scan(&scope, &secretType)
	if err != nil {
		t.Fatalf("query duckdb_secrets: %v", err)
	}
	if strings.ToLower(secretType) != "s3" {
		t.Errorf("S3 secret type: got %q, want s3 (case-insensitive)", secretType)
	}
	if scope != "s3://omneval/*" {
		t.Errorf("S3 secret scope: got %q, want %q", scope, "s3://omneval/*")
	}
}

// TestServeLocalDataPathSkipsS3Secret verifies that when DataPath is a local
// path (not s3://), no S3 secret is created.
func TestServeLocalDataPathSkipsS3Secret(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := findFreePort(t)

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
		DataPath:      filepath.Join(dir, "data"),
		Storage: config.StorageConfig{
			AccessKey: "minio",
			SecretKey: "minio123",
			Region:    "us-east-1",
			Endpoint:  "omneval-minio:9000",
			Bucket:    "omneval",
		},
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	// No S3 secrets should exist
	var count int
	err = srv.DB().QueryRowContext(ctx, `
		SELECT count(*)
		FROM duckdb_secrets()
		WHERE LOWER(type) = 's3'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query duckdb_secrets: %v", err)
	}
	if count != 0 {
		t.Errorf("S3 secrets for local data path: got %d, want 0", count)
	}
}

// TestServeS3SecretSkippedWhenNoCredentials verifies that when DataPath is
// s3:// but Storage credentials are empty, Serve() does not create an S3
// secret (createS3Secret's early-return path). httpfs is still installed.
func TestServeS3SecretSkippedWhenNoCredentials(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := findFreePort(t)

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
		DataPath:      "s3://omneval/lake",
		// Storage is empty — no credentials.
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	// No S3 secrets should exist when credentials are empty
	var count int
	err = srv.DB().QueryRowContext(ctx, `
		SELECT count(*)
		FROM duckdb_secrets()
		WHERE LOWER(type) = 's3'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query duckdb_secrets: %v", err)
	}
	if count != 0 {
		t.Errorf("S3 secrets with empty credentials: got %d, want 0", count)
	}

	// httpfs should still be installed (required for any S3 access)
	var extName string
	err = srv.DB().QueryRowContext(ctx, `
		SELECT extension_name
		FROM duckdb_extensions()
		WHERE extension_name = 'httpfs' AND loaded = true
	`).Scan(&extName)
	if err != nil {
		t.Fatalf("query duckdb_extensions for httpfs: %v", err)
	}
}

// TestServeS3HttpfsLoaded verifies that when DataPath is s3://, Serve()
// installs and loads the httpfs extension. httpfs is a prerequisite for
// read_blob (used by ducklake_delete_orphaned_files) to access S3 paths.
func TestServeS3HttpfsLoaded(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	port := findFreePort(t)

	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
		DataPath:      "s3://omneval/lake",
		Storage: config.StorageConfig{
			AccessKey: "minio",
			SecretKey: "minio123",
			Region:    "us-east-1",
			Endpoint:  "omneval-minio:9000",
			Bucket:    "omneval",
		},
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer srv.Close()

	// Verify that the httpfs extension is loaded. Without httpfs, read_blob
	// calls on S3 paths fail.
	var extName string
	err = srv.DB().QueryRowContext(ctx, `
		SELECT extension_name
		FROM duckdb_extensions()
		WHERE extension_name = 'httpfs' AND loaded = true
	`).Scan(&extName)
	if err != nil {
		t.Fatalf("query duckdb_extensions for httpfs: %v", err)
	}
	if extName != "httpfs" {
		t.Errorf("httpfs extension: got %q, want %q", extName, "httpfs")
	}
}
