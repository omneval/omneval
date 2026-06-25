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
	"strings"
	"syscall"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake"
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

		// One-time repair for the 2026-06-25 concurrent-compact-jobs
		// incident (see RepairMissingDataFiles's doc comment): marks any
		// data file the catalog still references but storage no longer has
		// as removed, so queries against the rest of the table stop
		// 404ing. Remove this call (not the function — it stays useful as
		// a general self-heal) once a production run has been confirmed to
		// have found and repaired the known 10 files.
		//
		// Needs a "ducklake:quack:" client attach (not srv.DB()) to list
		// active files via ducklake_list_files — that table function
		// resolves each file's full storage path for us, which is not
		// worth reimplementing by hand from the raw catalog's
		// path/path_is_relative chase across ducklake_data_file /
		// ducklake_table / ducklake_schema. The repair UPDATE itself still
		// runs against srv.DB(), the only connection that can write to it.
		repairClientCfg := lake.ConfigFromApp(cfg)
		// Loopback to our own quack_serve() listener directly rather than
		// through cfg.Quack.Client.URL's Kubernetes Service DNS: per a
		// prior spike (see the now-removed RunMaintenanceLoop loopback this
		// mirrors), only "localhost" was confirmed to work for a process
		// reaching its own listener this early in startup, and
		// cfg.Quack.Client.Token is unset on the Quack Server's own pod
		// (only its Helm-distinct OMNEVAL_QUACK_SERVER_TOKEN env var is) —
		// use the token this same process is actually serving under.
		repairClientCfg.QuackAddr = loopbackAddr(scfg.ListenAddr)
		repairClientCfg.QuackToken = srv.Token()
		repairClient, err := lake.Open(ctx, repairClientCfg)
		if err != nil {
			slog.Error("quack: repair missing data files: open client failed", "err", err)
		} else {
			repairResult, err := lakeserver.RepairMissingDataFiles(ctx, repairClient.DB(), srv.DB(), lakeserver.MaintenanceTables)
			if err != nil {
				slog.Error("quack: repair missing data files failed", "err", err)
			} else {
				slog.Info("quack: repaired missing data files", "files_checked", repairResult.FilesChecked, "files_repaired", len(repairResult.RepairedPaths), "paths", repairResult.RepairedPaths)
			}
			repairClient.Close()
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

// loopbackAddr derives the host:port a process should use to reach its own
// quack_serve() listener. Per a prior spike, only "localhost" was
// confirmed to work (not "127.0.0.1"); a bare ":PORT" ListenAddr becomes
// "localhost:PORT".
func loopbackAddr(listenAddr string) string {
	if strings.HasPrefix(listenAddr, ":") {
		return "localhost" + listenAddr
	}
	host, port, ok := strings.Cut(listenAddr, ":")
	if !ok {
		return "localhost:" + listenAddr
	}
	if host == "" || host == "0.0.0.0" {
		return "localhost:" + port
	}
	return listenAddr
}
