package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/lake"
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
	"github.com/prometheus/client_golang/prometheus"
	redisgo "github.com/redis/go-redis/v9"
)

// WiredDeps holds every component the writer needs to run, fully constructed
// and ready for use. Tests can build one by hand (with real or mock
// components) and pass it to RunWired without touching WireDeps.
type WiredDeps struct {
	Cfg          *config.Config
	Pipeline     *pipeline.Pipeline
	Syncer       *syncpkg.Syncer
	Flusher      *flush.Flusher
	Retention    *retention.Worker // nil when retention is disabled or S3 is not configured
	ScoreHandler http.Handler
	DB           *sql.DB
	DBPath       string
	Lake         *lake.Lake // nil unless writer.lake.enabled
	Meta         metadata.Store
	Redis        *redisgo.Client
	Election     *leader.LeaderElection // nil when leader election is disabled
	Reconciler   *Reconciler            // nil when S3 is not configured
	ReconStatus  *ReconciliationStatus  // nil unless fencing is enabled
	Prober       *probe.Prober
}

// Close releases the infrastructure handles held by the deps. It is used on
// startup failure paths; during normal operation RunWired manages shutdown
// ordering itself.
func (d *WiredDeps) Close() {
	if d.DB != nil {
		d.DB.Close()
	}
	if d.Lake != nil {
		d.Lake.Close()
	}
	if d.Meta != nil {
		d.Meta.Close()
	}
	if d.Redis != nil {
		d.Redis.Close()
	}
}

// WireDeps is the deep module behind Run: it validates config and connects
// all infrastructure (DuckDB, metadata store, Redis, pricing, S3, leader
// election), returning ready-to-use components. On error, any
// already-opened resources are closed.
func WireDeps(cfg *config.Config) (*WiredDeps, error) {
	// Default fencing_enabled to true when leader election is enabled.
	if cfg.Writer.LeaderElection.Enabled && !cfg.Writer.LeaderElection.FencingEnabled {
		cfg.Writer.LeaderElection.FencingEnabled = true
	}

	// Validate retention config before starting the worker.
	if err := cfg.Writer.Retention.Validate(); err != nil {
		return nil, fmt.Errorf("writer: retention config: %w", err)
	}

	// Register Prometheus metrics. Tolerate re-registration so WireDeps can
	// be called more than once per process (tests).
	if err := metrics.Register(cfg.Metrics.DisableProjectLabels); err != nil {
		var are prometheus.AlreadyRegisteredError
		if !errors.As(err, &are) {
			return nil, fmt.Errorf("writer: register metrics: %w", err)
		}
	}
	metricsHelper := metrics.NewWriterMetrics(cfg)

	// Open DuckDB.
	dbPath := cfg.Writer.DuckDBPath
	if dbPath == "" {
		dbPath = "omneval.db"
	}
	db, err := duckdb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("writer: open duckdb: %w", err)
	}

	deps := &WiredDeps{Cfg: cfg, DB: db, DBPath: dbPath}

	// Open metadata store based on configured database driver.
	meta, err := openMetadataStore(cfg)
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("writer: open metadata: %w", err)
	}
	deps.Meta = meta

	// Connect to Redis.
	rc := redisgo.NewClient(&redisgo.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	deps.Redis = rc
	if err := rc.Ping(context.Background()).Err(); err != nil {
		deps.Close()
		return nil, fmt.Errorf("writer: redis ping: %w", err)
	}

	// Create leader election (if enabled) and try an initial acquisition.
	if cfg.Writer.LeaderElection.Enabled {
		election, err := setupLeaderElection(cfg, leader.NewOpsFromRedis(rc))
		if err != nil {
			deps.Close()
			return nil, err
		}
		deps.Election = election
	}

	// Initialize bundled pricing (runs once, lazy) and load the pricing
	// table (live fetch, fallback to bundled).
	pricing.InitBundledPricing()
	overrides := make(map[string]pricing.ModelOverride)
	for model, ov := range cfg.Pricing.ModelOverrides {
		overrides[model] = pricing.ModelOverride{
			InputPerMillion:  ov.InputPerMillion,
			OutputPerMillion: ov.OutputPerMillion,
		}
	}
	pricingTable, err := pricing.Fetch(overrides)
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("writer: load pricing: %w", err)
	}

	// Attach the Lake when dual-writing is enabled (ADR-0004).
	if cfg.Writer.Lake.Enabled {
		lk, err := lake.Open(context.Background(), lake.ConfigFromApp(cfg))
		if err != nil {
			deps.Close()
			return nil, fmt.Errorf("writer: open lake: %w", err)
		}
		deps.Lake = lk
	}

	// Create queue clients and the span pipeline.
	ingestQ := qredis.NewIngestQueue(rc)
	evalQ := qredis.NewEvalQueue(rc)
	deps.Pipeline = pipeline.New(ingestQ, db, pricingTable, meta, evalQ, metricsHelper)
	if deps.Lake != nil {
		deps.Pipeline.WithLake(deps.Lake)
	}

	// Create S3 store (nil if no S3 config) and the components that need it.
	var s3store *s3pkg.Store
	if cfg.Storage.Bucket != "" || cfg.Storage.Endpoint != "" {
		s3store = s3pkg.New(&cfg.Storage)
		if s3store != nil {
			if err := s3store.EnsureBucket(context.Background()); err != nil {
				slog.Warn("writer: ensure bucket", "err", err)
			}
			deps.Reconciler = NewReconciler(s3store, dbPath, s3pkg.SnapshotKey())
			// With S3 available the pipeline runs the S3-first loop
			// (ADR-0004): acked-after-commit dequeue, Ingest Buffer
			// references resolved and deduped via the Batch Ledger.
			// Legacy payload entries still process unchanged.
			deps.Pipeline.WithBuffer(ingestQ, buffer.New(s3store), meta)
		}
	}

	deps.Syncer = syncpkg.New(s3store, db, dbPath, cfg, metricsHelper)
	deps.Flusher = flush.NewWithDB(s3store, db, cfg)
	if s3store != nil && cfg.Writer.Retention.Enabled {
		deps.Retention = retention.New(s3store, &cfg.Writer.Retention)
	}

	// Create score handler (handles POST /internal/v1/scores).
	var scoreLake handler.ScoreLakeWriter
	if deps.Lake != nil {
		scoreLake = deps.Lake
	}
	deps.ScoreHandler = handler.New(db, scoreLake)

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
	if deps.Election != nil && cfg.Writer.LeaderElection.FencingEnabled {
		deps.ReconStatus = &ReconciliationStatus{}
		p.AddCheck("reconciliation", deps.ReconStatus)
	}
	deps.Prober = p

	return deps, nil
}

// setupLeaderElection builds the LeaderElection from config and attempts an
// initial (non-blocking) acquisition of the leader lock.
func setupLeaderElection(cfg *config.Config, ops leader.Ops) (*leader.LeaderElection, error) {
	hostname, _ := os.Hostname()
	lockTTL := time.Duration(cfg.Writer.LeaderElection.LockTTL) * time.Second
	election, err := leader.NewLeaderElection(
		ops,
		"omneval:writer:leader",
		fmt.Sprintf("writer-%s-%d", hostname, os.Getpid()),
		lockTTL,
	)
	if err != nil {
		return nil, fmt.Errorf("writer: leader election: %w", err)
	}

	acquired, err := election.Acquire(context.Background())
	if err != nil {
		return nil, fmt.Errorf("writer: acquire leader lock: %w", err)
	}
	if acquired {
		slog.Info("writer: elected leader")
	} else {
		slog.Info("writer: not leader, waiting for lock",
			"current_leader", election.LeaderID())
	}
	return election, nil
}
