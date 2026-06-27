// Package harness provides a shared server bootstrap that replaces the
// repeated pattern in every service entrypoint: load config, configure slog,
// register Prometheus metrics, open metadata store, Redis ping, probe setup,
// metrics server startup, signal handling, and graceful HTTP shutdown.
package harness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/probe"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HarnessContext provides the harness's shared infrastructure to the factory.
// Services use this to register routes, add health checks, and start
// background goroutines.
type HarnessContext struct {
	Mux             *http.ServeMux  // Main HTTP router
	Prober          *probe.Prober   // Health/probe system
	Cfg             *config.Config  // Loaded configuration
	StartBackground func(func(context.Context) error) // Start a background goroutine
	SignalCtx       context.Context // Signal-managed context for goroutines
}

// ShutdownFunc is called by the harness during shutdown, after the HTTP
// server has stopped draining but before the process exits. Use it to close
// database connections, flush buffers, or signal background workers to stop.
type ShutdownFunc func()

// Factory creates service-specific components using the harness's
// infrastructure. The addr returned is the HTTP listen address (ignored if
// handler is nil, as with the eval service). The handler is the main HTTP
// server's handler (nil if the service has no HTTP server, like eval). The
// preShutdown function, if non-nil, is called before the HTTP server drains —
// critical for services like query and writer that must call Lake.Shutdown()
// before HTTP shutdown to prevent reconnect storms (may be nil). The
// ShutdownFunc is called during teardown after HTTP shutdown (may be nil).
// Error is returned for startup failures.
type Factory func(ctx *HarnessContext) (addr string, handler http.Handler, preShutdown ShutdownFunc, shutdown ShutdownFunc, err error)

// ShutdownTimeout is the default graceful HTTP shutdown drain timeout.
const ShutdownTimeout = 20 * time.Second

// Harness manages the common server bootstrap and lifecycle: config loading,
// slog setup, Prometheus metrics registration, Redis ping, probe setup,
// metrics server startup, signal handling, and graceful HTTP shutdown.
type Harness struct {
	cfg          *config.Config
	shutdownTime time.Duration
	registerFn   RegisterMetricsFn
}

// WithShutdownTimeout sets a custom shutdown drain timeout. Returns a
// copy of the Harness with the modified timeout.
func (h *Harness) WithShutdownTimeout(d time.Duration) *Harness {
	h2 := *h
	h2.shutdownTime = d
	return &h2
}

// WithRegisterMetrics registers a metrics registration function that the
// harness calls during Run(), before exposing the /metrics endpoint. Returns
// a copy of the Harness with the modified registration function.
func (h *Harness) WithRegisterMetrics(fn RegisterMetricsFn) *Harness {
	h2 := *h
	h2.registerFn = fn
	return &h2
}

// New creates a new Harness. If cfgPath is empty, config is loaded from the
// default location (OMNEVAL_CONFIG env var or omneval.yaml).
func New(cfgPath string) (*Harness, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	// Configure slog with the configured log level.
	logLevel := levelFromString(cfg.LogLevel)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))

	return &Harness{cfg: cfg}, nil
}

// Run executes the full server lifecycle with graceful shutdown:
// starts the metrics server, calls the factory to get service components,
// starts the HTTP server (if a handler is provided), waits for a shutdown
// signal, and performs graceful HTTP shutdown.
//
// Shutdown sequence:
//  1. Signal arrives → signalCtx is cancelled
//  2. Background goroutines see context cancelled and exit
//  3. Wait for background goroutines to finish (with timeout)
//  4. Call pre-http-shutdown hook (if provided by factory)
//  5. Graceful HTTP server shutdown
//  6. Call teardown function
//
// Graceful shutdown behavior:
// - Stops accepting new connections on SIGTERM/SIGINT
// - Waits up to 20s for in-flight HTTP requests to complete
// - Cancels the context to signal background goroutines
func (h *Harness) Run(ctx context.Context, factory Factory) error {
	// Set up signal handling: signal.NotifyContext forwards SIGINT/SIGTERM
	// to the context, and context cancellation also triggers shutdown
	// (useful for tests and programmatic shutdown).
	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start the dedicated Prometheus metrics server on cfg.Metrics.Addr (:9090).
	if err := startMetricsServer(ctx, h.cfg.Metrics.Addr); err != nil {
		return fmt.Errorf("start metrics server: %w", err)
	}

	// Register service-specific Prometheus metrics.
	if h.registerFn != nil {
		if err := h.registerFn(h.cfg.Metrics.DisableProjectLabels); err != nil {
			var are prometheus.AlreadyRegisteredError
			if !errors.As(err, &are) {
				return fmt.Errorf("register metrics: %w", err)
			}
		}
	}

	// Create the prober with a Redis ping check.
	p := probe.New()

	// Create the combined router with probes.
	router := http.NewServeMux()

	// Track background goroutines for coordinated shutdown.
	var wg sync.WaitGroup

	// Call the factory to get service-specific components.
	serviceCtx := &HarnessContext{
		Mux:             router,
		Prober:          p,
		Cfg:             h.cfg,
		StartBackground: func(fn func(context.Context) error) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				slog.Info("background worker started")
				if err := fn(signalCtx); err != nil && err != context.Canceled {
					slog.Error("background worker error", "err", err)
				}
				slog.Info("background worker stopped")
			}()
		},
		SignalCtx: signalCtx,
	}

	addr, handler, preShutdownFn, shutdownFn, err := factory(serviceCtx)
	if err != nil {
		return fmt.Errorf("factory: %w", err)
	}

	// Build the combined handler: /metrics + probes + service routes.
	var combined http.Handler
	if handler != nil {
		combined = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				p.Router().ServeHTTP(w, r)
			} else if r.URL.Path == "/metrics" {
				promhttp.Handler().ServeHTTP(w, r)
			} else {
				handler.ServeHTTP(w, r)
			}
		})
	} else {
		combined = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				p.Router().ServeHTTP(w, r)
			} else {
				promhttp.Handler().ServeHTTP(w, r)
			}
		})
	}

	if handler != nil {
		// Start the HTTP server.
		srv := &http.Server{
			Addr:    addr,
			Handler: combined,
		}

		go func() {
			slog.Info("server listening", "addr", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "err", err)
			}
		}()

		// Wait for shutdown signal or context cancellation.
		<-signalCtx.Done()
		slog.Info("server: shutting down")

		// Wait for background goroutines to finish (with timeout).
		// This allows in-flight work (like LLM calls, reconciliation sweeps)
		// to complete gracefully before HTTP shutdown.
		workerTimeout := h.shutdownTime
		if workerTimeout == 0 {
			workerTimeout = ShutdownTimeout
		}
		workerDone := make(chan struct{})
		go func() {
			wg.Wait()
			close(workerDone)
		}()
		select {
		case <-workerDone:
		case <-time.After(workerTimeout):
			slog.Warn("background workers did not finish within timeout")
		}

		// Call the pre-http-shutdown hook (if provided). This allows the
		// service to perform critical cleanup before the HTTP server drains,
		// such as calling Lake.Shutdown() to abort any reconnect loops that
		// would otherwise outlive the shutdown deadline.
		if preShutdownFn != nil {
			preShutdownFn()
		}

		// Graceful shutdown with configured drain timeout.
		shutdownTimeout := h.shutdownTime
		if shutdownTimeout == 0 {
			shutdownTimeout = ShutdownTimeout
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}

		slog.Info("server: stopped")
	} else {
		// No HTTP server — just wait for signal or context cancellation.
		<-signalCtx.Done()
		slog.Info("server: shutting down")

		// Wait for background goroutines to finish.
		wg.Wait()
		slog.Info("server: stopped")
	}

	// Run teardown.
	if shutdownFn != nil {
		shutdownFn()
	}

	return nil
}

// levelFromString maps a string to an slog.Level, defaulting to info.
func levelFromString(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// startMetricsServer starts a dedicated Prometheus metrics HTTP server on
// the given address. It binds synchronously (so a bind error is returned
// before the goroutine is launched), then serves in the background.
// Cancelling ctx triggers a graceful 5-second shutdown.
func startMetricsServer(ctx context.Context, addr string) error {
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
		slog.Info("metrics server listening", "addr", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "err", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("metrics server shutdown", "err", err)
		}
	}()

	return nil
}

// RegisterMetricsFn is a callback type for Prometheus metrics registration.
// The harness calls this to register service-specific metrics.
type RegisterMetricsFn func(disableProjectLabels bool) error

// RegisterMetrics registers service-specific Prometheus metrics via the
// global registry. Services call this in their factory to ensure their metrics
// families are registered before the metrics endpoint is exposed.
func RegisterMetrics(fn RegisterMetricsFn, disableProjectLabels bool) error {
	if err := fn(disableProjectLabels); err != nil {
		var are prometheus.AlreadyRegisteredError
		// Tolerate re-registration (tests).
		if !errors.As(err, &are) {
			return fmt.Errorf("register metrics: %w", err)
		}
	}
	return nil
}