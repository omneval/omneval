package lakeserver

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// RepairResult reports what RepairMissingDataFiles did.
type RepairResult struct {
	// FilesChecked is the number of currently-active data files examined
	// across all tables passed in.
	FilesChecked int
	// RepairedPaths lists the full paths of files found missing from
	// storage and marked removed.
	RepairedPaths []string
}

// RepairMissingDataFiles checks every currently-active data file (per
// ducklake_list_files, queried via clientDB — any "ducklake:quack:" client
// attach works, since listing files and checking storage are both
// read-only) in each of tables against storage, and marks any file that no
// longer exists there as removed: end_snapshot is set to its own
// begin_snapshot via rawDB, which DuckLake's visibility rule
// (begin_snapshot <= snapshot_id < end_snapshot) makes false for every
// snapshot, past or future — see
// ducklake.select/docs/stable/specification/queries. This is the
// documented mechanism for retiring a file; it leaves the catalog row
// itself (and its size/record-count audit trail) intact rather than
// deleting it.
//
// Without this, any query that scans a partition containing one of these
// files fails outright (404/NoSuchKey from the object store) even though
// the catalog believes the row data is present and reports a non-zero
// table row count.
//
// Production incident (2026-06-25): a manually-created Kubernetes Job ran
// services/quack/cmd/compact concurrently with the CronJob-scheduled
// instance of the same job, bypassing the CronJob's own
// concurrencyPolicy: Forbid (which only governs Jobs the CronJob
// controller itself creates — a Job created out-of-band is not subject to
// it). Two simultaneous ducklake_merge_adjacent_files /
// ducklake_cleanup_old_files passes against the same catalog raced; the
// losing transaction's rollback cleanup — confirmed expected DuckLake
// behavior when a transaction fails to commit, see
// github.com/duckdb/ducklake/issues/84 — deleted Parquet files the
// winning transaction's already-committed snapshot still referenced. 10
// files / 1270 rows (~1.8% of the affected table) ended up in this state.
//
// rawDB must be the Quack Server's own raw catalog connection (Server.DB()):
// ducklake_data_file is exposed read-only through a "ducklake:quack:"
// client attach (UPDATE fails with "Binder Error: Can only update base
// table"), by design — ADR-0005 centralizes catalog mutation in this one
// process. This mirrors PruneEmptyInlinedTables's same constraint. Unlike
// PruneEmptyInlinedTables's tables, ducklake_data_file is queried bare (no
// alias prefix) on this connection, exactly like ducklake_inlined_data_tables.
func RepairMissingDataFiles(ctx context.Context, clientDB *sql.DB, rawDB *sql.DB, tables []string) (RepairResult, error) {
	var result RepairResult

	for _, table := range tables {
		rows, err := clientDB.QueryContext(ctx, "SELECT data_file FROM ducklake_list_files('lake', ?)", table)
		if err != nil {
			return result, fmt.Errorf("lakeserver: repair missing files: list %s: %w", table, err)
		}
		var paths []string
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				rows.Close()
				return result, fmt.Errorf("lakeserver: repair missing files: scan %s: %w", table, err)
			}
			paths = append(paths, path)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return result, fmt.Errorf("lakeserver: repair missing files: rows %s: %w", table, err)
		}
		rows.Close()

		for _, path := range paths {
			result.FilesChecked++

			var found int
			if err := clientDB.QueryRowContext(ctx, "SELECT count(*) FROM glob(?)", path).Scan(&found); err != nil {
				return result, fmt.Errorf("lakeserver: repair missing files: probe %s: %w", path, err)
			}
			if found > 0 {
				continue
			}

			slog.WarnContext(ctx, "lakeserver: repair missing files: marking unreadable file removed", "table", table, "path", path)
			// ducklake_data_file.path is stored relative to the table's own
			// path (path_is_relative) and always forward-slashed, even when
			// ducklake_list_files's resolved full path comes back with
			// platform-native separators (observed on Windows); normalize
			// before the suffix match below rather than needing to
			// independently resolve the relative path's prefix here.
			normalizedPath := strings.ReplaceAll(path, `\`, "/")
			res, err := rawDB.ExecContext(ctx,
				`UPDATE ducklake_data_file
				 SET end_snapshot = begin_snapshot
				 WHERE end_snapshot IS NULL AND ? LIKE '%' || path`,
				normalizedPath,
			)
			if err != nil {
				return result, fmt.Errorf("lakeserver: repair missing files: mark removed %s: %w", path, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return result, fmt.Errorf("lakeserver: repair missing files: rows affected %s: %w", path, err)
			}
			if n != 1 {
				return result, fmt.Errorf("lakeserver: repair missing files: marking %s removed affected %d rows, want exactly 1", path, n)
			}
			result.RepairedPaths = append(result.RepairedPaths, path)
		}
	}
	return result, nil
}
