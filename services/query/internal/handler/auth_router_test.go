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
)

func TestAuthPolicy_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		policy AuthPolicy
		want   string
	}{
		{AuthPolicyPublic, "public"},
		{AuthPolicySession, "session"},
		{AuthPolicyAPIKeyOrSession, "session_or_api_key"},
		{AuthPolicyAdmin, "admin"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.policy.String(); got != tt.want {
				t.Errorf("AuthPolicy(%q).String() = %q, want %q", tt.policy, got, tt.want)
			}
		})
	}
}

func TestAuthRoute(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	route := AuthRoute{
		Method:   http.MethodPost,
		Path:     "/api/v1/spans/query",
		Handler:  handler,
		Policy:   AuthPolicySession,
	}

	if route.Method != http.MethodPost {
		t.Errorf("AuthRoute.Method = %q, want %q", route.Method, http.MethodPost)
	}
	if route.Path != "/api/v1/spans/query" {
		t.Errorf("AuthRoute.Path = %q, want %q", route.Path, "/api/v1/spans/query")
	}
	if route.Policy != AuthPolicySession {
		t.Errorf("AuthRoute.Policy = %v, want %v", route.Policy, AuthPolicySession)
	}
	if route.Handler == nil {
		t.Error("AuthRoute.Handler should not be nil")
	}
}

func TestPublicPaths(t *testing.T) {
	t.Parallel()

	// Verify all expected public paths are in the map.
	expectedPublic := []string{
		"/login",
		"/logout",
		"/healthz",
		"/readyz",
		"/metrics",
		"/api/v1/scores",
	}

	for _, path := range expectedPublic {
		if !isPublicPath(path) {
			t.Errorf("isPublicPath(%q) = false, want true", path)
		}
	}
}

func TestPublicPathPrefixMatch(t *testing.T) {
	t.Parallel()

	// Health check variants should also be public.
	if !isPublicPath("/healthz/") {
		t.Error("isPublicPath(/healthz/) = false, want true")
	}
	if !isPublicPath("/readyz/") {
		t.Error("isPublicPath(/readyz/) = false, want true")
	}
}

// TestBuildMiddleware_PathParameterRouteEnforcesPolicy is a regression test
// for a bug where Router.buildMiddleware's authPolicyLookup matched the
// concrete request path (e.g. "/api/v1/widgets/abc123") against the literal
// registered route pattern (e.g. "/api/v1/widgets/{id}"). Those strings never
// match, so every route with a path parameter silently fell back to
// AuthPolicyPublic — bypassing session/admin auth entirely regardless of the
// AuthPolicy declared in the route table. This affected real production
// routes including GET /api/v1/traces/{traceId} (trace detail, readable with
// no authentication at all), conversation detail, datasets, dataset runs,
// prompts, bookmarks, and API-key management.
//
// The fix resolves the concrete request to its registered mux pattern via
// mux.Handler(req) (the same path-parameter matching http.ServeMux uses to
// dispatch the request) before looking up the policy.
func TestBuildMiddleware_PathParameterRouteEnforcesPolicy(t *testing.T) {
	store := fake.NewFakeMetadataStore()
	authH := auth.NewHandler(store, false, time.Hour, "admin@example.com", "admin-password")
	if _, err := authH.BootstrapAdmin(nil); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	const wantBody = "secret-widget-data"
	protected := func(w http.ResponseWriter, r *http.Request) {
		if auth.CurrentUserFromContext(r) == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(wantBody))
	}

	mux := http.NewServeMux()
	authH.Register(mux)
	mux.HandleFunc("GET /api/v1/widgets/{id}", protected)

	rt := &Router{
		cfg:        &config.Config{},
		store:      store,
		sessionTTL: time.Hour,
		routes: []AuthRoute{
			{Method: http.MethodGet, Path: "/api/v1/widgets/{id}", Handler: protected, Policy: AuthPolicySession},
		},
	}

	ts := httptest.NewServer(rt.buildMiddleware(mux))
	defer ts.Close()

	t.Run("unauthenticated request to a path-parameter route is rejected", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/widgets/abc123")
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET /api/v1/widgets/abc123 without session: status=%d body=%q, want %d (unauthorized) — path-parameter route is not enforcing AuthPolicySession", resp.StatusCode, body, http.StatusUnauthorized)
		}
	})

	t.Run("authenticated request to a path-parameter route succeeds", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{"email": "admin@example.com", "password": "admin-password"})
		loginReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", bytes.NewReader(payload))
		loginReq.Header.Set("Content-Type", "application/json")
		loginResp, err := http.DefaultClient.Do(loginReq)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		defer loginResp.Body.Close()
		var sessionCookie *http.Cookie
		for _, c := range loginResp.Cookies() {
			if c.Name == "omneval_session" {
				sessionCookie = c
			}
		}
		if sessionCookie == nil {
			t.Fatal("no session cookie returned from login")
		}

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/widgets/abc123", nil)
		req.AddCookie(sessionCookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(body) != wantBody {
			t.Errorf("GET /api/v1/widgets/abc123 with session: status=%d body=%q, want %d %q", resp.StatusCode, body, http.StatusOK, wantBody)
		}
	})
}

func TestProtectedPathNotPublic(t *testing.T) {
	t.Parallel()

	protected := []string{
		"/api/v1/spans/query",
		"/api/v1/traces/abc123",
		"/api/v1/admin/status",
		"/api/v1/prompts",
		"/api/v1/datasets",
	}

	for _, path := range protected {
		if isPublicPath(path) {
			t.Errorf("isPublicPath(%q) = true, want false", path)
		}
	}
}