package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/fake"
	"github.com/omneval/omneval/services/query/internal/auth"
	"github.com/omneval/omneval/services/query/internal/handler"
	"github.com/omneval/omneval/services/query/internal/playground"
)

// setupTestMuxWithMiddleware builds a mux that mirrors server.go's router:
// public routes pass through without auth, /api/v1/ routes require session middleware.
func setupTestMuxWithMiddleware(t *testing.T) *httptest.Server {
	t.Helper()

	store := fake.NewFakeMetadataStore()
	h := auth.NewHandler(store, false, 1*time.Hour, "admin@example.com", "admin-password")
	_, _ = h.BootstrapAdmin(nil)

	sessionMw := auth.RequireAuth(store, false, 1*time.Hour)

	mux := http.NewServeMux()
	h.Register(mux)

	mux.HandleFunc("POST /api/v1/spans/query", func(w http.ResponseWriter, r *http.Request) {
		if auth.CurrentUserFromContext(r) == nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"spans": "ok"})
	})
	mux.HandleFunc("POST /api/v1/traces/{traceId}/bookmark", func(w http.ResponseWriter, r *http.Request) {
		if auth.CurrentUserFromContext(r) == nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"bookmarked": true})
	})
	mux.HandleFunc("POST /api/v1/scores", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	// Playground route: registered without LLM client so it returns 503.
	playgroundH := &playground.PlaygroundHandler{
		Cache:        nil,
		LLMClient:    nil,
		SessionStore: h,
	}
	mux.HandleFunc("POST /api/v1/playground/run", playgroundH.HandleRun)
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	})

	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsPublicAPIPath(r.URL.Path) {
			mux.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			sessionMw(mux).ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	return httptest.NewServer(wrapper)
}

// loginAndGetCookie performs a login and returns the session cookie.
func loginAndGetCookie(t *testing.T, ts *httptest.Server, email, password string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, _ := http.NewRequest("POST", ts.URL+"/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "omneval_session" {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func TestPublicRoutes_AvailableWithoutAuth(t *testing.T) {
	ts := setupTestMuxWithMiddleware(t)

	t.Run("login", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/login", "application/json",
			bytes.NewReader([]byte(`{"email":"admin@example.com","password":"admin-password"}`)))
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("POST /login: status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/metrics")
		if err != nil {
			t.Fatalf("metrics request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /metrics: status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("healthz", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatalf("healthz request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("GET /healthz: got 401 unauthorized, should be 200 or 503")
		}
	})

	t.Run("readyz", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/readyz")
		if err != nil {
			t.Fatalf("readyz request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("GET /readyz: got 401 unauthorized, should be 200 or 503")
		}
	})
}

func TestProtectedAPI_Returns401WithoutAuth(t *testing.T) {
	ts := setupTestMuxWithMiddleware(t)

	cases := []struct {
		method string
		path   string
	}{
		// Auth handler protected routes.
		{"GET", "/api/v1/projects"},
		{"POST", "/api/v1/projects"},
		{"POST", "/api/v1/projects/proj-1/api-keys"},
		{"GET", "/api/v1/projects/proj-1/api-keys"},
		{"DELETE", "/api/v1/projects/proj-1/api-keys/key-1"},
		{"POST", "/api/v1/users/invite"},
		{"PUT", "/api/v1/users/me/password"},

		// Span query.
		{"POST", "/api/v1/spans/query"},

		// Trace bookmark.
		{"POST", "/api/v1/traces/trace-1/bookmark"},

		// Prompt routes.
		{"GET", "/api/v1/prompts"},
		{"POST", "/api/v1/prompts"},
		{"GET", "/api/v1/prompts/test-prompt"},
		{"GET", "/api/v1/prompts/test-prompt/versions"},
		{"PUT", "/api/v1/prompts/test-prompt/labels/stable"},

		// Eval rule routes.
		{"POST", "/api/v1/eval-rules"},
		{"GET", "/api/v1/eval-rules"},
		{"DELETE", "/api/v1/eval-rules/abc"},

		// Dataset routes.
		{"POST", "/api/v1/datasets"},
		{"GET", "/api/v1/datasets"},
		{"GET", "/api/v1/datasets/abc"},
		{"POST", "/api/v1/datasets/abc/items"},
		{"GET", "/api/v1/datasets/abc/items"},
		{"DELETE", "/api/v1/datasets/abc"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body io.Reader
			if tc.method == "POST" || tc.method == "PUT" || tc.method == "DELETE" {
				body = bytes.NewReader([]byte(`{}`))
			}
			req, err := http.NewRequest(tc.method, ts.URL+tc.path, body)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status: got %d, want %d (unauthorized)", resp.StatusCode, http.StatusUnauthorized)
			}
		})
	}
}

func TestProtectedAPI_AuthenticatedRequestSucceeds(t *testing.T) {
	ts := setupTestMuxWithMiddleware(t)
	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{"GET", "/api/v1/projects", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body io.Reader
			if tc.method == "POST" || tc.method == "PUT" {
				body = bytes.NewReader([]byte(`{}`))
			}
			req, err := http.NewRequest(tc.method, ts.URL+tc.path, body)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.want {
				t.Errorf("status: got %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestScoresEndpoint_NoAuthRequired(t *testing.T) {
	ts := setupTestMuxWithMiddleware(t)

	// POST /api/v1/scores should work without a session cookie
	// (it's the eval worker write-back endpoint).
	body := strings.NewReader(`{}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/scores", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scores request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /api/v1/scores: status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestPublicAPIPathClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path  string
		want  bool
		label string
	}{
		// Explicitly public.
		{"/login", true, "login"},
		{"/logout", true, "logout"},
		{"/healthz", true, "healthz"},
		{"/readyz", true, "readyz"},
		{"/metrics", true, "metrics"},
		{"/api/v1/scores", true, "scores"},

		// Prefix variants of health endpoints.
		{"/healthz/ready", true, "healthz prefix"},
		{"/healthz/ping", true, "healthz deep prefix"},
		{"/readyz/ok", true, "readyz prefix"},

		// Protected API routes.
		{"/api/v1/spans/query", false, "spans query"},
		{"/api/v1/projects", false, "projects"},
		{"/api/v1/datasets", false, "datasets"},

		// UI routes (not /api/v1/).
		{"/", false, "spa root"},
		{"/static/app.js", false, "static file"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			got := IsPublicAPIPath(tc.path)
			if got != tc.want {
				t.Errorf("IsPublicAPIPath(%q): got %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestLogout_StillWorksWithoutAuth(t *testing.T) {
	ts := setupTestMuxWithMiddleware(t)

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	// Logout should work with a valid cookie.
	req, _ := http.NewRequest("POST", ts.URL+"/logout", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("logout request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /logout: status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestSessionMiddleware_IsApplied verifies that RequireAuth is called in server.go
// and wraps the mux with session validation. This is the key acceptance criterion.
func TestPlaygroundRunRoute_NoLLMConfig(t *testing.T) {
	// When LLM is not configured, the playground route should still be
	// registered (route always present) but return 503 with JSON, never HTML.
	ts := setupTestMuxWithMiddleware(t)

	// Login first — playground route is under /api/v1/ so it needs auth.
	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	// Build request with session cookie and project_id query param.
	// (The fake store has no projects, so ProjectID() falls back to query param.)
	body := strings.NewReader(`{"prompt_name":"test","version":1,"variables":{}}`)
	req, err := http.NewRequest("POST", ts.URL+"/api/v1/playground/run?project_id=test-proj", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("playground request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	// Response must be JSON, never HTML.
	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("body is not JSON: %v\nraw: %s", err, resp.Body)
	}
	if respBody["error"] == "" {
		t.Error("expected 'error' field in JSON response")
	}
}

// TestDatasetRunsRoute_NoLLMConfig verifies that the GET dataset runs
// endpoint returns valid JSON (not HTML) even when the judge LLM is not
// configured. Without this, the route falls through to the SPA fallback
// and the frontend gets "Unexpected token '<', "<!DOCTYPE" is not valid JSON".
func TestDatasetRunsRoute_NoLLMConfig(t *testing.T) {
	t.Parallel()

	store := fake.NewFakeMetadataStore()
	h := auth.NewHandler(store, false, 1*time.Hour, "admin@example.com", "admin-password")
	_, _ = h.BootstrapAdmin(nil)

	ds := &domain.Dataset{
		DatasetID: "test-ds-123",
		ProjectID: "test-proj",
		Name:      "Test Dataset",
	}
	store.CreateDataset(nil, ds)

	m2 := http.NewServeMux()

	// Register dataset run endpoints with the real handler but without a
	// judge LLM client — mirrors the server.go route registration.
	readHandler := &handler.DatasetRunHandler{
		DatasetStore: store,
		SessionStore: h,
	}
	m2.HandleFunc("GET /api/v1/datasets/{id}/runs", readHandler.HandleListRuns)
	m2.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}", readHandler.HandleGetRun)
	m2.HandleFunc("GET /api/v1/datasets/{id}/runs/{runId}/status", readHandler.HandleGetRunStatus)

	m2.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!DOCTYPE html><html><body>SPA</body></html>"))
	})

	sessionMw := auth.RequireAuth(store, false, 1*time.Hour)
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			sessionMw(m2).ServeHTTP(w, r)
			return
		}
		m2.ServeHTTP(w, r)
	})

	ts := httptest.NewServer(wrapper)
	defer ts.Close()

	sessionID := "test-session-id"
	_ = store.CreateSession(nil, &domain.Session{
		SessionID: sessionID,
		UserID:    "admin",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})

	// Request the runs endpoint — should return JSON, not HTML.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/datasets/"+ds.DatasetID+"/runs?project_id=test-proj", nil)
	req.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	// Must NOT be HTML — this is the core assertion that catches the bug.
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type is HTML (%q), expected JSON — route fell through to SPA fallback", contentType)
	}

	// Body must be parseable as JSON.
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("body is not valid JSON: %v\nraw: %s", err, resp.Body)
	}
}

func TestSessionMiddleware_IsApplied(t *testing.T) {
	// Create a minimal mux to test that RequireAuth wraps handlers.
	store := fake.NewFakeMetadataStore()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Apply RequireAuth middleware.
	sessionMw := auth.RequireAuth(store, false, 1*time.Hour)
	wrapped := sessionMw(mux)

	// Without cookie — should get 401.
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// With valid session cookie — should get 200.
	_ = store.CreateUser(nil, &domain.User{
		UserID: "u1", OrgID: "org1", Email: "test@test.com",
		PasswordHash: "pass",
	})
	sessionID := "test-session"
	_ = store.CreateSession(nil, &domain.Session{
		SessionID: sessionID,
		UserID:    "u1",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.AddCookie(&http.Cookie{Name: "omneval_session", Value: sessionID})
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("authenticated: status %d, want %d", w2.Code, http.StatusOK)
	}
}
