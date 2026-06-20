package lake

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/omneval/omneval/internal/config"
)

// TestS3SecretSQL_CloudflareR2UsesNativeSecretType proves that, when the
// configured endpoint is a Cloudflare R2 endpoint
// (<account_id>.r2.cloudflarestorage.com), s3SecretSQL emits a native
// "TYPE r2" secret with ACCOUNT_ID instead of the generic "TYPE s3" +
// ENDPOINT/URL_STYLE combination, explicitly SCOPEd to the bucket's s3://
// URI.
//
// This matters because DuckLake's internal S3 calls have a documented bug
// (duckdb/ducklake#562, see lakeserver/maintenance.go) where some glob/list
// operations against a generic S3 secret with a custom ENDPOINT ignore that
// secret's URL_STYLE/REGION and fall back to AWS virtual-hosted-style
// requests, which 404s against non-AWS endpoints. DuckDB's purpose-built R2
// secret type derives the correct endpoint from ACCOUNT_ID internally and
// isn't subject to the same fallback path.
//
// The explicit SCOPE is required: an unscoped "TYPE r2" secret defaults to
// matching only "r2://" URIs (verified directly against duckdb_secrets()),
// but the Lake's DATA_PATH and every file DuckLake touches use "s3://"
// URIs. Without SCOPE, the secret silently never matches and DuckDB falls
// back to the default (anonymous) AWS credential chain against the literal
// "*.s3.amazonaws.com" endpoint — a real production incident this test
// guards against.
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
	if !strings.Contains(got, "SCOPE 's3://omneval-homelab'") {
		t.Errorf("s3SecretSQL for R2 endpoint: want SCOPE matching the bucket's s3:// URI so the secret actually applies, got: %s", got)
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

// TestS3SecretSQL_R2SecretScopeMatchesS3Paths proves, against a real DuckDB
// instance, that the secret s3SecretSQL emits for an R2 endpoint is
// actually applied by DuckDB's secret matcher to the "s3://<bucket>/..."
// paths the Lake uses — not just that the generated SQL contains a SCOPE
// clause that looks right. This is the exact gap that caused a production
// incident: an unscoped TYPE r2 secret matches only "r2://" by default, so
// every DuckLake S3 call silently fell through to the default AWS
// credential chain against "*.s3.amazonaws.com" instead of R2.
func TestS3SecretSQL_R2SecretScopeMatchesS3Paths(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	sc := &config.StorageConfig{
		Endpoint:  "https://dee226c52e8c33561dacbbc793a9d207.r2.cloudflarestorage.com",
		Bucket:    "omneval-homelab",
		AccessKey: "key123",
		SecretKey: "secret456",
	}

	for _, stmt := range []string{"INSTALL httpfs", "LOAD httpfs", s3SecretSQL(sc)} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	var matchedName string
	err = db.QueryRowContext(ctx,
		"SELECT name FROM which_secret('s3://omneval-homelab/lake/main/spans/x.parquet', 'r2')",
	).Scan(&matchedName)
	if err != nil {
		t.Fatalf("which_secret: %v", err)
	}
	if matchedName != "lake_s3" {
		t.Errorf("secret matched for an s3://omneval-homelab path: got %q, want %q", matchedName, "lake_s3")
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
