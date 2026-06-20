package s3

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/omneval/omneval/internal/config"
)

// TestDeleteObjectsBatch_UsesIndividualDeleteCalls proves that
// DeleteObjectsBatch issues one single-object DELETE request per key instead
// of a single batch POST .../?delete request.
//
// This matters because Cloudflare R2's S3-compatible DeleteObjects (batch)
// API has a long-standing, documented bug: it returns HTTP 200 with the
// requested keys listed as deleted, but never actually removes the objects
// (reported on Cloudflare's community forum since 2023). Single-object
// DeleteObject calls are confirmed reliable against R2. Since this Store is
// also used against R2, it must never rely on the batch endpoint.
func TestDeleteObjectsBatch_UsesIndividualDeleteCalls(t *testing.T) {
	var mu sync.Mutex
	var deletedKeys []string
	sawBatchDeleteRequest := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, hasDeleteParam := r.URL.Query()["delete"]; hasDeleteParam {
			mu.Lock()
			sawBatchDeleteRequest = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodDelete {
			mu.Lock()
			key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
			key, _ = url.PathUnescape(key)
			deletedKeys = append(deletedKeys, key)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.StorageConfig{
		Endpoint:  "http://" + srv.Listener.Addr().String(),
		Bucket:    "test-bucket",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
	}
	store := New(cfg)
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	keys := []string{"a/key1.parquet", "a/key2.parquet", "a/key3.parquet"}
	if err := store.DeleteObjectsBatch(nil, "test-bucket", keys); err != nil {
		t.Fatalf("DeleteObjectsBatch: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if sawBatchDeleteRequest {
		t.Error("DeleteObjectsBatch issued a batch DeleteObjects (?delete) request — this hits the documented Cloudflare R2 bug where batch deletes silently no-op")
	}
	if len(deletedKeys) != len(keys) {
		t.Errorf("expected %d individual DELETE requests, got %d: %v", len(keys), len(deletedKeys), deletedKeys)
	}
}
