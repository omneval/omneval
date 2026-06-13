package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	miniocred "github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/lake/lakeservertest"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestLakeIntegration_PostgresCatalogMinIO is the ADR-0004 foundation
// proof: a span written through the Writer pipeline with dual-write
// enabled is readable from the Lake via a completely fresh DuckDB
// connection (Postgres Catalog + MinIO data path), with the hive
// partition layout the DSL compiler's pruning assumes.
func TestLakeIntegration_PostgresCatalogMinIO(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx, "postgres:17",
		tcpostgres.WithDatabase("lakecatalog"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	defer pgContainer.Terminate(ctx)

	pgHost, err := pgContainer.Host(ctx)
	if err != nil {
		t.Fatalf("postgres host: %v", err)
	}
	pgPort, err := pgContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("postgres port: %v", err)
	}
	catalogDSN := fmt.Sprintf("dbname=lakecatalog host=%s port=%s user=postgres password=postgres",
		pgHost, pgPort.Port())

	minioEndpoint := startMinIO(t, ctx)

	storage := &config.StorageConfig{
		Endpoint:  "http://" + minioEndpoint,
		Bucket:    "omneval-lake-test",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	}
	makeBucket(t, ctx, minioEndpoint, storage.Bucket)

	lakeCfg := lakeservertest.NewPostgres(t, catalogDSN)
	lakeCfg.DataPath = "s3://" + storage.Bucket + "/lake"
	lakeCfg.Storage = storage

	lk, err := lake.Open(ctx, lakeCfg)
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer lk.Close()

	// Legacy hot store alongside, as in dual-write production wiring.
	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	p := New(nil, db, testPricing, nil, nil, nil).WithLake(lk)

	span := &domain.Span{
		SpanID:       "span-lake-1",
		TraceID:      "trace-lake-1",
		ProjectID:    "proj-lake",
		Name:         "chat",
		Kind:         domain.SpanKind("llm"),
		StartTime:    time.Date(2026, 6, 7, 14, 30, 0, 0, time.UTC),
		EndTime:      time.Date(2026, 6, 7, 14, 30, 2, 0, time.UTC),
		Model:        "gpt-4o",
		Input:        `[{"role":"user","content":"hi"}]`,
		Output:       `{"role":"assistant","content":"hello"}`,
		InputTokens:  100,
		OutputTokens: 50,
	}
	if err := p.writeSpans(ctx, []*domain.Span{span}); err != nil {
		t.Fatalf("writeSpans: %v", err)
	}

	// Read back through a completely fresh DuckDB instance attached
	// read-only — the Query API's view of the Lake.
	roCfg := lakeCfg
	roCfg.ReadOnly = true
	ro, err := lake.Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open lake read-only: %v", err)
	}
	defer ro.Close()

	var gotModel string
	var gotCost float64
	err = ro.DB().QueryRowContext(ctx,
		"SELECT model, cost_usd FROM lake.spans WHERE span_id = 'span-lake-1'",
	).Scan(&gotModel, &gotCost)
	if err != nil {
		t.Fatalf("read span from lake: %v", err)
	}
	if gotModel != "gpt-4o" {
		t.Errorf("model: got %q, want gpt-4o", gotModel)
	}
	if gotCost <= 0 {
		t.Errorf("cost_usd: got %v, want > 0 (cost is computed at write time)", gotCost)
	}

	// Flush inlined data so the small insert materializes as a physical
	// Parquet file on S3 — DuckLake 1.5 inlines small inserts into the
	// catalog until flushed.
	if err := lk.FlushInlinedData(ctx); err != nil {
		t.Fatalf("flush inlined data: %v", err)
	}

	// Verify the partition layout on the object store itself.
	assertPartitionedObject(t, ctx, minioEndpoint, storage.Bucket,
		"project_id=proj-lake", "year=2026", "month=6", "day=7")
}

// startMinIO runs a MinIO container and returns its host:port endpoint.
func startMinIO(t *testing.T, ctx context.Context) string {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "minio/minio:latest",
		ExposedPorts: []string{"9000/tcp"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     "minioadmin",
			"MINIO_ROOT_PASSWORD": "minioadmin",
		},
		Cmd:        []string{"server", "/data"},
		WaitingFor: wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp"),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start minio: %v", err)
	}
	t.Cleanup(func() { c.Terminate(ctx) })

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("minio host: %v", err)
	}
	port, err := c.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatalf("minio port: %v", err)
	}
	return fmt.Sprintf("%s:%s", host, port.Port())
}

func minioClient(t *testing.T, endpoint string) *minio.Client {
	t.Helper()
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  miniocred.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	return mc
}

func makeBucket(t *testing.T, ctx context.Context, endpoint, bucket string) {
	t.Helper()
	if err := minioClient(t, endpoint).MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("make bucket: %v", err)
	}
}

// assertPartitionedObject lists the bucket and requires at least one
// Parquet object whose key contains every given partition segment.
func assertPartitionedObject(t *testing.T, ctx context.Context, endpoint, bucket string, segments ...string) {
	t.Helper()
	mc := minioClient(t, endpoint)

	var keys []string
	for obj := range mc.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			t.Fatalf("list objects: %v", obj.Err)
		}
		keys = append(keys, obj.Key)
		if !strings.HasSuffix(obj.Key, ".parquet") {
			continue
		}
		match := true
		for _, seg := range segments {
			if !strings.Contains(obj.Key, seg) {
				match = false
				break
			}
		}
		if match {
			return
		}
	}
	t.Errorf("no parquet object with partition segments %v; keys: %v", segments, keys)
}
