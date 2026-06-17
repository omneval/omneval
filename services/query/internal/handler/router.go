package handler

import (
	"net/http"
	"strings"
	"time"

	internalauth "github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// playgroundHandler is the interface for the playground endpoint.
// Defined here to avoid importing the playground package (which imports handler).
type playgroundHandler interface {
	HandleRun(http.ResponseWriter, *http.Request)
}

// RouterDeps collects the dependencies the Router needs. The server package
// populates this struct from WiredDeps before passing it to NewRouter, keeping
// the handler package from importing the server package (no circular dependency).
type RouterDeps struct {
	Cfg             *config.Config
	Store           metadata.Store
	Auth            *auth.Handler
	Span            *SpanHandler
	Bookmark        *BookmarkHandler
	Conversation    *ConversationHandler
	Prompt          *PromptHandler
	EvalRule        *EvalRuleHandler
	Admin           *AdminHandler
	Dataset         *DatasetHandler
	DatasetRun      *DatasetRunHandler
	Playground      playgroundHandler
	AdminLake       *lake.Lake
	SessionTTL      time.Duration
	APIKeyValidator internalauth.Validator
}

// Router is the deep interface for the query service HTTP layer. It owns all
// route registration, auth middleware application, project ID resolution, and
// response serialization. The existing handler structs (SpanHandler,
// ConversationHandler, DatasetHandler, EvalRuleHandler, PromptHandler, etc.)
// become thin adapters at a clean seam — they contain only domain logic and SQL,
// while the Router manages how they are composed and dispatched.
//
// Use [NewRouter] to construct one from router deps, then call
// [Router.RegisterRoutes] to wire a [http.ServeMux] and obtain the fully
// authenticated top-level handler.
type Router struct {
	cfg          *config.Config
	store        metadata.Store
	auth         *auth.Handler
	span         *SpanHandler
	bookmark     *BookmarkHandler
	conversation *ConversationHandler
	prompt       *PromptHandler
	evalRule     *EvalRuleHandler
	admin        *AdminHandler
	dataset      *DatasetHandler
	datasetRun   *DatasetRunHandler
	playground   playgroundHandler
	adminLake    *lake.Lake
	sessionTTL   time.Duration
	apiValidator internalauth.Validator
}

// NewRouter creates a Router from the provided dependencies. All handlers use
// the shared auth module for project ID extraction.
func NewRouter(deps *RouterDeps) *Router {
	return &Router{
		cfg:          deps.Cfg,
		store:        deps.Store,
		auth:         deps.Auth,
		span:         deps.Span,
		bookmark:     deps.Bookmark,
		conversation: deps.Conversation,
		prompt:       deps.Prompt,
		evalRule:     deps.EvalRule,
		admin:        deps.Admin,
		dataset:      deps.Dataset,
		datasetRun:   deps.DatasetRun,
		playground:   deps.Playground,
		adminLake:    deps.AdminLake,
		sessionTTL:   deps.SessionTTL,
		apiValidator: deps.APIKeyValidator,
	}
}

// RegisterRoutes registers all query-service routes on the given ServeMux
// and returns the fully authenticated top-level handler. The returned handler
// applies the auth middleware stack: public routes pass through, prompts and
// eval-rules accept session or API-key auth, and all other API routes require
// session auth.
func (rt *Router) RegisterRoutes(mux *http.ServeMux) http.Handler {
	// Register auth routes (login, logout, invite, change password).
	rt.auth.Register(mux)

	// Admin routes (require admin session).
	rt.admin.RegisterAdminRoutes(mux)

	// Projects list for the UI project switcher.
	mux.HandleFunc("GET /api/v1/projects", rt.span.HandleProjects)

	// Span list with keyset pagination.
	mux.HandleFunc("POST /api/v1/spans/query", rt.span.HandleSpansQuery)

	// Analytics: parameterized SQL compilation from structured DSL queries.
	mux.HandleFunc("POST /api/v1/analytics/spans", rt.span.HandleAnalyticsSpans)

	// Trace detail waterfall.
	mux.HandleFunc("GET /api/v1/traces/{traceId}", rt.span.HandleTraceDetail)

	// Trace bookmark toggle.
	mux.HandleFunc("POST /api/v1/traces/{traceId}/bookmark", rt.bookmark.HandleBookmark)

	// Conversation list and detail endpoints.
	mux.HandleFunc("GET /api/v1/conversations", rt.conversation.HandleListConversations)
	mux.HandleFunc("GET /api/v1/conversations/{conversationId}", rt.conversation.HandleConversationDetail)

	// Prompt Registry endpoints.
	mux.HandleFunc("GET /api/v1/prompts", rt.prompt.HandleListPrompts)
	mux.HandleFunc("POST /api/v1/prompts", rt.prompt.HandleCreatePrompt)
	mux.HandleFunc("GET /api/v1/prompts/{name}", rt.prompt.HandleGetPrompt)
	mux.HandleFunc("GET /api/v1/prompts/{name}/versions", rt.prompt.HandleListPromptVersions)
	mux.HandleFunc("PUT /api/v1/prompts/{name}/labels/{label}", rt.prompt.HandleSetLabel)

	// Eval rules endpoints.
	mux.HandleFunc("POST /api/v1/eval-rules", rt.evalRule.HandleCreate)
	mux.HandleFunc("GET /api/v1/eval-rules", rt.evalRule.HandleList)
	mux.HandleFunc("POST /api/v1/eval-rules/preview", rt.evalRule.HandlePreview)
	mux.HandleFunc("DELETE /api/v1/eval-rules/{id}", rt.evalRule.HandleDelete)

	// Dataset endpoints.
	mux.HandleFunc("POST /api/v1/datasets", rt.dataset.HandleCreate)
	mux.HandleFunc("GET /api/v1/datasets", rt.dataset.HandleList)
	mux.HandleFunc("GET /api/v1/datasets/{id}", rt.dataset.HandleGet)
	mux.HandleFunc("POST /api/v1/datasets/{id}/items", rt.dataset.HandleAddItems)
	mux.HandleFunc("POST /api/v1/datasets/{id}/items/batch", rt.dataset.HandleAddItemsBatch)
	mux.HandleFunc("GET /api/v1/datasets/{id}/items", rt.dataset.HandleListItems)
	mux.HandleFunc("DELETE /api/v1/datasets/{id}", rt.dataset.HandleDelete)

	// Dataset run endpoints — read endpoints (list, get, status) are always
	// available. POST (create run) requires judge LLM config.
	if rt.datasetRun.JudgeClient != nil {
		mux.HandleFunc("POST /api/v1/datasets/{id}/runs", rt.datasetRun.HandleRun)
	}
	mux.HandleFunc("GET /api/v1/datasets/{id}/runs", rt.datasetRun.HandleListRuns)
	mux.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}", rt.datasetRun.HandleGetRun)
	mux.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}/status", rt.datasetRun.HandleGetRunStatus)

	// Playground endpoint (route always registered; the handler returns 503
	// when the LLM is not configured).
	mux.HandleFunc("POST /api/v1/playground/run", rt.playground.HandleRun)

	// Score write endpoint (for eval worker score write-back, no auth required).
	// Scores are committed directly to the Lake via the AdminLake attachment
	// (ADR-0004/#91); SpanDB resolves span_start_time for partitioning.
	var spanDB DBHandle = rt.adminLake
	mux.HandleFunc("POST /api/v1/scores", NewScoreHandler(rt.adminLake, spanDB).ServeHTTP)

	// Prometheus metrics.
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)

	// Serve embedded UI for all other routes (SPA fallback to index.html).
	// The UI server function is injected at package init time by the server
	// package via InitServeUI so that the Router does not need to embed the UI.
	mux.HandleFunc("/", serveUI)

	return rt.buildMiddleware(mux)
}

// buildMiddleware returns the middleware-wrapped handler that routes requests
// through the correct auth layer.
func (rt *Router) buildMiddleware(mux *http.ServeMux) http.Handler {
	sessionMw := auth.RequireAuth(rt.store, rt.cfg.Auth.SecureCookie, rt.sessionTTL)
	adminMw := auth.RequireAdmin(rt.store, rt.cfg.Auth.SecureCookie, rt.sessionTTL, rt.cfg.Auth.AdminEmail)
	promptGetMw := auth.RequireSessionOrAPIKey(rt.store, rt.apiValidator, rt.cfg.Auth.SecureCookie, rt.sessionTTL, internalauth.APIKeyProjectIDContextKey)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path

		// Public routes bypass authentication entirely.
		if isPublicPath(path) {
			mux.ServeHTTP(w, req)
			return
		}

		// Admin routes require admin-level auth (email must match config).
		if strings.HasPrefix(path, "/api/v1/admin") {
			adminMw(mux).ServeHTTP(w, req)
			return
		}

		// Prompt and eval-rule endpoints accept X-API-Key (for SDKs) or session cookie.
		if strings.HasPrefix(path, "/api/v1/prompts") || strings.HasPrefix(path, "/api/v1/eval-rules") {
			promptGetMw(mux).ServeHTTP(w, req)
			return
		}

		// All other protected API routes require a valid session cookie.
		if strings.HasPrefix(path, "/api/v1/") {
			sessionMw(mux).ServeHTTP(w, req)
			return
		}

		// SPA fallback and anything else.
		mux.ServeHTTP(w, req)
	})
}

// isPublicPath reports whether the given path is a public API route
// that does not require authentication. Mirrors the logic in server.go.
func isPublicPath(path string) bool {
	publicPaths := map[string]struct{}{
		"/login":         {},
		"/logout":        {},
		"/healthz":       {},
		"/readyz":        {},
		"/metrics":       {},
		"/api/v1/scores": {},
	}
	if _, ok := publicPaths[path]; ok {
		return true
	}
	if strings.HasPrefix(path, "/healthz") || strings.HasPrefix(path, "/readyz") {
		return true
	}
	return false
}



// serveUI is the embedded UI server, injected by the server package at init.
// By default it returns 404 — the real handler is wired via InitServeUI.
var serveUI func(http.ResponseWriter, *http.Request) = func(w http.ResponseWriter, req *http.Request) {
	writeJSONError(w, "not found", http.StatusNotFound)
}

// InitServeUI injects the real UI server function that serves static files from
// the embedded UI dist directory. Call this once during server startup to replace
// the default no-op handler.
func InitServeUI(fn func(http.ResponseWriter, *http.Request)) {
	serveUI = fn
}
