package server

// Run starts the Writer Service: drains the Redis ingest queue, writes to
// DuckDB, syncs snapshots to S3, flushes aged partitions as Parquet, and
// serves POST /internal/v1/scores for Eval Worker score write-back.
func Run() error {
	panic("not implemented")
}
