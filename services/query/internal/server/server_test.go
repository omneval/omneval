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

	"github.com/zbloss/lantern/internal/fake"
	"github.com/zbloss/lantern/services/query/internal/auth"
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
	mux.HandleFunc("POST /api/v1/scores", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
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
		if c.Name == "lantern_session" {
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
		{"POST", "/api/v1/users/invite"},
		{"PUT", "/api/v1/users/me/password"},

		// Span query.
		{"POST", "/api/v1/spans/query"},

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
			req.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})

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
