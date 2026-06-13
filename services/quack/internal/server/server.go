// Package server wires and runs the Quack Server (ADR-0005): the sole
// process holding a direct DuckLake Catalog connection, serving it to
// every other service via quack_serve(), and running the Table Maintenance
// scheduler.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
// serves it over quack_serve(), starts the Table Maintenance scheduler, and
// serves /healthz and /readyz. Blocks until SIGTERM/SIGINT.
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

	// Attach a loopback Quack client to our own catalog for Table
	// Maintenance: the Quack Server is itself a valid Quack client of the
	// catalog it serves (same pattern as Writer/Query API), so it reuses
	// internal/lake's client-attach path rather than duplicating
	// ducklake:quack: ATTACH logic.
	dataPath := cfg.Quack.Server.DataPath
	if dataPath == "" {
		if cfg.Storage.Bucket != "" {
			dataPath = "s3://" + cfg.Storage.Bucket + "/lake"
		} else {
			dataPath = "lake/data"
		}
	}
	loopbackAddr := loopbackAddr(scfg.ListenAddr)
	maintLake, err := lake.Open(ctx, lake.Config{
		QuackAddr:  loopbackAddr,
		QuackToken: srv.Token(),
		DataPath:   dataPath,
		Storage:    storageIfS3(dataPath, cfg),
	})
	if err != nil {
		return fmt.Errorf("quack: open maintenance lake client: %w", err)
	}
	defer maintLake.Close()

	interval, err := time.ParseDuration(cfg.Quack.Server.MaintenanceInterval)
	if err != nil || interval <= 0 {
		interval = lakeserver.DefaultMaintenanceInterval
	}

	retention := lakeserver.RetentionConfig{
		Enabled:    cfg.Quack.Server.Retention.Enabled,
		MaxAgeDays: cfg.Quack.Server.Retention.MaxAgeDays,
	}

	maintDone := make(chan error, 1)
	go func() {
		maintDone <- lakeserver.RunMaintenanceLoop(ctx, maintLake.DB(), lakeserver.MaintenanceTables, interval, retention, func(result lakeserver.MaintenanceResult) {
			if retention.Enabled {
				slog.Info("quack: maintenance retention metrics",
					"spans_deleted", result.Retention.SpansDeleted,
					"scores_deleted", result.Retention.ScoresDeleted,
					"retention_duration", result.Retention.Duration,
				)
			}
		})
	}()

	// Health/readiness HTTP server.
	p := probe.New()
	mux := http.NewServeMux()
	mux.Handle("/healthz", p.HealthHandler())
	mux.Handle("/readyz", p.ReadyHandler())
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

	if err := <-maintDone; err != nil && err != context.Canceled {
		slog.Warn("quack: maintenance loop error", "err", err)
	}

	return nil
}

// loopbackAddr derives the host:port a process should use to reach its own
// quack_serve() listener. Per the spike, only "localhost" was confirmed to
// work (not "127.0.0.1"); a bare ":PORT" ListenAddr becomes
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

func storageIfS3(dataPath string, cfg *config.Config) *config.StorageConfig {
	if strings.HasPrefix(dataPath, "s3://") {
		return &cfg.Storage
	}
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
