package backfill

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake"
)

// Main is the entry point for `writer backfill`. It loads the application
// config (OMNEVAL_CONFIG / omneval.yaml), derives the Lake connection and
// the legacy source locations, runs the backfill, prints the per-partition
// row-count report, and returns a nonzero exit code on any source-vs-Lake
// mismatch.
func Main(args []string) int {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	hotDB := fs.String("hot-db", "", "legacy hot DuckDB file (default: writer.duckdb_path from config)")
	archive := fs.String("archive", "", "cold Parquet archive root (default: s3://<storage.bucket>/archive)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfgPath := os.Getenv("OMNEVAL_CONFIG")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfill: load config: %v\n", err)
		return 1
	}

	opts := Options{HotDBPath: *hotDB, ArchiveRoot: *archive}
	if opts.HotDBPath == "" {
		opts.HotDBPath = cfg.Writer.DuckDBPath
		if opts.HotDBPath == "" {
			opts.HotDBPath = "omneval.db"
		}
	}
	if opts.ArchiveRoot == "" && cfg.Storage.Bucket != "" {
		opts.ArchiveRoot = "s3://" + cfg.Storage.Bucket + "/archive"
	}

	report, err := Run(context.Background(), lake.ConfigFromApp(cfg), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfill: %v\n", err)
		return 1
	}

	report.Print(os.Stdout)
	if mismatched := report.Mismatched(); len(mismatched) > 0 {
		fmt.Fprintf(os.Stderr, "backfill: %d partition(s) mismatched between source and Lake\n", len(mismatched))
		return 1
	}
	fmt.Printf("backfill: %d partition(s) verified, source and Lake counts match\n", len(report.Partitions))
	return 0
}
