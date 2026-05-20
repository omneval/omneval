package probe

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Check represents a single readiness gate. If its Check method returns an
// error, the probe reports "not ready" (HTTP 503).
type Check interface {
	Check(ctx context.Context) error
}

// Prober aggregates checks and serves HTTP handlers for /healthz and /readyz.
type Prober struct {
	mu     sync.RWMutex
	checks map[string]Check
	names  []string // sorted for deterministic ordering
}

// New creates a Prober with a default liveness handler (always 200) and
// empty readiness checks.
func New() *Prober {
	p := &Prober{
		checks: make(map[string]Check),
	}
	return p
}

// AddCheck registers a readiness gate identified by name.
func (p *Prober) AddCheck(name string, ch Check) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.checks[name] = ch
	p.names = p.sortedNames()
}

// HealthHandler returns the HTTP handler for the liveness probe.
// It always returns 200 OK with body "ok".
func (p *Prober) HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// ReadyHandler returns the HTTP handler for the readiness probe.
// It runs all registered checks in order. If any check fails, it returns
// HTTP 503 with body "not ready".
func (p *Prober) ReadyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		p.mu.RLock()
		names := make([]string, len(p.names))
		copy(names, p.names)
		p.mu.RUnlock()

		for _, name := range names {
			p.mu.RLock()
			ch := p.checks[name]
			p.mu.RUnlock()

			if err := ch.Check(ctx); err != nil {
				slog.Warn("readiness check failed", "name", name, "err", err)
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("not ready"))
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// Router creates an http.Handler that mounts /healthz and /readyz.
// Use this to add both endpoints to an existing ServeMux.
func (p *Prober) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", p.HealthHandler().ServeHTTP)
	mux.HandleFunc("GET /readyz", p.ReadyHandler().ServeHTTP)
	return mux
}

func (p *Prober) sortedNames() []string {
	names := make([]string, 0, len(p.checks))
	for n := range p.checks {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// --- Built-in Checks ---

// RedisPing checks Redis connectivity via PING.
type RedisPing struct {
	Pinger func(ctx context.Context) error
}

func (r *RedisPing) Check(ctx context.Context) error {
	if err := r.Pinger(ctx); err != nil {
		return &ProbeError{Op: "redis_ping", Err: err}
	}
	return nil
}

// FileExists checks that a file path exists on disk.
type FileExists struct {
	Path string
}

func (f *FileExists) Check(ctx context.Context) error {
	exists, err := fileExists(f.Path)
	if err != nil {
		return &ProbeError{Op: "file_exists", Path: f.Path, Err: err}
	}
	if !exists {
		return &ProbeError{Op: "file_exists", Path: f.Path, Err: os.ErrNotExist}
	}
	return nil
}

// DuckDBWritable checks that a DuckDB database at the given path is open
// and accepts writes (CREATE TABLE + DROP TABLE).
type DuckDBWritable struct {
	Open func(path string) (WritableView, error)
	Path string
}

// WritableView is a minimal interface for write readiness checks.
// Implemented by *sql.DB and test doubles.
type WritableView interface {
	PingContext(ctx context.Context) error
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Close() error
}

func (d *DuckDBWritable) Check(ctx context.Context) error {
	db, err := d.Open(d.Path)
	if err != nil {
		return &ProbeError{Op: "duckdb_open", Path: d.Path, Err: err}
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return &ProbeError{Op: "duckdb_ping", Path: d.Path, Err: err}
	}

	_, err = db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS _probe (id INTEGER)")
	if err != nil {
		return &ProbeError{Op: "duckdb_write", Path: d.Path, Err: err}
	}

	_, err = db.ExecContext(ctx, "DROP TABLE IF EXISTS _probe")
	_ = err // drop is best-effort

	return nil
}

// MetadataStoreReachable checks that the metadata store is reachable via a
// simple query. The checker receives a function that opens a connection and
// runs the query.
type MetadataStoreReachable struct {
	Query func(ctx context.Context) error
}

func (m *MetadataStoreReachable) Check(ctx context.Context) error {
	if err := m.Query(ctx); err != nil {
		return &ProbeError{Op: "metadata_store", Err: err}
	}
	return nil
}

// ProbeError wraps an error with an operation name for logging.
type ProbeError struct {
	Op   string
	Path string
	Err  error
}

func (e *ProbeError) Error() string {
	var b strings.Builder
	b.WriteString("probe:")
	if e.Op != "" {
		b.WriteString(" ")
		b.WriteString(e.Op)
	}
	if e.Path != "" {
		b.WriteString(" path=")
		b.WriteString(e.Path)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *ProbeError) Unwrap() error { return e.Err }

// fileExists checks if a file exists. It is a variable for test injection.
var fileExists = func(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
