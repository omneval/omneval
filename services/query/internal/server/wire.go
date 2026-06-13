package server

import (
	"context"
	"database/sql"
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
	S3           *s3.Store // nil when storage is not configured
	SessionTTL   time.Duration
	QueryMetrics *metrics.QueryMetrics
	Prober       *probe.Prober

	// Lake is the DuckDB handle attached read-only to the Lake (DuckLake via
	// the Postgres Catalog). All span reads compile against this handle
	// (ADR-0004).
	Lake *sql.DB

	// AdminLake is a separate read-write Lake attachment used for durable
	// admin deletes (#91) and score writes.
	AdminLake *lake.Lake

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
	if d.Lake != nil {
		d.Lake.Close()
	}
	if d.AdminLake != nil {
		d.AdminLake.Close()
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
	deps.Span = &handler.SpanHandler{SessionStore: h, Meta: store}
	deps.Bookmark = &handler.BookmarkHandler{Store: store, SessionStore: h}
	deps.Conversation = &handler.ConversationHandler{SessionStore: h}

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
		Store:             store,
		SessionStore:      h,
		DefaultJudgeModel: cfg.Eval.LLMModel,
	}

	deps.Admin = &handler.AdminHandler{Store: store, SessionStore: h}

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
	if _, err := adminLake.DB().ExecContext(context.Background(),
		"CREATE OR REPLACE VIEW spans AS SELECT * FROM lake.spans"); err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: create admin lake view: %w", err)
	}
	deps.Admin.DB = adminLake.DB()
	deps.Admin.LakeRW = adminLake

	// Attach read-only to the Lake and gate readiness on Catalog
	// reachability (ADR-0004).
	lakeHandle, err := openLakeDB(cfg)
	if err != nil {
		deps.Close()
		return nil, fmt.Errorf("query: open lake: %w", err)
	}
	deps.Lake = lakeHandle
	deps.Span.Lake = lakeHandle
	deps.Conversation.Lake = lakeHandle
	// Eval-rule preview reads `spans` unqualified; the views
	// openLakeDB creates resolve that against the Lake.
	deps.EvalRule.DB = lakeHandle

	p.AddCheck("catalog", &probe.CatalogReachable{
		Ping: func(ctx context.Context) error {
			// A metadata-only scan of lake.spans forces a round trip
			// to the Catalog; pinging the in-memory DuckDB would not.
			var n int64
			return lakeHandle.
				QueryRowContext(ctx, "SELECT COUNT(*) FROM lake.spans WHERE 1 = 0").
				Scan(&n)
		},
	})
	slog.Info("query: routing all span reads to lake.spans")
	deps.Prober = p

	return deps, nil
}

// openLakeDB attaches read-only to the Lake (DuckLake via the Catalog) and
// returns the underlying *sql.DB.
func openLakeDB(cfg *config.Config) (*sql.DB, error) {
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
		if _, err := lk.DB().ExecContext(context.Background(), stmt); err != nil {
			lk.Close()
			return nil, fmt.Errorf("create lake view: %w", err)
		}
	}
	return lk.DB(), nil
}
