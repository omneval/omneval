// Package lake implements the Lake: the single authoritative span/score
// store from ADR-0004. DuckLake tables (spans, scores) stored as Parquet,
// partitioned by project_id and the span's start_time date, with the
// Catalog in Postgres (prod) or a local single-writer file (demo).
package lake

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
	_ "github.com/omneval/omneval/internal/duckdbfix"
)

// CatalogDriverPostgres uses the shared Postgres instance as the Catalog —
// the serialization point that makes multiple concurrent writers safe.
const CatalogDriverPostgres = "postgres"

// CatalogDriverLocal uses a local DuckDB-file Catalog (single-writer,
// demo profile only).
const CatalogDriverLocal = "duckdb"

// Config describes how to attach the Lake.
type Config struct {
	// CatalogDriver is "postgres" or "duckdb" (local single-writer catalog).
	CatalogDriver string
	// CatalogDSN is the Postgres DSN for the Catalog, or the local catalog
	// file path when CatalogDriver is "duckdb".
	CatalogDSN string
	// DataPath is where the Lake's Parquet files live: an s3://bucket/prefix
	// URL or a local directory.
	DataPath string
	// Storage supplies S3 credentials when DataPath is an s3:// URL.
	Storage *config.StorageConfig
	// ReadOnly attaches the Lake read-only (Query API).
	ReadOnly bool
}

// ConfigFromApp derives the Lake connection settings from the application
// config: explicit lake.* settings win; otherwise the Catalog follows the
// metadata-store database and the data path follows the storage bucket.
func ConfigFromApp(cfg *config.Config) Config {
	lc := Config{
		CatalogDriver: cfg.Lake.CatalogDriver,
		CatalogDSN:    cfg.Lake.CatalogDSN,
		DataPath:      cfg.Lake.DataPath,
	}
	if lc.CatalogDriver == "" {
		if cfg.Database.Driver == "postgres" {
			lc.CatalogDriver = CatalogDriverPostgres
		} else {
			lc.CatalogDriver = CatalogDriverLocal
		}
	}
	if lc.CatalogDSN == "" {
		if lc.CatalogDriver == CatalogDriverPostgres {
			lc.CatalogDSN = cfg.Database.DSN
		} else {
			lc.CatalogDSN = "lake/catalog.ducklake"
		}
	}
	if lc.DataPath == "" {
		if cfg.Storage.Bucket != "" {
			lc.DataPath = "s3://" + cfg.Storage.Bucket + "/lake"
		} else {
			lc.DataPath = "lake/data"
		}
	}
	if strings.HasPrefix(lc.DataPath, "s3://") {
		lc.Storage = &cfg.Storage
	}
	return lc
}

// Lake is an attached DuckLake catalog ready for reads and writes.
// The underlying *sql.DB is an in-memory DuckDB instance with the Lake
// attached as catalog "lake"; the attachment is shared by every pooled
// connection of the instance.
type Lake struct {
	db       *sql.DB
	readOnly bool
}

// Open attaches the Lake described by cfg and (unless read-only) creates
// the partitioned spans and scores tables idempotently.
func Open(ctx context.Context, cfg Config) (*Lake, error) {
	if cfg.CatalogDSN == "" {
		return nil, fmt.Errorf("lake: catalog DSN is required")
	}
	if cfg.DataPath == "" {
		return nil, fmt.Errorf("lake: data path is required")
	}

	if cfg.CatalogDriver != CatalogDriverPostgres && !cfg.ReadOnly {
		if err := ensureLocalCatalogDir(cfg.CatalogDSN); err != nil {
			return nil, fmt.Errorf("lake: create catalog dir: %w", err)
		}
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("lake: open duckdb: %w", err)
	}

	l := &Lake{db: db, readOnly: cfg.ReadOnly}
	if err := l.attach(ctx, cfg); err != nil {
		db.Close()
		return nil, err
	}
	if !cfg.ReadOnly {
		if err := l.ensureTables(ctx); err != nil {
			db.Close()
			return nil, err
		}
	}
	return l, nil
}

// attach loads the required extensions, registers S3 credentials, and
// attaches the DuckLake catalog as "lake". Extension loads, secrets, and
// attachments are DuckDB-instance level, so one setup pass covers every
// pooled connection.
func (l *Lake) attach(ctx context.Context, cfg Config) error {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("lake: conn: %w", err)
	}
	defer conn.Close()

	steps := []string{"INSTALL ducklake", "LOAD ducklake"}
	if cfg.CatalogDriver == CatalogDriverPostgres {
		steps = append(steps, "INSTALL postgres", "LOAD postgres")
	}
	if strings.HasPrefix(cfg.DataPath, "s3://") {
		steps = append(steps, "INSTALL httpfs", "LOAD httpfs")
		if cfg.Storage != nil {
			steps = append(steps, s3SecretSQL(cfg.Storage))
		}
	}
	steps = append(steps, attachSQL(cfg))

	for _, stmt := range steps {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("lake: %s: %w", firstWords(stmt, 3), err)
		}
	}
	return nil
}

// attachSQL builds the ATTACH statement for the configured Catalog.
func attachSQL(cfg Config) string {
	var target string
	switch cfg.CatalogDriver {
	case CatalogDriverPostgres:
		target = "ducklake:postgres:" + cfg.CatalogDSN
	default:
		target = "ducklake:" + cfg.CatalogDSN
	}
	options := []string{fmt.Sprintf("DATA_PATH %s", sqlQuote(cfg.DataPath))}
	if cfg.ReadOnly {
		options = append(options, "READ_ONLY")
	}
	return fmt.Sprintf("ATTACH IF NOT EXISTS %s AS lake (%s)",
		sqlQuote(target), strings.Join(options, ", "))
}

// s3SecretSQL builds the CREATE SECRET statement granting DuckDB access to
// the S3-compatible store holding the Lake's data path.
func s3SecretSQL(sc *config.StorageConfig) string {
	region := sc.Region
	if region == "" {
		region = "us-east-1"
	}
	fields := []string{
		"TYPE s3",
		"KEY_ID " + sqlQuote(sc.AccessKey),
		"SECRET " + sqlQuote(sc.SecretKey),
		"REGION " + sqlQuote(region),
	}
	if sc.Endpoint != "" {
		endpoint := strings.TrimPrefix(sc.Endpoint, "http://")
		endpoint = strings.TrimPrefix(endpoint, "https://")
		fields = append(fields,
			"ENDPOINT "+sqlQuote(endpoint),
			"URL_STYLE 'path'",
		)
		if strings.HasPrefix(sc.Endpoint, "http://") {
			fields = append(fields, "USE_SSL false")
		}
	}
	return fmt.Sprintf("CREATE OR REPLACE SECRET lake_s3 (%s)", strings.Join(fields, ", "))
}

// ensureTables creates the Lake's spans and scores tables and their
// partitioning idempotently. DuckLake enforces no primary keys (dedupe is
// the Batch Ledger's job, ADR-0004); the scores table carries the
// annotated span's start_time so a score partitions next to its span
// (ADR-0002).
func (l *Lake) ensureTables(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS lake.spans (
			span_id           VARCHAR      NOT NULL,
			trace_id          VARCHAR      NOT NULL,
			parent_id         VARCHAR,
			conversation_id   VARCHAR,
			project_id        VARCHAR      NOT NULL,
			service_name      VARCHAR,
			name              VARCHAR,
			kind              VARCHAR,
			start_time        TIMESTAMPTZ  NOT NULL,
			end_time          TIMESTAMPTZ,
			model             VARCHAR,
			input             VARCHAR,
			output            VARCHAR,
			input_tokens      BIGINT,
			output_tokens     BIGINT,
			cost_usd          DOUBLE,
			prompt_name       VARCHAR,
			prompt_version    BIGINT,
			status_code       VARCHAR,
			status_message    VARCHAR,
			attributes        VARCHAR
		)`,
		`ALTER TABLE lake.spans SET PARTITIONED BY (project_id, year(start_time), month(start_time), day(start_time))`,
		`CREATE TABLE IF NOT EXISTS lake.scores (
			score_id        VARCHAR      NOT NULL,
			span_id         VARCHAR      NOT NULL,
			trace_id        VARCHAR      NOT NULL,
			project_id      VARCHAR      NOT NULL,
			eval_name       VARCHAR,
			value           DOUBLE,
			reasoning       VARCHAR,
			judge_model     VARCHAR,
			prompt_name     VARCHAR,
			prompt_version  BIGINT,
			created_at      TIMESTAMPTZ  NOT NULL,
			span_start_time TIMESTAMPTZ  NOT NULL
		)`,
		`ALTER TABLE lake.scores SET PARTITIONED BY (project_id, year(span_start_time), month(span_start_time), day(span_start_time))`,
	}
	for _, stmt := range stmts {
		if _, err := l.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("lake: %s: %w", firstWords(stmt, 6), err)
		}
	}
	return nil
}

// DB exposes the underlying DuckDB handle with the Lake attached as
// catalog "lake".
func (l *Lake) DB() *sql.DB { return l.db }

// Close releases the DuckDB instance.
func (l *Lake) Close() error { return l.db.Close() }

const insertSpansSQL = `
	INSERT INTO lake.spans (
		span_id, trace_id, parent_id, conversation_id, project_id, service_name,
		name, kind, start_time, end_time,
		model, input, output, input_tokens, output_tokens, cost_usd,
		prompt_name, prompt_version,
		status_code, status_message, attributes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

// InsertSpans commits a batch of spans to the Lake in one transaction
// (one DuckLake snapshot). Spans are written as-is: cost must already be
// computed (cost is computed at write time, never at query time).
func (l *Lake) InsertSpans(ctx context.Context, spans []*domain.Span) error {
	if len(spans) == 0 {
		return nil
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("lake: begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, insertSpansSQL)
	if err != nil {
		return fmt.Errorf("lake: prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, span := range spans {
		startTime := span.StartTime
		if startTime.IsZero() {
			startTime = now
		}
		endTime := span.EndTime
		if endTime.IsZero() {
			endTime = now
		}

		// Coerce empty strings to NULL, mirroring the legacy writer, so
		// downstream JSON parsing treats absent payloads uniformly.
		var inputVal, outputVal any
		if span.Input != "" {
			inputVal = span.Input
		}
		if span.Output != "" {
			outputVal = span.Output
		}

		if _, err := stmt.ExecContext(ctx,
			span.SpanID,
			span.TraceID,
			span.ParentID,
			span.ConversationID,
			span.ProjectID,
			span.ServiceName,
			span.Name,
			string(span.Kind),
			startTime,
			endTime,
			span.Model,
			inputVal,
			outputVal,
			span.InputTokens,
			span.OutputTokens,
			span.CostUSD,
			span.PromptName,
			span.PromptVersion,
			span.StatusCode,
			span.StatusMessage,
			attributesJSON(span.Attributes),
		); err != nil {
			return fmt.Errorf("lake: insert span %s: %w", span.SpanID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("lake: commit: %w", err)
	}
	return nil
}

const insertScoresSQL = `
	INSERT INTO lake.scores (
		score_id, span_id, trace_id, project_id,
		eval_name, value, reasoning, judge_model,
		prompt_name, prompt_version, created_at, span_start_time
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

// InsertScores commits scores to the Lake. A score partitions by the
// annotated span's start_time (ADR-0002); when SpanStartTime is unknown
// the score's CreatedAt is the fallback so the row still lands in a
// recent partition.
func (l *Lake) InsertScores(ctx context.Context, scores []*domain.Score) error {
	if len(scores) == 0 {
		return nil
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("lake: begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, insertScoresSQL)
	if err != nil {
		return fmt.Errorf("lake: prepare: %w", err)
	}
	defer stmt.Close()

	for _, score := range scores {
		spanStart := score.SpanStartTime
		if spanStart.IsZero() {
			spanStart = score.CreatedAt
		}
		if _, err := stmt.ExecContext(ctx,
			score.ScoreID,
			score.SpanID,
			score.TraceID,
			score.ProjectID,
			score.EvalName,
			score.Value,
			score.Reasoning,
			score.JudgeModel,
			score.PromptName,
			score.PromptVersion,
			score.CreatedAt,
			spanStart,
		); err != nil {
			return fmt.Errorf("lake: insert score %s: %w", score.ScoreID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("lake: commit: %w", err)
	}
	return nil
}

// FlushInlinedData forces any rows DuckLake 1.5 has inlined into the
// Catalog (rather than written to Parquet immediately, for small inserts —
// see InsertSpans/InsertScores) out to physical Parquet files. lake.spans
// and lake.scores read inlined rows transparently, so query correctness
// does not depend on this; it exists for callers that inspect the physical
// data layout directly (partition-layout tests, Table Maintenance, #91
// reclaim).
func (l *Lake) FlushInlinedData(ctx context.Context) error {
	if _, err := l.db.ExecContext(ctx, "CALL ducklake_flush_inlined_data('lake')"); err != nil {
		return fmt.Errorf("lake: flush inlined data: %w", err)
	}
	return nil
}

// DeleteProject permanently deletes all of a project's spans and scores
// from the Lake (admin/compliance deletion, ADR-0004 / #91). Unlike the
// legacy snapshot path, this commits through the Catalog: the rows
// disappear from every reader's next query, with no resurrection on the
// next snapshot cycle. It then runs DuckLake's snapshot expiry and
// orphan/old-file cleanup so the deleted rows' physical Parquet files are
// reclaimed immediately rather than waiting for the next scheduled Table
// Maintenance pass (#89).
func (l *Lake) DeleteProject(ctx context.Context, projectID string) error {
	if l.readOnly {
		return fmt.Errorf("lake: cannot delete from a read-only attachment")
	}
	if _, err := l.db.ExecContext(ctx, "DELETE FROM lake.spans WHERE project_id = ?", projectID); err != nil {
		return fmt.Errorf("lake: delete spans for project %s: %w", projectID, err)
	}
	if _, err := l.db.ExecContext(ctx, "DELETE FROM lake.scores WHERE project_id = ?", projectID); err != nil {
		return fmt.Errorf("lake: delete scores for project %s: %w", projectID, err)
	}
	return l.reclaim(ctx)
}

// reclaim expires snapshots that are no longer the latest and deletes the
// Parquet files that backed them, so deleted rows stop occupying space
// immediately instead of lingering until the next leader-run Table
// Maintenance pass.
//
// DuckLake 1.5 records DELETE as an inlined delete vector against the
// existing Parquet files rather than rewriting them immediately, so in
// principle a preceding ducklake_rewrite_data_files pass is needed for
// ducklake_delete_orphaned_files/ducklake_cleanup_old_files to find a
// deleted project's data files: without a rewrite, those files are still
// "live" as far as the Catalog is concerned, just filtered by the delete
// vector at read time (query correctness is unaffected; lake.spans/scores
// already exclude the deleted rows).
//
// KNOWN LIMITATION (#111): against THIS package's direct local-file/Postgres
// DuckLake catalog attachment, ducklake_rewrite_data_files still cannot be
// sequenced around a preceding DELETE — confirmed again under the
// duckdb-go/v2 v2.10503.1 (DuckDB 1.5.3) driver, same combinations as
// before all hit "Not implemented Error: Scanning a DuckLake table after the
// transaction has ended":
//   - same *sql.DB, separate implicit transactions (delete autocommits,
//     then rewrite): fails.
//   - same *sql.DB, explicit transaction wrapping both DELETE and rewrite:
//     does not error on the rewrite call itself in some configurations, but
//     does not reliably see the uncommitted delete vector either.
//   - a brand-new *sql.DB with a fresh ATTACH to the same catalog/data path,
//     after the DELETE's transaction committed: fails with the same error.
//
// HOWEVER — the corrected quack:// spike (internal/lake/quack_spike,
// rewritten for #111) found that DuckLake's "quack:" CATALOG driver changes
// this picture entirely. With the catalog attached as
// `ATTACH 'ducklake:quack:localhost:<port>' AS lake (DATA_PATH '<dir>')`
// (Quack as the metadata/catalog backend, not a generic table proxy):
//   - the ATTACH itself succeeds (auth via a client-side
//     `CREATE SECRET quack_auth (TYPE quack, TOKEN '<server-issued-token>')`,
//     where the token comes from quack_serve()'s own result row — NOT from
//     any token passed to quack_serve at start time).
//   - a second, independently-attached client sees the first client's
//     committed inserts (shared catalog state works as expected).
//   - DELETE followed by ducklake_rewrite_data_files on the SAME connection
//     that issued the DELETE — as separate autocommit statements, exactly
//     reclaim()'s pattern — now SUCCEEDS and physically rewrites the Parquet
//     file without the deleted rows. The same sequence also succeeds inside
//     an explicit transaction that commits both together.
//   - the restriction is NOT fully lifted: a brand-new client/connection
//     attached fresh AFTER another client's DELETE already committed still
//     hits the same "Scanning a DuckLake table after the transaction has
//     ended" error when calling ducklake_rewrite_data_files. So the fix is
//     scoped to "rewrite on the same session that performed the delete",
//     which is exactly this function's existing call pattern
//     (DeleteProject -> reclaim on the same l.db).
//
// So: a deleted project's physical Parquet files are NOT immediately
// reclaimed by this function TODAY, because this package attaches the
// catalog directly (ducklake:<path> / ducklake:postgres:<dsn>), not via
// quack:. Switching the Lake's catalog attachment to
// `ducklake:quack:<host>:<port>` would unblock reclaim()'s
// ducklake_rewrite_data_files call (same-session DELETE+rewrite is exactly
// this code's shape) — that is the recommended direction for #105's Quack
// Server design: the Quack Server holds the long-lived catalog session and
// every writer attaches via ducklake:quack:<quack-server-addr>. Until #105
// stands up a real Quack Server, this package keeps the direct catalog
// attach and deleted rows remain filtered out at query time by the delete
// vector, compacted away by a future Table Maintenance pass.
// ducklake_expire_snapshots/ducklake_delete_orphaned_files/
// ducklake_cleanup_old_files below remain useful for snapshot/orphan
// cleanup that doesn't depend on rewriting live data files.
func (l *Lake) reclaim(ctx context.Context) error {
	stmts := []string{
		"CALL ducklake_expire_snapshots('lake', older_than => now())",
		"CALL ducklake_delete_orphaned_files('lake', cleanup_all => true)",
		"CALL ducklake_cleanup_old_files('lake', cleanup_all => true)",
	}
	for _, stmt := range stmts {
		if _, err := l.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("lake: %s: %w", firstWords(stmt, 2), err)
		}
	}
	return nil
}

func attributesJSON(attrs map[string]any) string {
	if len(attrs) == 0 {
		return "null"
	}
	data, err := json.Marshal(attrs)
	if err != nil {
		return "null"
	}
	return string(data)
}

// sqlQuote single-quotes a SQL string literal, doubling embedded quotes.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// firstWords returns the first n whitespace-separated words of s for
// error context without dumping whole statements.
func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.Join(fields, " ")
}

// ensureLocalCatalogDir creates the parent directory for a local catalog
// path so first-run demo profiles work without manual setup.
func ensureLocalCatalogDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}
