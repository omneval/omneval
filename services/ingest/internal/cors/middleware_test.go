package cors_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zbloss/lantern/services/ingest/internal/cors"
)

func TestMiddleware_PreflightReturns204(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	m := cors.New([]string{"*"})
	handler := m.Handler(next)

	req := httptest.NewRequest("OPTIONS", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestMiddleware_PreflightSetsAllowOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	m := cors.New([]string{"*"})
	handler := m.Handler(next)

	req := httptest.NewRequest("OPTIONS", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, "*")
	}
}

func TestMiddleware_PreflightSetsAllowMethods(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	m := cors.New([]string{"*"})
	handler := m.Handler(next)

	req := httptest.NewRequest("OPTIONS", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	allowMethods := rec.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(allowMethods, "POST") {
		t.Errorf("Access-Control-Allow-Methods: got %q, want to contain %q", allowMethods, "POST")
	}
	if !strings.Contains(allowMethods, "OPTIONS") {
		t.Errorf("Access-Control-Allow-Methods: got %q, want to contain %q", allowMethods, "OPTIONS")
	}
}

func TestMiddleware_PreflightSetsAllowHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	m := cors.New([]string{"*"})
	handler := m.Handler(next)

	req := httptest.NewRequest("OPTIONS", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "Content-Type") {
		t.Errorf("Access-Control-Allow-Headers: got %q, want to contain %q", allowHeaders, "Content-Type")
	}
	if !strings.Contains(allowHeaders, "Authorization") {
		t.Errorf("Access-Control-Allow-Headers: got %q, want to contain %q", allowHeaders, "Authorization")
	}
}

func TestMiddleware_PostWithWildcardOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	m := cors.New([]string{"*"})
	handler := m.Handler(next)

	req := httptest.NewRequest("POST", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, "*")
	}
}

func TestMiddleware_PostWithSpecificOriginMatch(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	m := cors.New([]string{"http://example.com", "http://another.com"})
	handler := m.Handler(next)

	req := httptest.NewRequest("POST", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, "http://example.com")
	}
}

func TestMiddleware_PostWithUnmatchedOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	m := cors.New([]string{"http://example.com"})
	handler := m.Handler(next)

	req := httptest.NewRequest("POST", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://malicious.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want empty (no match)", got)
	}
}

func TestMiddleware_WithoutOriginHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	m := cors.New([]string{"*"})
	handler := m.Handler(next)

	// No Origin header set
	req := httptest.NewRequest("POST", "/api/v1/spans", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want empty when no Origin header", got)
	}
}

func TestMiddleware_PreflightWithSpecificOriginMatch(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	m := cors.New([]string{"http://example.com", "http://another.com"})
	handler := m.Handler(next)

	req := httptest.NewRequest("OPTIONS", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://another.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://another.com" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, "http://another.com")
	}
}

func TestMiddleware_PreflightWithSpecificOriginMismatch(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	m := cors.New([]string{"http://example.com"})
	handler := m.Handler(next)

	req := httptest.NewRequest("OPTIONS", "/api/v1/spans", nil)
	req.Header.Set("Origin", "http://unknown.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want empty (no match)", got)
	}
}

func TestMiddleware_ResolveOriginWildcard(t *testing.T) {
	m := cors.New([]string{"*"})
	tests := []struct {
		name    string
		origin  string
		want    string
	}{
		{"wildcard matches any", "http://example.com", "*"},
		{"wildcard matches empty origin", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.ResolveOrigin(tt.origin)
			if got != tt.want {
				t.Errorf("ResolveOrigin(%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}

func TestMiddleware_ResolveOriginSpecific(t *testing.T) {
	m := cors.New([]string{"http://example.com"})
	tests := []struct {
		name    string
		origin  string
		want    string
	}{
		{"matched origin", "http://example.com", "http://example.com"},
		{"unmatched origin", "http://unknown.com", ""},
		{"empty origin", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.ResolveOrigin(tt.origin)
			if got != tt.want {
				t.Errorf("ResolveOrigin(%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}
