package s3

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/storage"
)

// ObjectInfo holds metadata for a single object returned by ListObjectsOlderThan.
type ObjectInfo struct {
	Key          string
	Bucket       string
	LastModified time.Time
	Size         int64
}

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

// bucketName returns the configured bucket name, falling back to "omneval"
// when no bucket is set in the config. In tests, override via SetBucket.
var defaultBucket = "omneval"

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
// Uses multipart upload (size -1) so the data is streamed without buffering
// the entire body in memory.
func (s *Store) Put(_ context.Context, key string, r io.Reader) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}

	_, err := s.client.PutObject(context.Background(), s.getBucket(), key,
		r, -1, minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		})
	if err != nil {
		return fmt.Errorf("s3: put %s: %w", key, err)
	}
	return nil
}

// PutSized uploads the reader's contents to the given key with the known size.
// The size is passed directly to PutObject, enabling a single-part upload
// without buffering the entire body in memory.
func (s *Store) PutSized(_ context.Context, key string, r io.Reader, size int64) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}

	_, err := s.client.PutObject(context.Background(), s.getBucket(), key,
		r, size, minio.PutObjectOptions{
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

// ListObjectsOlderThan lists all objects under the given prefix whose
// LastModified is before the cutoff time.
func (s *Store) ListObjectsOlderThan(_ context.Context, prefix string, cutoff time.Time) ([]ObjectInfo, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("s3: no client configured")
	}

	var result []ObjectInfo
	bucket := s.getBucket()
	for object := range s.client.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{
		Recursive: true,
		Prefix:    prefix,
	}) {
		if object.Err != nil {
			return result, fmt.Errorf("s3: list %s: %w", prefix, object.Err)
		}
		if object.LastModified.Before(cutoff) {
			result = append(result, ObjectInfo{
				Key:          object.Key,
				Bucket:       bucket,
				LastModified: object.LastModified,
				Size:         object.Size,
			})
		}
	}
	return result, nil
}

// CopyObject copies an object from the source key in the store's bucket to a
// destination bucket and key. If storageClass is non-empty it is set via the
// UserMetadata header on the destination.
func (s *Store) CopyObject(_ context.Context, dstBucket, dstKey, srcKey, storageClass string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}

	src := minio.CopySrcOptions{
		Bucket: s.getBucket(),
		Object: srcKey,
	}
	dstOpts := minio.CopyDestOptions{
		Bucket: dstBucket,
		Object: dstKey,
	}
	if storageClass != "" {
		dstOpts.UserMetadata = map[string]string{
			"X-Amz-Storage-Class": storageClass,
		}
		dstOpts.ReplaceMetadata = true
	}

	_, err := s.client.CopyObject(context.Background(), dstOpts, src)
	if err != nil {
		return fmt.Errorf("s3: copy %s → %s/%s: %w", srcKey, dstBucket, dstKey, err)
	}
	return nil
}

// DeleteObjectsBatch removes multiple objects from the given bucket.
// Returns nil when the key list is empty.
//
// Deliberately issues one single-object DeleteObject call per key instead of
// using minio's batch RemoveObjects (S3 DeleteObjects API): Cloudflare R2's
// implementation of DeleteObjects has a long-standing, documented bug where
// it returns HTTP 200 with the requested keys listed as deleted, but never
// actually removes the objects (reported on Cloudflare's community forum
// since 2023). Single-object DeleteObject calls are confirmed reliable
// against R2, so this trades one API call for N to work correctly on every
// S3-compatible backend, not just AWS.
func (s *Store) DeleteObjectsBatch(_ context.Context, bucket string, keys []string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}
	if len(keys) == 0 {
		return nil
	}

	var errs []error
	for _, key := range keys {
		if err := s.client.RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{}); err != nil {
			errs = append(errs, fmt.Errorf("s3: delete %s: %w", key, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("s3: batch delete failed (%d errors): %v", len(errs), errs)
	}
	return nil
}
