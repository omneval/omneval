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

	"github.com/omneval/omneval/services/eval/internal/metrics"
)

// TestStartMetricsServer_ServesPrometheusFormat verifies that the eval metrics
// server binds and responds to GET /metrics with valid Prometheus text format
// containing omneval_eval_* metric names.
func TestStartMetricsServer_ServesPrometheusFormat(t *testing.T) {
	t.Parallel()

	// Register metrics (idempotent — ignore AlreadyRegistered errors).
	_ = metrics.Register(false)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := StartMetricsServer(ctx, addr); err != nil {
		t.Fatalf("StartMetricsServer: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

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

	if !strings.Contains(string(body), "omneval_eval_") {
		t.Errorf("metrics body does not contain omneval_eval_ metrics;\nbody:\n%s", body)
	}
}

// TestStartMetricsServer_ShutdownOnContextCancel verifies that cancelling the
// context causes the eval metrics server to stop accepting new connections.
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

	time.Sleep(20 * time.Millisecond)
	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", addr))
	if err != nil {
		t.Fatalf("pre-cancel GET /metrics: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("pre-cancel: status %d, want 200", resp.StatusCode)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)

	_, err = http.Get(fmt.Sprintf("http://%s/metrics", addr))
	if err == nil {
		t.Error("expected connection error after server shutdown, got nil")
	}
}
