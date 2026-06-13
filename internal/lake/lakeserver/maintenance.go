package lakeserver

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// DefaultMaintenanceInterval is how often RunMaintenanceLoop runs a pass
// when the configured interval is zero or unparsable.
const DefaultMaintenanceInterval = 5 * time.Minute

// MaintenanceTables lists the DuckLake tables Table Maintenance operates
// on: spans and scores (internal/lake's ensureTables).
var MaintenanceTables = []string{"spans", "scores"}

// RunMaintenance runs one Table Maintenance pass against db, which must
// have a DuckLake catalog attached as "lake" (the Quack Server attaches to
// its own catalog as a Quack client of itself, exactly like any other
// service, since it is the one process that can always reach its own
// quack_serve() loopback).
//
// The pass: rewrite data files (so pending deletes are physically dropped
// and the cleanup calls below can find orphaned files), merge adjacent
// small files, expire old snapshots, delete orphaned/old files, and finally
// flush any remaining inlined data to Parquet. This is the scheduled
// counterpart to internal/lake's per-DeleteProject reclaim() — reclaim
// handles the immediate "this project's data must be gone now" case;
// RunMaintenance handles steady-state compaction for everything else (small
// commits from the Commit Cadence fragmenting the Lake into many small
// Parquet files).
//
// Ordering note (#105, internal/lake/quack_spike6): against a quack-backed
// catalog, ducklake_rewrite_data_files fails with "Scanning a DuckLake table
// after the transaction has ended" if it runs in the same session
// immediately after ducklake_flush_inlined_data — even with no DELETE in
// between, and even from a brand-new connection after another client ran
// flush. ducklake_rewrite_data_files handles any inlined data itself, so
// flush is ordered LAST, after every catalog-scanning call in this pass.
func RunMaintenance(ctx context.Context, db *sql.DB, tables []string) error {
	var stmts []string
	for _, table := range tables {
		stmts = append(stmts, fmt.Sprintf("CALL ducklake_rewrite_data_files('lake', %s)", sqlQuote(table)))
		stmts = append(stmts, fmt.Sprintf("CALL ducklake_merge_adjacent_files('lake', %s)", sqlQuote(table)))
	}
	stmts = append(stmts,
		"CALL ducklake_expire_snapshots('lake', older_than => now())",
		"CALL ducklake_delete_orphaned_files('lake', cleanup_all => true)",
		"CALL ducklake_cleanup_old_files('lake', cleanup_all => true)",
		"CALL ducklake_flush_inlined_data('lake')",
	)

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("lakeserver: maintenance: %s: %w", firstWords(stmt, 3), err)
		}
	}
	return nil
}

// RunMaintenanceLoop runs RunMaintenance on db on the given interval until
// ctx is canceled. Errors are logged and the loop continues — a failed
// pass does not stop future scheduled passes.
func RunMaintenanceLoop(ctx context.Context, db *sql.DB, tables []string, interval time.Duration) error {
	if interval <= 0 {
		interval = DefaultMaintenanceInterval
	}

	slog.Info("lakeserver: table maintenance scheduler started", "interval", interval, "tables", tables)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("lakeserver: table maintenance scheduler stopped")
			return ctx.Err()
		case <-ticker.C:
			start := time.Now()
			if err := RunMaintenance(ctx, db, tables); err != nil {
				slog.Error("lakeserver: table maintenance pass failed", "err", err, "duration", time.Since(start))
				continue
			}
			slog.Info("lakeserver: table maintenance pass complete", "duration", time.Since(start))
		}
	}
}
