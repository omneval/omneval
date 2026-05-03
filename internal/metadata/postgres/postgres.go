package postgres

// Store is the Postgres-backed implementation of metadata.Store.
// Migrations live in ./migrations/ and are applied via golang-migrate.
type Store struct {
	// TODO: embed *sql.DB or pgx pool
}

// New opens a Postgres connection and returns a Store.
func New(dsn string) (*Store, error) {
	// TODO: implement
	panic("not implemented")
}
