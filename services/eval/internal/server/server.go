package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	redisgo "github.com/redis/go-redis/v9"
	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/duckdb"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	qredis "github.com/zbloss/lantern/internal/queue/redis"
	"github.com/zbloss/lantern/services/eval/internal/judge"
	"github.com/zbloss/lantern/internal/probe"
	"github.com/zbloss/lantern/services/eval/internal/worker"
)

// Run starts the Eval Worker pool: drains the Redis eval queue and
// dispatches LLM-as-a-Judge jobs. Serves /healthz and /readyz probes.
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

	// Set up health and readiness probes.
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{Pinger: func(ctx context.Context) error {
		return rc.Ping(ctx).Err()
	}})

	// Health + readiness router.
	probeHandler := p.Router()

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("eval: started")

	// Start server (ready to accept score write-back and future endpoints).
	addr := cfg.Eval.Addr
	srv := &http.Server{
		Addr:    addr,
		Handler: probeHandler,
	}

	go func() {
		slog.Info("eval workers listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("eval: server error", "err", err)
		}
	}()

	if err := w.Run(ctx); err != nil {
		return fmt.Errorf("eval: worker: %w", err)
	}

	// Block until signal.
	<-sigCh
	slog.Info("eval: shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("eval: shutdown: %w", err)
	}

	slog.Info("eval: stopped")
	return nil
}
