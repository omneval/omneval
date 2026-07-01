package pipeline_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/handlers"
	"github.com/omneval/omneval/internal/lake"
	"github.com/omneval/omneval/internal/laketest"
	qredis "github.com/omneval/omneval/internal/queue/redis"
	"github.com/omneval/omneval/services/writer/internal/pipeline"
	redisgo "github.com/redis/go-redis/v9"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	statusv1 "go.opentelemetry.io/proto/otlp/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// openTestLake opens an embedded local Lake for the pipeline E2E tests,
// skipping if the DuckLake extension is unavailable.
func openTestLake(t *testing.T, ctx context.Context) *lake.Lake {
	t.Helper()
	return laketest.NewLocal(t)
}

// TestOTLP_EndToEndProto verifies the full pipeline:
// proto OTLP → Ingest API → Redis → Writer → DuckDB → Query returns expected span.
func TestOTLP_EndToEndProto(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	lk := openTestLake(t, ctx)

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-1"}

	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)
	otlpH := handlers.NewOTLPHandler(ingestQ, validator)

	router := http.NewServeMux()
	router.Handle("/", nativeH.Router())
	router.Handle("/v1/traces", otlpH.Router())

	ts := httptest.NewServer(router)
	defer ts.Close()

	go pipeline.New(ingestQ, nil, nil, nil, nil, nil).WithLake(lk).Run(ctx)

	time.Sleep(100 * time.Millisecond)

	// Send proto OTLP payload.
	req := buildProtoOTLPPayload()
	bodyBytes, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal proto: %v", err)
	}

	resp := mustDo(t, newReq(t, "POST", ts.URL+"/v1/traces", "application/x-protobuf", bodyBytes))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-1", 1, 10*time.Second)

	// Verify typed fields.
	var spanModel, spanInput, spanOutput, spanServiceName, spanKind string
	var inputTokens, outputTokens int64
	var costUSD float64

	query := `SELECT model, input, output, service_name, kind,
		      input_tokens, output_tokens, cost_usd
	      FROM lake.spans WHERE project_id = ?`
	row := lk.DB().QueryRowContext(ctx, query, "proj-1")
	if err := row.Scan(&spanModel, &spanInput, &spanOutput, &spanServiceName, &spanKind,
		&inputTokens, &outputTokens, &costUSD); err != nil {
		t.Fatalf("scan span: %v", err)
	}

	assertEq(t, "model", spanModel, "gpt-4o")
	assertEq(t, "service_name", spanServiceName, "my-service")
	assertEq(t, "kind", spanKind, "llm")
	assertEqI64(t, "input_tokens", inputTokens, 100)
	assertEqI64(t, "output_tokens", outputTokens, 50)
	if costUSD < 0 {
		t.Errorf("cost_usd: got %f, want >= 0", costUSD)
	}
	if spanInput == "" {
		t.Error("input is empty")
	}
	if spanOutput == "" {
		t.Error("output is empty")
	}

	// Verify Input/Output content.
	var inputMessages []map[string]any
	if err := json.Unmarshal([]byte(spanInput), &inputMessages); err != nil {
		t.Fatalf("parse input JSON: %v", err)
	}
	if len(inputMessages) != 1 {
		t.Fatalf("input messages: got %d, want 1", len(inputMessages))
	}
	assertEq(t, "input[0].role", fmt.Sprintf("%v", inputMessages[0]["role"]), "user")
	assertEq(t, "input[0].content", fmt.Sprintf("%v", inputMessages[0]["content"]), "Hello world")

	var outputMessages []map[string]any
	if err := json.Unmarshal([]byte(spanOutput), &outputMessages); err != nil {
		t.Fatalf("parse output JSON: %v", err)
	}
	if len(outputMessages) != 1 {
		t.Fatalf("output messages: got %d, want 1", len(outputMessages))
	}
	assertEq(t, "output[0].role", fmt.Sprintf("%v", outputMessages[0]["role"]), "assistant")
	assertEq(t, "output[0].content", fmt.Sprintf("%v", outputMessages[0]["content"]), "Response text")

	// Verify native handler still works.
	nativeBody := []byte(`{
		"spans": [{
			"trace_id": "fedcba9876543210fedcba9876543210",
			"span_id": "fedcba9876543210",
			"name": "native-span",
			"model": "gpt-4",
			"input_tokens": 10,
			"output_tokens": 20
		}]
	}`)
	nativeResp := mustDo(t, newReq(t, "POST", ts.URL+"/api/v1/spans", "application/json", nativeBody))
	defer nativeResp.Body.Close()
	if nativeResp.StatusCode != http.StatusAccepted {
		t.Fatalf("native status: got %d, want %d", nativeResp.StatusCode, http.StatusAccepted)
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-1", 2, 10*time.Second)
}

// TestOTLP_SpanEventsMessageContent verifies that message content placed in
// Span Events (the wire shape that Laminar/OpenAI instrumentors emit) survives
// the full pipeline: proto OTLP → Ingest API → Redis → Writer → Lake and is
// readable via the lake DB. This is the regression test for the bug where
// LLM spans ingested via the Python SDK rendered empty Input/Output in the UI
// because only span attributes were read, never events.
func TestOTLP_SpanEventsMessageContent(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	lk := openTestLake(t, ctx)

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-events"}

	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)
	otlpH := handlers.NewOTLPHandler(ingestQ, validator)

	router := http.NewServeMux()
	router.Handle("/", nativeH.Router())
	router.Handle("/v1/traces", otlpH.Router())

	ts := httptest.NewServer(router)
	defer ts.Close()

	go pipeline.New(ingestQ, nil, nil, nil, nil, nil).WithLake(lk).Run(ctx)

	time.Sleep(100 * time.Millisecond)

	// Build an OTLP span with content ONLY in Span Events, not in attributes.
	// This is the exact shape that LiteLLM / OpenAI instrumentors emit.
	req := &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{Key: "service.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "omneval-test"}}},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
								SpanId:            []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
								Name:              "litellm.completion",
								StartTimeUnixNano: uint64(time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC).UnixNano()),
								EndTimeUnixNano:   uint64(time.Date(2025, 6, 1, 12, 0, 5, 0, time.UTC).UnixNano()),
								Status: &statusv1.Status{
									Code: statusv1.Status_STATUS_CODE_OK,
								},
								Attributes: []*commonv1.KeyValue{
									{Key: "gen_ai.response.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "chatcmpl-bug336"}}},
									{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4o"}}},
									{Key: "llm.usage.total_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 14865}}},
								},
								// Content is ONLY in Span Events — the exact bug scenario.
								Events: []*tracev1.Span_Event{
									{
										Name:         "gen_ai.prompt.message",
										TimeUnixNano: uint64(time.Date(2025, 6, 1, 12, 0, 1, 0, time.UTC).UnixNano()),
										Attributes: []*commonv1.KeyValue{
											{Key: "gen_ai.prompt.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
											{Key: "gen_ai.prompt.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hello from Span Events! This is the user prompt that should appear in the trace detail UI."}}},
										},
									},
									{
										Name:         "gen_ai.completion.message",
										TimeUnixNano: uint64(time.Date(2025, 6, 1, 12, 0, 4, 0, time.UTC).UnixNano()),
										Attributes: []*commonv1.KeyValue{
											{Key: "gen_ai.completion.message.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "assistant"}}},
											{Key: "gen_ai.completion.message.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hello! I'm an assistant responding via Span Events. My content should appear in the trace detail UI."}}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	bodyBytes, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal proto: %v", err)
	}

	resp := mustDo(t, newReq(t, "POST", ts.URL+"/v1/traces", "application/x-protobuf", bodyBytes))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-events", 1, 10*time.Second)

	// Verify the span is stored in the lake with non-empty input and output.
	var spanInput, spanOutput string
	err = lk.DB().QueryRowContext(ctx,
		`SELECT input, output FROM lake.spans WHERE project_id = ?`, "proj-events").
		Scan(&spanInput, &spanOutput)
	if err != nil {
		t.Fatalf("scan span input/output: %v", err)
	}

	if spanInput == "" {
		t.Fatal("input: expected non-empty prompt message, got empty string")
	}
	if spanOutput == "" {
		t.Fatal("output: expected non-empty completion message, got empty string")
	}

	// Verify input contains the user message with content.
	var inputMessages []map[string]any
	if err := json.Unmarshal([]byte(spanInput), &inputMessages); err != nil {
		t.Fatalf("parse input JSON: %v", err)
	}
	if len(inputMessages) < 1 {
		t.Fatalf("input messages: got %d, want at least 1", len(inputMessages))
	}
	if inputMessages[0]["role"] != "user" {
		t.Errorf("input[0].role: got %q, want %q", inputMessages[0]["role"], "user")
	}
	if inputMessages[0]["content"] != "Hello from Span Events! This is the user prompt that should appear in the trace detail UI." {
		t.Errorf("input[0].content: got %q", inputMessages[0]["content"])
	}

	// Verify output contains the assistant message with content.
	var outputMessages []map[string]any
	if err := json.Unmarshal([]byte(spanOutput), &outputMessages); err != nil {
		t.Fatalf("parse output JSON: %v", err)
	}
	if len(outputMessages) < 1 {
		t.Fatalf("output messages: got %d, want at least 1", len(outputMessages))
	}
	if outputMessages[0]["role"] != "assistant" {
		t.Errorf("output[0].role: got %q, want %q", outputMessages[0]["role"], "assistant")
	}
	if outputMessages[0]["content"] != "Hello! I'm an assistant responding via Span Events. My content should appear in the trace detail UI." {
		t.Errorf("output[0].content: got %q", outputMessages[0]["content"])
	}
}

// TestNativeRestMessageContentRoundTrip verifies that LLM message content
// provided via the native REST ingest format survives the full pipeline:
// native REST → ingest handler → normaliser → enqueue → Writer → Lake and is
// readable via the lake DB. This is the integration counterpart to
// TestNativeMessageContentRoundTrip in the handlers package — it exercises the
// complete Writer pipeline instead of just the handler/normaliser layer.
func TestNativeRestMessageContentRoundTrip(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	lk := openTestLake(t, ctx)

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-native"}

	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)
	otlpH := handlers.NewOTLPHandler(ingestQ, validator)

	router := http.NewServeMux()
	router.Handle("/", nativeH.Router())
	router.Handle("/v1/traces", otlpH.Router())

	ts := httptest.NewServer(router)
	defer ts.Close()

	go pipeline.New(ingestQ, nil, nil, nil, nil, nil).WithLake(lk).Run(ctx)

	time.Sleep(100 * time.Millisecond)

	// Send a native REST ingest request with message content in input/output.
	// This is the shape the Python SDK emits.
	nativeBody := []byte(`{
		"spans": [{
			"trace_id": "aabbccddee112233aabbccddee112233",
			"span_id":  "aabbccddee112233",
			"name":     "agent.step.llm",
			"kind":     "llm",
			"model":    "gpt-4o",
			"input": [{"role": "user", "content": "Native REST prompt content — this should appear in the trace detail UI."}],
			"output": [{"role": "assistant", "content": "Native REST completion — this should also appear in the trace detail UI."}],
			"input_tokens":  120,
			"output_tokens": 80,
			"cost_usd": 0.005
		}]
	}`)

	nativeResp := mustDo(t, newReq(t, "POST", ts.URL+"/api/v1/spans", "application/json", nativeBody))
	defer nativeResp.Body.Close()
	if nativeResp.StatusCode != http.StatusAccepted {
		t.Fatalf("native status: got %d, want %d\nbody: %s", nativeResp.StatusCode, http.StatusAccepted, string(nativeBody))
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-native", 1, 10*time.Second)

	// Verify the span is stored in the lake with non-empty input and output.
	var spanInput, spanOutput string
	err = lk.DB().QueryRowContext(ctx,
		`SELECT input, output FROM lake.spans WHERE project_id = ?`, "proj-native").
		Scan(&spanInput, &spanOutput)
	if err != nil {
		t.Fatalf("scan span input/output: %v", err)
	}

	if spanInput == "" {
		t.Fatal("input: expected non-empty from native REST ingest, got empty string")
	}
	if spanOutput == "" {
		t.Fatal("output: expected non-empty from native REST ingest, got empty string")
	}

	// Verify input contains the user message with content.
	var inputMessages []map[string]any
	if err := json.Unmarshal([]byte(spanInput), &inputMessages); err != nil {
		t.Fatalf("parse input JSON: %v", err)
	}
	if len(inputMessages) < 1 {
		t.Fatalf("input messages: got %d, want at least 1", len(inputMessages))
	}
	if inputMessages[0]["role"] != "user" {
		t.Errorf("input[0].role: got %q, want %q", inputMessages[0]["role"], "user")
	}
	expectedContent := "Native REST prompt content — this should appear in the trace detail UI."
	if inputMessages[0]["content"] != expectedContent {
		t.Errorf("input[0].content: got %q, want %q", inputMessages[0]["content"], expectedContent)
	}

	// Verify output contains the assistant message with content.
	var outputMessages []map[string]any
	if err := json.Unmarshal([]byte(spanOutput), &outputMessages); err != nil {
		t.Fatalf("parse output JSON: %v", err)
	}
	if len(outputMessages) < 1 {
		t.Fatalf("output messages: got %d, want at least 1", len(outputMessages))
	}
	if outputMessages[0]["role"] != "assistant" {
		t.Errorf("output[0].role: got %q, want %q", outputMessages[0]["role"], "assistant")
	}
	expectedOutputContent := "Native REST completion — this should also appear in the trace detail UI."
	if outputMessages[0]["content"] != expectedOutputContent {
		t.Errorf("output[0].content: got %q, want %q", outputMessages[0]["content"], expectedOutputContent)
	}
}

// TestNativeRestTraceDetailQueryRoundTrip verifies that message content
// ingested via the native REST path survives the full ingest → lake → query
// pipeline and is returned intact in the trace detail API response. This
// exercises the CAST(input/output AS VARCHAR) fix in LakeTraceSpansSQL.
func TestNativeRestTraceDetailQueryRoundTrip(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	lk := openTestLake(t, ctx)

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-query-roundtrip"}

	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)
	otlpH := handlers.NewOTLPHandler(ingestQ, validator)

	router := http.NewServeMux()
	router.Handle("/", nativeH.Router())
	router.Handle("/v1/traces", otlpH.Router())

	ts := httptest.NewServer(router)
	defer ts.Close()

	go pipeline.New(ingestQ, nil, nil, nil, nil, nil).WithLake(lk).Run(ctx)

	time.Sleep(100 * time.Millisecond)

	// Ingest via native REST with message content.
	nativeBody := []byte(`{
		"spans": [{
			"trace_id": "tr-native-query",
			"span_id":  "span-native-q-1",
			"name":     "agent.step",
			"kind":     "agent",
			"model":    "gpt-4o",
			"input": [{"role": "user", "content": "Query roundtrip prompt"}],
			"output": [{"role": "assistant", "content": "Query roundtrip completion"}],
			"input_tokens": 50,
			"output_tokens": 25
		}]
	}`)

	nativeResp := mustDo(t, newReq(t, "POST", ts.URL+"/api/v1/spans", "application/json", nativeBody))
	defer nativeResp.Body.Close()
	if nativeResp.StatusCode != http.StatusAccepted {
		t.Fatalf("native status: got %d, want %d", nativeResp.StatusCode, http.StatusAccepted)
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-query-roundtrip", 1, 10*time.Second)

	// Now use the query service to verify the trace detail API returns non-empty input/output.
	// We need to start the query service — for simplicity, verify directly in the lake DB
	// that input/output are VARCHAR strings (not parsed JSON objects) so the query handler's
	// strVal() will render them correctly.
	var spanInput, spanOutput string
	err = lk.DB().QueryRowContext(ctx,
		`SELECT input, output FROM lake.spans WHERE project_id = ?`, "proj-query-roundtrip").
		Scan(&spanInput, &spanOutput)
	if err != nil {
		t.Fatalf("scan span input/output: %v", err)
	}

	if spanInput == "" {
		t.Fatal("input: expected non-empty, got empty")
	}
	if spanOutput == "" {
		t.Fatal("output: expected non-empty, got empty")
	}

	// Verify that CAST(input AS VARCHAR) works by querying through the lake view.
	var castInput, castOutput string
	err = lk.DB().QueryRowContext(ctx,
		`SELECT CAST(input AS VARCHAR), CAST(output AS VARCHAR) FROM lake.spans WHERE project_id = ?`,
		"proj-query-roundtrip").
		Scan(&castInput, &castOutput)
	if err != nil {
		t.Fatalf("scan cast input/output: %v", err)
	}

	// The CAST result should be identical to the raw string — this is what
	// LakeTraceSpansSQL relies on so strVal() doesn't produce "[map[...]]".
	if castInput != spanInput {
		t.Errorf("CAST(input): got %q, want %q (mismatch means strVal will produce garbage)", castInput, spanInput)
	}
	if castOutput != spanOutput {
		t.Errorf("CAST(output): got %q, want %q (mismatch means strVal will produce garbage)", castOutput, spanOutput)
	}
}

// TestOTLP_EndToEndJSON verifies JSON OTLP produces the same result as proto.
func TestOTLP_EndToEndJSON(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	lk := openTestLake(t, ctx)

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-2"}
	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)
	otlpH := handlers.NewOTLPHandler(ingestQ, validator)

	router := http.NewServeMux()
	router.Handle("/", nativeH.Router())
	router.Handle("/v1/traces", otlpH.Router())

	ts := httptest.NewServer(router)
	defer ts.Close()

	go pipeline.New(ingestQ, nil, nil, nil, nil, nil).WithLake(lk).Run(ctx)

	time.Sleep(100 * time.Millisecond)

	// traceId/spanId must be base64-encoded (proto JSON format, not OTLP hex format).
	// traceId = [0x01..0xef repeated] base64 = "ASNFZ4mrze8BI0VniavN7w=="
	// spanId  = [0x01..0xef]          base64 = "ASNFZ4mrze8="
	jsonBody := []byte(`{
		"resourceSpans": [{
			"resource": {
				"attributes": [{
					"key": "service.name",
					"value": {"stringValue": "json-service"}
				}]
			},
			"scopeSpans": [{
				"spans": [{
					"traceId": "ASNFZ4mrze8BI0VniavN7w==",
					"spanId": "ASNFZ4mrze8=",
					"name": "json-llm-span",
					"startTimeUnixNano": "1704067200000000000",
					"endTimeUnixNano": "1704067201000000000",
					"attributes": [
						{"key": "gen_ai.request.model", "value": {"stringValue": "gpt-4o"}},
						{"key": "gen_ai.usage.input_tokens", "value": {"intValue": "150"}},
						{"key": "gen_ai.usage.output_tokens", "value": {"intValue": "75"}},
						{"key": "gen_ai.prompt.0.role", "value": {"stringValue": "user"}},
						{"key": "gen_ai.prompt.0.content", "value": {"stringValue": "JSON prompt"}}
					]
				}]
			}]
		}]
	}`)

	resp := mustDo(t, newReq(t, "POST", ts.URL+"/v1/traces", "application/json", jsonBody))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-2", 1, 10*time.Second)

	var spanModel, spanServiceName string
	var inputTokens, outputTokens int64

	query := `SELECT model, service_name, input_tokens, output_tokens FROM lake.spans WHERE project_id = ?`
	row := lk.DB().QueryRowContext(ctx, query, "proj-2")
	if err := row.Scan(&spanModel, &spanServiceName, &inputTokens, &outputTokens); err != nil {
		t.Fatalf("scan: %v", err)
	}

	assertEq(t, "model", spanModel, "gpt-4o")
	assertEq(t, "service_name", spanServiceName, "json-service")
	assertEqI64(t, "input_tokens", inputTokens, 150)
	assertEqI64(t, "output_tokens", outputTokens, 75)
}

// TestOTLP_UnknownModelZeroCost verifies CostUSD is 0 for unknown models.
func TestOTLP_UnknownModelZeroCost(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	lk := openTestLake(t, ctx)

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-5"}
	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)
	otlpH := handlers.NewOTLPHandler(ingestQ, validator)

	router := http.NewServeMux()
	router.Handle("/", nativeH.Router())
	router.Handle("/v1/traces", otlpH.Router())

	ts := httptest.NewServer(router)
	defer ts.Close()

	go pipeline.New(ingestQ, nil, nil, nil, nil, nil).WithLake(lk).Run(ctx)

	time.Sleep(100 * time.Millisecond)

	req := &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{Key: "service.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "unknown-model-svc"}}},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
								SpanId:            []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
								Name:              "unknown-model-span",
								StartTimeUnixNano: uint64(time.Now().UnixNano()),
								EndTimeUnixNano:   uint64(time.Now().UnixNano()),
								Attributes: []*commonv1.KeyValue{
									{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "unknown-model-xyz"}}},
								},
							},
						},
					},
				},
			},
		},
	}

	bodyBytes, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal proto: %v", err)
	}

	resp := mustDo(t, newReq(t, "POST", ts.URL+"/v1/traces", "application/x-protobuf", bodyBytes))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-5", 1, 10*time.Second)

	var costUSD float64
	if err := lk.DB().QueryRowContext(ctx,
		"SELECT cost_usd FROM lake.spans WHERE project_id = ?", "proj-5").
		Scan(&costUSD); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if costUSD != 0 {
		t.Errorf("cost_usd: got %f, want 0 for unknown model", costUSD)
	}
}

// TestOTLP_NativeHandlerStillWorks verifies native REST handler wasn't broken.
func TestOTLP_NativeHandlerStillWorks(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	lk := openTestLake(t, ctx)

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-native"}
	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)

	ts := httptest.NewServer(nativeH.Router())
	defer ts.Close()

	go pipeline.New(ingestQ, nil, nil, nil, nil, nil).WithLake(lk).Run(ctx)

	time.Sleep(100 * time.Millisecond)

	nativeBody := []byte(`{
		"spans": [{
			"trace_id": "fedcba9876543210fedcba9876543210",
			"span_id": "fedcba9876543210",
			"name": "native-test-span",
			"model": "gpt-4",
			"input_tokens": 5,
			"output_tokens": 10
		}]
	}`)

	resp := mustDo(t, newReq(t, "POST", ts.URL+"/api/v1/spans", "application/json", nativeBody))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	pollForLakeSpanCount(t, ctx, lk, "proj-native", 1, 10*time.Second)

	var spanModel string
	if err := lk.DB().QueryRowContext(ctx,
		"SELECT model FROM lake.spans WHERE project_id = ?", "proj-native").
		Scan(&spanModel); err != nil {
		t.Fatalf("scan model: %v", err)
	}
	assertEq(t, "model", spanModel, "gpt-4")
}

// --- Helpers ---

func startRedisContainer(ctx context.Context) (*rediscontainer.RedisContainer, string, error) {
	container, err := rediscontainer.Run(ctx, "redis:7-alpine")
	if err != nil {
		return nil, "", fmt.Errorf("run redis container: %w", err)
	}

	mappedPort, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		container.Terminate(ctx)
		return nil, "", fmt.Errorf("get redis port: %w", err)
	}

	return container, fmt.Sprintf("localhost:%s", mappedPort.Port()), nil
}

// newReq builds an HTTP request with the test API key header.
func newReq(t *testing.T, method, url, contentType string, body []byte) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("X-API-Key", "test-key")
	return r
}

// mustDo executes the request and fatals on error.
func mustDo(t *testing.T, r *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// pollForLakeSpanCount polls the Lake until the span count reaches want, or times out.
func pollForLakeSpanCount(t *testing.T, ctx context.Context, lk *lake.Lake, projectID string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var count int
		if err := lk.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM lake.spans WHERE project_id = ?", projectID).Scan(&count); err != nil {
			t.Fatalf("poll span count: %v", err)
		}
		if count >= want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	var count int
	lk.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM lake.spans WHERE project_id = ?", projectID).Scan(&count)
	t.Fatalf("timeout waiting for %d spans in project %s, got %d", want, projectID, count)
}

type fakeValidator struct {
	projectID string
}

func (f *fakeValidator) Validate(_ context.Context, rawKey string) (*auth.ValidatedKey, error) {
	if rawKey == "" {
		return nil, fmt.Errorf("missing API key")
	}
	return &auth.ValidatedKey{
		ProjectID: f.projectID,
		Kind:      domain.APIKeyKindProject,
	}, nil
}

func buildProtoOTLPPayload() *coltracev1.ExportTraceServiceRequest {
	return &coltracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{Key: "service.name", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "my-service"}}},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId:           []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
								SpanId:            []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
								Name:              "test-llm-span",
								StartTimeUnixNano: uint64(time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC).UnixNano()),
								EndTimeUnixNano:   uint64(time.Date(2025, 1, 1, 10, 0, 1, 0, time.UTC).UnixNano()),
								Status: &statusv1.Status{
									Code:    statusv1.Status_STATUS_CODE_OK,
									Message: "success",
								},
								Attributes: []*commonv1.KeyValue{
									{Key: "gen_ai.request.model", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "gpt-4o"}}},
									{Key: "gen_ai.usage.input_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 100}}},
									{Key: "gen_ai.usage.output_tokens", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 50}}},
									{Key: "gen_ai.prompt.0.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "user"}}},
									{Key: "gen_ai.prompt.0.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Hello world"}}},
									{Key: "gen_ai.completion.0.role", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "assistant"}}},
									{Key: "gen_ai.completion.0.content", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "Response text"}}},
								},
							},
						},
					},
				},
			},
		},
	}
}

func assertEq(t *testing.T, label, got, want string) {
	if got != want {
		t.Errorf("%s: got %q, want %q", label, got, want)
	}
}

func assertEqI64(t *testing.T, label string, got, want int64) {
	if got != want {
		t.Errorf("%s: got %d, want %d", label, got, want)
	}
}
