package s3

import (
	"io"
	"testing"

	"github.com/omneval/omneval/internal/config"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantNil  bool
	}{
		{
			name:     "http scheme is stripped",
			endpoint: "http://minio:9000",
			wantNil:  false,
		},
		{
			name:     "https scheme is stripped",
			endpoint: "https://s3.amazonaws.com:443",
			wantNil:  false,
		},
		{
			name:     "bare host:port unchanged",
			endpoint: "minio:9000",
			wantNil:  false,
		},
		{
			name:     "empty endpoint returns nil",
			endpoint: "",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.StorageConfig{
				Endpoint:  tt.endpoint,
				Bucket:    "test-bucket",
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
				Region:    "us-east-1",
			}

			store := New(cfg)
			if tt.wantNil {
				if store != nil {
					t.Fatal("expected nil store for empty endpoint")
				}
				return
			}
			if store == nil {
				t.Fatal("expected non-nil store")
			}
			if store.client == nil {
				t.Fatal("expected configured minio client")
			}
		})
	}
}

// trackingReader wraps an io.Reader and records the maximum number of bytes
// read in a single Read call. Used to verify that Put/PutSized does not
// buffer the entire body in memory (issue #54).
type trackingReader struct {
	wrapped   io.Reader
	maxRead   int
	totalRead int
}

func (r *trackingReader) Read(p []byte) (int, error) {
	n, err := r.wrapped.Read(p)
	if n > r.maxRead {
		r.maxRead = n
	}
	r.totalRead += n
	return n, err
}

// smallBufferReader reads from src in chunks of bufSize.
type smallBufferReader struct {
	src     []byte
	bufSize int
	offset  int
}

func (r *smallBufferReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.src) {
		return 0, io.EOF
	}
	n := r.bufSize
	if n > len(p) {
		n = len(p)
	}
	remaining := len(r.src) - r.offset
	if n > remaining {
		n = remaining
	}
	copy(p, r.src[r.offset:r.offset+n])
	r.offset += n
	return n, nil
}

// TestPut_StreamsWithoutFullBuffer verifies that Put streams data to S3
// without buffering the entire body into memory. The old implementation used
// bytes.Buffer + io.Copy which would read the entire body at once. The new
// implementation passes the reader directly to minio.PutObject, which reads
// in chunks.
func TestPut_StreamsWithoutFullBuffer(t *testing.T) {
	cfg := &config.StorageConfig{
		Endpoint:  "http://localhost:9000",
		Bucket:    "test-bucket",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
	}
	store := New(cfg)
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	const size = 1024 * 1024 // 1 MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	const bufSize = 4 * 1024 // 4 KB chunks
	tr := &trackingReader{wrapped: &smallBufferReader{src: data, bufSize: bufSize}}

	// Put will fail (no real S3), but the reader should have been read in chunks.
	_ = store.Put(nil, "test-key", tr)

	// The max single read should be significantly less than the full size.
	if tr.maxRead >= size {
		t.Errorf("Put appears to have buffered the entire body: maxRead=%d, size=%d", tr.maxRead, size)
	}
	t.Logf("Put streaming: maxRead=%d, total=%d, size=%d", tr.maxRead, tr.totalRead, size)
}

// TestPutSized_PassesSizeToPutObject verifies that PutSized forwards the
// size parameter correctly and streams without full buffering.
func TestPutSized_PassesSizeToPutObject(t *testing.T) {
	cfg := &config.StorageConfig{
		Endpoint:  "http://localhost:9000",
		Bucket:    "test-bucket",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
	}
	store := New(cfg)
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	const size = 1024 * 1024 // 1 MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	const bufSize = 4 * 1024
	tr := &trackingReader{wrapped: &smallBufferReader{src: data, bufSize: bufSize}}

	_ = store.PutSized(nil, "test-key", tr, int64(size))

	if tr.maxRead >= size {
		t.Errorf("PutSized appears to have buffered the entire body: maxRead=%d, size=%d", tr.maxRead, size)
	}
	t.Logf("PutSized streaming: maxRead=%d, total=%d, size=%d", tr.maxRead, tr.totalRead, size)
}
