// Package lake is the Quack Client (ADR-0005): the shared library that
// Writer, Query API, and the backfill tool use to reach the Lake — the
// single authoritative span/score store from ADR-0004. DuckLake tables
// (spans, scores) are stored as Parquet, partitioned by project_id and the
// span's start_time date.
//
// Per ADR-0005, this package never holds a direct Catalog connection: it
// attaches via `ATTACH 'ducklake:quack:<host>:<port>' AS lake (DATA_PATH
// ...)`, where the Quack Server (services/quack, internal/lake/lakeserver)
// is the sole holder of the direct Postgres/local-file Catalog connection.
// DATA_PATH is still read directly by this client — Quack carries only
// catalog metadata, not span/score data.
package lake

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
	_ "github.com/omneval/omneval/internal/duckdbfix"
)

// Config describes how to attach the Lake as a Quack client.
type Config struct {
	// QuackAddr is the Quack Server's host:port (no scheme), e.g.
	// "quack-server.omneval:9494".
	QuackAddr string
	// QuackToken authenticates this client via the META_TOKEN DuckLake
	// ATTACH option (see attachSQL). Must match the Quack Server's
	// configured token.
	QuackToken string
	// DataPath is where the Lake's Parquet files live: an s3://bucket/prefix
	// URL or a local directory. Read directly by this client for DATA_PATH
	// in the ATTACH statement.
	DataPath string
	// Storage supplies S3 credentials when DataPath is an s3:// URL.
	Storage *config.StorageConfig
	// ReadOnly attaches the Lake read-only (Query API).
	ReadOnly bool
}

// ConfigFromApp derives the Quack client connection settings from the
// application config: quack.client.* settings supply the Quack Server
// address/token, and the data path follows quack.client.data_path or the
// storage bucket.
func ConfigFromApp(cfg *config.Config) Config {
	lc := Config{
		QuackAddr:  strings.TrimPrefix(cfg.Quack.Client.URL, "quack://"),
		QuackToken: cfg.Quack.Client.Token,
		DataPath:   cfg.Quack.Client.DataPath,
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

// ensureTablesMu serializes DDL across all Lake instances so that
// concurrent Open() calls do not race on ALTER TABLE (which DuckLake
// rejects as a transaction conflict when two sessions modify table metadata).
var ensureTablesMu sync.Mutex

// Lake is an attached DuckLake catalog ready for reads and writes.
// The underlying *sql.DB is an in-memory DuckDB instance with the Lake
// attached as catalog "lake"; the attachment is shared by every pooled
// connection of the instance.
type Lake struct {
	mu       sync.Mutex
	db       *sql.DB
	readOnly bool
	cfg      Config // retained for reconnection after Quack Server restarts
}

// Open attaches the Lake described by cfg via the Quack Server and (unless
// read-only) creates the partitioned spans and scores tables idempotently.
func Open(ctx context.Context, cfg Config) (*Lake, error) {
	if cfg.QuackAddr == "" {
		return nil, fmt.Errorf("lake: quack server address is required")
	}
	if cfg.DataPath == "" {
		return nil, fmt.Errorf("lake: data path is required")
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("lake: open duckdb: %w", err)
	}

	// DuckLake's quack: catalog driver ties a DuckLake transaction/snapshot
	// to the DuckDB connection that started it (#111/#105 spike). database/sql
	// pools connections across calls on *sql.DB, so a DELETE and the
	// following ducklake_rewrite_data_files (reclaim) can land on different
	// pooled connections and hit "Scanning a DuckLake table after the
	// transaction has ended". Pinning the pool to a single connection makes
	// every statement on this *Lake run on the same DuckDB session, matching
	// the spike's verified same-session DELETE+rewrite pattern.
	db.SetMaxOpenConns(1)

	l := &Lake{db: db, readOnly: cfg.ReadOnly, cfg: cfg}

	// Retry attach with backoff. When a Quack Server backs its DuckLake
	// catalog by a local DuckDB file, concurrent Quack-client attachments
	// can race on the catalog's initialization protocol (e.g. "table already
	// exists" or "no snapshot found"). Retrying with a short backoff lets the
	// first writers finish their catalog setup so subsequent clients can
	// attach against the now-consistent catalog.
	if err := l.attachWithRetry(ctx, cfg); err != nil {
		db.Close()
		return nil, err
	}
	if !cfg.ReadOnly {
		if err := l.ensureTablesWithRetry(ctx); err != nil {
			db.Close()
			return nil, err
		}
		// DuckLake persists this as catalog metadata, so it only needs to be
		// set once ever for the catalog's lifetime — unlike ensureTables'
		// CREATE/ALTER statements, it must NOT be repeated on every
		// reconnect() (see reconnect's ensureTables call): reconnect runs
		// under a tight reconnectTimeout budget, and this catalog-wide
		// config write doesn't need to compete for that budget on every
		// stale-connection recovery.
		if err := l.configureCompression(ctx); err != nil {
			db.Close()
			return nil, err
		}
	}
	return l, nil
}

// configureCompression sets the Lake's Parquet write compression to zstd
// (DuckLake defaults to snappy). The heaviest columns (spans' input/output)
// hold LLM prompt/completion text, which zstd compresses meaningfully
// better than snappy — cost is pre-computed at write time and never
// recomputed at query time, so there's no repeated-decode penalty to worry
// about.
func (l *Lake) configureCompression(ctx context.Context) error {
	if _, err := l.db.ExecContext(ctx, "CALL ducklake_set_option('lake', 'parquet_compression', 'zstd')"); err != nil {
		return fmt.Errorf("lake: configure compression: %w", err)
	}
	return nil
}

// attach loads the required extensions, registers the Quack auth secret
// and S3 credentials, and attaches the DuckLake catalog as "lake" via the
// Quack Server. Extension loads, secrets, and attachments are
// DuckDB-instance level, so one setup pass covers every pooled connection.
func (l *Lake) attach(ctx context.Context, cfg Config) error {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("lake: conn: %w", err)
	}
	defer conn.Close()

	steps := []string{
		"INSTALL ducklake", "LOAD ducklake",
		"INSTALL quack", "LOAD quack",
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

// isAttachRace reports whether err indicates a transient DuckLake catalog
// initialization race: two clients simultaneously running the ATTACH
// protocol against a file-backed catalog. These errors are recoverable —
// retrying after a short backoff usually succeeds because the first writer
// finishes its catalog setup and the second client attaches against a
// consistent catalog.
func isAttachRace(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "No snapshot found in DuckLake") ||
		strings.Contains(msg, "write-write conflict") ||
		strings.Contains(msg, "GetDBPath not implemented yet") ||
		strings.Contains(msg, "Transaction conflict")
}

// isTxConflict reports whether err indicates a transient DuckLake transaction
// conflict: two concurrent transactions attempted to modify the same table or
// row. DuckLake's inlined-insert/inlined-delete semantics use optimistic
// concurrency control, so conflicting commits fail and must be retried.
func isTxConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Transaction conflict")
}

// attachWithRetry calls attach, retrying on transient catalog-race errors
// with exponential backoff (up to 3 retries).
func (l *Lake) attachWithRetry(ctx context.Context, cfg Config) error {
	var lastErr error
	for attempt := 0; attempt <= 3; attempt++ {
		if err := l.attach(ctx, cfg); err != nil {
			if !isAttachRace(err) {
				return err
			}
			lastErr = err
			if attempt < 3 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("lake: attach after 4 attempts (last: %w)", lastErr)
}

// isStaleConn reports whether err is a Quack-connectivity error that indicates
// a stale catalog attachment — the Quack Server restarted, went down, or the
// DuckDB operation was interrupted. These are all transient: reconnecting
// the Lake to a fresh DuckDB instance re-establishes the attachment.
func isStaleConn(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Invalid connection id: the classic DuckLake stale-attachment signal.
	if strings.Contains(msg, "Invalid connection id") {
		return true
	}
	// Could not connect to server: Quack is completely down during restart.
	if strings.Contains(msg, "Could not connect to server") {
		return true
	}
	// Authentication failed: token mismatch on Quack restart.
	if strings.Contains(msg, "Authentication failed") {
		return true
	}
	// Context canceled: interrupted DuckDB operations (LOAD httpfs, ATTACH, etc.).
	if strings.Contains(msg, "context canceled") {
		return true
	}
	// ATTACH failures: reattaching the DuckLake catalog.
	if strings.Contains(msg, "ATTACH") {
		return true
	}
	// INTERRUPT: DuckDB operation was aborted during Quack downtime.
	if strings.Contains(msg, "INTERRUPT") {
		return true
	}
	return false
}

// reconnectTimeout bounds the fresh context reconnect uses for ATTACH and
// table setup. Deliberately not derived from the caller's ctx: callers
// detect a stale connection via a context that may already be at or past
// its deadline (e.g. the readiness probe's 3s budget), and reusing that
// context here would make the reconnect attempt fail immediately with the
// same "context canceled"/"context deadline exceeded" error that triggered
// it in the first place.
const reconnectTimeout = 10 * time.Second

// reconnect closes the current DuckDB in-memory instance, opens a fresh one,
// and re-establishes the Quack Server attachment. Callers must hold l.mu.
func (l *Lake) reconnect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reconnectTimeout)
	defer cancel()

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return fmt.Errorf("lake: reconnect open: %w", err)
	}
	db.SetMaxOpenConns(1)
	tmp := &Lake{db: db, readOnly: l.readOnly}
	if err := tmp.attach(ctx, l.cfg); err != nil {
		db.Close()
		return err
	}
	if !l.readOnly {
		if err := tmp.ensureTables(ctx); err != nil {
			db.Close()
			return err
		}
	}
	l.db.Close()
	l.db = db
	slog.Info("lake: reconnected to quack server", "addr", l.cfg.QuackAddr)
	return nil
}

// attachSQL builds the ATTACH statement for the Lake via the Quack Server's
// `ducklake:quack:<host>:<port>` catalog driver (ADR-0005). DATA_PATH is
// still read directly by this client; only catalog metadata flows through
// Quack.
//
// DuckLake's ATTACH option parser rejects unknown options outright
// ("Unsupported option ... for DuckLake"), but forwards any option prefixed
// with "meta_" to the underlying metadata-catalog attach (quack_storage.cpp)
// after stripping the prefix. We use that to pass TOKEN and DISABLE_SSL
// through to the quack catalog attach:
//   - META_TOKEN authenticates this client (quack_storage.cpp reads
//     attach_options.options["token"]); CREATE SECRET TYPE quack does not
//     support DISABLE_SSL, so a secret-based approach can't combine both.
//   - META_DISABLE_SSL is always set to true. The quack extension defaults
//     to HTTPS for any host other than localhost/127.0.0.1/::1
//     (https://duckdb.org/docs/current/quack/overview), but the Quack Server
//     (services/quack) never terminates TLS — it's plain HTTP, with TLS
//     expected to be handled by a reverse proxy if ever exposed. Forcing
//     META_DISABLE_SSL true avoids HTTPS attempts against in-cluster Service
//     DNS names like "omneval-quack-server:9494"; it's a no-op for
//     localhost addresses, which already default to no SSL.
func attachSQL(cfg Config) string {
	target := "ducklake:quack:" + cfg.QuackAddr
	options := []string{fmt.Sprintf("DATA_PATH %s", sqlQuote(cfg.DataPath))}
	if cfg.QuackToken != "" {
		options = append(options, fmt.Sprintf("META_TOKEN %s", sqlQuote(cfg.QuackToken)))
	}
	options = append(options, "META_DISABLE_SSL true")
	if cfg.ReadOnly {
		options = append(options, "READ_ONLY")
	}
	return fmt.Sprintf("ATTACH IF NOT EXISTS %s AS lake (%s)",
		sqlQuote(target), strings.Join(options, ", "))
}

// r2EndpointPattern matches a Cloudflare R2 S3-compatible endpoint host,
// e.g. "dee226c52e8c33561dacbbc793a9d207.r2.cloudflarestorage.com", capturing
// the account ID.
var r2EndpointPattern = regexp.MustCompile(`^([0-9a-f]{32})\.r2\.cloudflarestorage\.com$`)

// s3SecretSQL builds the CREATE SECRET statement granting DuckDB access to
// the S3-compatible store holding the Lake's data path.
//
// Cloudflare R2 endpoints get DuckDB's purpose-built "TYPE r2" secret
// (ACCOUNT_ID only, no ENDPOINT/URL_STYLE) instead of the generic "TYPE s3"
// + ENDPOINT/URL_STYLE combination used for MinIO/AWS. This sidesteps a
// documented DuckLake bug (duckdb/ducklake#562, see
// lakeserver/maintenance.go) where some of DuckLake's internal S3 calls
// ignore a generic secret's URL_STYLE/ENDPOINT and fall back to AWS
// virtual-hosted-style requests, which fail against non-AWS endpoints —
// among other symptoms, this silently prevents ducklake_cleanup_old_files
// from physically deleting superseded Parquet files, so compaction output
// piles up unbounded in the bucket.
func s3SecretSQL(sc *config.StorageConfig) string {
	if sc.Endpoint != "" {
		endpoint := strings.TrimPrefix(sc.Endpoint, "http://")
		endpoint = strings.TrimPrefix(endpoint, "https://")
		if m := r2EndpointPattern.FindStringSubmatch(endpoint); m != nil {
			fields := []string{
				"TYPE r2",
				"KEY_ID " + sqlQuote(sc.AccessKey),
				"SECRET " + sqlQuote(sc.SecretKey),
				"ACCOUNT_ID " + sqlQuote(m[1]),
			}
			return fmt.Sprintf("CREATE OR REPLACE SECRET lake_s3 (%s)", strings.Join(fields, ", "))
		}
	}

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

// ensureTablesWithRetry calls ensureTables, retrying on transient catalog-race
// errors (e.g. "No snapshot found in DuckLake", "Transaction conflict" for
// ALTER TABLE DDL) that happen when a client reaches ensureTables before the
// catalog has finished initializing from a concurrent ATTACH. The call is
// serialized across all Lake instances via ensureTablesMu so that concurrent
// Open() callers never race on ALTER TABLE at the DuckLake layer.
func (l *Lake) ensureTablesWithRetry(ctx context.Context) error {
	ensureTablesMu.Lock()
	defer ensureTablesMu.Unlock()
	var lastErr error
	for attempt := 0; attempt <= 8; attempt++ {
		if err := l.ensureTables(ctx); err != nil {
			if !isAttachRace(err) {
				return err
			}
			lastErr = err
			if attempt < 8 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("lake: ensureTables after 9 attempts (last: %w)", lastErr)
}

// DB exposes the underlying DuckDB handle with the Lake attached as
// catalog "lake".
func (l *Lake) DB() *sql.DB { return l.db }

// QueryContext executes a query that returns rows against the Lake. On a
// stale Quack Server connection ("Invalid connection id") the Lake
// reconnects and retries once. Satisfies handler.DBHandle.
func (l *Lake) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rows, err := l.db.QueryContext(ctx, query, args...)
	if isStaleConn(err) {
		if rerr := l.reconnect(ctx); rerr != nil {
			return nil, fmt.Errorf("lake: reconnect: %w", rerr)
		}
		rows, err = l.db.QueryContext(ctx, query, args...)
	}
	return rows, err
}

// Query executes a query that returns rows. Satisfies handler.DBHandle.
func (l *Lake) Query(query string, args ...any) (*sql.Rows, error) {
	return l.QueryContext(context.Background(), query, args...)
}

// ExecContext executes a query without returning rows. On a stale Quack
// Server connection the Lake reconnects and retries once. Satisfies
// handler.DBHandle.
func (l *Lake) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, err := l.db.ExecContext(ctx, query, args...)
	if isStaleConn(err) {
		if rerr := l.reconnect(ctx); rerr != nil {
			return nil, fmt.Errorf("lake: reconnect: %w", rerr)
		}
		res, err = l.db.ExecContext(ctx, query, args...)
	}
	return res, err
}

// Exec executes a query without returning rows. Satisfies handler.DBHandle.
func (l *Lake) Exec(query string, args ...any) (sql.Result, error) {
	return l.ExecContext(context.Background(), query, args...)
}

// QueryRowContext executes a query expected to return at most one row.
// Unlike QueryContext, transparent reconnection is not possible here because
// *sql.Row defers the error to Scan. The call is serialised under the Lake
// mutex so any reconnect by a concurrent Ping provides a fresh connection
// before this call proceeds. Satisfies handler.DBHandle.
func (l *Lake) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.db.QueryRowContext(ctx, query, args...)
}

// QueryRow executes a query expected to return at most one row. Satisfies
// handler.DBHandle.
func (l *Lake) QueryRow(query string, args ...any) *sql.Row {
	return l.QueryRowContext(context.Background(), query, args...)
}

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
// On a stale Quack Server connection ("Invalid connection id"), the lake
// reconnects and retries once.
func (l *Lake) InsertSpans(ctx context.Context, spans []*domain.Span) error {
	if len(spans) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.insertSpansLocked(ctx, spans); isStaleConn(err) {
		if rerr := l.reconnect(ctx); rerr != nil {
			return fmt.Errorf("lake: reconnect: %w", rerr)
		}
		return l.insertSpansLocked(ctx, spans)
	} else {
		return err
	}
}

func (l *Lake) insertSpansLocked(ctx context.Context, spans []*domain.Span) error {
	var lastErr error
	for attempt := 0; attempt <= 3; attempt++ {
		if err := l.doInsertSpans(ctx, spans); err != nil {
			if !isTxConflict(err) {
				return err
			}
			lastErr = err
			if attempt < 3 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("lake: insertSpans after 4 attempts (last: %w)", lastErr)
}

func (l *Lake) doInsertSpans(ctx context.Context, spans []*domain.Span) error {
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
// On a stale Quack Server connection ("Invalid connection id"), the lake
// reconnects and retries once.
func (l *Lake) InsertScores(ctx context.Context, scores []*domain.Score) error {
	if len(scores) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.insertScoresLocked(ctx, scores); isStaleConn(err) {
		if rerr := l.reconnect(ctx); rerr != nil {
			return fmt.Errorf("lake: reconnect: %w", rerr)
		}
		return l.insertScoresLocked(ctx, scores)
	} else {
		return err
	}
}

func (l *Lake) insertScoresLocked(ctx context.Context, scores []*domain.Score) error {
	var lastErr error
	for attempt := 0; attempt <= 3; attempt++ {
		if err := l.doInsertScores(ctx, scores); err != nil {
			if !isTxConflict(err) {
				return err
			}
			lastErr = err
			if attempt < 3 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("lake: insertScores after 4 attempts (last: %w)", lastErr)
}

func (l *Lake) doInsertScores(ctx context.Context, scores []*domain.Score) error {
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

// SpanStartTime looks up the start_time of the span a score annotates, so
// the score can be written to the correct lake.scores partition (ADR-0002).
// Returns the zero time and no error if the span is not found; callers
// should fall back to the score's CreatedAt in that case (InsertScores does
// this automatically when SpanStartTime is zero).
func (l *Lake) SpanStartTime(ctx context.Context, traceID, spanID string) (time.Time, error) {
	var startTime time.Time
	err := l.db.QueryRowContext(ctx,
		"SELECT start_time FROM lake.spans WHERE trace_id = ? AND span_id = ?",
		traceID, spanID,
	).Scan(&startTime)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("lake: span start time %s/%s: %w", traceID, spanID, err)
	}
	return startTime, nil
}

// Ping verifies the Lake's Catalog connection is reachable by executing
// a metadata-only scan against lake.spans. This forces a round-trip
// through the Quack Server catalog — pinging the in-memory DuckDB via
// "SELECT 1" would not detect a stale Quack connection. On a stale
// connection, Ping reconnects and retries once so the readiness probe
// self-heals after a Quack Server restart.
func (l *Lake) Ping(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var n int64
	err := l.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM lake.spans WHERE 1 = 0").Scan(&n)
	if isStaleConn(err) {
		if rerr := l.reconnect(ctx); rerr != nil {
			return fmt.Errorf("lake: reconnect: %w", rerr)
		}
		err = l.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM lake.spans WHERE 1 = 0").Scan(&n)
	}
	return err
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
// from the Lake (admin/compliance deletion, ADR-0004 / #91). This commits
// through the Catalog: the rows disappear from every reader's next query,
// with no resurrection on the next poll. It then runs reclaim — rewriting
// data files to drop the deleted rows' Parquet pages, expiring snapshots,
// and cleaning up orphaned/old files — so the deleted rows' physical
// storage is reclaimed immediately rather than waiting for the next
// scheduled Table Maintenance pass (services/quack).
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

// reclaim rewrites data files to physically drop deleted rows, then expires
// snapshots that are no longer the latest and deletes the Parquet files
// that backed them, so deleted rows stop occupying space immediately
// instead of lingering until the next scheduled Table Maintenance pass.
//
// DuckLake 1.5 records DELETE as an inlined delete vector against the
// existing Parquet files rather than rewriting them immediately. Against a
// direct local-file/Postgres Catalog attachment (pre-#105), a subsequent
// ducklake_rewrite_data_files call hit "Not implemented Error: Scanning a
// DuckLake table after the transaction has ended" in every combination
// tried (#111).
//
// The #111/#105 quack_spike (internal/lake/quack_spike) found that DuckLake's
// "quack:" CATALOG driver — `ATTACH 'ducklake:quack:<host>:<port>' AS lake
// (DATA_PATH '<dir>')`, which this package now uses exclusively (ADR-0005) —
// changes this picture: DELETE followed by ducklake_rewrite_data_files on
// the SAME connection that issued the DELETE, as separate autocommit
// statements (exactly this function's call pattern from DeleteProject),
// SUCCEEDS and physically rewrites the Parquet file without the deleted
// rows. The restriction is not fully lifted — a brand-new connection
// attached AFTER another client's DELETE already committed still hits the
// "Scanning a DuckLake table after the transaction has ended" error when
// calling ducklake_rewrite_data_files — but that case does not arise here:
// reclaim always runs on the same l.db/session that performed the DELETE.
func (l *Lake) reclaim(ctx context.Context) error {
	stmts := []string{
		"CALL ducklake_rewrite_data_files('lake', 'spans')",
		"CALL ducklake_rewrite_data_files('lake', 'scores')",
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
