package flush

// Flusher exports spans older than 48 hours from DuckDB to Hive-partitioned
// Parquet files on S3 and prunes the corresponding rows from the hot store.
type Flusher struct{}
