package s3

import (
	"testing"

	"github.com/zbloss/lantern/internal/config"
)

func TestNew_EndpointWithHTTPScheme(t *testing.T) {
	// Endpoint includes http:// scheme — should be stripped before
	// passing to minio.New, which expects bare host:port.
	cfg := &config.StorageConfig{
		Endpoint:  "http://minio:9000",
		Bucket:    "test-bucket",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
	}

	store := New(cfg)
	if store == nil {
		t.Fatal("New returned nil for endpoint with http:// scheme; expected non-nil store")
	}
	if store.client == nil {
		t.Fatal("store.client is nil; expected configured minio client")
	}
}

func TestNew_EndpointWithHTTPSScheme(t *testing.T) {
	// Endpoint includes https:// scheme — should also be stripped.
	cfg := &config.StorageConfig{
		Endpoint:  "https://s3.amazonaws.com:443",
		Bucket:    "test-bucket",
		AccessKey: "test-key",
		SecretKey: "test-secret",
		Region:    "us-east-1",
	}

	store := New(cfg)
	if store == nil {
		t.Fatal("New returned nil for endpoint with https:// scheme; expected non-nil store")
	}
	if store.client == nil {
		t.Fatal("store.client is nil; expected configured minio client")
	}
}

func TestNew_EndpointWithoutScheme(t *testing.T) {
	// Bare host:port — should still work (regression test).
	cfg := &config.StorageConfig{
		Endpoint:  "minio:9000",
		Bucket:    "test-bucket",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
	}

	store := New(cfg)
	if store == nil {
		t.Fatal("New returned nil for bare host:port endpoint; expected non-nil store")
	}
	if store.client == nil {
		t.Fatal("store.client is nil; expected configured minio client")
	}
}

func TestNew_EmptyEndpoint(t *testing.T) {
	// No endpoint configured — should return nil (no error expected).
	cfg := &config.StorageConfig{}
	store := New(cfg)
	if store != nil {
		t.Fatal("New returned non-nil for empty endpoint; expected nil")
	}
}
