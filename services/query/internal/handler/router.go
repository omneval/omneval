package handler

import (
	"net/http"
	"time"

	internalauth "github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/metadata"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/public"
	"github.com/omneval/omneval/services/query/internal/routes"
	"github.com/omneval/omneval/services/query/internal/spansegment"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Re-export shared route types for backward compatibility.
type (
	AuthPolicy = routes.AuthPolicy
	AuthRoute  = routes.AuthRoute
)

const (
	AuthPolicyPublic     = routes.AuthPolicyPublic
	AuthPolicySession    = routes.AuthPolicySession
	AuthPolicyAPIKeyOrSession = routes.AuthPolicyAPIKeyOrSession
	AuthPolicyAdmin      = routes.AuthPolicyAdmin
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
	Span            spansegment.Segment
	Bookmark        *BookmarkHandler
	Conversation    *ConversationHandler
	Prompt          *PromptHandler
	EvalRule        *EvalRuleHandler
	Admin           *AdminHandler
	Dataset         *DatasetHandler
	DatasetRun      *DatasetRunHandler
	Playground      playgroundHandler
	Models          *ModelsHandler
	AdminLake       *lake.Lake
	SessionTTL      time.Duration
	APIKeyValidator internalauth.Validator
}

// authPolicyLookup maps a registered mux pattern ("METHOD /path/{param}", the
// exact string passed to mux.HandleFunc) to the auth policy that must be
// enforced. Built from allRoutes at router construction time.
//
// Lookups must be keyed by the *registered pattern*, not the concrete
// request path: a route like "/api/v1/traces/{traceId}" never equals a real
// request path like "/api/v1/traces/abc123", so looking this table up with
// req.URL.Path directly always misses for every route with a path
// parameter and silently falls back to AuthPolicyPublic — bypassing
// session/admin auth entirely for that route. Callers must resolve the
// concrete request to its registered pattern first via mux.Handler(req)
// (see buildMiddleware), which performs the same path-parameter matching
// http.ServeMux itself uses to dispatch the request.
type authPolicyLookup struct {
	policy map[string]AuthPolicy // "METHOD /registered/pattern" -> policy
}

// lookup returns the auth policy for the given registered mux pattern.
func (l *authPolicyLookup) lookup(pattern string) AuthPolicy {
	if p, ok := l.policy[pattern]; ok {
		return p
	}
	return AuthPolicyPublic // default to public for unmatched routes
}

// Router is the deep interface for the query service HTTP layer. It owns all
// route registration, auth middleware application, project ID resolution, and
// response serialization. Domain handlers (spansegment.Segment,
// ConversationHandler, DatasetHandler, etc.) become thin adapters at a clean
// seam — they contain only domain logic and SQL, while the Router manages how
// they are composed and dispatched.
//
// Use [NewRouter] to construct one from router deps, then call
// [Router.RegisterRoutes] to wire a [http.ServeMux] and obtain the fully
// authenticated top-level handler.
type Router struct {
	cfg          *config.Config
	store        metadata.Store
	auth         *auth.Handler
	span         spansegment.Segment
	bookmark     *BookmarkHandler
	conversation *ConversationHandler
	prompt       *PromptHandler
	evalRule     *EvalRuleHandler
	admin        *AdminHandler
	dataset      *DatasetHandler
	datasetRun   *DatasetRunHandler
	playground   playgroundHandler
	models       *ModelsHandler
	adminLake    *lake.Lake
	sessionTTL   time.Duration
	apiValidator internalauth.Validator
	routes       []AuthRoute // all registered routes with their policies
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
		models:       deps.Models,
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
	// Collect all route entries from handlers that implement the Routes() method.
	// Auth routes are defined inline because the auth handler lives in a
	// separate package (internal/auth) that cannot import this handler package.
	routes := make([]AuthRoute, 0, 80)

	// --- Auth routes (defined inline — auth.Handler is in a separate package) ---
	routes = append(routes, []AuthRoute{
		{Method: http.MethodGet, Path: "/api/v1/me", Handler: http.HandlerFunc(rt.auth.HandleMe), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/login", Handler: http.HandlerFunc(rt.auth.Login), Policy: AuthPolicyPublic},
		{Method: http.MethodPost, Path: "/logout", Handler: http.HandlerFunc(rt.auth.Logout), Policy: AuthPolicyPublic},
		{Method: http.MethodPost, Path: "/api/v1/users/invite", Handler: http.HandlerFunc(rt.auth.Invite), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/users/reset-password", Handler: http.HandlerFunc(rt.auth.ResetPassword), Policy: AuthPolicySession},
		{Method: http.MethodPut, Path: "/api/v1/users/me/password", Handler: http.HandlerFunc(rt.auth.ChangePassword), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/projects", Handler: http.HandlerFunc(rt.auth.HandleCreateProject), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/projects/{id}/api-keys", Handler: http.HandlerFunc(rt.auth.HandleGenerateAPIKey), Policy: AuthPolicySession},
		{Method: http.MethodGet, Path: "/api/v1/projects/{id}/api-keys", Handler: http.HandlerFunc(rt.auth.HandleListAPIKeys), Policy: AuthPolicySession},
		{Method: http.MethodDelete, Path: "/api/v1/projects/{id}/api-keys/{keyId}", Handler: http.HandlerFunc(rt.auth.HandleRevokeAPIKey), Policy: AuthPolicySession},
	}...)

	// --- Admin routes (require admin session) ---
	routes = append(routes, rt.admin.AdminRoutes()...)

	// --- Span / project routes ---
	routes = append(routes, rt.span.Routes()...)

	// --- Bookmark routes ---
	routes = append(routes, rt.bookmark.Routes()...)

	// --- Conversation routes ---
	routes = append(routes, rt.conversation.Routes()...)

	// --- Prompt routes ---
	routes = append(routes, rt.prompt.Routes()...)

	// --- Eval rule routes ---
	routes = append(routes, rt.evalRule.Routes()...)

	// --- Dataset routes ---
	routes = append(routes, rt.dataset.Routes()...)

	// --- Dataset run routes (POST requires judge LLM config) ---
	for _, r := range rt.datasetRun.Routes() {
		if r.Path != "/api/v1/datasets/{id}/runs" || rt.datasetRun.JudgeClient != nil {
			routes = append(routes, r)
		}
	}

	// --- Playground route ---
	routes = append(routes, AuthRoute{
		Method: http.MethodPost, Path: "/api/v1/playground/run",
		Handler: http.HandlerFunc(rt.playground.HandleRun),
		Policy:  AuthPolicySession,
	})

	// --- Models route (public — returns known model list) ---
	routes = append(routes, AuthRoute{
		Method:  http.MethodGet,
		Path:    "/api/v1/models",
		Handler: http.HandlerFunc(rt.models.HandleModels),
		Policy:  AuthPolicyPublic,
	})

	// --- Score route (public — used by eval worker) ---
	scoreHandler := NewScoreHandler(rt.adminLake, rt.adminLake)
	routes = append(routes, AuthRoute{
		Method:  http.MethodPost,
		Path:    "/api/v1/scores",
		Handler: func(w http.ResponseWriter, req *http.Request) { scoreHandler.ServeHTTP(w, req) },
		Policy:  AuthPolicyPublic,
	})

	// --- Prometheus metrics (public) ---
	metricsHandler := promhttp.Handler()
	routes = append(routes, AuthRoute{
		Method:  http.MethodGet,
		Path:    "/metrics",
		Handler: metricsHandler.ServeHTTP,
		Policy:  AuthPolicyPublic,
	})

	// Store routes on the router so buildMiddleware can look them up.
	rt.routes = routes

	// Register every route on the mux.
	for _, r := range routes {
		mux.HandleFunc(r.Method+" "+r.Path, r.Handler)
	}

	// SPA fallback — delegates to the embedded UI handler from the public package.
	mux.Handle("/", public.ServeHandler())

	return rt.buildMiddleware(mux)
}

// buildMiddleware returns the middleware-wrapped handler that routes requests
// through the correct auth layer based on the auth policy lookup table.
func (rt *Router) buildMiddleware(mux *http.ServeMux) http.Handler {
	sessionMw := auth.RequireAuth(rt.store, rt.cfg.Auth.SecureCookie, rt.sessionTTL)
	adminMw := auth.RequireAdmin(rt.store, rt.cfg.Auth.SecureCookie, rt.sessionTTL, rt.cfg.Auth.AdminEmail)
	promptGetMw := auth.RequireSessionOrAPIKey(rt.store, rt.apiValidator, rt.cfg.Auth.SecureCookie, rt.sessionTTL, internalauth.APIKeyProjectIDContextKey)

	// Build lookup table from rt.routes, keyed by the exact pattern string
	// each route was registered on the mux with (see RegisterRoutes:
	// mux.HandleFunc(r.Method+" "+r.Path, r.Handler)).
	lk := &authPolicyLookup{policy: make(map[string]AuthPolicy)}
	for _, r := range rt.routes {
		lk.policy[r.Method+" "+r.Path] = r.Policy
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path

		// Public routes bypass authentication entirely.
		if public.IsPublicPath(path) {
			mux.ServeHTTP(w, req)
			return
		}

		// Resolve the concrete request to the registered mux pattern it
		// matches (e.g. "/api/v1/traces/abc123" -> "GET /api/v1/traces/{traceId}")
		// using the same path-parameter matching http.ServeMux uses to
		// dispatch the request, then look up the policy for that pattern.
		// Matching on req.URL.Path directly would never hit any route with
		// a path parameter and silently default every such route to public.
		_, pattern := mux.Handler(req)
		policy := lk.lookup(pattern)

		// Apply the appropriate middleware based on the resolved policy.
		switch policy {
		case AuthPolicyPublic:
			mux.ServeHTTP(w, req)
		case AuthPolicyAdmin:
			adminMw(mux).ServeHTTP(w, req)
		case AuthPolicyAPIKeyOrSession:
			promptGetMw(mux).ServeHTTP(w, req)
		case AuthPolicySession:
			sessionMw(mux).ServeHTTP(w, req)
		}
	})
}
