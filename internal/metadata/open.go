package metadata

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/omneval/omneval/internal/metadata/postgres"
	"github.com/omneval/omneval/internal/metadata/sqlite"
)

// Compile-time checks that every driver satisfies the Store interface.
var (
	_ Store = (*sqlite.Store)(nil)
	_ Store = (*postgres.Store)(nil)
)

// DefaultSQLiteDSN is the SQLite database path used when no DSN is configured.
const DefaultSQLiteDSN = "omneval.db"

// Open creates a metadata store for the given driver, runs migrations, and
// returns it ready for use. It is the single factory shared by all services
// and the single place to add new drivers.
//
// Supported drivers: "sqlite" (also the default when driver is empty) and
// "postgres". SQLite falls back to DefaultSQLiteDSN when dsn is empty;
// Postgres requires a DSN.
func Open(driver, dsn string) (Store, error) {
	switch driver {
	case "", "sqlite":
		if dsn == "" {
			dsn = DefaultSQLiteDSN
		}
		slog.Info("metadata: opening SQLite store", "path", dsn)
		store, err := sqlite.New(dsn)
		if err != nil {
			return nil, fmt.Errorf("metadata: open sqlite store: %w", err)
		}
		if err := store.Migrate(context.Background()); err != nil {
			store.Close()
			return nil, fmt.Errorf("metadata: migrate sqlite store: %w", err)
		}
		return store, nil
	case "postgres":
		if dsn == "" {
			return nil, fmt.Errorf("metadata: postgres driver requires database.dsn")
		}
		slog.Info("metadata: opening Postgres store", "dsn", dsn)
		store, err := postgres.New(dsn)
		if err != nil {
			return nil, fmt.Errorf("metadata: open postgres store: %w", err)
		}
		if err := store.Migrate(context.Background()); err != nil {
			store.Close()
			return nil, fmt.Errorf("metadata: migrate postgres store: %w", err)
		}
		return store, nil
	default:
		return nil, fmt.Errorf("metadata: unknown database driver: %s", driver)
	}
}
