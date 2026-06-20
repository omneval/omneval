package probe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/probe"
)

func TestHealthHandler_Always200(t *testing.T) {
	p := probe.New()
	ts := httptest.NewServer(p.HealthHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHealthHandler_Body(t *testing.T) {
	p := probe.New()
	ts := httptest.NewServer(p.HealthHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body := make([]byte, 10)
	n, _ := resp.Body.Read(body)
	if string(body[:n]) != "ok" {
		t.Errorf("body: got %q, want %q", body[:n], "ok")
	}
}

func TestReadyHandler_NoChecksReturns200(t *testing.T) {
	p := probe.New()
	ts := httptest.NewServer(p.ReadyHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestReadyHandler_CheckPassesReturns200(t *testing.T) {
	p := probe.New()
	p.AddCheck("pass", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return nil },
	})

	ts := httptest.NewServer(p.ReadyHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestReadyHandler_CheckFailsReturns503(t *testing.T) {
	p := probe.New()
	p.AddCheck("fail", &probe.RedisPing{
		Pinger: func(ctx context.Context) error {
			return &probe.ProbeError{Op: "redis_ping", Err: os.ErrNotExist}
		},
	})

	ts := httptest.NewServer(p.ReadyHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestReadyHandler_BodyNotReady(t *testing.T) {
	p := probe.New()
	p.AddCheck("fail", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return os.ErrNotExist },
	})

	ts := httptest.NewServer(p.ReadyHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body := make([]byte, 20)
	n, _ := resp.Body.Read(body)
	if string(body[:n]) != "not ready" {
		t.Errorf("body: got %q, want %q", body[:n], "not ready")
	}
}

// TestHealthHandler_NonCriticalCheckFailureDoesNotAffectLiveness proves that
// a check registered via the plain AddCheck (readiness-only) does NOT gate
// the liveness handler, even while failing. This matters because liveness
// failures eventually restart the container — appropriate for a check whose
// failure means THIS PROCESS is broken (e.g. a permanently wedged
// single-connection database client), but wrong for a check that merely
// reflects an external dependency being temporarily unavailable (e.g.
// Redis): restarting this process can't fix Redis, and would just add
// restart churn on top of an already-degraded dependency.
func TestHealthHandler_NonCriticalCheckFailureDoesNotAffectLiveness(t *testing.T) {
	p := probe.New()
	p.AddCheck("redis", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return os.ErrNotExist },
	})

	ts := httptest.NewServer(p.HealthHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d (non-critical check failures must not affect liveness)", resp.StatusCode, http.StatusOK)
	}
}

// TestHealthHandler_CriticalCheckFailureReturns503 proves that a check
// registered via AddCriticalCheck DOES gate liveness: a permanently failing
// critical check (e.g. the Lake catalog being unreachable because the
// single underlying connection is wedged) must eventually fail liveness too,
// so Kubernetes restarts the container — readiness alone only removes the
// pod from Service routing, which left a wedged query/writer pod stuck
// forever in a real production incident with no automatic recovery.
func TestHealthHandler_CriticalCheckFailureReturns503(t *testing.T) {
	p := probe.New()
	p.AddCriticalCheck("catalog", &probe.CatalogReachable{
		Ping: func(ctx context.Context) error { return os.ErrNotExist },
	})

	ts := httptest.NewServer(p.HealthHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestHealthHandler_CriticalCheckPassesReturns200 proves a passing critical
// check doesn't break the common case.
func TestHealthHandler_CriticalCheckPassesReturns200(t *testing.T) {
	p := probe.New()
	p.AddCriticalCheck("catalog", &probe.CatalogReachable{
		Ping: func(ctx context.Context) error { return nil },
	})

	ts := httptest.NewServer(p.HealthHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestReadyHandler_CriticalCheckStillGatesReadiness proves AddCriticalCheck
// doesn't bypass readiness — a critical check must still behave exactly
// like a normal readiness check (fail fast, take the pod out of Service
// routing) in addition to eventually gating liveness.
func TestReadyHandler_CriticalCheckStillGatesReadiness(t *testing.T) {
	p := probe.New()
	p.AddCriticalCheck("catalog", &probe.CatalogReachable{
		Ping: func(ctx context.Context) error { return os.ErrNotExist },
	})

	ts := httptest.NewServer(p.ReadyHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestFileExists_CheckPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.db")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	ch := &probe.FileExists{Path: path}
	if err := ch.Check(context.Background()); err != nil {
		t.Errorf("check: got err %v, want nil", err)
	}
}

func TestFileExists_CheckFailsWhenMissing(t *testing.T) {
	ch := &probe.FileExists{Path: "/tmp/nonexistent_omneval_probe.db"}
	err := ch.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}

	if _, ok := err.(*probe.ProbeError); !ok {
		t.Errorf("error type: got %T, want *probe.ProbeError", err)
	}
}

func TestRouter_MountsBothEndpoints(t *testing.T) {
	p := probe.New()
	p.AddCheck("pass", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return nil },
	})

	router := p.Router()
	ts := httptest.NewServer(router)
	defer ts.Close()

	// Check /healthz
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	// Check /readyz
	resp, err = http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("readyz request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("readyz status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()
}

func TestReadyHandler_MultipleChecksAllMustPass(t *testing.T) {
	p := probe.New()
	p.AddCheck("first", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return nil },
	})
	p.AddCheck("second", &probe.RedisPing{
		Pinger: func(ctx context.Context) error { return os.ErrNotExist },
	})

	ts := httptest.NewServer(p.ReadyHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestProbeError_ErrorContainsOp(t *testing.T) {
	err := &probe.ProbeError{Op: "redis_ping", Err: os.ErrNotExist}
	msg := err.Error()
	if !strings.Contains(msg, "probe: redis_ping:") {
		t.Errorf("error message: got %q, want it to contain %q", msg, "probe: redis_ping:")
	}
}

func TestProbeError_Unwrap(t *testing.T) {
	want := os.ErrNotExist
	err := &probe.ProbeError{Op: "redis_ping", Err: want}
	if err.Unwrap() != want {
		t.Errorf("unwrap: got %v, want %v", err.Unwrap(), want)
	}
}

func TestCatalogReachable_CheckPasses(t *testing.T) {
	ch := &probe.CatalogReachable{
		Ping: func(ctx context.Context) error { return nil },
	}
	if err := ch.Check(context.Background()); err != nil {
		t.Errorf("check: got err %v, want nil", err)
	}
}

func TestCatalogReachable_CheckFailsWhenUnreachable(t *testing.T) {
	ch := &probe.CatalogReachable{
		Ping: func(ctx context.Context) error { return os.ErrNotExist },
	}
	err := ch.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable catalog, got nil")
	}
	if !strings.Contains(err.Error(), "catalog_reachable") {
		t.Errorf("error message: got %q, want it to contain %q", err.Error(), "catalog_reachable")
	}
}
