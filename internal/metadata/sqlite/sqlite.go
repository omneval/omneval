package sqlite

// Store is the SQLite implementation of metadata.Store.
// Used for demo deployments (docker compose) with zero cloud dependencies.
// Migrations are managed with golang-migrate using SQL files in ./migrations/.
type Store struct{}
