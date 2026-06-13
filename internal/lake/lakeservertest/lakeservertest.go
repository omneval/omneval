// Package lakeservertest provides shared Quack Server test fixtures for
// packages that need a Lake attachment in tests (writer pipeline/handler,
// query handler/dsl/query, backfill). Per ADR-0005, internal/lake's Config
// no longer carries a Catalog driver/DSN directly — every Quack client
// attaches via a running Quack Server (internal/lake/lakeserver). This
// package starts that server so test code doesn't duplicate the
// freePort/ListenAddr/CatalogDriver wiring internal/lake/lake_test.go
// pioneered.
package lakeservertest

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"

	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeserver"
)

// FreePort finds an available TCP port for a test Quack Server to bind to
// (quack_serve does not support port 0 / auto-assign).
func FreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("lakeservertest: find free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// NewLocal starts a Quack Server backed by a local DuckLake catalog file
// under t.TempDir() and returns a lake.Config that attaches to it as a
// Quack client, plus the temp directory root. The server is closed
// automatically via t.Cleanup.
func NewLocal(t *testing.T) (lake.Config, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	port := FreePort(t)
	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog", "lake.ducklake"),
	})
	if err != nil {
		t.Fatalf("lakeservertest: start quack server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	return lake.Config{
		QuackAddr:  fmt.Sprintf("localhost:%d", port),
		QuackToken: srv.Token(),
		DataPath:   filepath.Join(dir, "data"),
	}, dir
}

// NewPostgres starts a Quack Server backed by the given Postgres catalog DSN
// and returns a lake.Config template (QuackAddr/QuackToken set; DataPath
// and Storage left for the caller to fill in, since callers may use a local
// directory or an s3:// path). The server is closed automatically via
// t.Cleanup.
func NewPostgres(t *testing.T, catalogDSN string) lake.Config {
	t.Helper()
	ctx := context.Background()

	port := FreePort(t)
	srv, err := lakeserver.Serve(ctx, lakeserver.Config{
		ListenAddr:    fmt.Sprintf(":%d", port),
		CatalogDriver: lakeserver.CatalogDriverPostgres,
		CatalogDSN:    catalogDSN,
	})
	if err != nil {
		t.Fatalf("lakeservertest: start quack server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	return lake.Config{
		QuackAddr:  fmt.Sprintf("localhost:%d", port),
		QuackToken: srv.Token(),
	}
}
