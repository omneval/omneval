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

// RouteGroup is the interface that each handler type implements to expose its
// routes, auth policies, and registration. The Router holds a []RouteGroup
// slice and delegates route registration entirely to its groups.
//
// Routes returns the route entries that the group owns. Each entry includes
// the handler, HTTP method, path pattern, and auth policy.
//
// RegisterPolicyLookup registers the group's route/policy mappings into the
// provided lookup so that the Router's middleware can resolve auth policies
// for incoming requests.
type RouteGroup interface {
	Routes() []AuthRoute
	RegisterPolicyLookup(lk *authPolicyLookup)
}

// routeGroupAdapter wraps a Routes() function so that any function returning
// []AuthRoute can satisfy the RouteGroup interface. It also implements
// RegisterPolicyLookup by populating the lookup table with the group's own
// route/policy pairs.
type routeGroupAdapter struct {
	routesFunc func() []AuthRoute
}

func (g *routeGroupAdapter) Routes() []AuthRoute {
	if g.routesFunc != nil {
		return g.routesFunc()
	}
	return nil
}

func (g *routeGroupAdapter) RegisterPolicyLookup(lk *authPolicyLookup) {
	for _, r := range g.Routes() {
		lk.policy[r.Method+" "+r.Path] = r.Policy
	}
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
	Playground      *PlaygroundRouterGroup
	Models          *ModelsHandler
	AdminLake       *lake.Lake
	SessionTTL      time.Duration
	APIKeyValidator internalauth.Validator
}

// PlaygroundRouterGroup wraps a playground HandleRun function as a RouteGroup.
// This avoids importing the playground package directly while still giving
// the Router a RouteGroup for the playground endpoint.
type PlaygroundRouterGroup struct {
	HandleRun func(http.ResponseWriter, *http.Request)
}

func (g *PlaygroundRouterGroup) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodPost, Path: "/api/v1/playground/run", Handler: http.HandlerFunc(g.HandleRun), Policy: AuthPolicySession},
	}
}

func (g *PlaygroundRouterGroup) RegisterPolicyLookup(lk *authPolicyLookup) {
	for _, r := range g.Routes() {
		lk.policy[r.Method+" "+r.Path] = r.Policy
	}
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
// response serialization. Instead of embedding individual handler types, the
// Router holds a []RouteGroup slice — each group owns its own routes, policies,
// and registration, making the Router a thin composer.
//
// Use [NewRouter] to construct one from router deps, then call
// [Router.RegisterRoutes] to wire a [http.ServeMux] and obtain the fully
// authenticated top-level handler.
type Router struct {
	cfg         *config.Config
	store       metadata.Store
	auth        *auth.Handler
	routeGroups []RouteGroup // all registered route groups
	routes      []AuthRoute  // flattened routes from all groups
	sessionTTL  time.Duration
	apiValidator internalauth.Validator
}

// NewRouter creates a Router from the provided dependencies. All handlers use
// the shared auth module for project ID extraction. The router composes its
// route groups from the provided deps, delegating registration entirely to them.
func NewRouter(deps *RouterDeps) *Router {
	routeGroups := make([]RouteGroup, 0, 15)

	// Auth route group (auth.Handler is in a separate package).
	routeGroups = append(routeGroups, newAuthRouteGroup(deps.Auth))

	// Span route group.
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.Span.Routes})

	// Bookmark route group.
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.Bookmark.Routes})

	// Conversation route group.
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.Conversation.Routes})

	// Prompt route group.
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.Prompt.Routes})

	// EvalRule route group.
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.EvalRule.Routes})

	// Dataset route group.
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.Dataset.Routes})

	// DatasetRun route group.
	routeGroups = append(routeGroups, &datasetRunRouteGroup{handler: deps.DatasetRun})

	// Admin route group (routes now use Routes() instead of AdminRoutes()).
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.Admin.Routes})

	// Playground route group.
	if deps.Playground != nil {
		routeGroups = append(routeGroups, deps.Playground)
	}

	// Models route group.
	routeGroups = append(routeGroups, &routeGroupAdapter{routesFunc: deps.Models.Routes})

	// Score route group (constructed inline, public).
	routeGroups = append(routeGroups, newScoreRouteGroup(deps.AdminLake))

	// Prometheus metrics route group (public).
	routeGroups = append(routeGroups, newPrometheusRouteGroup())

	return &Router{
		cfg:         deps.Cfg,
		store:       deps.Store,
		auth:        deps.Auth,
		routeGroups: routeGroups,
		sessionTTL:  deps.SessionTTL,
		apiValidator: deps.APIKeyValidator,
	}
}

// RegisterRoutes registers all query-service routes on the given ServeMux
// and returns the fully authenticated top-level handler. The returned handler
// applies the auth middleware stack: public routes pass through, prompts and
// eval-rules accept session or API-key auth, and all other API routes require
// session auth.
//
// The Router delegates entirely to its RouteGroups: each group owns its routes,
// policies, and registration. The Router is a thin composer that collects
// everything and wires it up.
func (rt *Router) RegisterRoutes(mux *http.ServeMux) http.Handler {
	// Collect all route entries from route groups.
	// Each group owns its routes, auth policies, and registration.
	routes := make([]AuthRoute, 0, 80)

	// Delegate registration: each RouteGroup registers its own routes and
	// policy mappings through the group interface.
	for _, group := range rt.routeGroups {
		routes = append(routes, group.Routes()...)
	}

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

// --- Special route groups ---

// authRouteGroup wraps the auth.Handler to expose its routes as a RouteGroup.
// The auth handler lives in a separate package (internal/auth) that cannot
// import this handler package, so we define the routes here.
type authRouteGroup struct {
	auth *auth.Handler
}

func newAuthRouteGroup(a *auth.Handler) *authRouteGroup {
	return &authRouteGroup{auth: a}
}

func (g *authRouteGroup) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodGet, Path: "/api/v1/me", Handler: http.HandlerFunc(g.auth.HandleMe), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/login", Handler: http.HandlerFunc(g.auth.Login), Policy: AuthPolicyPublic},
		{Method: http.MethodPost, Path: "/logout", Handler: http.HandlerFunc(g.auth.Logout), Policy: AuthPolicyPublic},
		{Method: http.MethodPost, Path: "/api/v1/users/invite", Handler: http.HandlerFunc(g.auth.Invite), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/users/reset-password", Handler: http.HandlerFunc(g.auth.ResetPassword), Policy: AuthPolicySession},
		{Method: http.MethodPut, Path: "/api/v1/users/me/password", Handler: http.HandlerFunc(g.auth.ChangePassword), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/projects", Handler: http.HandlerFunc(g.auth.HandleCreateProject), Policy: AuthPolicySession},
		{Method: http.MethodPost, Path: "/api/v1/projects/{id}/api-keys", Handler: http.HandlerFunc(g.auth.HandleGenerateAPIKey), Policy: AuthPolicySession},
		{Method: http.MethodGet, Path: "/api/v1/projects/{id}/api-keys", Handler: http.HandlerFunc(g.auth.HandleListAPIKeys), Policy: AuthPolicySession},
		{Method: http.MethodDelete, Path: "/api/v1/projects/{id}/api-keys/{keyId}", Handler: http.HandlerFunc(g.auth.HandleRevokeAPIKey), Policy: AuthPolicySession},
	}
}

func (g *authRouteGroup) RegisterPolicyLookup(lk *authPolicyLookup) {
	for _, r := range g.Routes() {
		lk.policy[r.Method+" "+r.Path] = r.Policy
	}
}

// datasetRunRouteGroup wraps the DatasetRunHandler with the conditional route
// logic: the POST /api/v1/datasets/{id}/runs route is excluded when the
// handler has no JudgeClient configured (the run endpoint needs a judge LLM).
type datasetRunRouteGroup struct {
	handler *DatasetRunHandler
}

func (g *datasetRunRouteGroup) Routes() []AuthRoute {
	var routes []AuthRoute
	for _, r := range g.handler.Routes() {
		if r.Path != "/api/v1/datasets/{id}/runs" || g.handler.JudgeClient != nil {
			routes = append(routes, r)
		}
	}
	return routes
}

func (g *datasetRunRouteGroup) RegisterPolicyLookup(lk *authPolicyLookup) {
	for _, r := range g.Routes() {
		lk.policy[r.Method+" "+r.Path] = r.Policy
	}
}

// scoreRouteGroup wraps a newly constructed ScoreHandler as a RouteGroup.
// ScoreHandler is constructed inline here because it depends on the Lake
// attachment, which varies by deployment.
type scoreRouteGroup struct {
	lakeRW *lake.Lake
}

func newScoreRouteGroup(lakeRW *lake.Lake) *scoreRouteGroup {
	return &scoreRouteGroup{lakeRW: lakeRW}
}

func (g *scoreRouteGroup) Routes() []AuthRoute {
	return []AuthRoute{
		{Method: http.MethodPost, Path: "/api/v1/scores", Handler: NewScoreHandler(g.lakeRW, g.lakeRW).ServeHTTP, Policy: AuthPolicyPublic},
	}
}

func (g *scoreRouteGroup) RegisterPolicyLookup(lk *authPolicyLookup) {
	for _, r := range g.Routes() {
		lk.policy[r.Method+" "+r.Path] = r.Policy
	}
}

// prometheusRouteGroup provides the /metrics endpoint backed by Prometheus.
type prometheusRouteGroup struct{}

func newPrometheusRouteGroup() *prometheusRouteGroup {
	return &prometheusRouteGroup{}
}

func (g *prometheusRouteGroup) Routes() []AuthRoute {
	metricsHandler := promhttp.Handler()
	return []AuthRoute{
		{Method: http.MethodGet, Path: "/metrics", Handler: metricsHandler.ServeHTTP, Policy: AuthPolicyPublic},
	}
}

func (g *prometheusRouteGroup) RegisterPolicyLookup(lk *authPolicyLookup) {
	for _, r := range g.Routes() {
		lk.policy[r.Method+" "+r.Path] = r.Policy
	}
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
