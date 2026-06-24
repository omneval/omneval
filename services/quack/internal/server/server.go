// Package server wires and runs the Quack Server (ADR-0005): the sole
// process holding a direct DuckLake Catalog connection, serving it to every
// other service via quack_serve(). Table Maintenance runs as a separate
// scheduled job (services/quack/cmd/compact), not in this process — see
// Run's doc comment for why.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake/lakeserver"
	"github.com/omneval/omneval/internal/probe"
	"github.com/omneval/omneval/services/quack/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

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

// Run starts the Quack Server: attaches the configured DuckLake Catalog,
// serves it over quack_serve(), and serves /healthz and /readyz. Blocks
// until SIGTERM/SIGINT.
func Run() error {
	cfgPath := ""
	if p := os.Getenv("OMNEVAL_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: levelFromString(cfg.LogLevel)})))

	if err := metrics.Register(); err != nil {
		var are prometheus.AlreadyRegisteredError
		if !errors.As(err, &are) {
			return fmt.Errorf("quack: register metrics: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	scfg := lakeserver.ConfigFromApp(cfg)
	if scfg.Token == "" {
		token, err := randomToken()
		if err != nil {
			return fmt.Errorf("quack: generate token: %w", err)
		}
		scfg.Token = token
		slog.Info("quack: generated auth token (set quack.server.token to pin this across restarts)", "token", token)
	}

	srv, err := lakeserver.Serve(ctx, scfg)
	if err != nil {
		return fmt.Errorf("quack: serve: %w", err)
	}
	defer srv.Close()

	slog.Info("quack: serving Lake catalog",
		"listen_addr", scfg.ListenAddr,
		"reported_addr", srv.Addr(),
		"catalog_driver", scfg.CatalogDriver,
	)

	// One-time cleanup of empty catalog-resident inlined-data tables left
	// behind by DuckLake's data inlining (never pruned by DuckLake itself —
	// see PruneEmptyInlinedTables). Must run against the server's own raw
	// catalog connection (srv.DB()), not a Quack-client attach: the registry
	// table isn't visible through that layer. Only CatalogDriverLocal is
	// confirmed to expose the registry this way.
	if scfg.CatalogDriver == lakeserver.CatalogDriverLocal {
		pruneResult, err := lakeserver.PruneEmptyInlinedTables(ctx, srv.DB())
		if err != nil {
			slog.Error("quack: prune empty inlined tables failed", "err", err)
		} else {
			slog.Info("quack: pruned empty inlined tables", "tables_dropped", pruneResult.TablesDropped, "orphaned_rows_removed", pruneResult.OrphanedRowsRemoved)
		}
	}

	// Table Maintenance no longer runs as an in-process loop here — a
	// production incident (an unbounded ducklake_merge_adjacent_files call
	// against a heavily fragmented table ran 45+ minutes, holding this sole
	// Catalog connection, ADR-0005, hostage for every client the whole time)
	// showed that a stuck/slow pass and the serving process's own
	// liveness/health were too entangled when they shared one goroutine
	// inside this binary. Maintenance is now a separate scheduled job
	// (services/quack/cmd/compact, run via a Kubernetes CronJob — Helm:
	// quack.compact.schedule) that attaches as an ordinary Quack client, the
	// same way Writer/Query/Eval do, and runs one pass per invocation. This
	// does not change DuckLake's concurrency model — a pass still runs
	// synchronously against this same process while it executes — but it
	// decouples a stuck/crashing pass from this process's own health, and
	// lets the schedule be tuned (e.g. off-peak hours) without redeploying
	// the Quack Server.

	// Health/readiness/metrics HTTP server.
	p := probe.New()
	mux := newHTTPMux(p)
	addr := cfg.Metrics.Addr
	if addr == "" {
		addr = ":9090"
	}
	httpSrv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		slog.Info("quack: health server listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("quack: health server error", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("quack: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutdownCtx)

	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
