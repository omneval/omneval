package s3

import (
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
