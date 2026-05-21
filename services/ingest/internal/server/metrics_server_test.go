package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/services/ingest/internal/metrics"
)

// TestStartMetricsServer_ServesPrometheusFormat verifies that the metrics server
// binds to the given address and responds to GET /metrics with valid
// Prometheus text format containing omneval_ingest_* metric names.
func TestStartMetricsServer_ServesPrometheusFormat(t *testing.T) {
	t.Parallel()

	// Register metrics (idempotent for tests — ignore AlreadyRegistered errors).
	_ = metrics.Register(false)

	// Pick a free port by letting the OS assign one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// StartMetricsServer must not block — it runs the server in a goroutine
	// and returns immediately after binding.
	if err := StartMetricsServer(ctx, addr); err != nil {
		t.Fatalf("StartMetricsServer: %v", err)
	}

	// Give the server a moment to start.
	time.Sleep(20 * time.Millisecond)

	// GET /metrics must return 200 with Prometheus text format.
	url := fmt.Sprintf("http://%s/metrics", addr)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type: got %q, want text/plain", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// The body must contain at least one omneval_ingest_* metric.
	if !strings.Contains(string(body), "omneval_ingest_") {
		t.Errorf("metrics body does not contain omneval_ingest_ metrics;\nbody:\n%s", body)
	}
}

// TestStartMetricsServer_ShutdownOnContextCancel verifies that cancelling the
// context causes the metrics server to stop accepting new connections.
func TestStartMetricsServer_ShutdownOnContextCancel(t *testing.T) {
	t.Parallel()

	_ = metrics.Register(false)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())

	if err := StartMetricsServer(ctx, addr); err != nil {
		t.Fatalf("StartMetricsServer: %v", err)
	}

	// Server is up — verify it responds.
	time.Sleep(20 * time.Millisecond)
	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", addr))
	if err != nil {
		t.Fatalf("pre-cancel GET /metrics: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("pre-cancel: status %d, want 200", resp.StatusCode)
	}

	// Cancel the context — server should shut down.
	cancel()
	time.Sleep(100 * time.Millisecond)

	// After shutdown, the port should no longer be accepting connections.
	_, err = http.Get(fmt.Sprintf("http://%s/metrics", addr))
	if err == nil {
		t.Error("expected connection error after server shutdown, got nil")
	}
}
