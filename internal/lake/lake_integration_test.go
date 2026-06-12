package lake

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestDeleteProjectNoResurrection proves that, against the Postgres Catalog
// (the production configuration), an admin delete through a dedicated
// read-write Lake attachment is visible immediately to an independent
// read-only attachment — and stays gone after a poll interval and a Table
// Maintenance pass (#91). This is the durability guarantee the legacy
// snapshot-local DELETE never had: deleted rows do not resurrect.
func TestDeleteProjectNoResurrection(t *testing.T) {
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

	host, err := pgContainer.Host(ctx)
	if err != nil {
		t.Fatalf("postgres host: %v", err)
	}
	port, err := pgContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("postgres port: %v", err)
	}
	catalogDSN := fmt.Sprintf("dbname=lakecatalog host=%s port=%s user=postgres password=postgres",
		host, port.Port())

	dataDir := t.TempDir()
	cfg := Config{
		CatalogDriver: CatalogDriverPostgres,
		CatalogDSN:    catalogDSN,
		DataPath:      dataDir,
	}

	// adminLake is the dedicated read-write attachment used for admin
	// deletes (the same role as services/query's AdminLake).
	adminLake, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open admin lake: %v", err)
	}
	defer adminLake.Close()

	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := adminLake.InsertSpans(ctx, []*domain.Span{
		testSpan("proj-a", "s1", start),
		testSpan("proj-b", "s2", start),
	}); err != nil {
		t.Fatalf("insert spans: %v", err)
	}

	// queryLake stands in for the Query API's long-lived read-only
	// attachment, opened once before the delete.
	roCfg := cfg
	roCfg.ReadOnly = true
	queryLake, err := Open(ctx, roCfg)
	if err != nil {
		t.Fatalf("open query lake: %v", err)
	}
	defer queryLake.Close()

	var n int
	if err := queryLake.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans").Scan(&n); err != nil {
		t.Fatalf("count before delete: %v", err)
	}
	if n != 2 {
		t.Fatalf("spans before delete: got %d, want 2", n)
	}

	if err := adminLake.DeleteProject(ctx, "proj-a"); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	// The pre-existing read-only attachment sees the deletion immediately —
	// no resurrection on a later poll, and no reattach required.
	if err := queryLake.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE project_id = 'proj-a'").Scan(&n); err != nil {
		t.Fatalf("count proj-a after delete: %v", err)
	}
	if n != 0 {
		t.Errorf("proj-a spans immediately after delete: got %d, want 0", n)
	}

	// Simulate a poll interval / Table Maintenance pass: re-query after a
	// short delay and after a second snapshot-expiry pass on the admin
	// connection. proj-a must still be gone.
	time.Sleep(100 * time.Millisecond)
	if err := adminLake.reclaim(ctx); err != nil {
		t.Fatalf("second reclaim pass: %v", err)
	}
	if err := queryLake.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE project_id = 'proj-a'").Scan(&n); err != nil {
		t.Fatalf("count proj-a after maintenance: %v", err)
	}
	if n != 0 {
		t.Errorf("proj-a spans resurrected after maintenance pass: got %d, want 0", n)
	}

	// proj-b is untouched throughout.
	if err := queryLake.DB().QueryRowContext(ctx, "SELECT count(*) FROM lake.spans WHERE project_id = 'proj-b'").Scan(&n); err != nil {
		t.Fatalf("count proj-b: %v", err)
	}
	if n != 1 {
		t.Errorf("proj-b spans: got %d, want 1", n)
	}
}
