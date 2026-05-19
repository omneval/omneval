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
