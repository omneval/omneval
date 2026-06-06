package retention

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/config"
	s3pkg "github.com/omneval/omneval/internal/storage/s3"
)

// mockStore implements Store for testing.
type mockStore struct {
	listObjectsFn func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error)
	copyObjectFn  func(ctx context.Context, dstBucket, dstKey, srcKey, storageClass string) error
	deleteFn      func(ctx context.Context, bucket string, keys []string) error
}

func (m *mockStore) ListObjectsOlderThan(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
	return m.listObjectsFn(ctx, prefix, cutoff)
}

func (m *mockStore) CopyObject(ctx context.Context, dstBucket, dstKey, srcKey, storageClass string) error {
	return m.copyObjectFn(ctx, dstBucket, dstKey, srcKey, storageClass)
}

func (m *mockStore) DeleteObjectsBatch(ctx context.Context, bucket string, keys []string) error {
	return m.deleteFn(ctx, bucket, keys)
}

// Compile-time check: mockStore satisfies Store interface.
var _ Store = (*mockStore)(nil)

func TestNew(t *testing.T) {
	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "delete",
		MaxAgeDays: 30,
	}
	w := New(&mockStore{}, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestNew_DisabledReturnsNil(t *testing.T) {
	cfg := &config.RetentionConfig{
		Enabled: false,
	}
	w := New(&mockStore{}, cfg)
	if w != nil {
		t.Error("expected nil worker when retention is disabled")
	}
}

func TestRun_DeleteAction(t *testing.T) {
	objects := []s3pkg.ObjectInfo{
		{Key: "parquet/trace1.parquet", Bucket: "test-bucket", LastModified: time.Now().Add(-48 * time.Hour), Size: 1024},
		{Key: "parquet/trace2.parquet", Bucket: "test-bucket", LastModified: time.Now().Add(-72 * time.Hour), Size: 2048},
	}
	var deletedKeys []string
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
		deleteFn: func(ctx context.Context, bucket string, keys []string) error {
			deletedKeys = keys
			return nil
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "delete",
		MaxAgeDays: 30,
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	result, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ObjectsScanned != 2 {
		t.Errorf("ObjectsScanned = %d, want 2", result.ObjectsScanned)
	}
	if result.ObjectsActedOn != 2 {
		t.Errorf("ObjectsActedOn = %d, want 2", result.ObjectsActedOn)
	}
	if result.BytesActedOn != 3072 {
		t.Errorf("BytesActedOn = %d, want 3072", result.BytesActedOn)
	}
	if len(deletedKeys) != 2 {
		t.Errorf("deletedKeys length = %d, want 2", len(deletedKeys))
	}
}

type copyRecord struct {
	dstBucket, dstKey, srcKey, storageClass string
}

func TestRun_MoveAction(t *testing.T) {
	objects := []s3pkg.ObjectInfo{
		{Key: "parquet/trace1.parquet", Bucket: "test-bucket", LastModified: time.Now().Add(-48 * time.Hour), Size: 1024},
	}
	var copyRecords []copyRecord
	var deletedKeys []string
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
		copyObjectFn: func(ctx context.Context, dstBucket, dstKey, srcKey, storageClass string) error {
			copyRecords = append(copyRecords, copyRecord{dstBucket, dstKey, srcKey, storageClass})
			return nil
		},
		deleteFn: func(ctx context.Context, bucket string, keys []string) error {
			deletedKeys = keys
			return nil
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "move",
		MaxAgeDays: 30,
		Destination: config.RetentionDestinationConfig{
			Bucket:       "cold-archive",
			Prefix:       "archived/",
			StorageClass: "GLACIER",
		},
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	result, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ObjectsScanned != 1 {
		t.Errorf("ObjectsScanned = %d, want 1", result.ObjectsScanned)
	}
	if result.ObjectsActedOn != 1 {
		t.Errorf("ObjectsActedOn = %d, want 1", result.ObjectsActedOn)
	}
	if len(copyRecords) != 1 {
		t.Fatalf("copyRecords length = %d, want 1", len(copyRecords))
	}
	if copyRecords[0].dstBucket != "cold-archive" {
		t.Errorf("dstBucket = %q, want %q", copyRecords[0].dstBucket, "cold-archive")
	}
	if copyRecords[0].dstKey != "archived/parquet/trace1.parquet" {
		t.Errorf("dstKey = %q, want %q", copyRecords[0].dstKey, "archived/parquet/trace1.parquet")
	}
	if copyRecords[0].storageClass != "GLACIER" {
		t.Errorf("storageClass = %q, want %q", copyRecords[0].storageClass, "GLACIER")
	}
	if len(deletedKeys) != 1 {
		t.Errorf("deletedKeys length = %d, want 1", len(deletedKeys))
	}
}

func TestRun_NoObjects(t *testing.T) {
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return nil, nil
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "delete",
		MaxAgeDays: 30,
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	result, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ObjectsScanned != 0 {
		t.Errorf("ObjectsScanned = %d, want 0", result.ObjectsScanned)
	}
	if result.ObjectsActedOn != 0 {
		t.Errorf("ObjectsActedOn = %d, want 0", result.ObjectsActedOn)
	}
}

func TestRun_DeleteError(t *testing.T) {
	objects := []s3pkg.ObjectInfo{
		{Key: "parquet/trace1.parquet", Bucket: "test-bucket", LastModified: time.Now().Add(-48 * time.Hour), Size: 1024},
	}
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
		deleteFn: func(ctx context.Context, bucket string, keys []string) error {
			return fmt.Errorf("delete failed")
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "delete",
		MaxAgeDays: 30,
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	result, err := w.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Run")
	}
	if len(result.Errors) != 1 {
		t.Errorf("result.Errors length = %d, want 1", len(result.Errors))
	}
}

func TestRun_ListError(t *testing.T) {
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return nil, fmt.Errorf("list failed")
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "delete",
		MaxAgeDays: 30,
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	_, err := w.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Run")
	}
}

func TestRun_UsesParquetPrefix(t *testing.T) {
	var capturedPrefix string
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			capturedPrefix = prefix
			return nil, nil
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "delete",
		MaxAgeDays: 30,
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	_, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPrefix != "parquet/" {
		t.Errorf("prefix = %q, want %q", capturedPrefix, "parquet/")
	}
}

func TestRun_MovePartialFailure(t *testing.T) {
	objects := []s3pkg.ObjectInfo{
		{Key: "parquet/trace1.parquet", Bucket: "test-bucket", LastModified: time.Now().Add(-48 * time.Hour), Size: 1024},
		{Key: "parquet/trace2.parquet", Bucket: "test-bucket", LastModified: time.Now().Add(-72 * time.Hour), Size: 2048},
		{Key: "parquet/trace3.parquet", Bucket: "test-bucket", LastModified: time.Now().Add(-96 * time.Hour), Size: 512},
	}
	var copyRecords []copyRecord
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return objects, nil
		},
		copyObjectFn: func(ctx context.Context, dstBucket, dstKey, srcKey, storageClass string) error {
			copyRecords = append(copyRecords, copyRecord{dstBucket, dstKey, srcKey, storageClass})
			if srcKey == "parquet/trace2.parquet" {
				return fmt.Errorf("copy failed for trace2")
			}
			return nil
		},
		deleteFn: func(ctx context.Context, bucket string, keys []string) error {
			return nil
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "move",
		MaxAgeDays: 30,
		Destination: config.RetentionDestinationConfig{
			Bucket:       "cold-archive",
			Prefix:       "archived/",
			StorageClass: "GLACIER",
		},
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	result, err := w.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Run due to partial failure")
	}
	if result.ObjectsScanned != 3 {
		t.Errorf("ObjectsScanned = %d, want 3", result.ObjectsScanned)
	}
	// Only trace1 and trace3 should have succeeded
	if result.ObjectsActedOn != 2 {
		t.Errorf("ObjectsActedOn = %d, want 2", result.ObjectsActedOn)
	}
	// result.Errors has 2 entries: the copy error from moveObjects, plus the
	// summary error appended by Run.
	if len(result.Errors) != 2 {
		t.Errorf("result.Errors length = %d, want 2", len(result.Errors))
	}
	// Verify all three copy attempts were made
	if len(copyRecords) != 3 {
		t.Errorf("copyRecords length = %d, want 3 (all attempts made)", len(copyRecords))
	}
}

func TestWorker_LastRunAt(t *testing.T) {
	mock := &mockStore{
		listObjectsFn: func(ctx context.Context, prefix string, cutoff time.Time) ([]s3pkg.ObjectInfo, error) {
			return nil, nil
		},
	}

	cfg := &config.RetentionConfig{
		Enabled:    true,
		Action:     "delete",
		MaxAgeDays: 30,
	}
	w := New(mock, cfg)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	if !w.LastRunAt().IsZero() {
		t.Error("expected zero time before first run")
	}

	_, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lastRun := w.LastRunAt()
	if lastRun.IsZero() {
		t.Error("expected non-zero time after run")
	}
	if time.Since(lastRun) > 5*time.Second {
		t.Errorf("last run time is too old: %v", lastRun)
	}
}
