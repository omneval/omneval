package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	redisgo "github.com/redis/go-redis/v9"
	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/duckdb"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	qredis "github.com/zbloss/lantern/internal/queue/redis"
	"github.com/zbloss/lantern/services/eval/internal/judge"
	"github.com/zbloss/lantern/services/eval/internal/worker"
)

// Run starts the Eval Worker pool: drains the Redis eval queue and
// dispatches LLM-as-a-Judge jobs.
func Run() error {
	// Load config.
	cfgPath := ""
	if p := os.Getenv("LANTERN_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("eval: load config: %w", err)
	}

	// Open DuckDB (for fetching spans).
	dbPath := "eval_spans.db"
	if p := os.Getenv("LANTERN_WRITER_DUCKDB_PATH"); p != "" {
		dbPath = p
	}
	db, err := duckdb.Open(dbPath)
	if err != nil {
		return fmt.Errorf("eval: open duckdb: %w", err)
	}
	defer db.Close()

	// Open metadata store.
	meta, err := sqlite.New("eval_meta.db")
	if err != nil {
		return fmt.Errorf("eval: open metadata: %w", err)
	}
	defer meta.Close()

	// Run migrations.
	if err := meta.Migrate(context.Background()); err != nil {
		return fmt.Errorf("eval: migrate: %w", err)
	}

	// Connect to Redis.
	rc := redisgo.NewClient(&redisgo.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rc.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("eval: redis ping: %w", err)
	}

	// Create eval queue client.
	evalQ := qredis.NewEvalQueue(rc)

	// Create the judge.
	j := judge.New(meta, cfg.Eval.LLMBaseURL, cfg.Eval.LLMAPIKey)

	// Create the score writer (points to Writer service).
	writerAddr := ":8001"
	if p := os.Getenv("LANTERN_WRITER_ADDR"); p != "" {
		writerAddr = p
	}
	scoreWriter := worker.ScoreWriter{
		BaseURL: "http://" + writerAddr,
	}

	// Create the worker.
	concurrency := cfg.Eval.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	w := worker.New(evalQ, scoreWriter, j, db, concurrency)

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("eval: started")
	if err := w.Run(ctx); err != nil {
		return fmt.Errorf("eval: worker: %w", err)
	}

	return nil
}
