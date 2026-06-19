package lakeserver

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/omneval/omneval/internal/storage"
)

// BackupConfig holds the parameters for a single Catalog Backup pass.
type BackupConfig struct {
	// KeepCount is the maximum number of backup objects to retain.
	// Oldest objects beyond KeepCount are deleted.
	KeepCount int
}

// BackupResult reports what a Catalog Backup pass did.
type BackupResult struct {
	// Uploaded is 1 when a checkpoint was uploaded, 0 when the driver is
	// postgres (no-op).
	Uploaded int
	// Deleted is the number of old backup objects pruned in this pass.
	Deleted int
	// Skipped is true when the catalog driver is postgres and no backup
	// was attempted.
	Skipped bool
}

// DefaultBackupPrefix is the S3 prefix for catalog backup objects.
const DefaultBackupPrefix = "catalog-backups"

// RunBackup checkpoints the DuckDB catalog file via the CHECKPOINT SQL
// command, uploads the resulting snapshot to an S3 timestamped key under
// the configured prefix, and prunes old backups beyond b.KeepCount.
//
// When catalogDriver is "postgres" the function is a no-op: it returns
// immediately with BackupResult{Skipped: true} because Postgres does not
// need file-level checkpointing/backing up.
//
// The upload key is "<prefix>/<catalogName>/<timestamp>.duckdb" where
// <timestamp> uses RFC3339 nanosecond precision for unique sorting.
func RunBackup(ctx context.Context, db *sql.DB, catalogDriver string, store storage.ObjectStore, catalogName string, b BackupConfig) (BackupResult, error) {
	if b.KeepCount <= 0 {
		b.KeepCount = 24
	}

	// No-op for postgres: the catalog lives in Postgres and does not need
	// file-level checkpointing or S3 upload.
	if catalogDriver == CatalogDriverPostgres {
		slog.Info("lakeserver: catalog backup skipped (postgres driver)", "catalog", catalogName)
		return BackupResult{Skipped: true}, nil
	}

	// Step 1: CHECKPOINT to flush all inlined data to the catalog file.
	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		return BackupResult{}, fmt.Errorf("lakeserver: backup: checkpoint: %w", err)
	}

	// Step 2: Query the catalog file path from the DuckDB connection so we
	// can read back the actual file content for upload.
	catalogPath, err := readCatalogPath(ctx, db)
	if err != nil {
		return BackupResult{}, fmt.Errorf("lakeserver: backup: read catalog path: %w", err)
	}

	// Step 3: Read the catalog file bytes.
	fileData, err := os.ReadFile(catalogPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("lakeserver: backup: read catalog file %s: %w", catalogPath, err)
	}

	// Step 4: Build the S3 key with an RFC3339 timestamp for unique sorting.
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	uploadKey := filepath.Join(DefaultBackupPrefix, catalogName, ts+".duckdb")

	// Step 5: Upload the catalog file content to S3.
	payload := bytes.NewReader(fileData)
	if err := store.PutSized(ctx, uploadKey, payload, int64(len(fileData))); err != nil {
		return BackupResult{}, fmt.Errorf("lakeserver: backup: upload %s: %w", uploadKey, err)
	}

	slog.Info("lakeserver: catalog backup uploaded", "key", uploadKey, "catalog", catalogName, "size", len(fileData))
	uploaded := 1

	// Step 6: List existing backups, sort by timestamp (which is embedded
	// in the key name), and prune oldest beyond keepCount.
	deleted, err := pruneBackups(ctx, store, DefaultBackupPrefix, catalogName, b.KeepCount)
	if err != nil {
		slog.Error("lakeserver: backup: prune failed", "err", err)
		// Don't fail the backup just because pruning failed.
	}

	return BackupResult{
		Uploaded: uploaded,
		Deleted:  deleted,
		Skipped:  false,
	}, nil
}

// readCatalogPath queries the DuckDB connection for the underlying catalog
// file path. It returns the file path (the file column from PRAGMA
// database_list). For DuckDB-backed file catalogs the name column contains
// the base name of the catalog file, not "main". Returns an error if the
// path cannot be determined or the catalog is in-memory.
func readCatalogPath(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT file FROM pragma_database_list`)
	if err != nil {
		return "", fmt.Errorf("query pragma_database_list: %w", err)
	}
	defer rows.Close()

	var file string
	found := false
	for rows.Next() {
		if err := rows.Scan(&file); err != nil {
			return "", fmt.Errorf("scan pragma_database_list file: %w", err)
		}
		if file != "" {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate pragma_database_list: %w", err)
	}
	if !found {
		return "", fmt.Errorf("no file-based catalog found — catalog may be in-memory")
	}
	return file, nil
}

// DefaultBackupInterval is how often RunBackupLoop runs a pass when the
// configured interval is zero or unparsable.
const DefaultBackupInterval = 1 * time.Hour

// RunBackupLoop runs RunBackup on db on the given interval until ctx is
// canceled. Errors are logged and the loop continues — a failed pass does
// not stop future scheduled passes. onResult, if non-nil, is called with the
// result of each pass (used by services/quack to record backup metrics).
func RunBackupLoop(ctx context.Context, db *sql.DB, catalogDriver string, store storage.ObjectStore, catalogName string, b BackupConfig, interval time.Duration, onResult func(BackupResult)) error {
	if interval <= 0 {
		interval = DefaultBackupInterval
	}

	slog.Info("lakeserver: catalog backup scheduler started", "interval", interval, "keepCount", b.KeepCount)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("lakeserver: catalog backup scheduler stopped")
			return ctx.Err()
		case <-ticker.C:
			start := time.Now()
			result, err := RunBackup(ctx, db, catalogDriver, store, catalogName, b)
			if err != nil {
				slog.Error("lakeserver: catalog backup pass failed", "err", err, "duration", time.Since(start))
				continue
			}
			if result.Skipped {
				slog.Info("lakeserver: catalog backup skipped", "duration", time.Since(start))
			} else {
				slog.Info("lakeserver: catalog backup pass complete",
					"duration", time.Since(start),
					"uploaded", result.Uploaded,
					"deleted", result.Deleted,
				)
			}
			if onResult != nil {
				onResult(result)
			}
		}
	}
}

// pruneBackups lists all backups under the prefix/catalogName, sorts them by
// timestamp (embedded in the key), and deletes the oldest ones beyond
// keepCount. It returns the number of objects deleted.
func pruneBackups(ctx context.Context, store storage.ObjectStore, prefix, catalogName string, keepCount int) (int, error) {
	allPrefix := filepath.Join(prefix, catalogName)
	keys, err := store.ListPrefix(ctx, allPrefix)
	if err != nil {
		return 0, fmt.Errorf("lakeserver: backup: list: %w", err)
	}

	// Sort keys by timestamp — the timestamp is the RFC3339Nano portion
	// of the filename (before .duckdb), and lexicographic sort on RFC3339
	// is equivalent to chronological sort.
	sort.Strings(keys)

	toDelete := len(keys) - keepCount
	if toDelete <= 0 {
		return 0, nil
	}

	var deleted int
	for i := 0; i < toDelete; i++ {
		if err := store.Delete(ctx, keys[i]); err != nil {
			slog.Error("lakeserver: backup: delete old backup", "key", keys[i], "err", err)
			continue
		}
		deleted++
	}

	if deleted > 0 {
		slog.Info("lakeserver: catalog backup pruned old backups", "deleted", deleted, "remaining", len(keys)-deleted)
	}
	return deleted, nil
}