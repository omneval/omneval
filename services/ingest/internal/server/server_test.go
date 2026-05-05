package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zbloss/lantern/internal/auth"
	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/handlers"
	"github.com/zbloss/lantern/internal/probe"
)

// fakeIngestQueue stores enqueued spans in-memory for testing.
type fakeIngestQueue struct{}

func (f *fakeIngestQueue) Enqueue(_ context.Context, _ []*domain.Span) error {
	return nil
}

// fakeValidator always returns success.
type fakeValidator struct{}

func (f *fakeValidator) Validate(_ context.Context, rawKey string) (*auth.ValidatedKey, error) {
	if rawKey == "valid_key" {
		return &auth.ValidatedKey{
			ProjectID: "proj-1",
		}, nil
	}
	return nil, nil
}

func TestCombinedHandler_HealthzReturns200(t *testing.T) {
	// Create the probe with a passing check.
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return nil },
	})

	// Create a fake handler that serves the ingest API routes.
	fakeHandler := handlers.NewNativeHandler(&fakeIngestQueue{}, &fakeValidator{}, nil)

	// Combined handler mirrors the server logic.
	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			fakeHandler.Router().ServeHTTP(w, r)
		}
	})

	ts := httptest.NewServer(combined)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestCombinedHandler_ReadyzWithPassingCheckReturns200(t *testing.T) {
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return nil },
	})

	fakeHandler := handlers.NewNativeHandler(&fakeIngestQueue{}, &fakeValidator{}, nil)

	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			fakeHandler.Router().ServeHTTP(w, r)
		}
	})

	ts := httptest.NewServer(combined)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestCombinedHandler_ReadyzWithFailingCheckReturns503(t *testing.T) {
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return context.DeadlineExceeded },
	})

	fakeHandler := handlers.NewNativeHandler(&fakeIngestQueue{}, &fakeValidator{}, nil)

	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			fakeHandler.Router().ServeHTTP(w, r)
		}
	})

	ts := httptest.NewServer(combined)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestCombinedHandler_HealthzUnaffectedByReadyState(t *testing.T) {
	// Even when readyz would fail, healthz always returns 200.
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return context.DeadlineExceeded },
	})

	fakeHandler := handlers.NewNativeHandler(&fakeIngestQueue{}, &fakeValidator{}, nil)

	combined := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			p.Router().ServeHTTP(w, r)
		} else {
			fakeHandler.Router().ServeHTTP(w, r)
		}
	})

	ts := httptest.NewServer(combined)
	defer ts.Close()

	// Healthz should still return 200 even though the ready check fails.
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
