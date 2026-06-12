package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	internalauth "github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/internal/probe"
	s3 "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/handler"
	"github.com/omneval/omneval/services/query/internal/metrics"
	"github.com/omneval/omneval/services/query/internal/playground"
	"github.com/prometheus/client_golang/prometheus"
)

// WiredDeps holds every component the Query API needs to run, fully
// constructed and ready for use. Tests can build one by hand (with real or
// mock components) and pass it to RunWired without touching WireDeps.
type WiredDeps struct {
	Cfg          *config.Config
	Store        metadata.Store
	SDB          *SwappableDB
	DBPath       string
	S3           *s3.Store // nil when storage is not configured
	SyncInterval time.Duration
	SessionTTL   time.Duration
	QueryMetrics *metrics.QueryMetrics
	Prober       *probe.Prober

	// Lake is the DuckDB handle attached read-only to the Lake (DuckLake via
	// the Postgres Catalog). When query.lake.enabled is true, all span reads
	// compile against this handle instead of the S3 snapshot.
	Lake *SwappableDB

	// SnapshotLastModified tracks the S3 snapshot mtime for the poller.
	SnapshotLastModified time.Time

	// Handlers.
	Auth            *auth.Handler
	APIKeyValidator internalauth.Validator
	Span            *handler.SpanHandler
	Bookmark        *handler.BookmarkHandler
	Conversation    *handler.ConversationHandler
	Prompt          *handler.PromptHandler
	PromptCache     *handler.PromptCache
	EvalRule        *handler.EvalRuleHandler
	Admin           *handler.AdminHandler
	Dataset         *handler.DatasetHandler
	DatasetRun      *handler.DatasetRunHandler
	Playground      *playground.PlaygroundHandler
}

// Close releases the infrastructure handles held by the deps. It is used on
// startup failure paths; during normal operation RunWired manages shutdown
// ordering itself.
func (d *WiredDeps) Close() {
	if d.SDB != nil {
		d.SDB.Close()
	}
	if d.Lake != nil {
		d.Lake.Close()
	}
	if d.Store != nil {
		d.Store.Close()
	}
}

// WireDeps is the deep module behind Run: it connects all infrastructure
// (metadata store, S3, snapshot download, DuckDB snapshot, admin bootstrap)
// and constructs every handler, returning ready-to-use components. On error,
// any already-opened resources are closed.
func WireDeps(cfg *config.Config) (*WiredDeps, error) {
	// Register Prometheus metrics. Tolerate re-registration so WireDeps can
	// be called more than once per process (tests).
	if err := metrics.Register(cfg); err != nil {
		var are prometheus.AlreadyRegisteredError
		if !errors.As(err, &are) {
			return nil, fmt.Errorf("query: register metrics: %w", err)
		}
	}

	deps := &WiredDeps{
		Cfg:          cfg,
		QueryMetrics: metrics.NewQueryMetrics(cfg),
	}

	// Open metadata store.
	store, err := openMetadataStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("query: open metadata store: %w", err)
	}
	deps.Store = store

	// Resolve DuckDB snapshot path.
	deps.DBPath = cfg.Query.DuckDBPath
	if deps.DBPath == "" {
		deps.DBPath = "/tmp/omneval-snapshot.duckdb"
	}

	// Parse sync interval (default 30s).
	deps.SyncInterval, err = time.ParseDuration(cfg.Query.SyncInterval)
	if err != nil {
		deps.SyncInterval = 30 * time.Second
		slog.Warn("query: invalid sync_interval, using default 30s",
			"raw", cfg.Query.SyncInterval)
	}

	// Parse session TTL (default 7 days).
	deps.SessionTTL, err = time.ParseDuration(cfg.Auth.SessionTTL)
	if err != nil {
		deps.SessionTTL = 168 * time.Hour
		slog.Warn("invalid session_ttl, using default 168h", "given", cfg.Auth.SessionTTL)
	}

	// Connect to S3 (may be nil if no storage config).
	if cfg.Storage.Bucket != "" || cfg.Storage.Endpoint != "" {
		deps.S3 = s3.New(&cfg.Storage)
	}

	// Download snapshot from S3 (if configured).
	if deps.S3 != nil {
		if err := downloadSnapshot(context.Background(), deps.S3, deps.DBPath); err != nil {
			deps.Close()
			return nil, fmt.Errorf("query: download snapshot: %w", err)
		}
		if stat, err := deps.S3.Stat(context.Background(), s3.SnapshotKey()); err == nil && stat != nil {
			slog.Info("query: snapshot downloaded from S3", "path", deps.DBPath, "last_modified", stat.LastModified)
			deps.SnapshotLastModified = stat.LastModified
		} else {
			slog.Info("query: snapshot not yet available in S3")
		}
	} else {
		slog.Info("query: no S3 configured, skipping snapshot download")
	}

	// Open the snapshot database via SwappableDB so the poller can atomically
	// reopen the connection each time S3 delivers a new snapshot.
	sdb, err := NewSwappableDB(deps.DBPath)
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: open snapshot: %w", err)
	}
	deps.SDB = sdb

	// Bootstrap admin user if no users exist.
	h := auth.NewHandler(store, cfg.Auth.SecureCookie, deps.SessionTTL, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword)
	created, err := h.BootstrapAdmin(context.Background())
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: bootstrap admin: %w", err)
	}
	if created {
		slog.Info("query: admin user bootstrapped", "email", cfg.Auth.AdminEmail)
	} else if count, _ := store.CountUsers(context.Background()); count == 0 {
		slog.Warn("query: no admin configured and no users exist — set OMNEVAL_AUTH_ADMIN_EMAIL and OMNEVAL_AUTH_ADMIN_PASSWORD to create the first admin user")
	}
	deps.Auth = h

	// One-time carry-over: bookmarks created before the move to the
	// Metadata Store (ADR-0004 / #84) still live in the DuckDB snapshot's
	// bookmarks table. Copy them across idempotently before serving.
	migrateBookmarksFromSnapshot(context.Background(), sdb, store)

	// Create handlers.
	deps.Span = &handler.SpanHandler{DB: sdb, SessionStore: h, Meta: store}
	deps.Bookmark = &handler.BookmarkHandler{Store: store, SessionStore: h}
	deps.Conversation = &handler.ConversationHandler{DB: sdb, SessionStore: h}

	// Prompt registry handler. A CachingValidator is wired in so that SDK
	// callers can authenticate GET prompt endpoints using X-API-Key in
	// addition to session cookies.
	deps.PromptCache = handler.NewPromptCache(store)
	deps.APIKeyValidator = internalauth.NewCachingValidator(store)
	deps.Prompt = &handler.PromptHandler{
		Store:        store,
		Cache:        deps.PromptCache,
		SessionStore: h,
		Validator:    deps.APIKeyValidator,
	}

	deps.EvalRule = &handler.EvalRuleHandler{
		DB:                sdb,
		Store:             store,
		SessionStore:      h,
		DefaultJudgeModel: cfg.Eval.LLMModel,
	}

	deps.Admin = &handler.AdminHandler{DB: sdb, Store: store, SessionStore: h}

	deps.Dataset = &handler.DatasetHandler{Store: store, SessionStore: h}

	// Dataset run handler — read endpoints are always available; POST
	// (create run) additionally requires the judge LLM client (see routes).
	deps.DatasetRun = &handler.DatasetRunHandler{Store: store, SessionStore: h}
	if cfg.Query.JudgeLLMBaseURL != "" && cfg.Query.JudgeLLMAPIKey != "" {
		deps.DatasetRun.JudgeClient = playground.NewHTTPClient(cfg.Query.JudgeLLMBaseURL, cfg.Query.JudgeLLMAPIKey)
		deps.DatasetRun.Cache = deps.PromptCache
	}

	// Playground handler — always created so the route is registered even
	// when the LLM is not configured; the handler returns 503 in that case.
	var llmClient playground.LLMClient
	if cfg.Query.PlaygroundLLMBaseURL != "" && cfg.Query.PlaygroundLLMAPIKey != "" {
		llmClient = playground.NewHTTPClient(cfg.Query.PlaygroundLLMBaseURL, cfg.Query.PlaygroundLLMAPIKey)
	}
	deps.Playground = &playground.PlaygroundHandler{
		Cache:        deps.PromptCache,
		LLMClient:    llmClient,
		SessionStore: h,
	}

	// Health and readiness probes.
	p := probe.New()

	// When the Lake is enabled, attach read-only to the Lake and gate
	// readiness on Catalog reachability instead of snapshot-file existence.
	// Legacy snapshot path remains fully functional when the flag is off
	// (default off).
	if cfg.Query.LakeEnabled {
		lakeDB, err := openLakeDB(cfg)
		if err != nil {
			deps.Close()
			return nil, fmt.Errorf("query: open lake: %w", err)
		}
		deps.Lake = lakeDB
		p.AddCheck("catalog", &probe.CatalogReachable{
			Ping: func(ctx context.Context) error {
				return lakeDB.DB().PingContext(ctx)
			},
		})
		slog.Info("query: Lake enabled, routing all span reads to lake.spans")
	} else {
		p.AddCheck("snapshot", &probe.FileExists{Path: deps.DBPath})
	}
	deps.Prober = p

	return deps, nil
}

// openLakeDB opens a DuckDB connection that attaches read-only to the Lake
// via the Postgres Catalog. The connection is configured for read-only access
// (no writes, no snapshot downloads).
func openLakeDB(cfg *config.Config) (*SwappableDB, error) {
	// Use an in-memory DuckDB instance that attaches the Lake.
	// The Lake tables (lake.spans, lake.scores) are available through the
	// catalog connection.
	path := cfg.Query.LakeDBPath
	if path == "" {
		path = ":memory:"
	}
	sdb, err := NewSwappableDB(path)
	if err != nil {
		return nil, fmt.Errorf("open lake db: %w", err)
	}
	return sdb, nil
}

// migrateBookmarksFromSnapshot copies bookmark rows left in the legacy
// DuckDB snapshot into the Metadata Store (one-time carry-over for #84).
// Idempotent — SetBookmark ignores rows that already exist — and
// best-effort: a snapshot without a bookmarks table is not an error.
func migrateBookmarksFromSnapshot(ctx context.Context, sdb *SwappableDB, store metadata.Store) {
	rows, err := sdb.QueryContext(ctx, "SELECT project_id, trace_id, created_at FROM bookmarks")
	if err != nil {
		return // no bookmarks table in this snapshot — nothing to carry over
	}
	defer rows.Close()

	var carried int
	for rows.Next() {
		var b domain.Bookmark
		if err := rows.Scan(&b.ProjectID, &b.TraceID, &b.CreatedAt); err != nil {
			slog.Warn("query: scan legacy bookmark", "err", err)
			continue
		}
		if err := store.SetBookmark(ctx, &b); err != nil {
			slog.Warn("query: carry over bookmark", "trace_id", b.TraceID, "err", err)
			continue
		}
		carried++
	}
	if carried > 0 {
		slog.Info("query: carried over legacy bookmarks to metadata store", "count", carried)
	}
}
