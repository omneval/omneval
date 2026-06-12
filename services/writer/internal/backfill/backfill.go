// Package backfill implements the one-off data migration from ADR-0004:
// it reads the legacy stores — the hot DuckDB file and the Hive-partitioned
// cold Parquet archive — and inserts their spans and scores into the Lake,
// preserving the (project_id, span-date) partitioning.
//
// Idempotency is delete-and-rewrite per partition: re-running the command
// produces identical Lake row counts. The hot-window overlap (a span
// present in both tiers) is deduplicated on (trace_id, span_id) before
// insertion.
package backfill

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/omneval/omneval/internal/lake"
)

// spanColumns is the shared column list of the legacy spans schema, the
// cold Parquet files, and lake.spans — same names, same order.
const spanColumns = "span_id, trace_id, parent_id, conversation_id, project_id, service_name, " +
	"name, kind, start_time, end_time, model, input, output, input_tokens, output_tokens, " +
	"cost_usd, prompt_name, prompt_version, status_code, status_message, attributes"

// scoreColumns is the legacy scores column list (lake.scores adds
// span_start_time, which the backfill derives by joining spans).
const scoreColumns = "score_id, span_id, trace_id, project_id, eval_name, value, " +
	"reasoning, judge_model, prompt_name, prompt_version, created_at"

// Options selects the legacy sources to read.
type Options struct {
	// HotDBPath is the legacy hot DuckDB file. Empty or missing file: the
	// hot tier is skipped.
	HotDBPath string
	// ArchiveRoot is the root of the Hive-partitioned cold archive —
	// "s3://bucket/archive" in production, or a local directory in tests.
	// Empty or matching no files: the cold tier is skipped.
	ArchiveRoot string
}

// PartitionReport is the source-vs-Lake row count for one (project, date)
// partition.
type PartitionReport struct {
	ProjectID    string
	Date         string
	SourceSpans  int64
	LakeSpans    int64
	SourceScores int64
	LakeScores   int64
}

// Mismatch reports whether the Lake row counts differ from the source.
func (p PartitionReport) Mismatch() bool {
	return p.SourceSpans != p.LakeSpans || p.SourceScores != p.LakeScores
}

// Report is the per-partition outcome of a backfill run.
type Report struct {
	Partitions []PartitionReport
}

// Mismatched returns the partitions whose Lake counts differ from source.
func (r *Report) Mismatched() []PartitionReport {
	var out []PartitionReport
	for _, p := range r.Partitions {
		if p.Mismatch() {
			out = append(out, p)
		}
	}
	return out
}

// Print writes the per-partition report as an aligned table.
func (r *Report) Print(w io.Writer) {
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROJECT\tDATE\tSRC SPANS\tLAKE SPANS\tSRC SCORES\tLAKE SCORES\tSTATUS")
	for _, p := range r.Partitions {
		status := "ok"
		if p.Mismatch() {
			status = "MISMATCH"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
			p.ProjectID, p.Date, p.SourceSpans, p.LakeSpans, p.SourceScores, p.LakeScores, status)
	}
	tw.Flush()
}

// Run backfills the legacy stores into the Lake and returns the
// per-partition row-count report. The Lake is opened read-write from cfg;
// re-running is safe (delete-and-rewrite per partition).
func Run(ctx context.Context, lakeCfg lake.Config, opts Options) (*Report, error) {
	lk, err := lake.Open(ctx, lakeCfg)
	if err != nil {
		return nil, fmt.Errorf("backfill: open lake: %w", err)
	}
	defer lk.Close()
	return RunWithLake(ctx, lk, opts)
}

// RunWithLake backfills using an already-open Lake handle.
func RunWithLake(ctx context.Context, lk *lake.Lake, opts Options) (*Report, error) {
	db := lk.DB()

	// Source 1: hot DuckDB, attached read-only so the backfill can never
	// damage the legacy store.
	hot := false
	if opts.HotDBPath != "" {
		if _, err := os.Stat(opts.HotDBPath); err == nil {
			if _, err := db.ExecContext(ctx,
				fmt.Sprintf("ATTACH IF NOT EXISTS %s AS hot (READ_ONLY)", sqlQuote(opts.HotDBPath))); err != nil {
				return nil, fmt.Errorf("backfill: attach hot duckdb: %w", err)
			}
			hot = true
		}
	}

	// Source 2: cold Parquet archive. Spans and scores live under
	// distinct subdirectories, so each gets a precise glob — Hive
	// partition values (project_id, date) are redundant with in-file
	// columns, so hive_partitioning stays off and no column conflicts
	// arise.
	spanGlob := ""
	scoreGlob := ""
	if opts.ArchiveRoot != "" {
		root := strings.TrimRight(opts.ArchiveRoot, "/")
		if g := root + "/project_id=*/date=*/spans/*.parquet"; globHasFiles(ctx, db, g) {
			spanGlob = g
		}
		if g := root + "/project_id=*/date=*/scores/*.parquet"; globHasFiles(ctx, db, g) {
			scoreGlob = g
		}
	}

	if !hot && spanGlob == "" {
		return nil, fmt.Errorf("backfill: no legacy sources found (hot db %q, archive %q)", opts.HotDBPath, opts.ArchiveRoot)
	}

	hasScores, err := createSourceViews(ctx, db, hot, spanGlob, scoreGlob)
	if err != nil {
		return nil, err
	}

	report := &Report{}
	if err := backfillSpans(ctx, db, report); err != nil {
		return nil, err
	}
	if hasScores {
		if err := backfillScores(ctx, db, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// createSourceViews builds the deduplicated union of the legacy tiers.
// The second return reports whether any legacy score source exists.
func createSourceViews(ctx context.Context, db *sql.DB, hot bool, spanGlob, scoreGlob string) (bool, error) {
	var spanParts, scoreParts []string
	if hot {
		spanParts = append(spanParts, "SELECT "+spanColumns+" FROM hot.spans")
		scoreParts = append(scoreParts, "SELECT "+scoreColumns+" FROM hot.scores")
	}
	if spanGlob != "" {
		spanParts = append(spanParts,
			"SELECT "+spanColumns+" FROM read_parquet("+sqlQuote(spanGlob)+")")
	}
	if scoreGlob != "" {
		scoreParts = append(scoreParts,
			"SELECT "+scoreColumns+" FROM read_parquet("+sqlQuote(scoreGlob)+")")
	}

	// Dedupe the hot-window overlap on the legacy primary keys.
	spansSQL := `CREATE OR REPLACE TEMP VIEW backfill_spans AS
		SELECT * EXCLUDE (rn) FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY trace_id, span_id ORDER BY start_time DESC) AS rn
			FROM (` + strings.Join(spanParts, "\nUNION ALL\n") + `)
		) WHERE rn = 1`
	if _, err := db.ExecContext(ctx, spansSQL); err != nil {
		return false, fmt.Errorf("backfill: create spans view: %w", err)
	}

	if len(scoreParts) == 0 {
		return false, nil
	}
	// A score partitions next to its span (ADR-0002): derive
	// span_start_time by joining the deduped spans, falling back to the
	// score's created_at when the span is unknown.
	scoresSQL := `CREATE OR REPLACE TEMP VIEW backfill_scores AS
		SELECT s.* EXCLUDE (rn), COALESCE(sp.start_time, s.created_at) AS span_start_time FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY score_id ORDER BY created_at DESC) AS rn
			FROM (` + strings.Join(scoreParts, "\nUNION ALL\n") + `)
		) s
		LEFT JOIN backfill_spans sp ON s.trace_id = sp.trace_id AND s.span_id = sp.span_id
		WHERE s.rn = 1`
	if _, err := db.ExecContext(ctx, scoresSQL); err != nil {
		return false, fmt.Errorf("backfill: create scores view: %w", err)
	}
	return true, nil
}

// backfillSpans delete-and-rewrites every legacy span partition into
// lake.spans and records source-vs-Lake counts.
func backfillSpans(ctx context.Context, db *sql.DB, report *Report) error {
	parts, err := listPartitions(ctx, db,
		"SELECT project_id, CAST(CAST(start_time AS DATE) AS VARCHAR), COUNT(*) FROM backfill_spans GROUP BY 1, 2 ORDER BY 1, 2")
	if err != nil {
		return fmt.Errorf("backfill: list span partitions: %w", err)
	}

	for _, pt := range parts {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("backfill: begin span partition %s/%s: %w", pt.project, pt.date, err)
		}
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM lake.spans WHERE project_id = ? AND CAST(start_time AS DATE) = CAST(? AS DATE)",
			pt.project, pt.date); err != nil {
			tx.Rollback()
			return fmt.Errorf("backfill: clear span partition %s/%s: %w", pt.project, pt.date, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO lake.spans ("+spanColumns+") SELECT "+spanColumns+
				" FROM backfill_spans WHERE project_id = ? AND CAST(start_time AS DATE) = CAST(? AS DATE)",
			pt.project, pt.date); err != nil {
			tx.Rollback()
			return fmt.Errorf("backfill: insert span partition %s/%s: %w", pt.project, pt.date, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("backfill: commit span partition %s/%s: %w", pt.project, pt.date, err)
		}

		var lakeCount int64
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM lake.spans WHERE project_id = ? AND CAST(start_time AS DATE) = CAST(? AS DATE)",
			pt.project, pt.date).Scan(&lakeCount); err != nil {
			return fmt.Errorf("backfill: verify span partition %s/%s: %w", pt.project, pt.date, err)
		}
		report.Partitions = append(report.Partitions, PartitionReport{
			ProjectID:   pt.project,
			Date:        pt.date,
			SourceSpans: pt.count,
			LakeSpans:   lakeCount,
		})
	}
	return nil
}

// backfillScores delete-and-rewrites every legacy score partition into
// lake.scores, partitioned by the annotated span's date, and merges the
// counts into the report.
func backfillScores(ctx context.Context, db *sql.DB, report *Report) error {
	parts, err := listPartitions(ctx, db,
		"SELECT project_id, CAST(CAST(span_start_time AS DATE) AS VARCHAR), COUNT(*) FROM backfill_scores GROUP BY 1, 2 ORDER BY 1, 2")
	if err != nil {
		return fmt.Errorf("backfill: list score partitions: %w", err)
	}

	for _, pt := range parts {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("backfill: begin score partition %s/%s: %w", pt.project, pt.date, err)
		}
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM lake.scores WHERE project_id = ? AND CAST(span_start_time AS DATE) = CAST(? AS DATE)",
			pt.project, pt.date); err != nil {
			tx.Rollback()
			return fmt.Errorf("backfill: clear score partition %s/%s: %w", pt.project, pt.date, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO lake.scores ("+scoreColumns+", span_start_time) SELECT "+scoreColumns+
				", span_start_time FROM backfill_scores WHERE project_id = ? AND CAST(span_start_time AS DATE) = CAST(? AS DATE)",
			pt.project, pt.date); err != nil {
			tx.Rollback()
			return fmt.Errorf("backfill: insert score partition %s/%s: %w", pt.project, pt.date, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("backfill: commit score partition %s/%s: %w", pt.project, pt.date, err)
		}

		var lakeCount int64
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM lake.scores WHERE project_id = ? AND CAST(span_start_time AS DATE) = CAST(? AS DATE)",
			pt.project, pt.date).Scan(&lakeCount); err != nil {
			return fmt.Errorf("backfill: verify score partition %s/%s: %w", pt.project, pt.date, err)
		}
		mergeScoreCounts(report, pt.project, pt.date, pt.count, lakeCount)
	}
	return nil
}

// mergeScoreCounts attaches score counts to the matching span partition
// row, or appends a score-only partition (scores whose span date has no
// span rows — possible when only the cold scores file survived).
func mergeScoreCounts(report *Report, project, date string, src, lake int64) {
	for i := range report.Partitions {
		if report.Partitions[i].ProjectID == project && report.Partitions[i].Date == date {
			report.Partitions[i].SourceScores = src
			report.Partitions[i].LakeScores = lake
			return
		}
	}
	report.Partitions = append(report.Partitions, PartitionReport{
		ProjectID:    project,
		Date:         date,
		SourceScores: src,
		LakeScores:   lake,
	})
}

type partition struct {
	project string
	date    string
	count   int64
}

func listPartitions(ctx context.Context, db *sql.DB, query string) ([]partition, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []partition
	for rows.Next() {
		var p partition
		if err := rows.Scan(&p.project, &p.date, &p.count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// globHasFiles reports whether the glob matches at least one file. A
// failing glob (e.g. nonexistent local directory) counts as no files.
func globHasFiles(ctx context.Context, db *sql.DB, pattern string) bool {
	var n int64
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM glob("+sqlQuote(pattern)+")").Scan(&n)
	return err == nil && n > 0
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
