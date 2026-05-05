package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/zbloss/lantern/internal/config"
)

func TestRegister_Metrics(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansWritten)
	prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	prometheus.DefaultRegisterer.Unregister(ArchiveSpans)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestRecordSpansWritten(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansWritten)
	prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	prometheus.DefaultRegisterer.Unregister(ArchiveSpans)
	defer prometheus.DefaultRegisterer.Unregister(SpansWritten)
	defer prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	defer prometheus.DefaultRegisterer.Unregister(ArchiveSpans)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewWriterMetrics(&config.Config{})

	m.RecordSpansWritten("proj-1", 10)
	m.RecordSpansWritten("proj-2", 5)

	expected := `
		# HELP lantern_writer_spans_written_total Total number of spans written to DuckDB.
		# TYPE lantern_writer_spans_written_total counter
		lantern_writer_spans_written_total{project_id="proj-1"} 10
		lantern_writer_spans_written_total{project_id="proj-2"} 5
	`
	if err := testutil.GatherAndCompare(SpansWritten, strings.NewReader(expected)); err != nil {
		t.Errorf("SpansWritten: %v", err)
	}
}

func TestRecordSpansWritten_DisabledProjectLabels(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansWritten)
	prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	prometheus.DefaultRegisterer.Unregister(ArchiveSpans)
	defer prometheus.DefaultRegisterer.Unregister(SpansWritten)
	defer prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	defer prometheus.DefaultRegisterer.Unregister(ArchiveSpans)

	if err := Register(true); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewWriterMetrics(&config.Config{
		Metrics: config.MetricsConfig{
			DisableProjectLabels: true,
		},
	})

	m.RecordSpansWritten("any", 7)

	expected := `
		# HELP lantern_writer_spans_written_total Total number of spans written to DuckDB.
		# TYPE lantern_writer_spans_written_total counter
		lantern_writer_spans_written_total{project_id="unlabeled"} 7
	`
	if err := testutil.GatherAndCompare(SpansWritten, strings.NewReader(expected)); err != nil {
		t.Errorf("SpansWritten: %v", err)
	}
}

func TestRecordDuckDBWriteDuration(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansWritten)
	prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	prometheus.DefaultRegisterer.Unregister(ArchiveSpans)
	defer prometheus.DefaultRegisterer.Unregister(SpansWritten)
	defer prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	defer prometheus.DefaultRegisterer.Unregister(ArchiveSpans)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewWriterMetrics(&config.Config{})

	m.RecordDuckDBWriteDuration(0.05)
	m.RecordDuckDBWriteDuration(0.12)

	expected := `
		# HELP lantern_writer_duckdb_write_duration_seconds Duration of DuckDB write transactions in seconds.
		# TYPE lantern_writer_duckdb_write_duration_seconds histogram
		lantern_writer_duckdb_write_duration_seconds_bucket{le="0.005"} 0
		lantern_writer_duckdb_write_duration_seconds_bucket{le="0.01"} 0
		lantern_writer_duckdb_write_duration_seconds_bucket{le="0.025"} 0
		lantern_writer_duckdb_write_duration_seconds_bucket{le="0.05"} 1
		lantern_writer_duckdb_write_duration_seconds_bucket{le="0.1"} 1
		lantern_writer_duckdb_write_duration_seconds_bucket{le="0.25"} 2
		lantern_writer_duckdb_write_duration_seconds_bucket{le="+Inf"} 2
		# HELP lantern_writer_duckdb_write_duration_seconds_count Duration of DuckDB write transactions in seconds.
		# TYPE lantern_writer_duckdb_write_duration_seconds_count counter
		lantern_writer_duckdb_write_duration_seconds_count 2
		# HELP lantern_writer_duckdb_write_duration_seconds_sum Duration of DuckDB write transactions in seconds.
		# TYPE lantern_writer_duckdb_write_duration_seconds_sum counter
		lantern_writer_duckdb_write_duration_seconds_sum 0.17
	`
	if err := testutil.GatherAndCompare(DuckDBWriteDuration, strings.NewReader(expected)); err != nil {
		t.Errorf("DuckDBWriteDuration: %v", err)
	}
}

func TestRecordSnapshotSyncDuration(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansWritten)
	prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	prometheus.DefaultRegisterer.Unregister(ArchiveSpans)
	defer prometheus.DefaultRegisterer.Unregister(SpansWritten)
	defer prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	defer prometheus.DefaultRegisterer.Unregister(ArchiveSpans)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewWriterMetrics(&config.Config{})

	m.RecordSnapshotSyncDuration(0.3, "success")
	m.RecordSnapshotSyncDuration(0.5, "success")
	m.RecordSnapshotSyncDuration(1.2, "error")

	expected := `
		# HELP lantern_writer_snapshot_sync_duration_seconds Duration of DuckDB snapshot sync to S3 in seconds.
		# TYPE lantern_writer_snapshot_sync_duration_seconds histogram
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="success",le="0.005"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="success",le="0.01"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="success",le="0.25"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="success",le="0.5"} 1
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="success",le="1"} 2
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="success",le="+Inf"} 2
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="error",le="0.005"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="error",le="0.01"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="error",le="0.25"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="error",le="0.5"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="error",le="1"} 0
		lantern_writer_snapshot_sync_duration_seconds_bucket{status="error",le="+Inf"} 1
		# HELP lantern_writer_snapshot_sync_duration_seconds_count Duration of DuckDB snapshot sync to S3 in seconds.
		# TYPE lantern_writer_snapshot_sync_duration_seconds_count counter
		lantern_writer_snapshot_sync_duration_seconds_count{status="success"} 2
		lantern_writer_snapshot_sync_duration_seconds_count{status="error"} 1
		# HELP lantern_writer_snapshot_sync_duration_seconds_sum Duration of DuckDB snapshot sync to S3 in seconds.
		# TYPE lantern_writer_snapshot_sync_duration_seconds_sum counter
		lantern_writer_snapshot_sync_duration_seconds_sum{status="success"} 0.8
		lantern_writer_snapshot_sync_duration_seconds_sum{status="error"} 1.2
	`
	if err := testutil.GatherAndCompare(SnapshotSyncDuration, strings.NewReader(expected)); err != nil {
		t.Errorf("SnapshotSyncDuration: %v", err)
	}
}

func TestRecordArchiveSpans(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(SpansWritten)
	prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	prometheus.DefaultRegisterer.Unregister(ArchiveSpans)
	defer prometheus.DefaultRegisterer.Unregister(SpansWritten)
	defer prometheus.DefaultRegisterer.Unregister(DuckDBWriteDuration)
	defer prometheus.DefaultRegisterer.Unregister(SnapshotSyncDuration)
	defer prometheus.DefaultRegisterer.Unregister(ArchiveSpans)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewWriterMetrics(&config.Config{})

	m.RecordArchiveSpans("proj-1", 100)
	m.RecordArchiveSpans("proj-2", 50)

	expected := `
		# HELP lantern_writer_archive_spans_total Total number of spans archived to Parquet on S3.
		# TYPE lantern_writer_archive_spans_total counter
		lantern_writer_archive_spans_total{project_id="proj-1"} 100
		lantern_writer_archive_spans_total{project_id="proj-2"} 50
	`
	if err := testutil.GatherAndCompare(ArchiveSpans, strings.NewReader(expected)); err != nil {
		t.Errorf("ArchiveSpans: %v", err)
	}
}
