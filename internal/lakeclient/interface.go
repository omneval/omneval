// Package lakeclient defines the client interface for the Quack protocol.
//
// Callers (Writer, Query API) use this interface for all operations that cross
// the network boundary to the Quack Server. The concrete implementation is
// *lake.Lake in github.com/omneval/omneval/internal/lake.
package lakeclient

import (
	"context"
	"database/sql"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// Client is the interface for all Quack-protocol operations — the operations
// that cross the network boundary between a client process and the Quack
// Server.
//
// Every caller that reads or writes span/score data depends only on this
// interface (lakeclient.Client), not on the concrete *lake.Lake type, so the
// server and client packages stay decoupled.
type Client interface {
	// InsertSpans writes a batch of spans to the Lake.
	InsertSpans(ctx context.Context, spans []*domain.Span) error

	// InsertScores writes a batch of scores to the Lake.
	InsertScores(ctx context.Context, scores []*domain.Score) error

	// SpanStartTime returns the start time of the span with the given trace
	// and span IDs. Returns the zero time and no error if the span is not found.
	SpanStartTime(ctx context.Context, traceID, spanID string) (time.Time, error)

	// Ping verifies the Lake's Catalog connection is reachable.
	Ping(ctx context.Context) error

	// DeleteProject permanently deletes all of a project's spans and scores.
	DeleteProject(ctx context.Context, projectID string) error

	// FlushInlinedData forces any rows DuckLake has inlined into the Catalog
	// out to physical Parquet files.
	FlushInlinedData(ctx context.Context) error
}

// Querier is the minimal interface for SQL queries that go through the Lake's
// embedded DuckDB instance (attached to the Quack Lake via ATTACH).
//
// Query handlers (SpanHandler, QueryBuilder, etc.) execute SQL against
// lake.spans and lake.scores through this interface.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}