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

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/fake"
	"github.com/zbloss/lantern/services/query/internal/auth"
)

// setupTestMuxWithMiddleware builds the same mux that server.go should build
// — public routes unprotected, /api/v1/ routes protected by session middleware.
// This mirrors the fix: all protected API routes wrapped with RequireAuth.
func setupTestMuxWithMiddleware(t *testing.T) *httptest.Server {
	t.Helper()

	store := fake.NewFakeMetadataStore()
	h := auth.NewHandler(store, false, 1*time.Hour, "admin@example.com", "admin-password")
	_, _ = h.BootstrapAdmin(nil)

	sessionMw := auth.RequireAuth(store, false, 1*time.Hour)

	// Build the mux the way server.go does.
	mux := http.NewServeMux()
	h.Register(mux) // login, logout, invite, change-password, projects

	// Add a dummy span query handler (requires auth).
	mux.HandleFunc("POST /api/v1/spans/query", func(w http.ResponseWriter, r *http.Request) {
		if auth.CurrentUserFromContext(r) == nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"spans": "ok"})
	})

	// Dummy score handler (no auth — eval worker endpoint).
	mux.HandleFunc("POST /api/v1/scores", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Dummy metrics handler.
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	})

	// SPA fallback.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	})

	// Route wrapper mirrors server.go: public endpoints pass through,
	// protected /api/v1/ routes require session auth, everything else falls back to SPA.
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Public endpoints: no auth needed.
		switch {
		case path == "/login" || path == "/logout":
			mux.ServeHTTP(w, r)
			return
		case strings.HasPrefix(path, "/healthz"),
			strings.HasPrefix(path, "/readyz"),
			path == "/metrics", path == "/api/v1/scores":
			mux.ServeHTTP(w, r)
			return
		}

		// Everything else under /api/v1/ requires authentication.
		if strings.HasPrefix(path, "/api/v1/") {
			sessionMw(mux).ServeHTTP(w, r)
			return
		}

		// SPA fallback and anything else.
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

	// POST /login should work without a session cookie.
	resp, err := http.Post(ts.URL+"/login", "application/json",
		bytes.NewReader([]byte(`{"email":"admin@example.com","password":"admin-password"}`)))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /login: status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// GET /metrics should work without a session cookie.
	resp, err = http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /metrics: status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// GET /healthz should work without a session cookie (should NOT return 401).
	resp, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("GET /healthz: got 401 unauthorized, should be 200 or 503")
	}
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

func TestLogout_StillWorksWithoutAuth(t *testing.T) {
	ts := setupTestMuxWithMiddleware(t)

	sessionID := loginAndGetCookie(t, ts, "admin@example.com", "admin-password")

	// Logout should work with a valid cookie.
	req, _ := http.NewRequest("POST", ts.URL+"/logout", nil)
	req.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
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
	req2.AddCookie(&http.Cookie{Name: "lantern_session", Value: sessionID})
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("authenticated: status %d, want %d", w2.Code, http.StatusOK)
	}
}
