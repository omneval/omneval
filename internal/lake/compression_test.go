package lake

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/domain"
)

// TestEnsureTablesUsesZstdCompression proves that Parquet files written by
// the Lake (via ensureTables' ducklake_set_option call) use zstd compression
// instead of DuckLake's snappy default. Span `input`/`output` columns hold
// LLM prompt/completion text, which is exactly the kind of repetitive,
// structured text zstd compresses meaningfully better than snappy — and
// since cost is pre-computed once at write time (never recomputed), there's
// no query-time downside to paying zstd's slightly higher decode cost.
func TestEnsureTablesUsesZstdCompression(t *testing.T) {
	ctx := context.Background()
	cfg, _ := startTestServer(t)

	l, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	start := time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC)
	if err := l.InsertSpans(ctx, []*domain.Span{testSpan("proj-a", "s1", start)}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := l.FlushInlinedData(ctx); err != nil {
		t.Fatalf("flush inlined data: %v", err)
	}

	var parquetPath string
	err = filepath.WalkDir(cfg.DataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".parquet") {
			parquetPath = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk data path: %v", err)
	}
	if parquetPath == "" {
		t.Fatalf("no parquet file found under %s", cfg.DataPath)
	}

	rows, err := l.DB().QueryContext(ctx, "SELECT DISTINCT compression FROM parquet_metadata("+sqlQuote(parquetPath)+")")
	if err != nil {
		t.Fatalf("read parquet metadata: %v", err)
	}
	defer rows.Close()

	var codecs []string
	for rows.Next() {
		var codec string
		if err := rows.Scan(&codec); err != nil {
			t.Fatalf("scan codec: %v", err)
		}
		codecs = append(codecs, codec)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(codecs) == 0 {
		t.Fatal("no columns found in parquet metadata")
	}
	for _, codec := range codecs {
		if codec != "ZSTD" {
			t.Errorf("parquet column compression: got %q, want ZSTD (all codecs: %v)", codec, codecs)
		}
	}
}
