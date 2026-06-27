package server

import (
	"context"
	"fmt"
	"net/http"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/harness"
	_ "github.com/omneval/omneval/internal/duckdbfix"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/handler"
	"github.com/omneval/omneval/services/query/internal/public"
)

// IsPublicAPIPath reports whether the given path is a public API route
// that does not require authentication. Delegates to the canonical
// public.IsPublicPath so there is a single source of truth.
func IsPublicAPIPath(path string) bool {
	return public.IsPublicPath(path)
}

// Run starts the Query API: opens the DuckDB snapshot from S3 and the
// metadata store, bootstraps the admin user, and serves auth, span,
// analytics, prompt, and metrics endpoints.
func Run() error {
	h, err := harness.New("")
	if err != nil {
		return fmt.Errorf("create harness: %w", err)
	}

	return h.Run(context.Background(), func(ctx *harness.HarnessContext) (string, http.Handler, harness.ShutdownFunc, harness.ShutdownFunc, error) {
		cfg := ctx.Cfg

		deps, err := WireDeps(cfg)
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("wire deps: %w", err)
		}

		router := buildRouter(deps)

		// Pre-HTTP-shutdown: tell the Lake first so any reconnect() already
		// in flight (or queued behind one) aborts immediately instead of
		// running its own ~10s budget, which otherwise stacks across queued
		// callers and can exceed the shutdown drain timeout below (production
		// incident: query pod logged "shutting down..." and then kept retrying
		// "reconnected to quack server" until kubelet's SIGKILL, because
		// reconnect() ignored shutdown and ran to completion regardless).
		preShutdown := func() {
			deps.Lake.Shutdown()
		}

		// Teardown called after HTTP server has drained.
		teardown := func() {
			deps.Store.Close()
			deps.Lake.Close()
			deps.ProbeLake.Close()
		}

		return ctx.Cfg.Query.Addr, router, preShutdown, teardown, nil
	})
}

// buildRouter wires every route against the handlers in deps and wraps them
// in the session/API-key auth middleware, using the unified Router module.
func buildRouter(deps *WiredDeps) http.Handler {
	// Build the Router from wired deps and register all routes.
	router := handler.NewRouter(&handler.RouterDeps{
		Cfg:             deps.Cfg,
		Store:           deps.Store,
		Auth:            deps.Auth,
		Span:            deps.Span,
		Bookmark:        deps.Bookmark,
		Conversation:    deps.Conversation,
		Prompt:          deps.Prompt,
		EvalRule:        deps.EvalRule,
		Admin:           deps.Admin,
		Dataset:         deps.Dataset,
		DatasetRun:      deps.DatasetRun,
		Playground:      &handler.PlaygroundRouterGroup{HandleRun: deps.Playground.HandleRun},
		Models:          deps.Models,
		AdminLake:       deps.AdminLake,
		SessionTTL:      deps.SessionTTL,
		APIKeyValidator: deps.APIKeyValidator,
	})

	return router.RegisterRoutes(http.NewServeMux())
}

// openMetadataStore opens the configured metadata store via the shared
// factory. The factory applies the default SQLite path when no DSN is set.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	return metadata.Open(cfg.Database.Driver, cfg.Database.DSN)
}
