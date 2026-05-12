package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/storage"
)

// Store is the S3-compatible implementation of storage.ObjectStore.
// Configured via StorageConfig; works with AWS S3, GCS, Azure Blob, and MinIO.
type Store struct {
	client *minio.Client
	bucket string
}

// New creates a new S3 Store from the given StorageConfig.
// Returns nil if the config has no endpoint (no S3 configured).
func New(cfg *config.StorageConfig) *Store {
	if cfg.Endpoint == "" {
		return nil
	}

	// Strip http:// or https:// scheme — minio.New expects bare host:port.
	endpoint := strings.TrimPrefix(cfg.Endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	secure := !strings.HasPrefix(cfg.Endpoint, "http://")

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		slog.Warn("s3: minio client creation failed", "endpoint", cfg.Endpoint, "err", err)
		return nil
	}

	return &Store{client: client, bucket: cfg.Bucket}
}

// bucketName returns the configured bucket name, falling back to "lantern"
// when no bucket is set in the config. In tests, override via SetBucket.
var defaultBucket = "lantern"

// SetBucket overrides the default bucket name (for testing only).
func SetBucket(name string) {
	defaultBucket = name
}

func (s *Store) getBucket() string {
	if s.bucket != "" {
		return s.bucket
	}
	return defaultBucket
}

// Put uploads the reader's contents to the given key in the configured bucket.
func (s *Store) Put(_ context.Context, key string, r io.Reader) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}

	// Read the entire body to compute size for PutObject.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return fmt.Errorf("s3: read body: %w", err)
	}

	_, err := s.client.PutObject(context.Background(), s.getBucket(), key,
		&buf, int64(buf.Len()), minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		})
	if err != nil {
		return fmt.Errorf("s3: put %s: %w", key, err)
	}
	return nil
}

// Get returns a reader for the object at the given key.
func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("s3: no client configured")
	}

	object, err := s.client.GetObject(context.Background(), s.getBucket(), key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3: get %s: %w", key, err)
	}
	return object, nil
}

// Delete removes the object at the given key.
func (s *Store) Delete(_ context.Context, key string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}

	return s.client.RemoveObject(context.Background(), s.getBucket(), key, minio.RemoveObjectOptions{})
}

// ListPrefix returns all object keys with the given prefix.
func (s *Store) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("s3: no client configured")
	}

	var keys []string
	for object := range s.client.ListObjects(context.Background(), s.getBucket(), minio.ListObjectsOptions{
		Recursive: true,
		Prefix:    prefix,
	}) {
		if object.Err != nil {
			return keys, fmt.Errorf("s3: list %s: %w", prefix, object.Err)
		}
		keys = append(keys, object.Key)
	}
	return keys, nil
}

// Stat returns metadata about the object at the given key.
func (s *Store) Stat(_ context.Context, key string) (*storage.ObjectStat, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("s3: no client configured")
	}

	info, err := s.client.StatObject(context.Background(), s.getBucket(), key, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3: stat %s: %w", key, err)
	}

	return &storage.ObjectStat{
		ETag:         info.ETag,
		LastModified: info.LastModified,
	}, nil
}

// SnapshotKey returns the S3 key for the DuckDB snapshot file.
// The key is always "snapshots/duckdb.db" — the region is an S3 bucket
// property, not an object key property.
func SnapshotKey() string {
	return "snapshots/duckdb.db"
}

// EnsureBucket creates the bucket if it does not exist.
func (s *Store) EnsureBucket(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}

	bucket := s.getBucket()
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("s3: bucket exists check: %w", err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{
			Region: "us-east-1",
		}); err != nil {
			return fmt.Errorf("s3: create bucket %s: %w", bucket, err)
		}
	}
	return nil
}


