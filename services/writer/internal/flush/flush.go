package flush

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/storage"
)

const defaultBucket = "omneval"

// Flusher exports spans older than flushAge from DuckDB to Hive-partitioned
// Parquet files on S3 and prunes the corresponding rows from the hot store.
type Flusher struct {
	store    storage.ObjectStore
	db       *sql.DB
	cfg      *config.Config
	flushAge time.Duration
	writeDir string // optional local output directory (testing only)
}

// New creates a new Flusher (legacy, does not use db or store).
func New(client any, cfg *config.Config) *Flusher {
	return newFlusher(cfg)
}

// NewWithDB creates a new Flusher with an ObjectStore and DuckDB connection.
func NewWithDB(store storage.ObjectStore, db *sql.DB, cfg *config.Config) *Flusher {
	return newFlusherWithDB(store, db, cfg)
}

// newFlusher builds a Flusher with a computed flushAge from config.
func newFlusher(cfg *config.Config) *Flusher {
	return &Flusher{cfg: cfg, flushAge: flushAge(cfg)}
}

// newFlusherWithDB builds a Flusher with storage and database backing.
func newFlusherWithDB(store storage.ObjectStore, db *sql.DB, cfg *config.Config) *Flusher {
	return &Flusher{
		store:    store,
		db:       db,
		cfg:      cfg,
		flushAge: flushAge(cfg),
	}
}

// flushAge computes the flush age from config, falling back to 48h.
func flushAge(cfg *config.Config) time.Duration {
	if cfg.Writer.FlushAgeDays > 0 {
		return time.Duration(cfg.Writer.FlushAgeDays) * 24 * time.Hour
	}
	return 48 * time.Hour
}

// WithFlushAge sets a custom flush age, useful in tests.
func (f *Flusher) WithFlushAge(d time.Duration) *Flusher {
	f.flushAge = d
	return f
}

// WithWriteDir sets a local output directory, useful in tests.
func (f *Flusher) WithWriteDir(dir string) *Flusher {
	f.writeDir = dir
	return f
}

// Run blocks until ctx is canceled. Every flush interval it exports aged
// spans from DuckDB to Parquet on S3 and prunes them from the hot store.
func (f *Flusher) Run(ctx context.Context) error {
	flushInterval, err := time.ParseDuration(f.cfg.Writer.FlushInterval)
	if err != nil {
		flushInterval = 30 * time.Minute
	}
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := f.doFlush(ctx); err != nil {
				slog.ErrorContext(ctx, "writer: flusher error", "err", err)
			}
		}
	}
}

// doFlush performs a single flush cycle. It identifies partitions (project_id + date)
// older than flushAge, writes them as Parquet to S3, and only then deletes from DuckDB.
// The delete is transactional — if any S3 write fails, rows remain in DuckDB.
func (f *Flusher) doFlush(ctx context.Context) error {
	if f.cfg.Storage.Endpoint == "" {
		slog.InfoContext(ctx, "writer: flusher skipped", "reason", "no_s3_endpoint")
		return nil
	}

	if f.store == nil {
		slog.InfoContext(ctx, "writer: flusher skipped", "reason", "no_object_store")
		return nil
	}

	cutoff := time.Now().UTC().Add(-f.flushAge)

	// Identify distinct (project_id, date) partitions older than the cutoff.
	partitions, err := f.listAgedPartitions(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("flusher: list aged partitions: %w", err)
	}

	if len(partitions) == 0 {
		slog.InfoContext(ctx, "writer: flusher: no aged partitions to flush")
		return nil
	}

	slog.InfoContext(ctx, "writer: flusher: flushing partitions", "count", len(partitions))

	var firstErr error
	for _, p := range partitions {
		if err := f.flushPartition(ctx, p); err != nil {
			slog.ErrorContext(ctx, "writer: flusher: failed to flush partition",
				"project_id", p.projectID,
				"date", p.date,
				"err", err,
			)
			if firstErr == nil {
				firstErr = err
			}
			// Continue with other partitions — one failure doesn't block the rest.
		}
	}

	if firstErr != nil {
		return fmt.Errorf("flusher: %w", firstErr)
	}
	return nil
}

// partitionKey represents a single (project_id, date) partition to flush.
type partitionKey struct {
	projectID string
	date      string // YYYY-MM-DD format
}

// listAgedPartitions returns distinct (project_id, date) pairs where spans
// in the partition are older than cutoff.
func (f *Flusher) listAgedPartitions(ctx context.Context, cutoff time.Time) ([]partitionKey, error) {
	rows, err := f.db.QueryContext(ctx, `
		SELECT DISTINCT
			project_id,
			DATE(start_time) AS pdate
		FROM spans
		WHERE start_time < ?
		ORDER BY project_id, pdate
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list aged partitions: %w", err)
	}
	defer rows.Close()

	var partitions []partitionKey
	for rows.Next() {
		var pk partitionKey
		if err := rows.Scan(&pk.projectID, &pk.date); err != nil {
			return nil, fmt.Errorf("scan partition: %w", err)
		}
		// Normalize date format: ensure YYYY-MM-DD (DuckDB may return timestamp strings).
		if d, err := parseDuckDBDate(pk.date); err == nil {
			pk.date = d
		}
		partitions = append(partitions, pk)
	}
	return partitions, rows.Err()
}

// flushPartition writes spans and scores for a single partition to S3 as Parquet,
// then deletes from DuckDB. Both Parquet files are written before any deletion,
// ensuring no partition is ever in a state where cold spans exist without cold scores.
func (f *Flusher) flushPartition(ctx context.Context, pk partitionKey) error {
	spansKey := partitionPath(pk, "spans", "spans.parquet")
	scoresKey := partitionPath(pk, "scores", "scores.parquet")

	slog.InfoContext(ctx, "writer: flusher: writing partition",
		"project_id", pk.projectID,
		"date", pk.date,
		"spans_key", spansKey,
		"scores_key", scoresKey,
	)

	if f.writeDir != "" {
		// Testing mode: write to local directories, then upload to S3.
		spansDir := filepath.Join(f.writeDir, partitionPath(pk, "spans", ""))
		scoresDir := filepath.Join(f.writeDir, partitionPath(pk, "scores", ""))
		if err := os.MkdirAll(spansDir, 0755); err != nil {
			return fmt.Errorf("create spans dir: %w", err)
		}
		if err := os.MkdirAll(scoresDir, 0755); err != nil {
			return fmt.Errorf("create scores dir: %w", err)
		}
		spansURL := "file://" + filepath.Join(spansDir, "spans.parquet")
		scoresURL := "file://" + filepath.Join(scoresDir, "scores.parquet")

		if err := f.writeSpansParquet(ctx, pk, spansURL); err != nil {
			return fmt.Errorf("write spans parquet: %w", err)
		}
		if err := f.writeScoresParquet(ctx, pk, scoresURL); err != nil {
			return fmt.Errorf("write scores parquet: %w", err)
		}

		if err := f.uploadToS3(ctx, spansKey, spansURL); err != nil {
			return fmt.Errorf("upload spans to s3: %w", err)
		}
		if err := f.uploadToS3(ctx, scoresKey, scoresURL); err != nil {
			return fmt.Errorf("upload scores to s3: %w", err)
		}

		// Clean up local Parquet files after uploading to S3.
		for _, u := range []string{spansURL, scoresURL} {
			os.Remove(strings.TrimPrefix(u, "file://"))
		}
	} else {
		// Production mode: write directly to S3 via DuckDB httpfs.
		spansS3URL := f.s3URL(spansKey)
		scoresS3URL := f.s3URL(scoresKey)

		if err := f.writeSpansParquet(ctx, pk, spansS3URL); err != nil {
			return fmt.Errorf("write spans parquet: %w", err)
		}
		if err := f.writeScoresParquet(ctx, pk, scoresS3URL); err != nil {
			return fmt.Errorf("write scores parquet: %w", err)
		}
	}

	slog.InfoContext(ctx, "writer: flusher: partition Parquet files written, pruning from DuckDB",
		"project_id", pk.projectID,
		"date", pk.date,
	)

	// Both Parquet files confirmed — now safely delete from DuckDB.
	if err := f.deleteFlushedRows(ctx, pk); err != nil {
		return fmt.Errorf("delete flushed rows: %w", err)
	}

	slog.InfoContext(ctx, "writer: flusher: partition flushed",
		"project_id", pk.projectID,
		"date", pk.date,
	)
	return nil
}

// uploadToS3 reads a local Parquet file (optionally prefixed with file://) and uploads it to S3.
func (f *Flusher) uploadToS3(ctx context.Context, s3Key string, localPath string) error {
	// Strip file:// prefix if present.
	localPath = strings.TrimPrefix(localPath, "file://")

	fh, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer fh.Close()

	info, err := fh.Stat()
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}

	if err := f.store.PutSized(ctx, s3Key, fh, info.Size()); err != nil {
		return fmt.Errorf("put s3 %s: %w", s3Key, err)
	}
	return nil
}

// writeSpansParquet uses DuckDB's httpfs to COPY spans to a Parquet file.
func (f *Flusher) writeSpansParquet(ctx context.Context, pk partitionKey, url string) error {
	query := fmt.Sprintf(`
		COPY (
			SELECT span_id, trace_id, parent_id, conversation_id, project_id, service_name, name, kind,
			       start_time, end_time, model, input, output,
			       input_tokens, output_tokens, cost_usd,
			       prompt_name, prompt_version,
			       status_code, status_message, attributes
			FROM spans
			WHERE project_id = '%s' AND DATE(start_time) = '%s'
		) TO '%s' (FORMAT PARQUET)
	`, pk.projectID, pk.date, url)

	if _, err := f.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("COPY spans to %s: %w", url, err)
	}
	return nil
}

// writeScoresParquet uses DuckDB's httpfs to COPY scores to a Parquet file.
func (f *Flusher) writeScoresParquet(ctx context.Context, pk partitionKey, url string) error {
	query := fmt.Sprintf(`
		COPY (
			SELECT score_id, span_id, trace_id, project_id, eval_name, value,
			       reasoning, judge_model, prompt_name, prompt_version, created_at
			FROM scores
			WHERE project_id = '%s' AND DATE(created_at) = '%s'
		) TO '%s' (FORMAT PARQUET)
	`, pk.projectID, pk.date, url)

	if _, err := f.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("COPY scores to %s: %w", url, err)
	}
	return nil
}

// s3URL builds an S3 URL for the given object key.
func (f *Flusher) s3URL(key string) string {
	bucket := f.cfg.Storage.Bucket
	if bucket == "" {
		bucket = defaultBucket
	}
	return fmt.Sprintf("s3://%s/%s", bucket, key)
}

// deleteFlushedRows removes spans and scores for the flushed partition from DuckDB.
// This is only called after both Parquet files are confirmed on S3.
func (f *Flusher) deleteFlushedRows(ctx context.Context, pk partitionKey) error {
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete spans.
	_, err = tx.ExecContext(ctx, `
		DELETE FROM spans
		WHERE project_id = ? AND DATE(start_time) = ?
	`, pk.projectID, pk.date)
	if err != nil {
		return fmt.Errorf("delete spans: %w", err)
	}

	// Delete scores for the same partition.
	_, err = tx.ExecContext(ctx, `
		DELETE FROM scores
		WHERE project_id = ? AND DATE(created_at) = ?
	`, pk.projectID, pk.date)
	if err != nil {
		return fmt.Errorf("delete scores: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// partitionPath builds the Hive-partitioned S3 key for a partition.
// When filename is empty, returns the directory path instead.
// Pattern: archive/project_id={id}/date={date}/{type}/{filename}
func partitionPath(pk partitionKey, typ, filename string) string {
	parts := []string{"archive",
		fmt.Sprintf("project_id=%s", pk.projectID),
		fmt.Sprintf("date=%s", pk.date),
		typ,
	}
	if filename != "" {
		parts = append(parts, filename)
	}
	return strings.Join(parts, "/")
}

// parseDuckDBDate normalizes a DuckDB DATE value to YYYY-MM-DD format.
// DuckDB's DATE type may return as "2025-01-15" or "2025-01-15T00:00:00Z".
func parseDuckDBDate(s string) (string, error) {
	if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
		return t.Format("2006-01-02"), nil
	}
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		return s, nil
	}
	return s, nil
}
