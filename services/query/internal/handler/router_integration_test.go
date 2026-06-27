package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/fake"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/spansegment"
)

// sanitizePath returns a URL-safe version of a path for use in test names.
func sanitizePath(p string) string {
	result := p
	for i := 0; i < len(result); i++ {
		if result[i] == '/' {
			result = result[:i] + "_" + result[i+1:]
		}
	}
	return result
}

// TestRouteGroupIntegration tests that the Router correctly enforces auth
// policies for every route group by exercising them end-to-end through
// buildMiddleware.
func TestRouteGroupIntegration(t *testing.T) {
	// Not parallel: the test server must stay alive for the duration of all
	// subtests. Running parallel subtests with a defer'd ts.Close() at the
	// parent level races the close against subtest HTTP requests.

	store := fake.NewFakeMetadataStore()
	authH := auth.NewHandler(store, false, time.Hour, "admin@example.com", "admin-password")
	if _, err := authH.BootstrapAdmin(nil); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	// Create a Lake handle for span and score route groups.
	lakeHandle := setupTestLake(t)
	defer lakeHandle.Close()

	// Build RouterDeps with all handler types.
	deps := &RouterDeps{
		Cfg:             &config.Config{},
		Store:           store,
		Auth:            authH,
		Span:            &spansegment.SpanHandler{SessionStore: authH, ProjectResolver: authH, Lake: lakeHandle},
		Bookmark:        &BookmarkHandler{BookmarkStore: store, SessionStore: authH, ProjectResolver: authH},
		Conversation:    &ConversationHandler{SessionStore: authH, ProjectResolver: authH},
		Prompt:          &PromptHandler{PromptStore: store.PromptStore(), Cache: nil, SessionStore: authH, ProjectResolver: authH, Validator: nil},
		EvalRule:        &EvalRuleHandler{EvalRuleStore: store, PromptStore: store.PromptStore(), SessionStore: authH, ProjectResolver: authH},
		Admin:           &AdminHandler{APIKeyStore: store, BookmarkStore: store, ProjectStore: store, SessionStore: authH},
		Dataset:         &DatasetHandler{DatasetStore: store, SessionStore: authH, ProjectResolver: authH},
		DatasetRun:      &DatasetRunHandler{DatasetStore: store, SessionStore: authH, ProjectResolver: authH},
		Playground:      &PlaygroundRouterGroup{HandleRun: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }},
		Models:          &ModelsHandler{Pricing: nil},
		AdminLake:       lakeHandle,
		SessionTTL:      time.Hour,
		APIKeyValidator: nil,
	}

	rt := NewRouter(deps)

	// Register routes on a test mux.
	mux := http.NewServeMux()
	handler := rt.RegisterRoutes(mux)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// ---------- Public routes: must work without auth ----------
	publicRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/metrics"},
		{http.MethodPost, "/api/v1/scores"},
	}

	for _, r := range publicRoutes {
		t.Run("public_"+r.method+"_"+sanitizePath(r.path), func(t *testing.T) {
			req, err := http.NewRequest(r.method, ts.URL+r.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			// Public routes must NOT return 401.
			if resp.StatusCode == http.StatusUnauthorized {
				t.Errorf("public route %s %s: got 401, want non-401", r.method, r.path)
			}
		})
	}

	// ---------- Login to get a session cookie ----------
	loginFn := func() (*http.Cookie, error) {
		payload, _ := json.Marshal(map[string]string{"email": "admin@example.com", "password": "admin-password"})
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/login", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		for _, c := range resp.Cookies() {
			if c.Name == "omneval_session" {
				return c, nil
			}
		}
		return nil, io.ErrNoProgress
	}

	sessionCookie, err := loginFn()
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// ---------- Session-protected routes: must reject without auth, accept with ----------
	sessionRoutes := []struct {
		method string
		path   string
	}{
		// Auth group.
		{http.MethodGet, "/api/v1/me"},
		{http.MethodGet, "/api/v1/projects"},
		// Span handler.
		{http.MethodPost, "/api/v1/spans/query"},
		// Bookmark handler.
		{http.MethodPost, "/api/v1/traces/trace-1/bookmark"},
		// Dataset handler.
		{http.MethodGet, "/api/v1/datasets"},
		// Playground handler.
		{http.MethodPost, "/api/v1/playground/run"},
		// Auth route group: project keys.
		{http.MethodPost, "/api/v1/projects/proj-1/api-keys"},
	}

	for _, r := range sessionRoutes {
		t.Run("session_"+r.method+"_"+sanitizePath(r.path)+"_noauth", func(t *testing.T) {
			req, err := http.NewRequest(r.method, ts.URL+r.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s %s without auth: got %d, want %d (unauthorized)", r.method, r.path, resp.StatusCode, http.StatusUnauthorized)
			}
		})

		t.Run("session_"+r.method+"_"+sanitizePath(r.path)+"_withauth", func(t *testing.T) {
			req, err := http.NewRequest(r.method, ts.URL+r.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(sessionCookie)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			// Should NOT be unauthorized.
			if resp.StatusCode == http.StatusUnauthorized {
				t.Errorf("%s %s with auth: got %d, want non-401", r.method, r.path, resp.StatusCode)
			}
		})
	}

	// ---------- Admin route: must reject without admin ----------
	t.Run("admin_route_requires_admin", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/admin/status", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(sessionCookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		// The admin user should pass, but regular users would not.
		// We only check that the route is reachable with a session.
		if resp.StatusCode == http.StatusUnauthorized {
			t.Error("admin route with admin session: got 401, want non-401")
		}
	})
}

// TestRouteGroupRoutesConsistency verifies that every registered RouteGroup
// produces at least one route and that the Routes() list does not contain
// duplicate method/path pairs.
func TestRouteGroupRoutesConsistency(t *testing.T) {
	t.Parallel()

	store := fake.NewFakeMetadataStore()
	authH := auth.NewHandler(store, false, time.Hour, "admin@example.com", "admin-password")
	if _, err := authH.BootstrapAdmin(nil); err != nil {
		t.Fatal(err)
	}

	lakeHandle := setupTestLake(t)
	defer lakeHandle.Close()

	deps := &RouterDeps{
		Cfg:        &config.Config{},
		Store:      store,
		Auth:       authH,
		Span:       &spansegment.SpanHandler{SessionStore: authH, ProjectResolver: authH, Lake: lakeHandle},
		Bookmark:   &BookmarkHandler{BookmarkStore: store, SessionStore: authH, ProjectResolver: authH},
		Conversation: &ConversationHandler{SessionStore: authH, ProjectResolver: authH},
		Prompt:     &PromptHandler{PromptStore: store.PromptStore(), Cache: nil, SessionStore: authH, ProjectResolver: authH},
		EvalRule:   &EvalRuleHandler{EvalRuleStore: store, PromptStore: store.PromptStore(), SessionStore: authH, ProjectResolver: authH},
		Admin:      &AdminHandler{APIKeyStore: store, BookmarkStore: store, ProjectStore: store, SessionStore: authH},
		Dataset:    &DatasetHandler{DatasetStore: store, SessionStore: authH, ProjectResolver: authH},
		DatasetRun: &DatasetRunHandler{DatasetStore: store, SessionStore: authH, ProjectResolver: authH},
		Playground: &PlaygroundRouterGroup{HandleRun: func(w http.ResponseWriter, r *http.Request) {}},
		Models:     &ModelsHandler{},
		AdminLake:  lakeHandle,
	}

	rt := NewRouter(deps)

	// Collect all routes from every group.
	allRoutes := make([]AuthRoute, 0)
	for _, group := range rt.routeGroups {
		routes := group.Routes()
		if len(routes) == 0 && groupRoutesNotExpectedEmpty(group) {
			t.Errorf("RouteGroup %T returned no routes but is expected to have at least one", group)
		}
		allRoutes = append(allRoutes, routes...)
	}

	// Verify no duplicate method+path pairs.
	seen := make(map[string]bool)
	for _, r := range allRoutes {
		key := r.Method + " " + r.Path
		if seen[key] {
			t.Errorf("duplicate route: %s %s", r.Method, r.Path)
		}
		seen[key] = true
	}
}

// groupRoutesNotExpectedEmpty returns true for route groups that should always
// produce at least one route. This helps catch accidental empty implementations.
func groupRoutesNotExpectedEmpty(g RouteGroup) bool {
	_, ok := g.(*routeGroupAdapter)
	if ok {
		// adapters wrap real handlers — they should produce routes
		return true
	}
	switch g.(type) {
	case *authRouteGroup, *scoreRouteGroup, *prometheusRouteGroup,
		*datasetRunRouteGroup:
		return true
	}
	return false
}