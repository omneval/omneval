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

// AuthPolicy defines the authentication requirement for a route.
type AuthPolicy int

const (
	// AuthPolicyPublic routes bypass authentication entirely.
	AuthPolicyPublic AuthPolicy = iota
	// AuthPolicySession routes require a valid session cookie.
	AuthPolicySession
	// AuthPolicyAPIKeyOrSession routes accept a valid session cookie or X-API-Key header.
	AuthPolicyAPIKeyOrSession
	// AuthPolicyAdmin routes require a valid session cookie AND an admin user.
	AuthPolicyAdmin
)

// String returns the canonical string representation of the auth policy.
func (a AuthPolicy) String() string {
	switch a {
	case AuthPolicyPublic:
		return "public"
	case AuthPolicySession:
		return "session"
	case AuthPolicyAPIKeyOrSession:
		return "session_or_api_key"
	case AuthPolicyAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

// AuthRoute pairs an HTTP method/path pattern with its handler and auth policy.
type AuthRoute struct {
	Method  string
	Path    string
	Handler func(http.ResponseWriter, *http.Request)
	Policy  AuthPolicy
}

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

	// SPA fallback.
	mux.HandleFunc("/", serveUI)

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
		if isPublicPath(path) {
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

// publicPaths is the single canonical source of truth for public (unauthenticated)
// API paths. It is referenced by both the router middleware dispatcher and the
// server package's health-check probe logic.
var publicPaths = map[string]struct{}{
	"/login":         {},
	"/logout":        {},
	"/healthz":       {},
	"/readyz":        {},
	"/metrics":       {},
	"/api/v1/scores": {},
}

// IsPublicPath reports whether the given path is a public API route
// that does not require authentication.
func IsPublicPath(path string) bool {
	if _, ok := publicPaths[path]; ok {
		return true
	}
	if strings.HasPrefix(path, "/healthz") || strings.HasPrefix(path, "/readyz") {
		return true
	}
	return false
}

// isPublicPath reports whether the given path is a public API route
// that does not require authentication. Mirrors the logic in server.go.
func isPublicPath(path string) bool {
	return IsPublicPath(path)
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
