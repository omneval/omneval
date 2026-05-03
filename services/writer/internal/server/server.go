package server

// Run starts the Writer Service: drains the Redis ingest queue, writes to
// DuckDB, syncs snapshots to S3, and flushes aged partitions as Parquet.
func Run() error {
	panic("not implemented")
}
