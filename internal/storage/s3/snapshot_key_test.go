package s3

import (
	"testing"
)

func TestSnapshotKey_AlwaysReturnsDuckDBSnapshotPath(t *testing.T) {
	got := SnapshotKey()
	want := "snapshots/duckdb.db"
	if got != want {
		t.Errorf("SnapshotKey() = %q, want %q", got, want)
	}
}

func TestSnapshotKey_NoRegionPrefix(t *testing.T) {
	// SnapshotKey should NOT include a region prefix — the region is an
	// S3 bucket property, not an object key property.
	got := SnapshotKey()
	if got == "" {
		t.Error("SnapshotKey() returned empty string")
	}
	if len(got) == 0 || got[0] == '/' {
		t.Errorf("SnapshotKey() should not have a leading slash: %q", got)
	}
}
