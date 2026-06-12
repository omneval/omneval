package lake

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
)

func localConfig(t *testing.T) (Config, string) {
	t.Helper()
	dir := t.TempDir()
	return Config{
		CatalogDriver: CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
		DataPath:      filepath.Join(dir, "data"),
	}, dir
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

// TestOpenIdempotentAndRoundTrip proves Open creates the partitioned tables
// idempotently, writes survive a reopen, and a fresh attachment reads the
// same rows.
func TestOpenIdempotentAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	cfg, _ := localConfig(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}

	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		testSpan("proj-a", "s1", start),
		testSpan("proj-b", "s2", start.Add(time.Hour)),
	}
	if err := l.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	score := &domain.Score{
		ScoreID:       "score-1",
		SpanID:        "s1",
		TraceID:       "trace-s1",
		ProjectID:     "proj-a",
		EvalName:      "helpfulness",
		Value:         0.9,
		Reasoning:     "good",
		JudgeModel:    "gpt-4o",
		CreatedAt:     time.Now(),
		SpanStartTime: start,
	}
	if err := l.InsertScores(ctx, []*domain.Score{score}); err != nil {
		t.Fatalf("insert scores: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: table creation and SET PARTITIONED BY must be idempotent.
	l2, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer l2.Close()

	var n int
	if err := l2.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count spans: %v", err)
	}
	if n != 2 {
		t.Errorf("spans: got %d, want 2", n)
	}

	var model, input string
	var cost float64
	err = l2.DB().QueryRowContext(ctx,
		"SELECT model, input, cost_usd FROM lake.spans WHERE span_id = 's1'",
	).Scan(&model, &input, &cost)
	if err != nil {
		t.Fatalf("read span: %v", err)
	}
	if model != "gpt-4o" || cost != 0.001 {
		t.Errorf("span fields: model=%q cost=%v", model, cost)
	}
	if !strings.Contains(input, "hi") {
		t.Errorf("input round-trip: %q", input)
	}

	var evalName string
	var val float64
	err = l2.DB().QueryRowContext(ctx,
		"SELECT eval_name, value FROM lake.scores WHERE score_id = 'score-1'",
	).Scan(&evalName, &val)
	if err != nil {
		t.Fatalf("read score: %v", err)
	}
	if evalName != "helpfulness" || val != 0.9 {
		t.Errorf("score fields: eval=%q value=%v", evalName, val)
	}
}

// TestPartitionLayout proves Parquet files land under hive-style
// project_id / date partition directories.
func TestPartitionLayout(t *testing.T) {
	ctx := context.Background()
	cfg, _ := localConfig(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var found bool
	err = filepath.WalkDir(cfg.DataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".parquet") &&
			strings.Contains(path, "project_id=proj-a") &&
			strings.Contains(path, "year=2026") &&
			strings.Contains(path, "month=6") &&
			strings.Contains(path, "day=2") {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk data path: %v", err)
	}
	if !found {
		t.Errorf("no parquet file under project_id=proj-a/year=2026/month=6/day=2 in %s", cfg.DataPath)
	}
}

// TestReadOnlyAttachRejectsWrites proves the Query API's read-only
// attachment cannot mutate the Lake.
func TestReadOnlyAttachRejectsWrites(t *testing.T) {
	ctx := context.Background()
	cfg, _ := localConfig(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "s1", time.Now())}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	l.Close()

	roCfg := cfg
	roCfg.ReadOnly = true
	ro, err := Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer ro.Close()

	var n int
	if err := ro.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("ro read: %v", err)
	}
	if n != 1 {
		t.Errorf("ro count: got %d, want 1", n)
	}
	if err := ro.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "s2", time.Now())}); err == nil {
		t.Error("read-only insert succeeded, want error")
	}
}

// TestScoreFallsBackToCreatedAt proves a score with unknown span start
// time still lands in a partition (keyed by CreatedAt).
func TestScoreFallsBackToCreatedAt(t *testing.T) {
	ctx := context.Background()
	cfg, _ := localConfig(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	created := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	score := &domain.Score{
		ScoreID: "score-1", SpanID: "s1", TraceID: "t1", ProjectID: "p1",
		EvalName: "e", Value: 1, CreatedAt: created,
	}
	if err := l.InsertScores(ctx, []*domain.Score{score}); err != nil {
		t.Fatalf("insert score: %v", err)
	}

	var spanStart time.Time
	err = l.DB().QueryRowContext(ctx,
		"SELECT span_start_time FROM lake.scores WHERE score_id = 'score-1'",
	).Scan(&spanStart)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !spanStart.Equal(created) {
		t.Errorf("span_start_time: got %v, want %v", spanStart, created)
	}
}

// TestDeleteProject proves DeleteProject removes a project's spans and
// scores durably, leaves other projects untouched, and reclaims the
// deleted project's Parquet files (#91).
func TestDeleteProject(t *testing.T) {
	ctx := context.Background()
	cfg, _ := localConfig(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{
		testSpan("proj-a", "s1", start),
		testSpan("proj-b", "s2", start),
	}); err != nil {
		t.Fatalf("insert spans: %v", err)
	}
	if err := l.InsertScores(ctx, []*domain.Score{
		{ScoreID: "score-1", SpanID: "s1", TraceID: "trace-s1", ProjectID: "proj-a",
			EvalName: "e", Value: 1, CreatedAt: start, SpanStartTime: start},
		{ScoreID: "score-2", SpanID: "s2", TraceID: "trace-s2", ProjectID: "proj-b",
			EvalName: "e", Value: 1, CreatedAt: start, SpanStartTime: start},
	}); err != nil {
		t.Fatalf("insert scores: %v", err)
	}

	if err := l.DeleteProject(ctx, "proj-a"); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	var n int
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE project_id = 'proj-a'").Scan(&n); err != nil {
		t.Fatalf("count proj-a spans: %v", err)
	}
	if n != 0 {
		t.Errorf("proj-a spans: got %d, want 0", n)
	}
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.scores WHERE project_id = 'proj-a'").Scan(&n); err != nil {
		t.Fatalf("count proj-a scores: %v", err)
	}
	if n != 0 {
		t.Errorf("proj-a scores: got %d, want 0", n)
	}

	// proj-b is untouched.
	if err := l.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE project_id = 'proj-b'").Scan(&n); err != nil {
		t.Fatalf("count proj-b spans: %v", err)
	}
	if n != 1 {
		t.Errorf("proj-b spans: got %d, want 1", n)
	}

	// The reclaim pass deletes the Parquet files that backed proj-a's rows.
	var found bool
	err = filepath.WalkDir(cfg.DataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".parquet") && strings.Contains(path, "project_id=proj-a") {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk data path: %v", err)
	}
	if found {
		t.Error("proj-a Parquet file still present after DeleteProject")
	}
}

// TestDeleteProjectReadOnlyRejected proves a read-only attachment cannot
// run DeleteProject.
func TestDeleteProjectReadOnlyRejected(t *testing.T) {
	ctx := context.Background()
	cfg, _ := localConfig(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "s1", time.Now())}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	l.Close()

	roCfg := cfg
	roCfg.ReadOnly = true
	ro, err := Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer ro.Close()

	if err := ro.DeleteProject(ctx, "proj-a"); err == nil {
		t.Error("DeleteProject on read-only attachment succeeded, want error")
	}
}

func TestConfigFromApp(t *testing.T) {
	t.Run("postgres prod defaults", func(t *testing.T) {
		app := &config.Config{
			Database: config.DatabaseConfig{Driver: "postgres", DSN: "postgres://u:p@h/db"},
			Storage:  config.StorageConfig{Bucket: "omneval", Endpoint: "http://minio:9000"},
		}
		lc := ConfigFromApp(app)
		if lc.CatalogDriver != CatalogDriverPostgres {
			t.Errorf("driver: %q", lc.CatalogDriver)
		}
		if lc.CatalogDSN != "postgres://u:p@h/db" {
			t.Errorf("dsn: %q", lc.CatalogDSN)
		}
		if lc.DataPath != "s3://omneval/lake" {
			t.Errorf("data path: %q", lc.DataPath)
		}
		if lc.Storage == nil {
			t.Error("storage creds not propagated for s3 data path")
		}
	})

	t.Run("demo defaults", func(t *testing.T) {
		app := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", DSN: "demo.db"}}
		lc := ConfigFromApp(app)
		if lc.CatalogDriver != CatalogDriverLocal {
			t.Errorf("driver: %q", lc.CatalogDriver)
		}
		if lc.CatalogDSN == "" || lc.DataPath == "" {
			t.Errorf("defaults missing: dsn=%q data=%q", lc.CatalogDSN, lc.DataPath)
		}
		if lc.Storage != nil {
			t.Error("unexpected storage creds for local data path")
		}
	})

	t.Run("explicit overrides win", func(t *testing.T) {
		app := &config.Config{
			Database: config.DatabaseConfig{Driver: "postgres", DSN: "postgres://meta"},
			Lake: config.LakeConfig{
				CatalogDriver: CatalogDriverLocal,
				CatalogDSN:    "/tmp/cat.ducklake",
				DataPath:      "/tmp/lakedata",
			},
		}
		lc := ConfigFromApp(app)
		if lc.CatalogDriver != CatalogDriverLocal || lc.CatalogDSN != "/tmp/cat.ducklake" || lc.DataPath != "/tmp/lakedata" {
			t.Errorf("overrides not honored: %+v", lc)
		}
	})
}
