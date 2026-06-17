package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/omneval/omneval/internal/buffer"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/pricing"
	"github.com/omneval/omneval/internal/probe"
	qredis "github.com/omneval/omneval/internal/queue/redis"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/writer/internal/handler"
	"github.com/omneval/omneval/services/writer/internal/metrics"
	"github.com/omneval/omneval/services/writer/internal/pipeline"
	"github.com/omneval/omneval/services/writer/internal/reconcile"
	"github.com/prometheus/client_golang/prometheus"
	redisgo "github.com/redis/go-redis/v9"
)

// WiredDeps holds every component the writer needs to run, fully constructed
// and ready for use. Tests can build one by hand (with real or mock
// components) and pass it to RunWired without touching WireDeps.
type WiredDeps struct {
	Cfg          *config.Config
	Pipeline     *pipeline.Pipeline
	Reconcile    *reconcile.Worker // nil when reconciliation is disabled or S3 is not configured
	ScoreHandler http.Handler
	Lake         *lake.Lake // required when writer.lake.enabled (the default)
	Meta         metadata.Store
	Redis        *redisgo.Client
	Prober       *probe.Prober
}

// Close releases the infrastructure handles held by the deps. It is used on
// startup failure paths; during normal operation RunWired manages shutdown
// ordering itself.
func (d *WiredDeps) Close() {
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
// all infrastructure (the Lake, metadata store, Redis, pricing, S3),
// returning ready-to-use components. On error, any already-opened resources
// are closed.
func WireDeps(cfg *config.Config) (*WiredDeps, error) {
	// Validate reconciliation config before starting the worker.
	if err := cfg.Writer.Reconciliation.Validate(); err != nil {
		return nil, fmt.Errorf("writer: reconciliation config: %w", err)
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

	deps := &WiredDeps{Cfg: cfg}

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

	// Open the Lake — the sole storage tier (ADR-0004). Required when
	// writer.lake.enabled (the default); failure to open is a hard
	// startup error.
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
	deps.Pipeline = pipeline.New(ingestQ, pricingTable, meta, evalQ, metricsHelper)
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
			// With S3 available the pipeline runs the S3-first loop
			// (ADR-0004): acked-after-commit dequeue, Ingest Buffer
			// references resolved and deduped via the Batch Ledger.
			deps.Pipeline.WithBuffer(ingestQ, buffer.New(s3store), meta)
		}
	}

	if s3store != nil && cfg.Writer.Reconciliation.Enabled {
		deps.Reconcile = reconcile.New(s3store, meta, ingestQ, metricsHelper, &cfg.Writer.Reconciliation)
	}

	// Create score handler (handles POST /internal/v1/scores). The Lake is
	// authoritative for score writes too.
	var scoreLake handler.ScoreLakeWriter
	if deps.Lake != nil {
		scoreLake = deps.Lake
	}
	deps.ScoreHandler = handler.New(scoreLake)

	// Set up health and readiness probes.
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{Pinger: func(ctx context.Context) error {
		return rc.Ping(ctx).Err()
	}})
	if deps.Lake != nil {
		p.AddCheck("catalog", &probe.CatalogReachable{
			Ping: deps.Lake.Ping,
		})
	}
	deps.Prober = p

	return deps, nil
}
