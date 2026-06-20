package lake

import (
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/config"
)

// TestS3SecretSQL_CloudflareR2UsesNativeSecretType proves that, when the
// configured endpoint is a Cloudflare R2 endpoint
// (<account_id>.r2.cloudflarestorage.com), s3SecretSQL emits a native
// "TYPE r2" secret with ACCOUNT_ID instead of the generic "TYPE s3" +
// ENDPOINT/URL_STYLE combination.
//
// This matters because DuckLake's internal S3 calls have a documented bug
// (duckdb/ducklake#562, see lakeserver/maintenance.go) where some glob/list
// operations against a generic S3 secret with a custom ENDPOINT ignore that
// secret's URL_STYLE/REGION and fall back to AWS virtual-hosted-style
// requests, which 404s against non-AWS endpoints. DuckDB's purpose-built R2
// secret type derives the correct endpoint from ACCOUNT_ID internally and
// isn't subject to the same fallback path.
func TestS3SecretSQL_CloudflareR2UsesNativeSecretType(t *testing.T) {
	sc := &config.StorageConfig{
		Endpoint:  "https://dee226c52e8c33561dacbbc793a9d207.r2.cloudflarestorage.com",
		Bucket:    "omneval-homelab",
		AccessKey: "key123",
		SecretKey: "secret456",
	}

	got := s3SecretSQL(sc)

	if !strings.Contains(got, "TYPE r2") {
		t.Errorf("s3SecretSQL for R2 endpoint: want TYPE r2, got: %s", got)
	}
	if !strings.Contains(got, "ACCOUNT_ID 'dee226c52e8c33561dacbbc793a9d207'") {
		t.Errorf("s3SecretSQL for R2 endpoint: want ACCOUNT_ID extracted from host, got: %s", got)
	}
	if strings.Contains(got, "ENDPOINT") || strings.Contains(got, "URL_STYLE") {
		t.Errorf("s3SecretSQL for R2 endpoint: ENDPOINT/URL_STYLE should not be needed with TYPE r2, got: %s", got)
	}
	if !strings.Contains(got, "KEY_ID 'key123'") || !strings.Contains(got, "SECRET 'secret456'") {
		t.Errorf("s3SecretSQL for R2 endpoint: credentials missing, got: %s", got)
	}
}

// TestS3SecretSQL_GenericS3EndpointUnchanged proves that non-R2 S3-compatible
// endpoints (e.g. a homelab MinIO instance) keep using the existing generic
// "TYPE s3" + ENDPOINT/URL_STYLE secret — this is a regression guard for the
// MinIO setup the project ran before migrating to R2.
func TestS3SecretSQL_GenericS3EndpointUnchanged(t *testing.T) {
	sc := &config.StorageConfig{
		Endpoint:  "http://minio.omneval.svc:9000",
		Bucket:    "omneval",
		Region:    "us-east-1",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	}

	got := s3SecretSQL(sc)

	if !strings.Contains(got, "TYPE s3") {
		t.Errorf("s3SecretSQL for MinIO endpoint: want TYPE s3, got: %s", got)
	}
	if !strings.Contains(got, "ENDPOINT 'minio.omneval.svc:9000'") {
		t.Errorf("s3SecretSQL for MinIO endpoint: want ENDPOINT, got: %s", got)
	}
	if !strings.Contains(got, "URL_STYLE 'path'") {
		t.Errorf("s3SecretSQL for MinIO endpoint: want URL_STYLE 'path', got: %s", got)
	}
	if !strings.Contains(got, "USE_SSL false") {
		t.Errorf("s3SecretSQL for MinIO endpoint: want USE_SSL false for http://, got: %s", got)
	}
	if strings.Contains(got, "TYPE r2") {
		t.Errorf("s3SecretSQL for MinIO endpoint: must not use TYPE r2, got: %s", got)
	}
}

// TestS3SecretSQL_NoEndpointUsesAWS proves that with no endpoint configured
// (plain AWS S3), s3SecretSQL emits a bare TYPE s3 secret with no
// ENDPOINT/URL_STYLE/ACCOUNT_ID at all.
func TestS3SecretSQL_NoEndpointUsesAWS(t *testing.T) {
	sc := &config.StorageConfig{
		Bucket:    "omneval-prod",
		Region:    "us-west-2",
		AccessKey: "AKIA...",
		SecretKey: "secret",
	}

	got := s3SecretSQL(sc)

	if !strings.Contains(got, "TYPE s3") {
		t.Errorf("s3SecretSQL with no endpoint: want TYPE s3, got: %s", got)
	}
	if strings.Contains(got, "ENDPOINT") || strings.Contains(got, "ACCOUNT_ID") {
		t.Errorf("s3SecretSQL with no endpoint: should not set ENDPOINT/ACCOUNT_ID, got: %s", got)
	}
}
