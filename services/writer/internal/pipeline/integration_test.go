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

	redisgo "github.com/redis/go-redis/v9"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/omneval/omneval/internal/auth"
	"github.com/omneval/omneval/internal/domain"
	"github.com/omneval/omneval/internal/handlers"
	"github.com/omneval/omneval/internal/duckdb"
	qredis "github.com/omneval/omneval/internal/queue/redis"
	"github.com/omneval/omneval/services/writer/internal/pipeline"
	"google.golang.org/protobuf/proto"
	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	statusv1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

// TestOTLP_EndToEndProto verifies the full pipeline:
// proto OTLP → Ingest API → Redis → Writer → DuckDB → Query returns expected span.
func TestOTLP_EndToEndProto(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx := context.Background()
	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

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

	pipelineErr := make(chan error, 1)
	go func() {
		pl := pipeline.New(ingestQ, db, nil, nil, nil, nil)
		pipelineErr <- pl.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Send proto OTLP payload.
	req := buildProtoOTLPPayload()
	bodyBytes, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal proto: %v", err)
	}

	resp, err := http.Post(ts.URL+"/v1/traces",
		"application/x-protobuf", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for writer pipeline to process span")
	case err := <-pipelineErr:
		if err != nil {
			t.Fatalf("pipeline error: %v", err)
		}
	}

	// Verify span in DuckDB.
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-1").
		Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Fatalf("span count: got %d, want 1", count)
	}

	// Verify typed fields.
	var spanModel, spanInput, spanOutput, spanServiceName, spanKind string
	var inputTokens, outputTokens int64
	var costUSD float64

	query := `SELECT model, input, output, service_name, kind,
		      input_tokens, output_tokens, cost_usd
	      FROM spans WHERE project_id = $1`
	row := db.QueryRowContext(ctx, query, "proj-1")
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
	nativeResp, err := http.Post(ts.URL+"/api/v1/spans",
		"application/json", bytes.NewReader(nativeBody))
	if err != nil {
		t.Fatalf("native request: %v", err)
	}
	defer nativeResp.Body.Close()

	if nativeResp.StatusCode != http.StatusAccepted {
		t.Fatalf("native status: got %d, want %d", nativeResp.StatusCode, http.StatusAccepted)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for second span")
	case err := <-pipelineErr:
		if err != nil {
			t.Fatalf("pipeline error: %v", err)
		}
	}

	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-1").
		Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 2 {
		t.Fatalf("span count: got %d, want 2", count)
	}
}

// TestOTLP_EndToEndJSON verifies JSON OTLP produces the same result as proto.
func TestOTLP_EndToEndJSON(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
		t.Skip("Docker not available, skipping integration test")
	}

	ctx := context.Background()
	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

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

	pipelineErr := make(chan error, 1)
	go func() {
		pl := pipeline.New(ingestQ, db, nil, nil, nil, nil)
		pipelineErr <- pl.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	jsonBody := `{
		"resourceSpans": [{
			"resource": {
				"attributes": [{
					"key": "service.name",
					"value": {"stringValue": "json-service"}
				}]
			},
			"scopeSpans": [{
				"spans": [{
					"traceId": "0123456789abcdef0123456789abcdef",
					"spanId": "0123456789abcdef",
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
	}`

	resp, err := http.Post(ts.URL+"/v1/traces", "application/json", bytes.NewReader([]byte(jsonBody)))
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for writer pipeline")
	case err := <-pipelineErr:
		if err != nil {
			t.Fatalf("pipeline error: %v", err)
		}
	}

	var spanModel, spanServiceName string
	var inputTokens, outputTokens int64

	query := `SELECT model, service_name, input_tokens, output_tokens FROM spans WHERE project_id = $1`
	row := db.QueryRowContext(ctx, query, "proj-2")
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

	ctx := context.Background()
	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

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

	pipelineErr := make(chan error, 1)
	go func() {
		pl := pipeline.New(ingestQ, db, nil, nil, nil, nil)
		pipelineErr <- pl.Run(ctx)
	}()

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

	resp, err := http.Post(ts.URL+"/v1/traces", "application/x-protobuf", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for writer pipeline")
	case err := <-pipelineErr:
		if err != nil {
			t.Fatalf("pipeline error: %v", err)
		}
	}

	var costUSD float64
	if err := db.QueryRowContext(ctx,
		"SELECT cost_usd FROM spans WHERE project_id = $1", "proj-5").
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

	ctx := context.Background()
	redisContainer, redisPort, err := startRedisContainer(ctx)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx)

	db, err := duckdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	defer db.Close()

	rdb := redisgo.NewClient(&redisgo.Options{Addr: redisPort})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	ingestQ := qredis.NewIngestQueue(rdb)

	validator := &fakeValidator{projectID: "proj-native"}
	nativeH := handlers.NewNativeHandler(ingestQ, validator, nil)

	ts := httptest.NewServer(nativeH.Router())
	defer ts.Close()

	pipelineErr := make(chan error, 1)
	go func() {
		pl := pipeline.New(ingestQ, db, nil, nil, nil, nil)
		pipelineErr <- pl.Run(ctx)
	}()

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

	resp, err := http.Post(ts.URL+"/api/v1/spans", "application/json", bytes.NewReader(nativeBody))
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for writer pipeline")
	case err := <-pipelineErr:
		if err != nil {
			t.Fatalf("pipeline error: %v", err)
		}
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans WHERE project_id = $1", "proj-native").
		Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Fatalf("span count: got %d, want 1", count)
	}

	var spanModel string
	if err := db.QueryRowContext(ctx,
		"SELECT model FROM spans WHERE project_id = $1", "proj-native").
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
