package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	internalauth "github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake"
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
	Lake         *lake.Lake // nil unless query.lake.enabled
	DBPath       string
	S3           *s3.Store // nil when storage is not configured
	SyncInterval time.Duration
	SessionTTL   time.Duration
	QueryMetrics *metrics.QueryMetrics
	Prober       *probe.Prober

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

	lakeEnabled := cfg.Query.Lake.Enabled

	// Download snapshot from S3 (if configured). Lake mode reads committed
	// data straight from the Lake — no snapshot download, no polling.
	if deps.S3 != nil && !lakeEnabled {
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
	} else if lakeEnabled {
		slog.Info("query: lake mode enabled, skipping snapshot download")
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

	// Attach the Lake read-only when enabled (ADR-0004). All span/score
	// reads — span list, trace detail, conversations, Analytics DSL —
	// are served from it through the Lake's main-schema views.
	if lakeEnabled {
		lakeCfg := lake.ConfigFromApp(cfg)
		lakeCfg.ReadOnly = true
		lk, err := lake.Open(context.Background(), lakeCfg)
		if err != nil {
			deps.Close()
			return nil, fmt.Errorf("query: open lake: %w", err)
		}
		deps.Lake = lk
	}

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

	// Create handlers. In lake mode, span/score readers query the Lake
	// (through its main-schema views); the bookmark toggle and admin
	// endpoints stay on the legacy snapshot until #84/#91 land their
	// metadata-store and Lake-write moves.
	var readDB handler.DBHandle = sdb
	if deps.Lake != nil {
		readDB = deps.Lake.DB()
	}
	deps.Span = &handler.SpanHandler{DB: readDB, SessionStore: h, LakeMode: lakeEnabled}
	deps.Bookmark = &handler.BookmarkHandler{DB: sdb, SessionStore: h}
	deps.Conversation = &handler.ConversationHandler{DB: readDB, SessionStore: h}

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
		DB:                readDB,
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

	// Health and readiness probes. Lake mode gates on Catalog
	// reachability; the legacy path gates on the snapshot file.
	p := probe.New()
	if deps.Lake != nil {
		p.AddCheck("lake-catalog", checkFunc(deps.Lake.CheckCatalog))
	} else {
		p.AddCheck("snapshot", &probe.FileExists{Path: deps.DBPath})
	}
	deps.Prober = p

	return deps, nil
}

// checkFunc adapts a plain function to the probe.Check interface.
type checkFunc func(ctx context.Context) error

func (f checkFunc) Check(ctx context.Context) error { return f(ctx) }
