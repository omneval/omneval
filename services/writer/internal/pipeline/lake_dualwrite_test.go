package pipeline

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/duckdb"
	"github.com/omneval/omneval/internal/lake"
)

func dualWriteSpan(spanID string) *domain.Span {
	return &domain.Span{
		SpanID:       spanID,
		TraceID:      "trace-" + spanID,
		ProjectID:    "proj-1",
		Name:         "llm-call",
		Kind:         domain.SpanKind("llm"),
		StartTime:    time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2026, 6, 5, 10, 0, 1, 0, time.UTC),
		Model:        "gpt-4o",
		InputTokens:  100,
		OutputTokens: 50,
	}
}

// TestDualWrite_SpansLandInBothStores proves that with a Lake attached,
// a batch lands in the legacy DuckDB store and the Lake, with the same
// pre-computed cost in both.
func TestDualWrite_SpansLandInBothStores(t *testing.T) {
	ctx := context.Background()

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	dir := t.TempDir()
	lk, err := lake.Open(ctx, lake.Config{
		CatalogDriver: lake.CatalogDriverLocal,
		CatalogDSN:    filepath.Join(dir, "catalog.ducklake"),
		DataPath:      filepath.Join(dir, "data"),
	})
	if err != nil {
		t.Fatalf("open lake: %v", err)
	}
	defer lk.Close()

	p := New(nil, db, testPricing, nil, nil, nil).WithLake(lk)

	if err := p.writeSpans(ctx, []*domain.Span{dualWriteSpan("s1")}); err != nil {
		t.Fatalf("writeSpans: %v", err)
	}

	var legacyCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM spans").Scan(&legacyCount); err != nil {
		t.Fatalf("legacy count: %v", err)
	}
	if legacyCount != 1 {
		t.Errorf("legacy spans: got %d, want 1", legacyCount)
	}

	var lakeCount int
	var lakeCost float64
	if err := lk.DB().QueryRowContext(ctx,
		"SELECT count(*), max(cost_usd) FROM lake.spans",
	).Scan(&lakeCount, &lakeCost); err != nil {
		t.Fatalf("lake count: %v", err)
	}
	if lakeCount != 1 {
		t.Errorf("lake spans: got %d, want 1", lakeCount)
	}

	var legacyCost float64
	if err := db.QueryRowContext(ctx, "SELECT cost_usd FROM spans").Scan(&legacyCost); err != nil {
		t.Fatalf("legacy cost: %v", err)
	}
	if legacyCost != lakeCost {
		t.Errorf("cost mismatch: legacy=%v lake=%v", legacyCost, lakeCost)
	}
	if legacyCost == 0 {
		t.Error("cost was not computed before dual-write")
	}
}

type failingLake struct{}

func (failingLake) InsertSpans(context.Context, []*domain.Span) error {
	return errors.New("lake unavailable")
}

// TestDualWrite_LakeFailureKeepsLegacyWrite proves a lake-write failure
// does not fail the batch: the legacy write stands and writeSpans
// returns nil.
func TestDualWrite_LakeFailureKeepsLegacyWrite(t *testing.T) {
	ctx := context.Background()

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	p := New(nil, db, testPricing, nil, nil, nil).WithLake(failingLake{})

	if err := p.writeSpans(ctx, []*domain.Span{dualWriteSpan("s1")}); err != nil {
		t.Fatalf("writeSpans returned error on lake failure: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM spans").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("legacy spans: got %d, want 1", n)
	}
}

// TestDualWrite_NoLakeIsLegacyOnly proves behavior with the flag off is
// unchanged: no lake, no error, legacy write only.
func TestDualWrite_NoLakeIsLegacyOnly(t *testing.T) {
	ctx := context.Background()

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	p := New(nil, db, testPricing, nil, nil, nil)

	if err := p.writeSpans(ctx, []*domain.Span{dualWriteSpan("s1")}); err != nil {
		t.Fatalf("writeSpans: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM spans").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("legacy spans: got %d, want 1", n)
	}
}
