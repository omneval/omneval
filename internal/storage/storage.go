package storage

import (
	"context"
	"io"
	"time"
)

// ObjectStat holds metadata about an object in object storage.
type ObjectStat struct {
	ETag         string
	LastModified time.Time
}

// ObjectStore abstracts S3-compatible object storage (AWS S3, GCS, Azure
// Blob, MinIO). Used for DuckDB snapshot sync and Parquet archive.
type ObjectStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	// ListPrefix returns all object keys with the given prefix.
	ListPrefix(ctx context.Context, prefix string) ([]string, error)
	// Stat returns metadata about the object at the given key.
	Stat(ctx context.Context, key string) (*ObjectStat, error)
}
