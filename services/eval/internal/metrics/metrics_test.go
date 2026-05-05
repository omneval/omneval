package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/zbloss/lantern/internal/config"
)

func TestRegister_Metrics(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	prometheus.DefaultRegisterer.Unregister(JobDuration)
	prometheus.DefaultRegisterer.Unregister(JudgeErrors)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestRecordJobProcessed(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	prometheus.DefaultRegisterer.Unregister(JobDuration)
	prometheus.DefaultRegisterer.Unregister(JudgeErrors)
	defer prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	defer prometheus.DefaultRegisterer.Unregister(JobDuration)
	defer prometheus.DefaultRegisterer.Unregister(JudgeErrors)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewEvalMetrics(&config.Config{})

	m.RecordJobProcessed("proj-1", "rule-helpfulness")
	m.RecordJobProcessed("proj-1", "rule-accuracy")
	m.RecordJobProcessed("proj-2", "rule-helpfulness")

	expected := `
		# HELP lantern_eval_jobs_processed_total Total number of eval jobs processed by the judge.
		# TYPE lantern_eval_jobs_processed_total counter
		lantern_eval_jobs_processed_total{project_id="proj-1",rule_id="rule-helpfulness"} 1
		lantern_eval_jobs_processed_total{project_id="proj-1",rule_id="rule-accuracy"} 1
		lantern_eval_jobs_processed_total{project_id="proj-2",rule_id="rule-helpfulness"} 1
	`
	if err := testutil.GatherAndCompare(JobsProcessed, strings.NewReader(expected)); err != nil {
		t.Errorf("JobsProcessed: %v", err)
	}
}

func TestRecordJobProcessed_DisabledProjectLabels(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	prometheus.DefaultRegisterer.Unregister(JobDuration)
	prometheus.DefaultRegisterer.Unregister(JudgeErrors)
	defer prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	defer prometheus.DefaultRegisterer.Unregister(JobDuration)
	defer prometheus.DefaultRegisterer.Unregister(JudgeErrors)

	if err := Register(true); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewEvalMetrics(&config.Config{
		Metrics: config.MetricsConfig{
			DisableProjectLabels: true,
		},
	})

	m.RecordJobProcessed("any", "rule-1")
	m.RecordJobProcessed("any", "rule-2")

	expected := `
		# HELP lantern_eval_jobs_processed_total Total number of eval jobs processed by the judge.
		# TYPE lantern_eval_jobs_processed_total counter
		lantern_eval_jobs_processed_total{project_id="unlabeled",rule_id="rule-1"} 1
		lantern_eval_jobs_processed_total{project_id="unlabeled",rule_id="rule-2"} 1
	`
	if err := testutil.GatherAndCompare(JobsProcessed, strings.NewReader(expected)); err != nil {
		t.Errorf("JobsProcessed: %v", err)
	}
}

func TestRecordJobDuration(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	prometheus.DefaultRegisterer.Unregister(JobDuration)
	prometheus.DefaultRegisterer.Unregister(JudgeErrors)
	defer prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	defer prometheus.DefaultRegisterer.Unregister(JobDuration)
	defer prometheus.DefaultRegisterer.Unregister(JudgeErrors)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewEvalMetrics(&config.Config{})

	m.RecordJobDuration(2.5)
	m.RecordJobDuration(1.8)

	expected := `
		# HELP lantern_eval_job_duration_seconds Duration of eval jobs in seconds.
		# TYPE lantern_eval_job_duration_seconds histogram
		lantern_eval_job_duration_seconds_bucket{le="0.005"} 0
		lantern_eval_job_duration_seconds_bucket{le="0.01"} 0
		lantern_eval_job_duration_seconds_bucket{le="0.25"} 0
		lantern_eval_job_duration_seconds_bucket{le="0.5"} 0
		lantern_eval_job_duration_seconds_bucket{le="1"} 0
		lantern_eval_job_duration_seconds_bucket{le="2.5"} 2
		lantern_eval_job_duration_seconds_bucket{le="+Inf"} 2
		# HELP lantern_eval_job_duration_seconds_count Duration of eval jobs in seconds.
		# TYPE lantern_eval_job_duration_seconds_count counter
		lantern_eval_job_duration_seconds_count 2
		# HELP lantern_eval_job_duration_seconds_sum Duration of eval jobs in seconds.
		# TYPE lantern_eval_job_duration_seconds_sum counter
		lantern_eval_job_duration_seconds_sum 4.3
	`
	if err := testutil.GatherAndCompare(JobDuration, strings.NewReader(expected)); err != nil {
		t.Errorf("JobDuration: %v", err)
	}
}

func TestRecordJudgeError(t *testing.T) {
	prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	prometheus.DefaultRegisterer.Unregister(JobDuration)
	prometheus.DefaultRegisterer.Unregister(JudgeErrors)
	defer prometheus.DefaultRegisterer.Unregister(JobsProcessed)
	defer prometheus.DefaultRegisterer.Unregister(JobDuration)
	defer prometheus.DefaultRegisterer.Unregister(JudgeErrors)

	if err := Register(false); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := NewEvalMetrics(&config.Config{})

	m.RecordJudgeError()
	m.RecordJudgeError()
	m.RecordJudgeError()

	expected := `
		# HELP lantern_eval_judge_errors_total Total number of errors when calling the judge LLM.
		# TYPE lantern_eval_judge_errors_total counter
		lantern_eval_judge_errors_total 3
	`
	if err := testutil.GatherAndCompare(JudgeErrors, strings.NewReader(expected)); err != nil {
		t.Errorf("JudgeErrors: %v", err)
	}
}
