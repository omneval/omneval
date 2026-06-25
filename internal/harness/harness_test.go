package harness

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestHarnessNew(t *testing.T) {
	h, err := New("")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if h == nil {
		t.Fatal("New() returned nil")
	}
	if h.cfg == nil {
		t.Fatal("New() returned Harness with nil cfg")
	}
	// Default log level should be set
	if h.cfg.LogLevel != "info" {
		t.Errorf("default log level = %q, want %q", h.cfg.LogLevel, "info")
	}
}

func TestHarnessNewConfigError(t *testing.T) {
	_, err := New("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("New() with nonexistent config: expected error, got nil")
	}
}

func TestRegisterMetrics(t *testing.T) {
	// A valid registration should succeed
	err := RegisterMetrics(func(_ bool) error {
		return nil
	}, false)
	if err != nil {
		t.Fatalf("RegisterMetrics() error = %v", err)
	}

	// An erroring registration should fail
	err = RegisterMetrics(func(_ bool) error {
		return fmt.Errorf("register fail")
	}, false)
	if err == nil {
		t.Fatal("RegisterMetrics() with erroring func: expected error, got nil")
	}
}

func TestRegisterMetricsWithHarness(t *testing.T) {
	// A valid registration via WithRegisterMetrics should succeed
	err := RegisterMetrics(func(_ bool) error {
		return nil
	}, false)
	if err != nil {
		t.Fatalf("RegisterMetrics() error = %v", err)
	}
}

// waitForServer polls the given URL until it responds or the deadline is exceeded.
func waitForServer(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server did not become ready at %s within %v", url, timeout)
}

// minimalConfig writes a minimal config file for testing.
func minimalConfig(t *testing.T, dir string) string {
	t.Helper()
	path := dir + "/config.yaml"
	cfg := `log_level: debug
ingest:
  addr: ":8080"
writer:
  addr: ":8081"
query:
  addr: ":8082"
eval:
  addr: ":8083"
metrics:
  addr: ":9091"
`
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHarnessRunWithHTTPServer(t *testing.T) {
	dir := t.TempDir()
	cfgPath := minimalConfig(t, dir)

	h, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Use a short shutdown timeout for testing.
	h = h.WithShutdownTimeout(500 * time.Millisecond)

	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		err := h.Run(ctx, func(ctx *HarnessContext) (string, http.Handler, ShutdownFunc, ShutdownFunc, error) {
			ctx.Mux.Handle("/test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			}))
			return ctx.Cfg.Ingest.Addr, ctx.Mux, nil, nil, nil
		})
		done <- err
	}()

	// Wait for the server to become ready
	if err := waitForServer("http://localhost:8080/test", 3*time.Second); err != nil {
		t.Fatalf("server did not become ready: %v", err)
	}

	// Verify the test endpoint is reachable
	resp, err := http.Get("http://localhost:8080/test")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("response body = %q, want %q", body, "ok")
	}

	// Cancel to trigger shutdown
	cancel()

	// Wait for Run() to complete with its own timeout (independent of ctx).
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not complete within 5s of shutdown signal")
	}
}

func TestHarnessRunNoHTTPServer(t *testing.T) {
	dir := t.TempDir()
	cfgPath := minimalConfig(t, dir)

	h, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		err := h.Run(ctx, func(ctx *HarnessContext) (string, http.Handler, ShutdownFunc, ShutdownFunc, error) {
			// No HTTP server (eval-like) — just add a probe
			ctx.Prober.AddCheck("test", &testCheck{})
			return "", nil, nil, nil, nil
		})
		done <- err
	}()

	// Verify /metrics on the metrics server is reachable
	if err := waitForServer("http://localhost:9091/metrics", 3*time.Second); err != nil {
		t.Fatalf("metrics server did not become ready: %v", err)
	}

	// Cancel to trigger shutdown
	cancel()

	// Wait for Run() to complete with its own timeout.
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not complete within 5s of shutdown signal")
	}
}

func TestHarnessRunShutdownFunc(t *testing.T) {
	dir := t.TempDir()
	cfgPath := minimalConfig(t, dir)

	h, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	shutdownCalled := false
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		h.Run(ctx, func(ctx *HarnessContext) (string, http.Handler, ShutdownFunc, ShutdownFunc, error) {
			return "", nil, nil, func() {
				shutdownCalled = true
			}, nil
		})
		close(done)
	}()

	// Verify server is running
	if err := waitForServer("http://localhost:9091/metrics", 3*time.Second); err != nil {
		t.Fatalf("metrics server did not become ready: %v", err)
	}

	// Cancel to trigger shutdown
	cancel()

	// Wait for shutdown to complete
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not complete within 5s of shutdown signal")
	}

	if !shutdownCalled {
		t.Error("shutdown function was not called")
	}
}

// testCheck is a simple probe check for testing.
type testCheck struct{}

func (testCheck) Check(_ context.Context) error {
	return nil
}