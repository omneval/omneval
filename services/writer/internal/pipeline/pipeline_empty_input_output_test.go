package pipeline

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zbloss/lantern/internal/domain"
	"github.com/zbloss/lantern/internal/duckdb"
)

// TestPipeline_WriteSpans_EmptyInputOutput verifies that spans with empty
// Input/Output strings do not cause the entire batch to fail. DuckDB rejects
// empty strings as invalid JSON in JSON columns, so empty values must be
// coerced to NULL.
func TestPipeline_WriteSpans_EmptyInputOutput(t *testing.T) {
	ctx := context.Background()

	db, err := duckdb.Open("test_empty_input.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer func() { db.Close(); os.Remove("test_empty_input.db") }()

	// Batch with 3 spans: one with input/output, one with empty input only,
	// and one with empty input and output (like a chain root span).
	spans := []*domain.Span{
		{
			TraceID:      "trace-00000000000000001",
			SpanID:       "span-00000000000000001",
			Model:        "gpt-4o",
			ProjectID:    "proj-1",
			Input:        `[{"role":"user","content":"Hello"}]`,
			Output:       `[{"role":"assistant","content":"Hi"}]`,
			InputTokens:  10,
			OutputTokens: 5,
		},
		{
			TraceID:      "trace-00000000000000001",
			SpanID:       "span-00000000000000002",
			Model:        "gpt-4o",
			ProjectID:    "proj-1",
			Name:         "tool-call",
			Input:        "",
			Output:       `[{"role":"tool","content":"tool result"}]`,
			InputTokens:  5,
			OutputTokens: 3,
		},
		{
			TraceID:      "trace-00000000000000001",
			SpanID:       "span-00000000000000003",
			Model:        "gpt-4o",
			ProjectID:    "proj-1",
			Name:         "chain-root",
			Input:        "",
			Output:       "",
			InputTokens:  0,
			OutputTokens: 0,
		},
	}

	p := &Pipeline{
		db:      db,
		pricing: testPricing,
		metrics: nil,
	}

	err = p.writeSpans(ctx, spans)
	if err != nil {
		t.Fatalf("writeSpans failed: %v", err)
	}

	// All 3 spans should be written.
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-1").Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 3 {
		t.Errorf("span count: got %d, want 3", count)
	}

	// Verify the span with empty Input/Output has NULL in DuckDB.
	var span3Input, span3Output *string
	err = db.QueryRowContext(ctx,
		"SELECT input, output FROM spans WHERE span_id = $1", "span-00000000000000003").
		Scan(&span3Input, &span3Output)
	if err != nil {
		t.Fatalf("scan span3 input/output: %v", err)
	}
	if span3Input != nil {
		t.Errorf("span3 input: got %q, want nil (NULL in DuckDB)", *span3Input)
	}
	if span3Output != nil {
		t.Errorf("span3 output: got %q, want nil (NULL in DuckDB)", *span3Output)
	}
}

// TestPipeline_Run_EmptyInputOutputEndToEnd verifies the full pipeline loop
// handles spans with empty input/output correctly from dequeue through write.
func TestPipeline_Run_EmptyInputOutputEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := duckdb.Open("test_empty_input_e2e.db")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer func() { db.Close(); os.Remove("test_empty_input_e2e.db") }()

	// A batch containing spans with and without input/output.
	spans := []*domain.Span{
		{
			TraceID:      "trace-00000000000000002",
			SpanID:       "span-00000000000000004",
			Model:        "gpt-4o",
			ProjectID:    "proj-2",
			Input:        `[{"role":"user","content":"test"}]`,
			Output:       `[{"role":"assistant","content":"ok"}]`,
			InputTokens:  10,
			OutputTokens: 5,
		},
		{
			TraceID:      "trace-00000000000000002",
			SpanID:       "span-00000000000000005",
			Model:        "gpt-4o",
			ProjectID:    "proj-2",
			Name:         "internal-span",
			Input:        "",
			Output:       "",
			InputTokens:  0,
			OutputTokens: 0,
		},
	}

	ingestQ := &fakeIngestQueue{
		batches:    [][]*domain.Span{spans},
		dequeueErr: nil,
	}

	p := New(ingestQ, db, testPricing, &fakeMetaStore{}, &fakeEvalQueue{}, nil)

	pipelineDone := make(chan error, 1)
	go func() {
		pipelineDone <- p.Run(ctx)
	}()

	err = <-pipelineDone
	if err == nil {
		t.Fatal("expected pipeline to stop when context canceled")
	}

	// All spans should have been written.
	var count int
	qctx, qcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer qcancel()
	if err := db.QueryRowContext(qctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-2").Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 2 {
		t.Errorf("span count: got %d, want 2", count)
	}

	// Verify the internal span (no input/output) has NULL values.
	var inputVal, outputVal any
	if err := db.QueryRowContext(qctx,
		"SELECT input, output FROM spans WHERE span_id = $1", "span-00000000000000005").
		Scan(&inputVal, &outputVal); err != nil {
		t.Fatalf("scan internal span: %v", err)
	}
	if inputVal != nil {
		t.Errorf("internal span input: got %v, want nil (NULL)", inputVal)
	}
	if outputVal != nil {
		t.Errorf("internal span output: got %v, want nil (NULL)", outputVal)
	}

	// Verify the LLM span still has its input/output content.
	var llmInput, llmOutput string
	if err := db.QueryRowContext(qctx,
		"SELECT CAST(input AS VARCHAR), CAST(output AS VARCHAR) FROM spans WHERE span_id = $1", "span-00000000000000004").
		Scan(&llmInput, &llmOutput); err != nil {
		t.Fatalf("scan LLM span: %v", err)
	}
	if llmInput == "" {
		t.Error("LLM span input is empty, expected JSON content")
	}
	if llmOutput == "" {
		t.Error("LLM span output is empty, expected JSON content")
	}
}
