package lakeserver

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"time"
)

// DefaultMaintenanceInterval is how often RunMaintenanceLoop runs a pass
// when the configured interval is zero or unparsable. 15m rather than the
// original 5m: a shorter cadence creates more merge-lineage generations
// (each compaction pass's rewrite/merge output) than light workloads need,
// independent of whether cleanup successfully reclaims the superseded
// ones — skip-when-idle (see RunMaintenanceLoop) avoids wasted passes when
// nothing changed, but a longer baseline interval also means fewer
// intermediate file generations are ever created in the first place.
const DefaultMaintenanceInterval = 15 * time.Minute

// MaintenanceTables lists the DuckLake tables Table Maintenance operates
// on: spans and scores (internal/lake's ensureTables).
var MaintenanceTables = []string{"spans", "scores"}

// RetentionConfig controls the Lake-native retention step that runs as part
// of Table Maintenance (#92). It replaces the legacy S3-prefix-based,
// file-deletion retention worker (services/writer/internal/retention),
// which is wrong under DuckLake: deleting files out from under the Catalog
// corrupts the table. This DELETEs aged rows through the Catalog instead,
// and lets the same maintenance pass's rewrite/cleanup steps reclaim the
// physical Parquet files.
type RetentionConfig struct {
	// Enabled turns on the retention DELETE step. Default false: Table
	// Maintenance runs its compaction steps only, with no data loss.
	Enabled bool
	// MaxAgeDays is how old a span (by start_time) or score (by
	// span_start_time, ADR-0002) must be to be deleted. Rows with
	// start_time/span_start_time < now() - MaxAgeDays are deleted.
	MaxAgeDays int
}

// RetentionResult reports what the retention step of a maintenance pass did.
type RetentionResult struct {
	// SpansDeleted is the number of rows removed from lake.spans.
	SpansDeleted int64
	// ScoresDeleted is the number of rows removed from lake.scores.
	ScoresDeleted int64
	// Duration is how long the retention DELETEs took (not the whole
	// maintenance pass).
	Duration time.Duration
}

// MaintenanceResult reports the outcome of a Table Maintenance pass.
type MaintenanceResult struct {
	// Retention reports the retention step's outcome. Zero value when
	// retention was disabled (no DELETEs ran).
	Retention RetentionResult
	// Skipped is true when RunMaintenanceLoop skipped this tick's
	// compaction statements because no new snapshot was committed since
	// the previous completed pass (nothing to rewrite/merge/clean up).
	// Retention is always its zero value when Skipped is true.
	Skipped bool
}

// RunMaintenance runs one Table Maintenance pass against db, which must
// have a DuckLake catalog attached as "lake" (the Quack Server attaches to
// its own catalog as a Quack client of itself, exactly like any other
// service, since it is the one process that can always reach its own
// quack_serve() loopback).
//
// The pass: (if retention is enabled) delete spans/scores older than
// retention.MaxAgeDays, then rewrite data files (so pending deletes —
// retention's or any other DELETEs since the last pass — are physically
// dropped and the cleanup call below can find stale files), merge
// adjacent small files, expire old snapshots, delete old (catalog-tracked
// stale) files, and finally flush any remaining inlined data to Parquet.
// This is the scheduled
// counterpart to internal/lake's per-DeleteProject reclaim() — reclaim
// handles the immediate "this project's data must be gone now" case;
// RunMaintenance handles steady-state compaction for everything else (small
// commits from the Commit Cadence fragmenting the Lake into many small
// Parquet files) plus, now, time-based retention.
//
// Retention ordering (#92): the DELETE runs FIRST, before
// ducklake_rewrite_data_files, on the same session/connection — mirroring
// reclaim()'s DELETE-then-rewrite pattern (internal/lake.DeleteProject),
// which the #111/#105 quack_spike found is required against a quack-backed
// catalog (a DELETE followed by rewrite on a NEW connection hits "Scanning a
// DuckLake table after the transaction has ended"). This way the deleted
// rows' Parquet pages are reclaimed in the SAME pass, not a subsequent one.
//
// Ordering note (#105, internal/lake/quack_spike6): against a quack-backed
// catalog, ducklake_rewrite_data_files fails with "Scanning a DuckLake table
// after the transaction has ended" if it runs in the same session
// immediately after ducklake_flush_inlined_data — even with no DELETE in
// between, and even from a brand-new connection after another client ran
// flush. ducklake_rewrite_data_files handles any inlined data itself, so
// flush is ordered LAST, after every catalog-scanning call (including
// retention's DELETE) in this pass.
func RunMaintenance(ctx context.Context, db *sql.DB, tables []string, retention RetentionConfig) (MaintenanceResult, error) {
	var result MaintenanceResult

	if retention.Enabled {
		start := time.Now()
		cutoff := time.Now().AddDate(0, 0, -retention.MaxAgeDays)

		spansRes, err := db.ExecContext(ctx, "DELETE FROM lake.spans WHERE start_time < ?", cutoff)
		if err != nil {
			return result, fmt.Errorf("lakeserver: maintenance: retention: delete spans: %w", err)
		}
		spansDeleted, err := spansRes.RowsAffected()
		if err != nil {
			return result, fmt.Errorf("lakeserver: maintenance: retention: spans rows affected: %w", err)
		}

		scoresRes, err := db.ExecContext(ctx, "DELETE FROM lake.scores WHERE span_start_time < ?", cutoff)
		if err != nil {
			return result, fmt.Errorf("lakeserver: maintenance: retention: delete scores: %w", err)
		}
		scoresDeleted, err := scoresRes.RowsAffected()
		if err != nil {
			return result, fmt.Errorf("lakeserver: maintenance: retention: scores rows affected: %w", err)
		}

		result.Retention = RetentionResult{
			SpansDeleted:  spansDeleted,
			ScoresDeleted: scoresDeleted,
			Duration:      time.Since(start),
		}
	}

	var stmts []string
	for _, table := range tables {
		stmts = append(stmts, fmt.Sprintf("CALL ducklake_rewrite_data_files('lake', %s)", sqlQuote(table)))
		stmts = append(stmts, fmt.Sprintf("CALL ducklake_merge_adjacent_files('lake', %s)", sqlQuote(table)))
	}
	stmts = append(stmts,
		"CALL ducklake_expire_snapshots('lake', older_than => now())",
		// ducklake_delete_orphaned_files is intentionally NOT called here.
		// Per its purpose (finding files in the data path that the catalog
		// has no record of at all), it always globs the data path via
		// read_blob(<data_path> || '**') regardless of cleanup_all. That
		// glob ignores the lake_s3 secret's ENDPOINT/URL_STYLE/REGION and
		// falls back to virtual-hosted-style requests against the default
		// AWS endpoint, failing with NoSuchBucket against MinIO
		// (duckdb/ducklake#562) — confirmed broken even without cleanup_all
		// in the issue's own reproduction. There is no SQL-level
		// workaround, so orphan-file cleanup is skipped; ducklake_rewrite_*
		// + ducklake_cleanup_old_files (without cleanup_all, which operate
		// on files the catalog metadata already knows are stale via GET/PUT)
		// still reclaim the normal steady-state compaction churn.
		"CALL ducklake_cleanup_old_files('lake')",
		"CALL ducklake_flush_inlined_data('lake')",
	)

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return result, fmt.Errorf("lakeserver: maintenance: %s: %w", firstWords(stmt, 3), err)
		}
	}
	return result, nil
}

// inlinedTableNamePattern matches DuckLake's machine-generated naming for
// catalog-resident inlined-data tables: ducklake_inlined_data_<table_id>_<schema_version>.
// PruneEmptyInlinedTables only ever operates on rows whose table_name
// matches this pattern, as defense-in-depth before DROP TABLE.
var inlinedTableNamePattern = regexp.MustCompile(`^ducklake_inlined_data_[0-9]+_[0-9]+$`)

// PruneResult reports what PruneEmptyInlinedTables did.
type PruneResult struct {
	// TablesDropped is the number of empty inlined tables that were
	// dropped and deregistered.
	TablesDropped int
	// OrphanedRowsRemoved is the number of registry rows that referenced a
	// table that no longer physically exists at all (DuckLake itself can
	// leave these behind; confirmed in production), deregistered without a
	// DROP TABLE since there was nothing to drop.
	OrphanedRowsRemoved int
}

// PruneEmptyInlinedTables drops every catalog-resident inlined-data table
// (ducklake_inlined_data_<table_id>_<schema_version>) that is currently
// empty, and removes its row from the ducklake_inlined_data_tables
// registry. ducklake_flush_inlined_data empties an inlined table into
// Parquet but never drops the now-dead table or its registry row —
// DuckLake's query planner unions across every entry in that registry when
// scanning a table, so thousands of long-dead empty entries make even a
// trivial query take minutes, independent of real data volume (confirmed:
// 1 entry -> 2.1s, 4000 entries -> 28m for an identical COUNT(*) query).
//
// db must be the Quack Server's own raw catalog connection (Server.DB()),
// never a "ducklake:quack:" client attach — the registry table isn't
// visible at all through that layer. For CatalogDriverLocal this
// connection is the sole writer (Serve sets db.SetMaxOpenConns(1)), so
// every other catalog operation (including remote Quack-client requests)
// is already serialized against this same connection, making the
// count-then-drop sequence below race-free without an explicit transaction.
func PruneEmptyInlinedTables(ctx context.Context, db *sql.DB) (PruneResult, error) {
	var result PruneResult

	rows, err := db.QueryContext(ctx, "SELECT table_id, table_name, schema_version FROM ducklake_inlined_data_tables")
	if err != nil {
		return result, fmt.Errorf("lakeserver: prune inlined tables: list: %w", err)
	}
	type entry struct {
		tableID       int64
		tableName     string
		schemaVersion int64
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.tableID, &e.tableName, &e.schemaVersion); err != nil {
			rows.Close()
			return result, fmt.Errorf("lakeserver: prune inlined tables: scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, fmt.Errorf("lakeserver: prune inlined tables: rows: %w", err)
	}
	rows.Close()

	for _, e := range entries {
		if !inlinedTableNamePattern.MatchString(e.tableName) {
			return result, fmt.Errorf("lakeserver: prune inlined tables: registry table_name %q does not match expected pattern, refusing to touch it", e.tableName)
		}

		deregister := func() error {
			_, err := db.ExecContext(ctx,
				"DELETE FROM ducklake_inlined_data_tables WHERE table_id = ? AND table_name = ? AND schema_version = ?",
				e.tableID, e.tableName, e.schemaVersion,
			)
			return err
		}

		var exists int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_tables() WHERE table_name = ?", e.tableName).Scan(&exists); err != nil {
			return result, fmt.Errorf("lakeserver: prune inlined tables: check exists %s: %w", e.tableName, err)
		}
		if exists == 0 {
			// Confirmed in production: DuckLake can leave a registry row
			// behind whose table was already dropped/never materialized.
			// Nothing to lose by deregistering it.
			if err := deregister(); err != nil {
				return result, fmt.Errorf("lakeserver: prune inlined tables: deregister orphaned %s: %w", e.tableName, err)
			}
			result.OrphanedRowsRemoved++
			continue
		}

		var n int
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"`, e.tableName)).Scan(&n); err != nil {
			return result, fmt.Errorf("lakeserver: prune inlined tables: count %s: %w", e.tableName, err)
		}
		if n != 0 {
			continue
		}

		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE "%s"`, e.tableName)); err != nil {
			return result, fmt.Errorf("lakeserver: prune inlined tables: drop %s: %w", e.tableName, err)
		}
		if err := deregister(); err != nil {
			return result, fmt.Errorf("lakeserver: prune inlined tables: deregister %s: %w", e.tableName, err)
		}
		result.TablesDropped++
	}
	return result, nil
}

// latestSnapshotID returns the highest DuckLake snapshot ID committed
// against the "lake" catalog so far.
func latestSnapshotID(ctx context.Context, db *sql.DB) (int64, error) {
	var id int64
	if err := db.QueryRowContext(ctx, "SELECT max(snapshot_id) FROM ducklake_snapshots('lake')").Scan(&id); err != nil {
		return 0, fmt.Errorf("lakeserver: maintenance: latest snapshot id: %w", err)
	}
	return id, nil
}

// RunMaintenanceLoop runs RunMaintenance on db on the given interval until
// ctx is canceled. Errors are logged and the loop continues — a failed
// pass does not stop future scheduled passes. onResult, if non-nil, is
// called with the result of each tick (used by services/quack to record
// retention metrics), including skipped ticks.
//
// Each tick after the first is skipped — RunMaintenance's
// rewrite/merge/expire/cleanup statements are not run — when no new
// DuckLake snapshot has been committed since the previous completed pass
// and retention is disabled. With two idle agents and a short interval,
// most ticks otherwise have nothing to compact; running the full pass
// anyway writes a fresh (often near-empty) merged Parquet file every
// interval for no benefit. Retention is never skipped this way because the
// retention cutoff is time-based, not snapshot-based: rows can become
// newly eligible for deletion purely because time passed, even with zero
// new writes.
func RunMaintenanceLoop(ctx context.Context, db *sql.DB, tables []string, interval time.Duration, retention RetentionConfig, onResult func(MaintenanceResult)) error {
	if interval <= 0 {
		interval = DefaultMaintenanceInterval
	}

	slog.Info("lakeserver: table maintenance scheduler started", "interval", interval, "tables", tables, "retention_enabled", retention.Enabled)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastSnapshotID int64
	haveBaseline := false

	for {
		select {
		case <-ctx.Done():
			slog.Info("lakeserver: table maintenance scheduler stopped")
			return ctx.Err()
		case <-ticker.C:
			if !retention.Enabled && haveBaseline {
				current, err := latestSnapshotID(ctx, db)
				if err == nil && current == lastSnapshotID {
					slog.Info("lakeserver: table maintenance pass skipped (no new snapshot)", "snapshot_id", current)
					if onResult != nil {
						onResult(MaintenanceResult{Skipped: true})
					}
					continue
				}
			}

			start := time.Now()
			result, err := RunMaintenance(ctx, db, tables, retention)
			if err != nil {
				slog.Error("lakeserver: table maintenance pass failed", "err", err, "duration", time.Since(start))
				continue
			}
			if id, err := latestSnapshotID(ctx, db); err == nil {
				lastSnapshotID = id
				haveBaseline = true
			}
			if retention.Enabled {
				slog.Info("lakeserver: table maintenance pass complete",
					"duration", time.Since(start),
					"retention_spans_deleted", result.Retention.SpansDeleted,
					"retention_scores_deleted", result.Retention.ScoresDeleted,
					"retention_duration", result.Retention.Duration,
				)
			} else {
				slog.Info("lakeserver: table maintenance pass complete", "duration", time.Since(start))
			}
			if onResult != nil {
				onResult(result)
			}
		}
	}
}
