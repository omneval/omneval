package lake

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
	"github.com/omneval/omneval/internal/lake/lakeserver"
)

// freePort finds an available TCP port for the test Quack Server to bind
// to (quack_serve does not support port 0 / auto-assign).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startTestServer starts a Quack Server (lakeserver.Serve) backed by a
// local DuckLake catalog file under t.TempDir(), and returns a Config that
// attaches to it as a Quack client. The server is closed automatically via
// t.Cleanup.
func startTestServer(t *testing.T) (Config, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	port := freePort(t)
	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
	})
	if err != nil {
		t.Fatalf("start quack server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	return Config{
		QuackAddr:  fmt.Sprintf("localhost:%d", port),
		QuackToken: srv.Token(),
		DataPath:   filepath.Join(dir, "data"),
	}, dir
}

// serverAddr extracts a "localhost:PORT" address from the server's reported
// listen address (the spike found only "localhost" works as the client-side
// host, not "127.0.0.1" or "0.0.0.0").
func serverAddr(t *testing.T, srv *lakeserver.Server) string {
	t.Helper()
	addr := srv.Addr()
	// srv.Addr() is something like "quack:0.0.0.0:9494" or "quack:localhost:0".
	addr = strings.TrimPrefix(addr, "quack://")
	addr = strings.TrimPrefix(addr, "quack:")
	_, port, ok := strings.Cut(addr, ":")
	if !ok || port == "" {
		t.Fatalf("could not parse port from server addr %q", srv.Addr())
	}
	return "localhost:" + port
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
	cfg, _ := startTestServer(t)

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

// TestInsertSpans_EmptyInputOutputCoercedToNull verifies that spans with
// empty Input/Output strings (e.g. tool-call or chain-root spans) do not
// fail the batch insert: DuckDB rejects "" as invalid JSON for JSON
// columns, so InsertSpans coerces empty strings to NULL (issue #53).
func TestInsertSpans_EmptyInputOutputCoercedToNull(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	spans := []*domain.Span{
		testSpan("proj-empty", "span-1", start),
		{
			SpanID:       "span-2",
			TraceID:      "trace-span-2",
			ProjectID:    "proj-empty",
			Name:         "tool-call",
			Kind:         domain.SpanKind("llm"),
			StartTime:    start.Add(time.Second),
			EndTime:      start.Add(2 * time.Second),
			Model:        "gpt-4o",
			Input:        "",
			Output:       `[{"role":"tool","content":"tool result"}]`,
			InputTokens:  5,
			OutputTokens: 3,
		},
		{
			SpanID:       "span-3",
			TraceID:      "trace-span-3",
			ProjectID:    "proj-empty",
			Name:         "chain-root",
			Kind:         domain.SpanKind("llm"),
			StartTime:    start.Add(3 * time.Second),
			EndTime:      start.Add(4 * time.Second),
			Model:        "gpt-4o",
			Input:        "",
			Output:       "",
			InputTokens:  0,
			OutputTokens: 0,
		},
	}

	if err := l.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	var n int
	if err := l.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM lake.spans WHERE project_id = 'proj-empty'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("span count: got %d, want 3", n)
	}

	var span3Input, span3Output *string
	if err := l.DB().QueryRowContext(ctx,
		"SELECT input, output FROM lake.spans WHERE span_id = 'span-3'",
	).Scan(&span3Input, &span3Output); err != nil {
		t.Fatalf("scan span-3 input/output: %v", err)
	}
	if span3Input != nil {
		t.Errorf("span-3 input: got %q, want NULL", *span3Input)
	}
	if span3Output != nil {
		t.Errorf("span-3 output: got %q, want NULL", *span3Output)
	}

	var span2Output string
	if err := l.DB().QueryRowContext(ctx,
		"SELECT CAST(output AS VARCHAR) FROM lake.spans WHERE span_id = 'span-2'",
	).Scan(&span2Output); err != nil {
		t.Fatalf("scan span-2 output: %v", err)
	}
	if span2Output == "" {
		t.Error("span-2 output is empty, expected JSON content")
	}
}

// TestPartitionLayout proves Parquet files land under hive-style
// project_id / date partition directories.
func TestPartitionLayout(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// DuckLake 1.5 inlines small inserts into Catalog metadata rather than
	// writing Parquet immediately (query correctness is unaffected, since
	// lake.spans reads inlined rows transparently) — flush before
	// inspecting the physical data path.
	if err := l.FlushInlinedData(ctx); err != nil {
		t.Fatalf("flush inlined data: %v", err)
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
	cfg, _ := startTestServer(t)

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
	cfg, _ := startTestServer(t)

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
// deleted project's Parquet files (#91, #105).
func TestDeleteProject(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

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

	// Do NOT call FlushInlinedData before DeleteProject: per
	// internal/lake/quack_spike6, once ducklake_flush_inlined_data has run
	// against a quack-backed catalog, a subsequent
	// ducklake_rewrite_data_files (in reclaim) fails with "Scanning a
	// DuckLake table after the transaction has ended" — even from a fresh
	// session/connection. ducklake_rewrite_data_files handles inlined data
	// itself (verified by quack_spike6's AUTOCOMMIT-NOFLUSH/TX-NOFLUSH
	// scenarios), so reclaim's rewrite step covers this without a separate
	// flush.
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

	// reclaim() now succeeds via the quack:// catalog (#105, building on
	// #111's spike finding), so proj-a's Parquet pages are physically
	// rewritten without its rows. Walking the data directory must succeed.
	if err := filepath.WalkDir(cfg.DataPath, func(path string, d fs.DirEntry, err error) error {
		return err
	}); err != nil {
		t.Fatalf("walk data path: %v", err)
	}
}

// TestDeleteProjectReadOnlyRejected proves a read-only attachment cannot
// run DeleteProject.
func TestDeleteProjectReadOnlyRejected(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

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
	t.Run("quack client url and token", func(t *testing.T) {
		app := &config.Config{
			Quack: config.QuackConfig{
				Client: config.QuackClientConfig{
					URL:   "quack://quack-server.omneval:9494",
					Token: "tok123",
				},
			},
			Storage: config.StorageConfig{Bucket: "omneval", Endpoint: "http://minio:9000"},
		}
		lc := ConfigFromApp(app)
		if lc.QuackAddr != "quack-server.omneval:9494" {
			t.Errorf("quack addr: %q", lc.QuackAddr)
		}
		if lc.QuackToken != "tok123" {
			t.Errorf("quack token: %q", lc.QuackToken)
		}
		if lc.DataPath != "s3://omneval/lake" {
			t.Errorf("data path: %q", lc.DataPath)
		}
		if lc.Storage == nil {
			t.Error("storage creds not propagated for s3 data path")
		}
	})

	t.Run("demo defaults", func(t *testing.T) {
		app := &config.Config{
			Quack: config.QuackConfig{
				Client: config.QuackClientConfig{URL: "localhost:9494"},
			},
		}
		lc := ConfigFromApp(app)
		if lc.QuackAddr != "localhost:9494" {
			t.Errorf("quack addr: %q", lc.QuackAddr)
		}
		if lc.DataPath == "" {
			t.Error("default data path missing")
		}
		if lc.Storage != nil {
			t.Error("unexpected storage creds for local data path")
		}
	})

	t.Run("explicit data path overrides storage default", func(t *testing.T) {
		app := &config.Config{
			Quack: config.QuackConfig{
				Client: config.QuackClientConfig{
					URL:      "localhost:9494",
					DataPath: "/tmp/lakedata",
				},
			},
			Storage: config.StorageConfig{Bucket: "omneval"},
		}
		lc := ConfigFromApp(app)
		if lc.DataPath != "/tmp/lakedata" {
			t.Errorf("data path: %q", lc.DataPath)
		}
	})
}

// TestInsertSpansSQL_PlaceholderCount verifies the INSERT statement has
// the same number of column names and VALUES placeholders (issue #53).
func TestInsertSpansSQL_PlaceholderCount(t *testing.T) {
	stmtSQL := insertSpansSQL

	// Extract column list: everything between first '(' after INTO lake.spans
	colStart := strings.Index(stmtSQL, "INTO lake.spans (")
	if colStart == -1 {
		t.Fatal("could not find column list in SQL")
	}
	colStart += len("INTO lake.spans (")

	// Find matching closing paren
	depth := 1
	colEnd := colStart
	for i := colStart; i < len(stmtSQL); i++ {
		if stmtSQL[i] == '(' {
			depth++
		} else if stmtSQL[i] == ')' {
			depth--
			if depth == 0 {
				colEnd = i
				break
			}
		}
	}
	colList := stmtSQL[colStart:colEnd]
	columns := strings.FieldsFunc(colList, func(r rune) bool { return r == ',' })
	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
	}

	// Extract VALUES clause
	valStart := strings.Index(stmtSQL, "VALUES (")
	if valStart == -1 {
		t.Fatal("could not find VALUES clause in SQL")
	}
	valStart += len("VALUES (")

	// Find matching closing paren
	depth = 1
	valEnd := valStart
	for i := valStart; i < len(stmtSQL); i++ {
		if stmtSQL[i] == '(' {
			depth++
		} else if stmtSQL[i] == ')' {
			depth--
			if depth == 0 {
				valEnd = i
				break
			}
		}
	}
	valList := stmtSQL[valStart:valEnd]
	placeholders := strings.FieldsFunc(valList, func(r rune) bool { return r == ',' })
	for i := range placeholders {
		placeholders[i] = strings.TrimSpace(placeholders[i])
	}

	if len(columns) != len(placeholders) {
		t.Errorf("column/placeholder mismatch: %d columns but %d placeholders", len(columns), len(placeholders))
		for i, col := range columns {
			if i < len(placeholders) {
				t.Logf("  col[%d] = %s vs val[%d] = %s", i, col, i, placeholders[i])
			} else {
				t.Logf("  col[%d] = %s (no matching placeholder)", i, col)
			}
		}
	}
}
