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
	"github.com/omneval/omneval/internal/lakeclient"
)

// Verify compile-time: *lake.Lake implements lakeclient.Client and lakeclient.Querier.
var (
	_ lakeclient.Client = (*Lake)(nil)
	_ lakeclient.Querier = (*Lake)(nil)
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
	// MaxOpenConns bounds the underlying connection pool. Zero (the field's
	// default) falls back to defaultMaxOpenConns rather than 1: a
	// single-connection pool means every Lake call — including the
	// readiness/liveness Ping — serializes behind whatever query is
	// currently running, so one slow UI query can freeze health checks for
	// the entire pod even though the Quack Server itself is healthy.
	MaxOpenConns int
	// MemoryLimit bounds this client's own embedded DuckDB buffer manager
	// (e.g. "1536MiB"), applied via `SET memory_limit` after attach. Empty
	// means no pragma is set (DuckDB sizes against the host's total RAM
	// rather than the container's cgroup limit). Mirrors the Quack Server's
	// memory_limit fix: a larger MaxOpenConns means more concurrent
	// client-side Parquet scans can run in this process at once, so the
	// same OOM risk that motivated the server-side pragma applies here too.
	MemoryLimit string
	// Threads caps this client's embedded DuckDB intra-query parallelism,
	// applied via `SET threads` after attach. Zero means no pragma is set,
	// so DuckDB auto-detects against the host's total CPU count rather than
	// the container's cgroup CPU limit: under Kubernetes this lets a single
	// query fan out across far more threads than the container's CPU quota
	// allows, exhausting an entire CFS period's budget in one burst and
	// throttling the whole cgroup — including the Go runtime's health check
	// goroutine — for the rest of that period (production incident: query
	// pod's /healthz and /readyz timed out under normal Traces-list load
	// even after the container's CPU limit was raised, because DuckDB kept
	// detecting and using the host's full core count regardless).
	Threads int
}

// defaultMaxOpenConns is used when Config.MaxOpenConns is unset (zero).
// Chosen to give real headroom for concurrent UI queries and ingest writes
// within one pod without per-connection memory cost growing unbounded.
const defaultMaxOpenConns = 2

// maxOpenConnsOrDefault applies defaultMaxOpenConns when n is unset (zero or
// negative), so a Config built without ConfigFromApp (e.g. an older config
// file, or a test) still gets a usable pool instead of silently reverting to
// the single-connection wedge risk.
func maxOpenConnsOrDefault(n int) int {
	if n <= 0 {
		return defaultMaxOpenConns
	}
	return n
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
	lc.MaxOpenConns = cfg.Quack.Client.MaxOpenConns
	lc.MemoryLimit = cfg.Quack.Client.MemoryLimit
	lc.Threads = cfg.Quack.Client.Threads
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
	// mu is a RWMutex, not a plain Mutex: every normal operation
	// (Query/Exec/QueryRow/Ping/InsertSpans/InsertScores) takes the shared
	// RLock so concurrent calls run in parallel across the connection pool
	// instead of fully serializing within this process. Only reconnect()
	// — which swaps l.db itself — needs the exclusive Lock, and only for
	// the brief duration of re-attaching.
	mu       sync.RWMutex
	db       *sql.DB
	readOnly bool
	cfg      Config // retained for reconnection after Quack Server restarts

	// shutdownCh is closed by Shutdown to tell any in-flight or future
	// reconnect() call to give up immediately instead of running its full
	// reconnectTimeout budget. Without this, a process-level SIGTERM arriving
	// while several callers are queued behind reconnectExclusive's lock (e.g.
	// because the Quack Server is mid-compaction and refusing connections)
	// makes each queued reconnect attempt run its own ~10s before the next
	// one gets a turn — easily exceeding a Kubernetes pod's
	// terminationGracePeriodSeconds and turning a graceful exit into a
	// SIGKILL (production incident: query pod logged "shutting down..." and
	// then kept retrying "reconnected to quack server" until killed).
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}

// Shutdown tells the Lake that the process is exiting: any reconnect() call
// already running or started afterward aborts as soon as it next checks its
// context instead of running its full reconnectTimeout. Safe to call more
// than once. Does not close the underlying connection pool — callers should
// still call Close() once in-flight requests have drained.
func (l *Lake) Shutdown() {
	l.shutdownOnce.Do(func() { close(l.shutdownCh) })
}

// raceContext returns a context derived from parent that is also canceled
// the moment stopCh is closed, whichever happens first. Used so reconnect's
// detached, fixed-length timeout context can still be cut short by a
// process-shutdown signal without reintroducing the per-request short
// deadline that context.WithoutCancel(ctx) was added to avoid.
func raceContext(parent context.Context, stopCh <-chan struct{}) (context.Context, context.CancelFunc) {
	out, cancel := context.WithCancel(parent)
	if stopCh != nil {
		go func() {
			select {
			case <-out.Done():
			case <-stopCh:
				cancel()
			}
		}()
	}
	return out, cancel
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
	// to the DuckDB connection that started it (#111/#105 spike): a DELETE
	// and the following ducklake_rewrite_data_files (reclaim) must land on
	// the SAME connection or it hits "Scanning a DuckLake table after the
	// transaction has ended". DeleteProject/reclaim handle this by pinning
	// their statement sequence to one explicit *sql.Conn (see reclaim),
	// so the pool itself can run multiple connections — required so a
	// single slow query doesn't serialize every other call on this Lake,
	// including the readiness/liveness Ping (see MaxOpenConns doc).
	db.SetMaxOpenConns(maxOpenConnsOrDefault(cfg.MaxOpenConns))

	l := &Lake{db: db, readOnly: cfg.ReadOnly, cfg: cfg, shutdownCh: make(chan struct{})}

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
	if cfg.MemoryLimit != "" {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET memory_limit = %s", sqlQuote(cfg.MemoryLimit))); err != nil {
			db.Close()
			return nil, fmt.Errorf("lake: set memory_limit: %w", err)
		}
	}
	if cfg.Threads > 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET threads = %d", cfg.Threads)); err != nil {
			db.Close()
			return nil, fmt.Errorf("lake: set threads: %w", err)
		}
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
// and re-establishes the Quack Server attachment. Callers must hold l.mu for
// writing (the exclusive Lock) — use reconnectExclusive rather than calling
// this directly.
func (l *Lake) reconnect(ctx context.Context) error {
	base, baseCancel := raceContext(context.WithoutCancel(ctx), l.shutdownCh)
	defer baseCancel()
	ctx, cancel := context.WithTimeout(base, reconnectTimeout)
	defer cancel()

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return fmt.Errorf("lake: reconnect open: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConnsOrDefault(l.cfg.MaxOpenConns))
	tmp := &Lake{db: db, readOnly: l.readOnly}
	if err := tmp.attach(ctx, l.cfg); err != nil {
		db.Close()
		return err
	}
	if l.cfg.MemoryLimit != "" {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET memory_limit = %s", sqlQuote(l.cfg.MemoryLimit))); err != nil {
			db.Close()
			return fmt.Errorf("lake: set memory_limit: %w", err)
		}
	}
	if l.cfg.Threads > 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET threads = %d", l.cfg.Threads)); err != nil {
			db.Close()
			return fmt.Errorf("lake: set threads: %w", err)
		}
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

// reconnectExclusive takes the exclusive lock and reconnects. Multiple
// concurrent callers may each observe the same stale connection (e.g. right
// after a Quack Server restart) and each call this; redundant reconnects
// are wasteful but harmless — the lock serializes them and the last one to
// run leaves l.db in a healthy, internally-consistent state.
func (l *Lake) reconnectExclusive(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reconnect(ctx)
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
	// Disables DuckLake data inlining (small writes held as catalog-resident
	// mini-tables instead of Parquet files). DuckLake never prunes an inlined
	// table from ducklake_inlined_data_tables even once ducklake_flush_inlined_data
	// has emptied it, and its planner unions across every entry in that
	// registry when scanning a table — thousands of long-dead empty entries
	// made a trivial query take minutes in production. This must be set as
	// an ATTACH option on the quack: client connection itself: neither the
	// GLOBAL DuckDB setting ducklake_default_data_inlining_row_limit nor the
	// catalog-level ducklake_set_option('lake', 'data_inlining_row_limit', ...)
	// metadata key has any effect on writes routed through a remote Quack
	// Server attachment.
	options = append(options, "DATA_INLINING_ROW_LIMIT 0")
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
				// An unscoped TYPE r2 secret defaults to matching only
				// "r2://" URIs (verified against duckdb_secrets()), but the
				// Lake's DATA_PATH and every file DuckLake touches use
				// "s3://" URIs. Without this, the secret silently never
				// matches and DuckDB falls back to the default AWS
				// credential chain against the literal
				// "*.s3.amazonaws.com" endpoint.
				"SCOPE " + sqlQuote("s3://"+sc.Bucket),
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
// reconnects and retries once. Runs under the shared RLock so it can
// proceed concurrently with other queries/inserts/pings on this Lake
// instead of serializing behind them. Satisfies handler.DBHandle.
func (l *Lake) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	l.mu.RLock()
	rows, err := l.db.QueryContext(ctx, query, args...)
	l.mu.RUnlock()
	if isStaleConn(err) {
		if rerr := l.reconnectExclusive(ctx); rerr != nil {
			return nil, fmt.Errorf("lake: reconnect: %w", rerr)
		}
		l.mu.RLock()
		rows, err = l.db.QueryContext(ctx, query, args...)
		l.mu.RUnlock()
	}
	return rows, err
}

// Query executes a query that returns rows. Satisfies handler.DBHandle.
func (l *Lake) Query(query string, args ...any) (*sql.Rows, error) {
	return l.QueryContext(context.Background(), query, args...)
}

// ExecContext executes a query without returning rows. On a stale Quack
// Server connection the Lake reconnects and retries once. Runs under the
// shared RLock so it can proceed concurrently with other Lake calls.
// Satisfies handler.DBHandle.
func (l *Lake) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	l.mu.RLock()
	res, err := l.db.ExecContext(ctx, query, args...)
	l.mu.RUnlock()
	if isStaleConn(err) {
		if rerr := l.reconnectExclusive(ctx); rerr != nil {
			return nil, fmt.Errorf("lake: reconnect: %w", rerr)
		}
		l.mu.RLock()
		res, err = l.db.ExecContext(ctx, query, args...)
		l.mu.RUnlock()
	}
	return res, err
}

// Exec executes a query without returning rows. Satisfies handler.DBHandle.
func (l *Lake) Exec(query string, args ...any) (sql.Result, error) {
	return l.ExecContext(context.Background(), query, args...)
}

// QueryRowContext executes a query expected to return at most one row.
// Unlike QueryContext, transparent reconnection is not possible here because
// *sql.Row defers the error to Scan. The call runs under the shared RLock
// (concurrent with other reads/writes/pings); RLock still blocks until any
// in-progress reconnectExclusive completes, so a concurrent Ping's reconnect
// is guaranteed to land before this call proceeds. Satisfies handler.DBHandle.
func (l *Lake) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	l.mu.RLock()
	defer l.mu.RUnlock()
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
	l.mu.RLock()
	err := l.insertSpansLocked(ctx, spans)
	l.mu.RUnlock()
	if isStaleConn(err) {
		if rerr := l.reconnectExclusive(ctx); rerr != nil {
			return fmt.Errorf("lake: reconnect: %w", rerr)
		}
		l.mu.RLock()
		err = l.insertSpansLocked(ctx, spans)
		l.mu.RUnlock()
	}
	return err
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
	l.mu.RLock()
	err := l.insertScoresLocked(ctx, scores)
	l.mu.RUnlock()
	if isStaleConn(err) {
		if rerr := l.reconnectExclusive(ctx); rerr != nil {
			return fmt.Errorf("lake: reconnect: %w", rerr)
		}
		l.mu.RLock()
		err = l.insertScoresLocked(ctx, scores)
		l.mu.RUnlock()
	}
	return err
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

// pingRetryTimeout bounds the post-reconnect retry query in Ping. Deliberately
// not derived from the caller's ctx, for the same reason reconnectTimeout
// isn't (see its doc comment): reconnect() runs under its own decoupled
// budget and can take up to reconnectTimeout to succeed, by which point a
// short-lived caller context (e.g. the readiness probe's 3s budget) has
// almost certainly already expired. Reusing that expired ctx for the retry
// would guarantee failure on every reconnect — even though the connection it
// just repaired is healthy — defeating Ping's self-heal entirely.
const pingRetryTimeout = 3 * time.Second

// Ping verifies the Lake's Catalog connection is reachable by executing
// a metadata-only scan against lake.spans. This forces a round-trip
// through the Quack Server catalog — pinging the in-memory DuckDB via
// "SELECT 1" would not detect a stale Quack connection. On a stale
// connection, Ping reconnects and retries once so the readiness probe
// self-heals after a Quack Server restart.
//
// Runs under the shared RLock, not the exclusive Lock: Ping backs the
// readiness/liveness probes, so it must never queue behind an unrelated
// slow query on the same Lake instance — that queuing (when this used a
// plain Mutex) is exactly what let one slow UI query make a pod's health
// checks hang even though the Quack Server itself was healthy.
func (l *Lake) Ping(ctx context.Context) error {
	l.mu.RLock()
	var n int64
	err := l.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM lake.spans WHERE 1 = 0").Scan(&n)
	l.mu.RUnlock()
	if isStaleConn(err) {
		if rerr := l.reconnectExclusive(ctx); rerr != nil {
			return fmt.Errorf("lake: reconnect: %w", rerr)
		}
		retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pingRetryTimeout)
		defer cancel()
		l.mu.RLock()
		err = l.db.QueryRowContext(retryCtx, "SELECT COUNT(*) FROM lake.spans WHERE 1 = 0").Scan(&n)
		l.mu.RUnlock()
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
//
// Pins the whole DELETE+reclaim statement sequence to one explicit
// *sql.Conn (see reclaim's doc comment for why) rather than relying on the
// pool being capped at a single connection — the pool now runs multiple
// connections (see MaxOpenConns) so every other Lake operation can proceed
// concurrently instead of serializing behind this one.
func (l *Lake) DeleteProject(ctx context.Context, projectID string) error {
	if l.readOnly {
		return fmt.Errorf("lake: cannot delete from a read-only attachment")
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	conn, err := l.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("lake: delete project conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "DELETE FROM lake.spans WHERE project_id = ?", projectID); err != nil {
		return fmt.Errorf("lake: delete spans for project %s: %w", projectID, err)
	}
	if _, err := conn.ExecContext(ctx, "DELETE FROM lake.scores WHERE project_id = ?", projectID); err != nil {
		return fmt.Errorf("lake: delete scores for project %s: %w", projectID, err)
	}
	return l.reclaim(ctx, conn)
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
// reclaim always runs on the same *sql.Conn that performed the DELETE
// (passed in by DeleteProject), regardless of how many other connections
// the pool has open for unrelated concurrent operations.
func (l *Lake) reclaim(ctx context.Context, conn *sql.Conn) error {
	stmts := []string{
		"CALL ducklake_rewrite_data_files('lake', 'spans')",
		"CALL ducklake_rewrite_data_files('lake', 'scores')",
		"CALL ducklake_expire_snapshots('lake', older_than => now())",
		"CALL ducklake_delete_orphaned_files('lake', cleanup_all => true)",
		"CALL ducklake_cleanup_old_files('lake', cleanup_all => true)",
	}
	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
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
