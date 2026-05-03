package redis

// IngestQueue is the Redis-backed implementation of queue.IngestQueue.
// Uses a dedicated Redis list key for ingest payloads.
type IngestQueue struct{}

// EvalQueue is the Redis-backed implementation of queue.EvalQueue.
// Uses a separate Redis list key to avoid head-of-line blocking.
type EvalQueue struct{}
