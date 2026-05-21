package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartMetricsServer starts a dedicated Prometheus metrics HTTP server on
// the given address. It binds synchronously (so a bind error is returned
// before the goroutine is launched), then serves in the background.
// Cancelling ctx triggers a graceful 5-second shutdown.
func StartMetricsServer(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Bind the listener eagerly so that the caller gets a bind error
	// synchronously rather than via a late log message.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("metrics server: listen %s: %w", addr, err)
	}

	go func() {
		slog.Info("ingest: metrics server listening", "addr", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("ingest: metrics server error", "err", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("ingest: metrics server shutdown", "err", err)
		}
	}()

	return nil
}
