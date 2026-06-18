// Package lakeserver implements the Quack Server side of ADR-0005: the sole
// process (services/quack) holding a direct DuckLake Catalog connection
// (Postgres in prod, a local single-writer file in demo) and serving it to
// Quack clients (internal/lake) via `quack_serve()`.
//
// # Catalog wiring (spike findings for #105)
//
// The #111 spike (internal/lake/quack_spike) established that the SERVER
// side of quack_serve() is a "plain" DuckDB session: it does NOT itself run
// `ATTACH ... AS lake (DATA_PATH ...)` — DuckLake's "quack:" catalog driver
// is what clients use, and Quack just relays the catalog RPCs to whatever
// database is "current" in the server's session.
//
// A follow-up spike for #105 (internal/lake/quack_spike2) tested what
// "current database" needs to be for the catalog to be durable:
//
//   - Opening the server's session against a PERSISTENT local DuckDB FILE
//     (sql.Open("duckdb", "/path/to/catalog.db") instead of an in-memory
//     "") is sufficient: when a `ducklake:quack:` client creates
//     `lake.spans` and inserts rows, DuckLake's quack catalog driver
//     creates the full `ducklake_*` metadata table set INSIDE that file via
//     RPC (verified by inspecting the file directly with a separate
//     connection after the server closed).
//   - Catalog state persists across a full server restart against the same
//     file/port: a fresh server process + fresh client sees the prior
//     server's committed rows.
//
// This generalizes directly to CatalogDriverPostgres: this package attaches
// the configured Postgres DSN as the server session's default database
// (`ATTACH '<dsn>' AS pgcatalog (TYPE postgres); USE pgcatalog;`) before
// calling quack_serve(), so the same RPC-driven `ducklake_*` table creation
// lands in Postgres instead of a local file. This mirrors exactly how
// internal/lake's pre-#105 `ducklake:postgres:<dsn>` attach used Postgres as
// the Catalog — the difference is that DuckLake's quack driver (used by
// every OTHER service) now talks to THIS session's default database rather
// than each service attaching Postgres directly.
//
// NOTE: the Postgres path (internal/lake/quack_spike3) requires a reachable
// Postgres instance to verify end-to-end and was not run in this
// environment (no Docker). The local-file path (quack_spike2) IS verified.
// If the Postgres ATTACH-as-default-database pattern does not work as
// expected in a real cluster, the fallback is to keep the server's own
// session attached to a local DuckDB file as its catalog store regardless
// of CatalogDriver, and treat CatalogDriverPostgres purely as a future
// option — but per ADR-0005 Postgres remains the documented prod catalog,
// so this package implements the ATTACH-as-default-database approach as the
// primary path. Document any deviation found when wiring this up against a
// real Postgres instance.
//
// # quack_serve hostname restrictions (#105, quack_spike5)
//
// quack_serve() rejects any listen hostname other than "localhost" unless
// allow_other_hostname=true is passed. Serve translates a bare ":PORT"
// ListenAddr (the demo/test default) to "localhost:PORT"; any other
// explicit host (e.g. "0.0.0.0" or a Kubernetes Service DNS name, needed so
// other pods can reach this server) is passed through with
// allow_other_hostname=true.
package lakeserver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/omneval/omneval/internal/config"
	_ "github.com/omneval/omneval/internal/duckdbfix"
)

// CatalogDriverPostgres uses the shared Postgres instance as the Catalog —
// the serialization point that makes multiple concurrent Quack clients
// safe.
const CatalogDriverPostgres = "postgres"

// CatalogDriverLocal uses a local DuckDB-file Catalog (single-writer, demo
// profile only — but per ADR-0005 the demo profile still runs a Quack
// Server in front of it).
const CatalogDriverLocal = "duckdb"

// Config describes how the Quack Server attaches its Catalog and serves it.
type Config struct {
	// ListenAddr is the address quack_serve() binds to, e.g. ":9494".
	ListenAddr string
	// Token authenticates Quack clients via `CREATE SECRET ... TYPE quack`.
	// If empty, Serve generates a random token and returns it via Token().
	Token string
	// CatalogDriver is "postgres" or "duckdb" (local single-writer catalog).
	CatalogDriver string
	// CatalogDSN is the Postgres DSN for the Catalog, or the local catalog
	// file path when CatalogDriver is "duckdb".
	CatalogDSN string
}

// ConfigFromApp derives the Quack Server's catalog settings from the
// application config: explicit quack.server.* settings win; otherwise the
// Catalog driver follows the metadata-store database.
func ConfigFromApp(cfg *config.Config) Config {
	sc := Config{
		ListenAddr:    cfg.Quack.Server.ListenAddr,
		Token:         cfg.Quack.Server.Token,
		CatalogDriver: cfg.Quack.Server.CatalogDriver,
		CatalogDSN:    cfg.Quack.Server.CatalogDSN,
	}
	if sc.ListenAddr == "" {
		sc.ListenAddr = ":9494"
	}
	if sc.CatalogDriver == "" {
		if cfg.Database.Driver == "postgres" {
			sc.CatalogDriver = CatalogDriverPostgres
		} else {
			sc.CatalogDriver = CatalogDriverLocal
		}
	}
	if sc.CatalogDSN == "" {
		if sc.CatalogDriver == CatalogDriverPostgres {
			sc.CatalogDSN = cfg.Database.DSN
		} else {
			sc.CatalogDSN = "lake/catalog.ducklake"
		}
	}
	return sc
}

// Server is a running Quack Server: a plain DuckDB session calling
// quack_serve() over the configured Catalog.
type Server struct {
	db    *sql.DB
	addr  string
	token string
	done  chan struct{}
	errCh chan error
}

// Serve opens the configured Catalog as the server session's default
// database, installs the quack extension, and starts quack_serve() in the
// background. Serve blocks only long enough to learn the server's listen
// address and auth token (reported by quack_serve()'s first result row);
// quack_serve() itself keeps running until ctx is canceled or Close is
// called.
func Serve(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.CatalogDSN == "" {
		return nil, fmt.Errorf("lakeserver: catalog DSN is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":9494"
	}

	// For the local-file catalog driver, open the DuckDB session directly
	// against the catalog file so DuckLake's quack catalog driver writes
	// ducklake_* metadata into the file (not into the in-memory primary DB).
	// Quack_spike2 verified that this approach persists across server restarts.
	// The Postgres driver must still open in-memory and ATTACH Postgres,
	// because the duckdb-go driver cannot open a Postgres DSN directly.
	dbPath := ""
	if cfg.CatalogDriver == CatalogDriverLocal {
		if err := ensureLocalCatalogDir(cfg.CatalogDSN); err != nil {
			return nil, fmt.Errorf("lakeserver: create catalog dir: %w", err)
		}
		dbPath = cfg.CatalogDSN
	}
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("lakeserver: open duckdb: %w", err)
	}

	if err := attachCatalog(ctx, db, cfg); err != nil {
		db.Close()
		return nil, err
	}

	steps := []string{"INSTALL quack", "LOAD quack"}
	for _, stmt := range steps {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("lakeserver: %s: %w", stmt, err)
		}
	}

	// quack_serve wants a host:port URI and, per quack_spike5, rejects any
	// hostname other than "localhost" unless allow_other_hostname=true is
	// passed. A bare ":9494" ListenAddr becomes "0.0.0.0:9494" so the server
	// binds every interface — reachable both from other pods via the
	// Kubernetes Service DNS name and from "localhost" in tests, since
	// 0.0.0.0 covers the loopback interface too. Binding "localhost:9494"
	// instead (as this used to do) only accepts loopback connections, which
	// makes the Quack Server unreachable from Writer/Query pods entirely.
	// allow_other_hostname=true is always passed since the bound host is
	// never "localhost".
	hostPort := cfg.ListenAddr
	if strings.HasPrefix(hostPort, ":") {
		hostPort = "0.0.0.0" + hostPort
	}
	allowOtherHostname := true
	serveURI := "quack://" + hostPort

	s := &Server{db: db, done: make(chan struct{}), errCh: make(chan error, 1)}

	type result struct {
		addr  string
		token string
		err   error
	}
	resultCh := make(chan result, 1)

	go func() {
		defer close(s.done)
		args := []string{sqlQuote(serveURI)}
		if allowOtherHostname {
			args = append(args, "allow_other_hostname => true")
		}
		if cfg.Token != "" {
			// quack_serve accepts a `token =>` keyword argument (verified it
			// does not error in internal/lake/quack_spike4); whether it
			// actually pins the returned auth_token to this value, or still
			// generates a fresh one, was not independently confirmed. Either
			// way Server.Token() below returns whatever quack_serve reports,
			// so clients always get a working token.
			args = append(args, fmt.Sprintf("token => %s", sqlQuote(cfg.Token)))
		}
		query := fmt.Sprintf("SELECT * FROM quack_serve(%s)", strings.Join(args, ", "))
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			resultCh <- result{err: fmt.Errorf("lakeserver: quack_serve: %w", err)}
			s.errCh <- err
			return
		}
		defer rows.Close()

		first := true
		for rows.Next() {
			var listenURI, listenURL, authToken string
			if err := rows.Scan(&listenURI, &listenURL, &authToken); err != nil {
				if first {
					resultCh <- result{err: fmt.Errorf("lakeserver: quack_serve scan: %w", err)}
				}
				s.errCh <- err
				return
			}
			if first {
				resultCh <- result{addr: listenURI, token: authToken}
				first = false
			}
		}
		if err := rows.Err(); err != nil {
			s.errCh <- err
			return
		}
		s.errCh <- nil
	}()

	res := <-resultCh
	if res.err != nil {
		db.Close()
		return nil, res.err
	}
	s.addr = res.addr
	s.token = res.token
	if s.token == "" {
		s.token = cfg.Token
	}
	return s, nil
}

// Addr returns the address reported by quack_serve() (e.g.
// "quack:0.0.0.0:9494").
func (s *Server) Addr() string { return s.addr }

// Token returns the auth token Quack clients must present via
// `CREATE SECRET ... TYPE quack, TOKEN '<token>'`. If Config.Token was
// empty, this is the randomly generated token quack_serve() issued.
func (s *Server) Token() string { return s.token }

// DB exposes the underlying session for the Table Maintenance scheduler.
func (s *Server) DB() *sql.DB { return s.db }

// Close stops quack_serve() and releases the DuckDB session.
func (s *Server) Close() error {
	err := s.db.Close()
	<-s.done
	return err
}

// attachCatalog makes the configured Catalog the server session's default
// database, so DuckLake's "quack:" catalog driver creates its ducklake_*
// metadata tables there.
//
// For CatalogDriverLocal the session was already opened directly against the
// catalog file (sql.Open("duckdb", path)), so no ATTACH is needed — the file
// IS the default database and DuckLake writes persist to it automatically.
//
// For CatalogDriverPostgres the session is in-memory and we ATTACH Postgres
// as the default, because duckdb-go cannot open a Postgres DSN directly.
func attachCatalog(ctx context.Context, db *sql.DB, cfg Config) error {
	switch cfg.CatalogDriver {
	case CatalogDriverPostgres:
		steps := []string{
			"INSTALL postgres", "LOAD postgres",
			fmt.Sprintf("ATTACH %s AS catalog_db (TYPE postgres)", sqlQuote(cfg.CatalogDSN)),
			"USE catalog_db",
		}
		for _, stmt := range steps {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("lakeserver: %s: %w", firstWords(stmt, 3), err)
			}
		}
		return nil
	default:
		// Session is already open against the file; just ensure its directory exists.
		return ensureLocalCatalogDir(cfg.CatalogDSN)
	}
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

// sqlQuote single-quotes a SQL string literal, doubling embedded quotes.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// firstWords returns the first n whitespace-separated words of s for error
// context without dumping whole statements.
func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.Join(fields, " ")
}
