package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/omneval/omneval/internal/harness"
)

// Run starts the Writer Service: drains the Redis ingest queue, computes
// cost, commits batches to the Lake (ADR-0004), and serves
// POST /internal/v1/scores for Eval Worker score write-back.
func Run() error {
	h, err := harness.New("")
	if err != nil {
		return fmt.Errorf("create harness: %w", err)
	}

	return h.Run(context.Background(), func(ctx *harness.HarnessContext) (string, http.Handler, harness.ShutdownFunc, harness.ShutdownFunc, error) {
		deps, err := WireDeps(ctx.Cfg)
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("wire deps: %w", err)
		}

		// Build the full router: /internal/v1/scores + probes.
		ctx.Mux.Handle("/internal/v1/scores", deps.ScoreHandler)

		// Start background workers. Every replica runs the Ingest Buffer
		// reconciliation sweep on its own ticker (#90): DuckLake supports
		// multi-writer, so there is no leader-election gate.
		if deps.Reconcile != nil {
			ctx.StartBackground(func(ctx context.Context) error {
				slog.Info("writer: reconciliation worker started")
				if err := deps.Reconcile.RunLoop(ctx); err != nil && err != context.Canceled {
					slog.Error("writer: reconciliation worker error", "err", err)
				}
				return nil
			})
		}

		// Start pipeline (blocks until ctx is canceled).
		ctx.StartBackground(func(ctx context.Context) error {
			slog.Info("writer: pipeline started")
			if err := deps.Pipeline.Run(ctx); err != nil && err != context.Canceled {
				slog.Error("writer: pipeline error", "err", err)
			}
			return nil
		})

		// Pre-HTTP-shutdown: signal the Lake so any reconnect() already in
		// flight (or queued behind one) aborts immediately instead of running
		// its own ~10s budget per queued caller — see Lake.Shutdown's doc for
		// the production incident this avoids.
		preShutdown := func() {
			if deps.Lake != nil {
				deps.Lake.Shutdown()
			}
		}

		// Teardown function called during graceful shutdown (after HTTP
		// server has drained).
		teardown := func() {
			deps.Meta.Close()
		}

		return ctx.Cfg.Writer.Addr, ctx.Mux, preShutdown, teardown, nil
	})
}
