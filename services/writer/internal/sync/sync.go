package sync

// Syncer copies the live DuckDB file to S3 every 30 seconds so Query API
// replicas always have a fresh snapshot.
type Syncer struct{}
