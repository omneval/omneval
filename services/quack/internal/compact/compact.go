// Package compact runs one Table Maintenance pass (internal/lake/lakeserver)
// against the Lake and exits, instead of looping on an interval inside the
// long-running Quack Server process. It is the binary a Kubernetes CronJob
// invokes on a schedule (Helm: quack.compact.schedule).
//
// Externalizing the schedule does not change DuckLake's concurrency model —
// the Quack Server is still the sole Catalog connection (ADR-0005), and a
// compaction pass still runs synchronously against that same process while
// it executes, exactly as it did when triggered by the in-process ticker.
// What changes: a stuck or crashing maintenance pass can no longer affect
// the serving process's own health/restart behavior, the schedule can be
// tuned (e.g. to off-peak hours) without redeploying the Quack Server, and
// a pass can be paused (suspending the CronJob) independently of serving.
package compact

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeserver"
)

// Run loads config, attaches to the Lake as a Quack client (the same
// quack.client.* configuration Writer/Query/Eval use — this process never
// holds a direct Catalog connection itself), runs one Table Maintenance
// pass, and returns. Each CronJob-scheduled invocation is one pass; the
// CronJob's schedule is the cadence, replacing the old in-process ticker.
func Run() error {
	cfgPath := ""
	if p := os.Getenv("OMNEVAL_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("compact: load config: %w", err)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	l, err := lake.Open(ctx, lake.ConfigFromApp(cfg))
	if err != nil {
		return fmt.Errorf("compact: open lake: %w", err)
	}
	defer l.Close()

	retention := lakeserver.RetentionConfig{
		Enabled:    cfg.Quack.Server.Retention.Enabled,
		MaxAgeDays: cfg.Quack.Server.Retention.MaxAgeDays,
	}

	start := time.Now()
	result, err := lakeserver.RunMaintenance(ctx, l.DB(), lakeserver.MaintenanceTables, retention)
	if err != nil {
		return fmt.Errorf("compact: maintenance pass: %w", err)
	}

	if retention.Enabled {
		slog.Info("compact: maintenance pass complete",
			"duration", time.Since(start),
			"retention_spans_deleted", result.Retention.SpansDeleted,
			"retention_scores_deleted", result.Retention.ScoresDeleted,
		)
	} else {
		slog.Info("compact: maintenance pass complete", "duration", time.Since(start))
	}
	return nil
}
