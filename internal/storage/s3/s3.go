package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/zbloss/lantern/internal/config"
)

// Store is the S3-compatible implementation of storage.ObjectStore.
// Configured via StorageConfig; works with AWS S3, GCS, Azure Blob, and MinIO.
type Store struct {
	client *minio.Client
}

// New creates a new S3 Store from the given StorageConfig.
// Returns nil if the config has no endpoint (no S3 configured).
func New(cfg *config.StorageConfig) *Store {
	if cfg.Endpoint == "" {
		return nil
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: !hasHTTPPrefix(cfg.Endpoint),
		Region: cfg.Region,
	})
	if err != nil {
		return nil
	}

	return &Store{client: client}
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

	_, err := s.client.PutObject(context.Background(), getBucket(), key,
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

	object, err := s.client.GetObject(context.Background(), getBucket(), key, minio.GetObjectOptions{})
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

	return s.client.RemoveObject(context.Background(), getBucket(), key, minio.RemoveObjectOptions{})
}

// ListPrefix returns all object keys with the given prefix.
func (s *Store) ListPrefix(_ context.Context, prefix string) ([]string, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("s3: no client configured")
	}

	var keys []string
	for object := range s.client.ListObjects(context.Background(), getBucket(), minio.ListObjectsOptions{
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

// EnsureBucket creates the bucket if it does not exist.
func (s *Store) EnsureBucket(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("s3: no client configured")
	}

	bucket := getBucket()
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

// hasHTTPPrefix returns true if the endpoint starts with "http://".
func hasHTTPPrefix(endpoint string) bool {
	return len(endpoint) > 7 && endpoint[:7] == "http://"
}

// getBucket returns the configured storage bucket name.
// In tests, override via SetBucket.
var bucketName = "lantern"

// SetBucket overrides the bucket name (for testing).
func SetBucket(name string) {
	bucketName = name
}

func getBucket() string {
	return bucketName
}
