package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/probe"
)

// TestNewHTTPMux_ServesMetrics verifies that GET /metrics returns 200 with
// Prometheus text format, not the 404 it returned before quack-server wired
// up a metrics handler on its health/readiness mux.
func TestNewHTTPMux_ServesMetrics(t *testing.T) {
	mux := newHTTPMux(probe.New())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics: status = %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}

// TestNewHTTPMux_StillServesHealthAndReady verifies the existing /healthz
// and /readyz routes are unaffected by adding /metrics to the same mux.
func TestNewHTTPMux_StillServesHealthAndReady(t *testing.T) {
	mux := newHTTPMux(probe.New())

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}
