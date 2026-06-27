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
	"github.com/omneval/omneval/internal/pricing"
	"github.com/omneval/omneval/internal/probe"
	s3 "github.com/omneval/omneval/internal/storage/s3"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/handler"
	"github.com/omneval/omneval/services/query/internal/metrics"
	"github.com/omneval/omneval/services/query/internal/playground"
	"github.com/omneval/omneval/services/query/internal/querybuild"
	"github.com/omneval/omneval/services/query/internal/spansegment"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// WiredDeps holds every component the Query API needs to run, fully
// constructed and ready for use. Tests can build one by hand (with real or
// mock components) and pass it to RunWired without touching WireDeps.
type WiredDeps struct {
	Cfg          *config.Config
	Store        metadata.Store
	S3           *s3.Store // nil when storage is not configured
	SessionTTL   time.Duration
	QueryMetrics *metrics.QueryMetrics
	Prober       *probe.Prober
	Redis        *redis.Client

	// Lake is the read-only Lake attachment. All span reads compile against
	// this handle (ADR-0004). It implements handler.DBHandle with transparent
	// reconnection on Quack Server restarts.
	Lake *lake.Lake

	// AdminLake is a separate read-write Lake attachment used for durable
	// admin deletes (#91) and score writes.
	AdminLake *lake.Lake

	// ProbeLake is a dedicated, single-connection, read-only Lake attachment
	// used exclusively by the readiness/liveness Catalog check (see
	// WireDeps). It is never touched by UI query handlers, so a burst of
	// concurrent UI queries that saturates Lake's own connection pool can
	// never make the probe's Ping queue behind it.
	ProbeLake *lake.Lake

	// QueryBuilder encapsulates the full DSL → SQL → execute → scan pipeline
	// for both span and analytics queries.
	QueryBuilder *querybuild.QueryBuilder

	// Handlers.
	Auth            *auth.Handler
	APIKeyValidator internalauth.Validator
	Span            *spansegment.SpanHandler
	Bookmark        *handler.BookmarkHandler
	Conversation    *handler.ConversationHandler
	Prompt          *handler.PromptHandler
	PromptCache     *handler.PromptCache
	EvalRule        *handler.EvalRuleHandler
	Admin           *handler.AdminHandler
	Dataset         *handler.DatasetHandler
	DatasetRun      *handler.DatasetRunHandler
	Playground      *playground.PlaygroundHandler
	Models          *handler.ModelsHandler

	// Pricing table used by the models endpoint and cost calculations.
	Pricing *pricing.Table
}

// Close releases the infrastructure handles held by the deps. It is used on
// startup failure paths; during normal operation RunWired manages shutdown
// ordering itself.
func (d *WiredDeps) Close() {
	if d.Lake != nil {
		d.Lake.Close() //nolint:errcheck
	}
	if d.AdminLake != nil {
		d.AdminLake.Close()
	}
	if d.ProbeLake != nil {
		d.ProbeLake.Close()
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

	// Connect to Redis for health-check reads (read-only ops metrics).
	// If the Redis address is empty, deps.Redis stays nil and the ops
	// endpoint gracefully returns an empty metrics map.
	if cfg.Redis.Addr != "" {
		deps.Redis = redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
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

	// Create handlers.
	deps.QueryBuilder = &querybuild.QueryBuilder{
		Lake:          deps.Lake,
		BookmarkStore: store.BookmarkStore(),
	}
	deps.Span = &spansegment.SpanHandler{
		SessionStore:    h,
		ProjectResolver: h,
		Lake:            deps.Lake,
		QueryBuilder:    deps.QueryBuilder,
	}
	deps.Bookmark = &handler.BookmarkHandler{BookmarkStore: store.BookmarkStore(), SessionStore: h, ProjectResolver: h}
	deps.Conversation = &handler.ConversationHandler{SessionStore: h, ProjectResolver: h}

	// Prompt registry handler. A CachingValidator is wired in so that SDK
	// callers can authenticate GET prompt endpoints using X-API-Key in
	// addition to session cookies.
	promptStore := store.PromptStore()
	deps.PromptCache = handler.NewPromptCache(promptStore)
	deps.APIKeyValidator = internalauth.NewCachingValidator(store)
	deps.Prompt = &handler.PromptHandler{
		PromptStore:     promptStore,
		Cache:           deps.PromptCache,
		SessionStore:    h,
		ProjectResolver: h,
		Validator:       deps.APIKeyValidator,
	}

	deps.EvalRule = &handler.EvalRuleHandler{
		EvalRuleStore:   store,
		PromptStore:     promptStore,
		SessionStore:    h,
		ProjectResolver: h,
		// DefaultJudgeModel is set below.
	}

	deps.Admin = &handler.AdminHandler{
		APIKeyStore:   store,
		BookmarkStore: store.BookmarkStore(),
		ProjectStore:  store.ProjectStore(),
		SessionStore:  h,
	}
	// Wire Redis as the ingest queue depth source for the ops endpoint.
	// *redis.Client satisfies ingestQueueLLEN via its LLen method.
	if deps.Redis != nil {
		deps.Admin.IngestQueueDB = deps.Redis
	}

	deps.Dataset = &handler.DatasetHandler{DatasetStore: store, SessionStore: h, ProjectResolver: h}

	// Dataset run handler — read endpoints are always available; POST
	// (create run) additionally requires the judge LLM client (see routes).
	deps.DatasetRun = &handler.DatasetRunHandler{
		DatasetStore:    store,
		EvalRuleStore:   store,
		SessionStore:    h,
		ProjectResolver: h,
	}
	deps.EvalRule.DefaultJudgeModel = cfg.Eval.LLMModel
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

	// Load pricing table for the models endpoint (live fetch, bundled fallback).
	pricing.InitBundledPricing()
	pricingTable, err := pricing.Fetch(nil)
	if err != nil {
		slog.Warn("query: pricing unavailable, models endpoint will return empty list", "error", err)
	} else {
		deps.Pricing = pricingTable
	}

	// Models handler — reads from the pricing table.
	deps.Models = &handler.ModelsHandler{Pricing: pricingTable}

	// Health and readiness probes.
	p := probe.New()

	// Admin gets its own read-write Lake attachment (#91): DuckLake's
	// catalog transactions make a second writer safe, and durable
	// deletes must commit through the Catalog rather than a read-only
	// attachment. Admin counts also read through this attachment so a
	// delete is reflected immediately, without waiting on the read-only
	// attachment's cached catalog snapshot. The same attachment backs
	// score writes (POST /api/v1/scores).
	//
	// This attachment is opened first (and read-write) because it is
	// responsible for creating the DuckLake catalog tables on a brand-new
	// Lake (ensureTables); the read-only attachment below fails to attach
	// if the catalog does not exist yet.
	adminLake, err := lake.Open(context.Background(), lake.ConfigFromApp(cfg))
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: open admin lake: %w", err)
	}
	deps.AdminLake = adminLake
	if _, err := adminLake.ExecContext(context.Background(),
		"CREATE OR REPLACE VIEW spans AS SELECT * FROM lake.spans"); err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: create admin lake view: %w", err)
	}
	deps.Admin.DB = adminLake
	deps.Admin.LakeRW = adminLake

	// Attach read-only to the Lake and gate readiness on Catalog
	// reachability (ADR-0004). The returned *lake.Lake implements
	// handler.DBHandle with transparent reconnection on Quack Server restarts.
	lk, err := openLakeDB(cfg)
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: open lake: %w", err)
	}
	deps.Lake = lk
	deps.Span.Lake = lk
	deps.Span.QueryBuilder = &querybuild.QueryBuilder{Lake: lk, BookmarkStore: store.BookmarkStore()}
	deps.Conversation.Lake = lk
	// Eval-rule preview reads `spans` unqualified; the views
	// openLakeDB creates resolve that against the Lake.
	deps.EvalRule.DB = lk

	// A third, dedicated Lake attachment backing only the readiness/
	// liveness Catalog check — see the AddCriticalCheck comment below for
	// why this must not share lk's connection pool. MaxOpenConns is forced
	// to 1 regardless of quack.client.max_open_conns: a health check never
	// needs more than one connection, and keeping it minimal keeps this
	// attachment's footprint against the Quack Server negligible.
	probeLakeCfg := lake.ConfigFromApp(cfg)
	probeLakeCfg.ReadOnly = true
	probeLakeCfg.MaxOpenConns = 1
	probeLake, err := lake.Open(context.Background(), probeLakeCfg)
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: open probe lake: %w", err)
	}
	deps.ProbeLake = probeLake

	// The Catalog check pings through a dedicated, single-connection Lake
	// attachment (ProbeLake) rather than lk — the pool UI query handlers
	// share. lk's pool (sized by quack.client.max_open_conns, tuned low —
	// see its history — to avoid contending too hard against the Quack
	// Server) can legitimately be fully subscribed by ordinary UI load: the
	// Dashboard alone fans out ten concurrent analytics queries on a single
	// page load. That's not a wedge, but Ping sharing lk's pool used to
	// queue behind it exactly the same as a real wedge would, and this check
	// gates liveness as well as readiness (below) — so a burst of normal
	// Dashboard traffic was enough to fail healthz and get kubelet to
	// restart a perfectly healthy pod, which then hit the same Dashboard
	// load again on the next request and crash-looped (production incident:
	// query pod liveness/readiness timed out under nothing worse than
	// normal Dashboard load). ProbeLake's own single connection is never
	// touched by any query handler, so it can't be starved by one.
	//
	// Ping is still catalog-touching and reconnect-aware (lake.go); on a
	// stale connection it re-attaches and retries so the pod self-heals
	// after a Quack Server restart without a manual rollout restart.
	// Critical: even with ProbeLake isolated from lk's pool, ProbeLake's own
	// single connection can still wedge (e.g. Quack Server itself hangs); if
	// that happens, readiness alone would only pull this pod out of Service
	// routing forever — it needs to also gate liveness so Kubernetes
	// eventually restarts it instead of requiring a human to notice and
	// intervene manually.
	p.AddCriticalCheck("catalog", &probe.CatalogReachable{Ping: deps.ProbeLake.Ping})
	slog.Info("query: routing all span reads to lake.spans")
	deps.Prober = p

	return deps, nil
}

// openLakeDB attaches read-only to the Lake (DuckLake via the Catalog) and
// returns the *lake.Lake, which implements handler.DBHandle with transparent
// reconnection on Quack Server restarts.
func openLakeDB(cfg *config.Config) (*lake.Lake, error) {
	lc := lake.ConfigFromApp(cfg)
	lc.ReadOnly = true
	lk, err := lake.Open(context.Background(), lc)
	if err != nil {
		return nil, fmt.Errorf("open lake: %w", err)
	}
	// Handlers that predate the Lake reference `spans`/`scores` unqualified
	// (eval-rule preview, admin counts). The Lake attach is read-only, but
	// views in the in-memory default catalog are allowed and resolve those
	// references to the Lake tables.
	for _, stmt := range []string{
		"CREATE OR REPLACE VIEW spans AS SELECT * FROM lake.spans",
		"CREATE OR REPLACE VIEW scores AS SELECT * FROM lake.scores",
	} {
		if _, err := lk.ExecContext(context.Background(), stmt); err != nil {
			lk.Close()
			return nil, fmt.Errorf("create lake view: %w", err)
		}
	}
	return lk, nil
}
