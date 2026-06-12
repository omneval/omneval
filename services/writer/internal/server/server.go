package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/leader"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/pricing"
	"github.com/omneval/omneval/internal/probe"
	qredis "github.com/omneval/omneval/internal/queue/redis"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/writer/internal/flush"
	"github.com/omneval/omneval/services/writer/internal/handler"
	"github.com/omneval/omneval/services/writer/internal/metrics"
	"github.com/omneval/omneval/services/writer/internal/pipeline"
	"github.com/omneval/omneval/services/writer/internal/retention"
	syncpkg "github.com/omneval/omneval/services/writer/internal/sync"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	redisgo "github.com/redis/go-redis/v9"
)

// Run starts the Writer Service: drains the Redis ingest queue, writes to
// DuckDB, syncs snapshots to S3, flushes aged partitions as Parquet, and
// serves POST /internal/v1/scores for Eval Worker score write-back.
func Run() error {
	// Load config.
	cfgPath := ""
	if p := os.Getenv("OMNEVAL_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("writer: load config: %w", err)
	}

	// Default fencing_enabled to true when leader election is enabled.
	if cfg.Writer.LeaderElection.Enabled && !cfg.Writer.LeaderElection.FencingEnabled {
		cfg.Writer.LeaderElection.FencingEnabled = true
	}

	// Validate retention config before starting the worker.
	if err := cfg.Writer.Retention.Validate(); err != nil {
		return fmt.Errorf("writer: retention config: %w", err)
	}

	// Register Prometheus metrics.
	if err := metrics.Register(cfg.Metrics.DisableProjectLabels); err != nil {
		return fmt.Errorf("writer: register metrics: %w", err)
	}

	metricsHelper := metrics.NewWriterMetrics(cfg)

	// Initialize bundled pricing (runs once, lazy).
	pricing.InitBundledPricing()

	// Load pricing table (live fetch, fallback to bundled).
	overrides := make(map[string]pricing.ModelOverride)
	for model, ov := range cfg.Pricing.ModelOverrides {
		overrides[model] = pricing.ModelOverride{
			InputPerMillion:  ov.InputPerMillion,
			OutputPerMillion: ov.OutputPerMillion,
		}
	}
	pricingTable, err := pricing.Fetch(overrides)
	if err != nil {
		return fmt.Errorf("writer: load pricing: %w", err)
	}

	// Open DuckDB.
	dbPath := cfg.Writer.DuckDBPath
	if dbPath == "" {
		dbPath = "omneval.db"
	}
	db, err := duckdb.Open(dbPath)
	if err != nil {
		return fmt.Errorf("writer: open duckdb: %w", err)
	}

	// Open metadata store based on configured database driver.
	meta, err := openMetadataStore(cfg)
	if err != nil {
		db.Close()
		return fmt.Errorf("writer: open metadata: %w", err)
	}

	// Connect to Redis.
	rc := redisgo.NewClient(&redisgo.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rc.Ping(context.Background()).Err(); err != nil {
		db.Close()
		meta.Close()
		return fmt.Errorf("writer: redis ping: %w", err)
	}

	// Resolve hostname for leader election instance ID.
	hostname, _ := os.Hostname()

	// Create queue clients.
	ingestQ := qredis.NewIngestQueue(rc)
	evalQ := qredis.NewEvalQueue(rc)

	// Create leader election (if enabled).
	var election *leader.LeaderElection
	if cfg.Writer.LeaderElection.Enabled {
		lockTTL := time.Duration(cfg.Writer.LeaderElection.LockTTL) * time.Second
		ops := leader.NewOpsFromRedis(rc)
		election, err = leader.NewLeaderElection(
			ops,
			"omneval:writer:leader",
			fmt.Sprintf("writer-%s-%d", hostname, os.Getpid()),
			lockTTL,
		)
		if err != nil {
			db.Close()
			meta.Close()
			return fmt.Errorf("writer: leader election: %w", err)
		}

		// Acquire the leader lock.
		acquired, err := election.Acquire(context.Background())
		if err != nil {
			db.Close()
			meta.Close()
			return fmt.Errorf("writer: acquire leader lock: %w", err)
		}
		if acquired {
			slog.Info("writer: elected leader")
		} else {
			slog.Info("writer: not leader, waiting for lock",
				"current_leader", election.LeaderID())
		}
	}

	// Create pipeline.
	pl := pipeline.New(ingestQ, db, pricingTable, meta, evalQ, metricsHelper)

	// Create S3 store (nil if no S3 config).
	var s3store *s3pkg.Store
	var snapshotKey string
	if cfg.Storage.Bucket != "" || cfg.Storage.Endpoint != "" {
		s3store = s3pkg.New(&cfg.Storage)
		if s3store != nil {
			if err := s3store.EnsureBucket(context.Background()); err != nil {
				slog.Warn("writer: ensure bucket", "err", err)
			}
		}
		snapshotKey = s3pkg.SnapshotKey()
	}

	// Create reconciler (uses S3 store and snapshot key).
	var reconciler *Reconciler
	if s3store != nil {
		reconciler = NewReconciler(s3store, dbPath, snapshotKey)
	}

	// Create syncer (S3 snapshot sync).
	syncer := syncpkg.New(s3store, db, dbPath, cfg, metricsHelper)

	// Create flusher (aged partition flush to Parquet on S3).
	flusher := flush.NewWithDB(s3store, db, cfg)

	// Create retention worker (rotates/deletes aged S3 spans).
	var retentionWorker *retention.Worker
	if s3store != nil && cfg.Writer.Retention.Enabled {
		retentionWorker = retention.New(s3store, &cfg.Writer.Retention)
	}

	// Create score handler (handles POST /internal/v1/scores).
	scoreHandler := handler.New(db)

	// Set up health and readiness probes.
	p := probe.New()
	p.AddCheck("duckdb", &probe.DuckDBWritable{
		Open: func(path string) (probe.WritableView, error) {
			return duckdb.Open(path)
		},
		Path: dbPath,
	})
	p.AddCheck("redis", &probe.RedisPing{Pinger: func(ctx context.Context) error {
		return rc.Ping(ctx).Err()
	}})

	// Add reconciliation readiness gate if fencing is enabled.
	var reconStatus *reconciliationStatus
	if election != nil && cfg.Writer.LeaderElection.FencingEnabled {
		reconStatus = &reconciliationStatus{}
		p.AddCheck("reconciliation", reconStatus)
	}

	// Set up signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the dedicated Prometheus metrics server on cfg.Metrics.Addr (:9090).
	if err := StartMetricsServer(ctx, cfg.Metrics.Addr); err != nil {
		return fmt.Errorf("writer: start metrics server: %w", err)
	}

	// Build the full router: /metrics + /internal/v1/scores + probes.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/internal/v1/scores", scoreHandler)

	// Combine with probe routes.
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})

	// Start server.
	scoreServer := &http.Server{
		Addr:    cfg.Writer.Addr,
		Handler: combined,
	}
	go func() {
		slog.Info("writer: score handler listening", "addr", cfg.Writer.Addr)
		if err := scoreServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("writer: score handler error", "err", err)
		}
	}()

	// Start syncer (separate goroutine).
	go func() {
		if err := syncer.Run(ctx); err != nil {
			slog.Error("writer: syncer error", "err", err)
		}
	}()

	// Start flusher (separate goroutine).
	go func() {
		slog.Info("writer: flusher started")
		if err := flusher.Run(ctx); err != nil && err != context.Canceled {
			slog.Error("writer: flusher error", "err", err)
		}
	}()

	// Start retention worker (separate goroutine).
	if retentionWorker != nil {
		go func() {
			slog.Info("writer: retention worker started")
			if err := retentionWorker.RunLoop(ctx); err != nil && err != context.Canceled {
				slog.Error("writer: retention worker error", "err", err)
			}
		}()
	}

	// Start pipeline (blocks until ctx is canceled).
	slog.Info("writer: pipeline started")

	var pipelineErr error
	if election != nil {
		pipelineErr = runWithLeaderElection(ctx, election, pl, reconciler, reconStatus, db, cfg)
	} else {
		pipelineErr = pl.Run(ctx)
	}

	// Graceful shutdown.
	cancel()

	// Close DuckDB on shutdown.
	db.Close()
	meta.Close()

	// Release leader lock on shutdown.
	if election != nil {
		if err := releaseLeaderLock(ctx, election); err != nil {
			slog.Warn("writer: failed to release leader lock", "err", err)
		}
	}

	if err := gracefulShutdown(scoreServer, 30*time.Second); err != nil {
		return fmt.Errorf("writer: shutdown: %w", err)
	}
	slog.Info("writer: stopped")
	return pipelineErr
}

// reconciliationStatus tracks the reconciliation state after a leader transition.
// It implements the probe.Check interface to gate readiness until reconciliation
// completes.
type reconciliationStatus struct {
	mu         sync.Mutex
	reconciled bool // true after successful reconciliation
	err        error
}

// SetComplete marks reconciliation as successfully completed.
func (r *reconciliationStatus) SetComplete() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconciled = true
	r.err = nil
}

// SetError marks reconciliation as failed or in-progress.
func (r *reconciliationStatus) SetError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconciled = false
	r.err = err
}

// Check implements probe.Check: returns error until reconciliation completes.
func (r *reconciliationStatus) Check(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.reconciled {
		if r.err != nil {
			return fmt.Errorf("reconciliation not ready: %w", r.err)
		}
		return fmt.Errorf("reconciliation not yet started")
	}
	return nil
}

// gracefulShutdown shuts down an HTTP server with the given drain timeout.
func gracefulShutdown(srv *http.Server, timeout time.Duration) error {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	return nil
}

// runWithLeaderElection runs the pipeline only when this instance holds the leader lock.
// It starts a background renew loop and retries acquisition if leadership is lost.
// When fencing is enabled, it reconciles the S3 snapshot before accepting writes,
// and closes DuckDB immediately on lock loss.
func runWithLeaderElection(
	ctx context.Context,
	election *leader.LeaderElection,
	pl *pipeline.Pipeline,
	reconciler *Reconciler,
	reconStatus *reconciliationStatus,
	db *sql.DB,
	cfg *config.Config,
) error {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for {
		// Start renew loop in background.
		renewCtx, renewCancel := context.WithCancel(ctx)
		renewErrCh := make(chan error, 1)
		go func() {
			renewErrCh <- election.RenewLoop(renewCtx, leader.RenewIntervalDefault)
		}()

		// Try to acquire leadership if not already the leader.
		if !election.IsLeader() {
			if err := waitForLeadership(ctx, election, rng); err != nil {
				renewCancel()
				<-renewErrCh
				return err
			}
			slog.Info("writer: elected leader")
		}

		// Reconcile the S3 snapshot before accepting writes (if fencing is enabled).
		if err := reconcileLeaderSnapshot(ctx, reconciler, reconStatus); err != nil {
			slog.Warn("writer: snapshot reconciliation non-fatal", "err", err)
		}

		// Run the pipeline.
		plErr := pl.Run(ctx)

		// Pipeline completed (likely due to context cancellation).
		renewCancel()
		renewErr := <-renewErrCh

		// If we lost leadership and fencing is enabled, close DuckDB immediately
		// to prevent any window of dual-write.
		if errors.Is(renewErr, leader.ErrLostLeadership) {
			if cfg.Writer.LeaderElection.FencingEnabled {
				slog.Warn("writer: lost leadership — closing DuckDB to prevent dual-write")
				db.Close()
			}
		}

		// Release the leader lock.
		if err := releaseLeaderLock(ctx, election); err != nil {
			slog.Warn("writer: failed to release leader lock", "err", err)
		}

		if plErr != nil && plErr != context.Canceled {
			return fmt.Errorf("pipeline: %w", plErr)
		}
		if renewErr != nil && renewErr != context.Canceled {
			// We lost leadership — reset reconciliation status for next leader run.
			if reconStatus != nil {
				reconStatus.SetError(fmt.Errorf("reconciliation reset (lock lost)"))
			}
			slog.Info("writer: lost leadership, retrying...")
			continue
		}

		return ctx.Err()
	}
}

// waitForLeadership blocks until this instance acquires the leader lock
// or the context is cancelled. Returns with jitter-backed retries.
func waitForLeadership(ctx context.Context, election *leader.LeaderElection, rng *rand.Rand) error {
	for {
		acquired, err := election.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("leader election: acquire: %w", err)
		}
		if acquired {
			return nil
		}

		// Wait with jitter before retrying.
		wait := time.Duration(rng.Intn(3)+1) * time.Second
		slog.Info("writer: not leader, retrying in", "seconds", wait)
		select {
		case <-time.After(wait):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// reconcileLeaderSnapshot reconciles the S3 snapshot when this instance
// becomes leader. If fencing is disabled (reconciler is nil), it returns nil.
// Returns nil on success or when reconciliation is skipped.
func reconcileLeaderSnapshot(
	ctx context.Context,
	reconciler *Reconciler,
	reconStatus *reconciliationStatus,
) error {
	if reconciler == nil {
		return nil
	}

	slog.Info("writer: fencing enabled, reconciling snapshot before accepting writes")
	reconStatus.SetError(fmt.Errorf("reconciliation in progress"))

	if err := reconciler.Reconcile(ctx); err != nil {
		reconStatus.SetError(err)
		return err
	}

	slog.Info("writer: fencing: snapshot reconciled, ready to accept writes")
	reconStatus.SetComplete()
	return nil
}

// releaseLeaderLock releases the leader lock gracefully.
func releaseLeaderLock(ctx context.Context, election *leader.LeaderElection) error {
	releaseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	released, err := election.Release(releaseCtx)
	if err != nil {
		return fmt.Errorf("leader: release: %w", err)
	}
	if released {
		slog.Info("writer: released leader lock")
	}
	return nil
}

// openMetadataStore opens the configured metadata store via the shared
// factory, applying the writer's default SQLite path when no DSN is set.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	dsn := cfg.Database.DSN
	if dsn == "" && (cfg.Database.Driver == "" || cfg.Database.Driver == "sqlite") {
		dsn = "omneval_meta.db"
	}
	return metadata.Open(cfg.Database.Driver, dsn)
}
